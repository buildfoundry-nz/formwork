// Package sqlextract reassembles SQL held in Go string literals and reports the
// compositions it could not reassemble. It is the shared substrate for the
// SQL-aware rule types (sql/statement-predicate, sql/parses,
// sql/locking-select-order). It knows nothing about rules.
//
// A "candidate" is a maximal compile-time-constant string expression: a lone
// string literal or a '+'-chain of them, constant-folded. Candidates are NOT
// split into statements — callers that need statements split them (sqltext) or
// hand the whole candidate to a real parser (sqlparse).
//
// HEURISTIC LIMITS, AND THE TWO EXTRACTORS DO NOT SHARE THEM. FromGo reports
// only fmt.Sprint{,f,ln} calls and '+' concats with a non-literal operand as
// unresolved Sites; literals spread across assignments and strings.Builder are
// dropped silently, as before. FromGoReassembled RESOLVES both of FromGo's two
// shapes into fw_expr text and ADDITIONALLY folds straight-line and if-branch
// assignment flow (`x := …` then `x += …`) into candidates (#36) — but FromGo's
// candidate set is unchanged (byte-identical for sqltext).
//
// The two limit sets are therefore near-complements, which is what #311 was
// filed about: the census asked FromGo which compositions a rule declined and
// attributed the answer to rules that source through FromGoReassembled, so it
// denied analysis of every composition those rules read and stayed silent about
// every one they do not. strings.Builder, loop/switch/closure composition and
// if/else-both-append remain DROPPED in both — and in FromGoReassembled each of
// them now returns a Site saying so (sites.go), which is the difference between
// a limit that is disclosed and one that is merely true.
//
// SQL COMMENTS are NOT handled here, and that is a division of labour rather
// than a gap. Reassembly is a Go-level question — what bytes did this literal
// hold — and a candidate's text is returned verbatim, comments included.
// Deciding that a `--` or `/* … */` run is not statement text is a SQL-level
// question, and it belongs to whoever interprets the candidate: sqltext blanks
// comments in splitStatements via preprocess.DecommentSQL (#137), and sqlparse
// hands whole text to pg_query, which lexes them itself. A consumer that reads
// Candidate.Text and matches tokens over it without decommenting inherits the
// evasion those two closed — a token carried only by a comment reads as code.
package sqlextract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileKind classifies a path as an SQL-bearing source: "sql" for .sql, "go"
// for .go (SQL in string literals), "" otherwise. Case-insensitive on the
// extension. Shared by sqlparse and sqltext so the .sql/.go/other dispatch
// logic lives in exactly one place.
func FileKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sql":
		return "sql"
	case ".go":
		return "go"
	default:
		return ""
	}
}

// Candidate is one maximal reassembled SQL-bearing Go string literal plus the
// 1-based source line of the string expression it came from. Partial is true
// when this literal is a fragment of a composition that could not be
// reassembled (see FromGo) — not a standalone statement.
type Candidate struct {
	Text string
	Line int
	// Col is the 1-based column of the expression this candidate came from.
	// It exists to tell two candidates seeded on ONE physical line apart (#44):
	// consumers dedup .go findings by position, and line alone collapsed
	// genuinely distinct queries into a single finding. Zero when unknown.
	Col     int
	Partial bool
	// InfeasibleBaseRisk marks the `base` world of a variable that has at
	// least one complementary guard pair (#42/#238). `base` drops every
	// optional append, and a complementary pair guarantees one of them fires,
	// so such a world is one no path produces AT THE END of the function.
	//
	// "Risk", not "infeasible", and the hedge is load-bearing: complementarity
	// constrains only the FINAL value. A query observed between the two
	// branches — run(q), an early return, a db.Query — genuinely IS base on a
	// real path, so a consumer must phrase this as a possibility. It is set
	// only where a pair exists, because a marker on every finding carries no
	// information.
	InfeasibleBaseRisk bool
	// ClosureEscapeRisk names the call a closure escaped into whose appends are
	// therefore NOT in this world — `run(add)`, or the inline `run(func(){…})`.
	// Empty when the scope that built this candidate has no such escape (#337).
	//
	// The extractor is parse-only (spec §2 "Out"), so `func run(f func()){ f() }`
	// and `func register(f func()){}` are ONE PROGRAM to it: whether the callee
	// invokes f or merely stores it is cross-function flow. The fold therefore
	// keeps folding rather than untracking — untracking would silence the
	// register() case, where the closure never runs, the world without its
	// appends IS the value, and a finding on it is the hazard the rule exists
	// for. Spec §10 measured that trade: ten findings gone, eight of them true.
	//
	// "Risk", not "is", on InfeasibleBaseRisk's model and for its reason: this
	// world is real whenever the callee does not call f and fabricated whenever
	// it does, and nothing here can tell those apart, so a consumer must phrase
	// it as a possibility. It is set only where the shape exists, because a
	// marker on every finding carries no information.
	ClosureEscapeRisk string
}

// Site is a Go-literal SQL composition an extractor could not read, at the
// position of the construct that made it unreadable.
//
// THE TWO EXTRACTORS HAVE DIFFERENT LIMITS AND THEREFORE DIFFERENT SITES, and
// that is the whole reason Key exists (#311). FromGo reports the two shapes it
// declines to constant-fold; FromGoReassembled folds both of those and reports
// instead the writes its assignment-flow fold cannot see. A consumer that asks
// one extractor and attributes the answer to a rule sourcing through the other
// states the exact opposite of the truth in both directions — it denies analysis
// of a line the rule just fired on, and stays silent about every composition the
// rule really cannot read.
type Site struct {
	Path string
	Line int
	// Key is the stable machine-readable name of the shape, so a consumer can
	// switch on it and a disclosure can be checked against it rather than
	// against a prose reason that drifts.
	Key string
	// Reason is the operator-facing phrase, rendered inside "could not be read
	// (…)". A noun phrase naming the construct, never a sentence.
	Reason string
	// Text is the SQL-bearing text this site is about: the reassembled
	// composition for FromGo, the tracked variable's seed for a fold site, the
	// accumulated literal for a builder. It exists so a consumer can tell a
	// declined SQL query from a declined import path — the census would
	// otherwise list every untracked non-SQL string in the repo — and it is
	// never rendered.
	Text string
}

// FromGo parses src as Go source and returns each reassembled string candidate
// plus every composition it could not reassemble. A Go parse failure is an
// error (the engine surfaces it as exit 2), never a silent pass.
//
// The candidate walk (Text, Line, order) is byte-identical to sqltext's former
// extractGoCandidates. Partial is a post-process: a collected literal whose
// source position falls inside an unresolved node's [Pos, End) span is a
// fragment of that unresolvable composition. This adds the Partial signal
// WITHOUT changing which candidates are collected — sqltext still gets every
// one, exactly as before.
func FromGo(path string, src []byte) (resolved []Candidate, unresolved []Site, err error) {
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, perr)
	}
	type rawCand struct {
		text string
		pos  token.Pos
		line int
		col  int
	}
	type span struct{ lo, hi token.Pos }
	var raws []rawCand
	var partialSpans []span

	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if s, ok := stringValue(expr); ok {
			raws = append(raws, rawCand{text: s, pos: expr.Pos(), line: fset.Position(expr.Pos()).Line, col: fset.Position(expr.Pos()).Column})
			return false // fully consumed; do not descend
		}
		if key, reason, ok := unresolvedReason(expr); ok {
			partialSpans = append(partialSpans, span{lo: expr.Pos(), hi: expr.End()})
			text, _ := reassemble(expr)
			unresolved = append(unresolved, Site{
				Path:   path,
				Line:   fset.Position(expr.Pos()).Line,
				Key:    key,
				Reason: reason,
				Text:   text,
			})
			// keep descending: nested resolvable literals are still collected
		}
		return true
	})

	for _, rc := range raws {
		partial := false
		for _, sp := range partialSpans {
			if rc.pos >= sp.lo && rc.pos < sp.hi {
				partial = true
				break
			}
		}
		resolved = append(resolved, Candidate{Text: rc.text, Line: rc.line, Col: rc.col, Partial: partial})
	}
	return resolved, unresolved, nil
}

// stringValue folds expr into its constant string value when it is a string
// literal, a parenthesised string expression, or a '+' chain of such.
func stringValue(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.ParenExpr:
		return stringValue(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := stringValue(v.X)
		if !lok {
			return "", false
		}
		r, rok := stringValue(v.Y)
		if !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// fwExprPlaceholder is the synthetic identifier FromGoReassembled substitutes
// for every non-literal part of a reassembled composition.
const fwExprPlaceholder = "fw_expr"

// verbPattern matches one fmt verb: an optional explicit argument index
// ([n]), optional flags (+ - space 0 #), an optional width and/or precision
// (digits or '*'), and a verb letter — or a literal "%%".
var verbPattern = regexp.MustCompile(`%(\[\d+\])?[-+ 0#]*(\*|\d+)?(\.(\*|\d+))?[a-zA-Z%]`)

// FromGoReassembled parses src as Go source and returns, for each top-level
// SQL-bearing string expression, ONE best-effort *complete* reassembly:
// unlike FromGo, a composition mixing literal and non-literal parts is not
// dropped or merely flagged unresolved — every non-literal part is replaced
// by the synthetic identifier "fw_expr" so the result is a syntactically
// complete text a real SQL parser can attempt. Partial is always false.
//
// Handled compositions:
//   - a lone string literal → its unquoted value;
//   - a '+'-chain (BinaryExpr ADD) with at least one string literal somewhere
//     in it → concatenated, each non-literal operand contributing "fw_expr".
//     A chain with no literal text at all (integer arithmetic, two variables)
//     is NOT a candidate: it is left for further descent, since an operand may
//     itself hold SQL;
//   - a fmt.Sprintf call whose first arg is a string literal → the format
//     string with each verb (and "%%") substituted;
//   - a fmt.Sprint/Sprintln call whose first arg is a string literal → every
//     operand reassembled and concatenated (Sprintln space-separated with a
//     trailing newline). These take no format string, so no verb substitution.
//
// Straight-line and if-branch assignment flow (`x := …` then `x += …`) IS folded
// here, additively, by the same walk (#36, see fold.go), and an
// IMMEDIATELY-INVOKED closure's appends are folded in with them WHEN IT IS
// INVOKED AS A STATEMENT AND ITS BODY IS FULLY MODELLABLE (#72) — one
// containing so much as a plain function call is not, nor is one invoked in an
// `if` condition, and in both cases the appends are silently dropped exactly
// like a named closure's even though the literal provably runs.
// Other compositions (strings.Builder, loop/switch-built queries, what a NAMED
// closure, a disqualified IIFE, or a condition-position literal appends,
// if/else where both branches append, cross-function flow) cannot be
// reassembled and produce no candidate — never a finding, and since #311 never
// a silence either: each returns a Site instead (below). The seed literal is
// emitted by the expression walk regardless, so such a query is analyzed as if
// the seed were the whole of it. This does not change FromGo's candidate set,
// byte-identical for sqltext's sake.
//
// A non-literal part standing in for a SQL-syntactic position (e.g. an
// ORDER BY column or a WHERE-key operand built from a runtime variable)
// reassembles to the "fw_expr" placeholder, which callers that check
// structural properties (e.g. sql/locking-select-order) cannot verify —
// by design, this is treated as unverifiable rather than assumed safe.
//
// EVERY COMPOSITION IT DROPS IS ALSO RETURNED, as a Site (#311). The drops
// above are the fold's real coverage limits, and until #311 they left no trace
// anywhere: no candidate, no finding, and nothing for an operator to count. The
// Sites are that count. They are NOT findings and NOT errors — a consumer
// reports them as "not read", never as a pass and never as a failure. sites.go
// holds the collection; coverage.go holds the reasons one can carry.
//
// err is only for a Go AST parse failure.
func FromGoReassembled(path string, src []byte) ([]Candidate, []Site, error) {
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, perr)
	}
	sink := newSiteSink(path, fset, src)
	sink.readPackageSeeds(file)
	// ONE traversal doing both jobs (#45). It used to be two full walks over the
	// same tree — this one, then foldAssignments' own — which measured 296ms of
	// the 840ms this function spends on a 10,827-file corpus.
	//
	// THE MERGE IS BY POSITION, NOT BY `return false`, and that is the whole
	// difficulty. The old collector stopped descending into an expression it
	// consumed; the fold walk descended everything independently. So a function
	// literal inside a CONSUMED expression was reached only by the second walk:
	//
	//	fmt.Sprintf("SELECT %s FROM t", func() string { c := …; c += " FOR UPDATE"; … }())
	//
	// The Sprintf is consumed, so keeping `return false` here and folding on the
	// way past drops that inner locking SELECT — a silent miss on a deadlock
	// hazard. Instead the walk always descends, and consumedEnd suppresses
	// nested EXPRESSION candidates only: ast.Inspect is pre-order and a
	// consumed expression's descendants all start before its End, so anything
	// beginning before consumedEnd is inside one. Function bodies keep being
	// folded throughout. TestFoldReachesAFuncLitInsideAConsumedExpression pins
	// the trap and TestNestedExpressionInsideAConsumedOneIsNotItsOwnCandidate
	// pins the suppression.
	//
	// Two slices, concatenated, because ORDER IS CONSUMED DOWNSTREAM (dedup,
	// finding sort): every expression candidate preceded every fold candidate
	// when these were sequential walks, and collecting into one slice would
	// interleave them (TestExpressionCandidatesStillPrecedeFoldCandidates).
	var exprCands, foldCands []Candidate
	var consumedEnd token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				foldCands = append(foldCands, foldBlock(fn.Type, fn.Body, fset, sink)...)
			}
			return true
		case *ast.FuncLit:
			if fn.Body != nil {
				foldCands = append(foldCands, foldBlock(fn.Type, fn.Body, fset, sink)...)
			}
			return true
		}
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if expr.Pos() < consumedEnd {
			return true // inside a consumed expression: descend, but do not re-collect
		}
		if text, ok := reassemble(expr); ok {
			exprCands = append(exprCands, Candidate{
				Text: text,
				Line: fset.Position(expr.Pos()).Line,
				Col:  fset.Position(expr.Pos()).Column,
			})
			consumedEnd = expr.End()
		}
		return true
	})
	return append(exprCands, foldCands...), sink.collected(), nil
}

// reassemble reports the best-effort complete reassembly of expr, when expr
// is one of the top-level compositions FromGoReassembled handles (see its
// doc comment). ok is false for anything else (a bare identifier, a
// strings.Builder call, …), which the caller leaves for further descent so
// nested candidates elsewhere in the tree are still found.
func reassemble(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.ParenExpr:
		return reassemble(v.X)
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lReal := reassembleOperand(v.X)
		r, rReal := reassembleOperand(v.Y)
		// A '+' chain with no real literal text anywhere is not a string
		// composition at all (integer arithmetic, two variables concatenated).
		// Claiming it would both manufacture a junk "fw_exprfw_expr" candidate
		// and stop FromGoReassembled descending into operands that may
		// themselves hold SQL, so leave it for further descent.
		if !lReal && !rReal {
			return "", false
		}
		return l + r, true
	case *ast.CallExpr:
		return reassembleSprintf(v)
	}
	return "", false
}

// reassembleOperand returns the reassembled text for one operand of a
// '+'-chain, and whether that text is REAL literal text rather than a bare
// placeholder. A nested foldable literal, '+'-chain, or fmt.Sprint{,f,ln} call
// contributes its own reassembly; anything else (a variable, some other call,
// …) contributes the synthetic placeholder "fw_expr". Always produces text —
// every operand contributes something — so the bool is about provenance, not
// success.
//
// The fmt.Sprint{,f,ln} arm matters for more than fidelity: FromGoReassembled
// stops descending once a '+' expression is consumed, so SQL held inside a
// Sprintf operand would be lost entirely if this flattened it to a
// placeholder.
func reassembleOperand(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.ParenExpr:
		return reassembleOperand(v.X)
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return s, true
			}
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			l, lReal := reassembleOperand(v.X)
			r, rReal := reassembleOperand(v.Y)
			return l + r, lReal || rReal
		}
	case *ast.CallExpr:
		if s, ok := reassembleSprintf(v); ok {
			return s, true
		}
	}
	return fwExprPlaceholder, false
}

// reassembleSprintf reports the placeholder-substituted reassembly of a
// fmt.Sprintf/Sprint/Sprintln call whose first argument is a string literal.
// ok is false for any other call (wrong package, wrong function, no literal
// first argument, …).
//
// Only Sprintf takes a format string. Sprint and Sprintln CONCATENATE every
// operand, so their remaining arguments are reassembled as operands and
// appended rather than discarded — dropping them loses real SQL text (a
// trailing ORDER BY, say) and can turn a compliant query into a false finding.
// Their first argument is likewise literal text, not a format, so its "%s" is
// left alone.
//
// Operand spacing is best-effort: fmt.Sprint only inserts a space between two
// operands when neither is a string, which is a type fact this package cannot
// see, so operands are concatenated directly (correct for the common
// all-strings case). Sprintln always separates with spaces and appends a
// newline, so that is reproduced exactly.
func reassembleSprintf(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return "", false
	}
	name := sel.Sel.Name
	switch name {
	case "Sprintf", "Sprint", "Sprintln":
	default:
		return "", false
	}
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	first, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	if name == "Sprintf" {
		return substituteVerbs(first), true
	}
	sep := ""
	if name == "Sprintln" {
		sep = " "
	}
	var b strings.Builder
	b.WriteString(first)
	for _, arg := range call.Args[1:] {
		text, _ := reassembleOperand(arg)
		b.WriteString(sep)
		b.WriteString(text)
	}
	if name == "Sprintln" {
		b.WriteString("\n")
	}
	return b.String(), true
}

// substituteVerbs replaces each fmt verb in format with the synthetic
// placeholder "fw_expr", and each literal "%%" with "%".
func substituteVerbs(format string) string {
	return verbPattern.ReplaceAllStringFunc(format, func(m string) string {
		if m == "%%" {
			return "%"
		}
		return fwExprPlaceholder
	})
}

// unresolvedReason reports whether expr is a SQL-bearing composition that could
// not be folded: a fmt.Sprint{,f,ln} call whose first arg is a string literal,
// or a '+' concat mixing a foldable string operand with a non-foldable one.
//
// THESE TWO SHAPES ARE FromGo'S WHOLE UNREADABLE SET, and naming them here is
// what makes that checkable elsewhere: FromGoReassembled resolves both of them
// into fw_expr placeholder text, so a rule sourcing through IT reads every
// composition this function names and none of the ones it does not
// (sites.go, #311).
func unresolvedReason(expr ast.Expr) (key, reason string, ok bool) {
	switch v := expr.(type) {
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" {
				switch sel.Sel.Name {
				case "Sprintf", "Sprint", "Sprintln":
					if len(v.Args) > 0 {
						if _, ok := stringValue(v.Args[0]); ok {
							return "sprintf-composed", "fmt." + sel.Sel.Name + "-composed string", true
						}
					}
				}
			}
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			_, lok := stringValue(v.X)
			_, rok := stringValue(v.Y)
			// exactly one side folds to a string ⇒ a partial SQL composition
			if lok != rok {
				return "concat-nonliteral", "non-literal operand in string concatenation", true
			}
		}
	}
	return "", "", false
}
