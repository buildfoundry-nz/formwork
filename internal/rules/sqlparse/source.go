package sqlparse

import (
	"errors"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
	pg "github.com/pganalyze/pg_query_go/v6"
	pgparser "github.com/wasilibs/go-pgquery/parser"
)

// Stmt is one successfully-parsed SQL statement with its 1-based source line.
type Stmt struct {
	Node *pg.Node
	Line int
	// Col is the seed column of the Go candidate this statement came from, 0
	// for .sql. It exists so consumers can tell two candidates seeded on ONE
	// physical line apart when deduping (#44) — line alone collapsed genuinely
	// distinct queries into a single finding.
	Col int
	// InfeasibleBaseRisk is carried from the Go candidate: this statement is
	// the `base` world of a variable with a complementary guard pair, so a
	// finding on it MAY be the disclosed #42 false positive. Carried rather
	// than re-derived — the fold is the only layer that knows the guards.
	InfeasibleBaseRisk bool
	// ClosureEscapeRisk is carried from the Go candidate too: a closure that
	// appends to this query had its NAME handed to a call — `run(add)` — so
	// this statement is the world those appends are missing from, and a finding
	// on it MAY be the disclosed #337 false positive. The string is the escape
	// as the operator will see it spelled, because the check they have to run
	// is "go read that callee" and a flag with no callee in it does not ask a
	// question anyone can answer. Empty when no closure escaped.
	ClosureEscapeRisk string
}

// candidateRisks is what a Go candidate knows about itself that the SQL text
// cannot say: the two disclosed shapes whose findings a consumer must qualify
// (#42/#238, #337). It exists as a struct rather than as trailing parameters
// because parseChunk already took a positional bool and a second one beside it
// is a transposition nobody reviewing the call site can see — and both of these
// are, precisely, "this finding may not be real", which is the one class of
// argument that must never be silently swapped.
//
// Carried rather than re-derived at every layer: only the fold knows which
// world it built. The parsed tree keeps no record of which world it came from —
// `base` and `full` are two ordinary SELECTs — so a consumer downstream of the
// parse has nothing left to re-derive either flag from.
type candidateRisks struct {
	infeasibleBase bool
	closureEscape  string
}

// risksOf reads the fold's disclosures off one candidate. A .sql chunk has none
// — there is no Go composition behind it — and passes the zero value.
func risksOf(cand sqlextract.Candidate) candidateRisks {
	return candidateRisks{
		infeasibleBase: cand.InfeasibleBaseRisk,
		closureEscape:  cand.ClosureEscapeRisk,
	}
}

// parseFailure is one SQL chunk that failed to parse: the whole .sql file or a
// single Go string-literal candidate.
type parseFailure struct {
	Line   int
	Text   string
	FromGo bool
	Err    error
}

// statements parses every SQL chunk in f and returns the parsed statements plus
// the chunks that failed to parse. err is only for infrastructure failures
// (unreadable file, .go AST parse failure); a SQL syntax error is a
// parseFailure. A file that is neither .sql nor .go returns all-nil.
//
// statements is used only by sql/parses, which discards stmts and reports a
// fail iff it is SQL-shaped (looksLikeSQL). So on the .go path, a candidate
// that isn't SQL-shaped is skipped before parsing (#10) rather than parsed and
// filtered afterward — it never becomes a Stmt or a parseFailure. This is an
// internal-contract change only; sql/parses' observable findings (and the set
// of things it would have reported) are unchanged.
func statements(f *scan.File) ([]Stmt, []parseFailure, error) {
	switch sqlextract.FileKind(f.Path()) {
	case "sql":
		content, err := f.Content()
		if err != nil {
			return nil, nil, err
		}
		return parseChunk(string(content), 1, 0, false, candidateRisks{})
	case "go":
		content, err := f.Content()
		if err != nil {
			return nil, nil, err
		}
		// The unresolved Sites are discarded here and the Partial fragments they
		// explain are skipped just below, so an unreassemblable composition
		// leaves no trace in THIS RULE's output — by design (#75). Coverage
		// gaps are lint's to report: its escape-hatch census asks THIS
		// extractor the same question over the files this rule governs and
		// enumerates each one with its reason. A rule that failed on unreadable
		// SQL would fail on ordinary dynamic SQL nobody meant to police.
		//
		// "This extractor" is load-bearing and was not true of the census until
		// #311: it asked FromGo for every rule spelled `sql/`, which is right
		// here and wrong for the two locking types, whose limits are the fold's
		// and not FromGo's. CensusSites (unreadable.go) owns that mapping now.
		cands, _, err := sqlextract.FromGo(f.Path(), content)
		if err != nil {
			return nil, nil, err // Go AST parse failure ⇒ exit 2
		}
		var stmts []Stmt
		var fails []parseFailure
		for _, cand := range cands {
			if cand.Partial {
				continue // [R9] a fragment of an unresolvable query is not a parseable statement
			}
			// [#10] gate moved upstream: sql/parses (the sole caller of
			// statements()) only ever reports a FromGo failure when it is
			// SQL-shaped (see parses.CheckFile's looksLikeSQL filter), so a
			// candidate that isn't SQL-shaped is never WASM-parsed at all —
			// it is neither a Stmt nor a parseFailure. This changes
			// statements()'s internal contract (see doc comment) but not any
			// observable finding: the set of reported failures (SQL-shaped
			// AND fails to parse) is unchanged, and stmts is discarded by
			// the only caller.
			if !looksLikeSQL(cand.Text) {
				continue
			}
			cs, cf, _ := parseChunk(cand.Text, cand.Line, cand.Col, true, risksOf(cand))
			stmts = append(stmts, cs...)
			fails = append(fails, cf...)
		}
		return stmts, fails, nil
	default:
		return nil, nil, nil
	}
}

// lockingStatements sources SQL for sql/locking-select-order ONLY — no other
// rule type should call this. It differs from statements() on the .go path:
// where statements() uses sqlextract.FromGo and skips Partial fragments
// (leaving concatenated/Sprintf-composed queries invisible to this rule, see
// #12), lockingStatements uses sqlextract.FromGoReassembled, whose
// candidates are always a best-effort *complete* text with a synthetic
// "fw_expr" placeholder standing in for each non-literal part. Each
// reassembled candidate is parsed whole; one that fails to parse (a
// placeholder artifact, or a non-SQL literal like an import path) is
// silently skipped — never surfaced as any kind of finding, since a
// placeholder text has no real tree for this rule to analyze and reporting
// SQL syntax errors is sql/parses' job, not this rule's.
//
// For .sql files this behaves exactly like statements(): the whole file is
// parsed and a parse failure is dropped silently (unchanged from before —
// locking only ever acted on parse success here).
//
// err is only for infrastructure failures (unreadable file, .go AST parse
// failure); a file that is neither .sql nor .go returns nil.
//
// [#11] Before WASM-parsing, a cheap pre-parse gate: every locking clause
// (FOR UPDATE, FOR NO KEY UPDATE, FOR SHARE, FOR KEY SHARE) contains the word
// "update" or "share", so text that lowercases to neither can never hold a
// locking SELECT — return no statements without WASM-parsing it. This is
// conservative: it can never produce a false negative (skip text that does
// have a real lock), only skip text that provably cannot match. A tighter
// substring like "for update" is deliberately avoided, since whitespace/case
// variants across the literal (e.g. "for  update", built across a line
// break) would risk one.
//
// On the .go path the gate is applied to the REASSEMBLED candidate texts
// (sqlextract.FromGoReassembled), never to the raw file content: the Go-AST
// parse that produces those candidates always runs first, and its error is
// always returned, so a malformed .go file still exit-2s here regardless of
// whether its raw source happens to contain "update"/"share" (fail-closed).
// Checking the reassembled text (rather than raw source) also means a lock
// keyword split across a '+' concatenation — "FOR UPD" + "ATE" — is
// reassembled to "FOR UPDATE" before the gate sees it, so it is correctly
// not skipped.
func lockingStatements(f *scan.File) ([]Stmt, error) {
	kind := sqlextract.FileKind(f.Path())
	if kind == "" {
		return nil, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	switch kind {
	case "sql":
		lower := strings.ToLower(string(content))
		if !strings.Contains(lower, "update") && !strings.Contains(lower, "share") {
			return nil, nil
		}
		stmts, _, err := parseChunk(string(content), 1, 0, false, candidateRisks{})
		return stmts, err
	case "go":
		// The Sites are discarded HERE and only here: this rule reports
		// hazards, and a composition it could not read has no tree to report a
		// hazard in. They are not lost — UnreadableSites (unreadable.go) runs
		// the same extractor for the #75 census, so the fold's coverage limits
		// reach an operator through the channel built for them rather than as a
		// finding on ordinary dynamic SQL nobody meant to police (#311).
		cands, _, err := sqlextract.FromGoReassembled(f.Path(), content)
		if err != nil {
			return nil, err // Go AST parse failure ⇒ exit 2 — always checked first
		}
		hasLockKeyword := false
		for _, cand := range cands {
			lower := strings.ToLower(cand.Text)
			if strings.Contains(lower, "update") || strings.Contains(lower, "share") {
				hasLockKeyword = true
				break
			}
		}
		if !hasLockKeyword {
			return nil, nil
		}
		var stmts []Stmt
		for _, cand := range cands {
			cs, _, _ := parseChunk(cand.Text, cand.Line, cand.Col, true, risksOf(cand))
			stmts = append(stmts, cs...)
		}
		return stmts, nil
	default:
		return nil, nil
	}
}

// parseChunk parses one whole SQL text. On success every RawStmt is returned
// with its line: for a .sql chunk (fromGo=false) the true line from the byte
// offset; for a Go candidate (fromGo=true) the candidate's base line, since a
// reassembled literal has no reliable interior offsets. On a syntax error the
// whole chunk is one parseFailure: for a .sql chunk the line is taken from
// pg_query's error cursor position when available (falling back to baseLine
// otherwise); for a Go candidate the interior offset isn't reliable, so
// baseLine is used unconditionally.
func parseChunk(text string, baseLine, baseCol int, fromGo bool, risks candidateRisks) ([]Stmt, []parseFailure, error) {
	res, err := parse(text)
	if err != nil {
		line := baseLine
		if !fromGo {
			var perr *pgparser.Error
			if errors.As(err, &perr) {
				// Cursorpos is pg_query's 1-based CHARACTER position of the
				// error within text (PostgreSQL's scanner_errposition counts
				// with pg_mbstrlen_with_len, not bytes; it can be one past the
				// end for an end-of-input error). lineAt wants a 0-based BYTE
				// offset, so convert — on any text with multibyte characters
				// before the error the two disagree and the finding would land
				// on an earlier, innocent line.
				//
				// Only the error cursor needs this. RawStmt.StmtLocation on
				// the success path below is already a byte offset.
				line = baseLine - 1 + lineAt(text, byteOffsetOfChar(text, perr.Cursorpos-1))
			}
		}
		return nil, []parseFailure{{Line: line, Text: text, FromGo: fromGo, Err: err}}, nil
	}
	var stmts []Stmt
	for _, raw := range res.GetStmts() {
		line := baseLine
		if !fromGo {
			line = baseLine - 1 + lineAt(text, skipInsignificant(text, int(raw.GetStmtLocation())))
		}
		stmts = append(stmts, Stmt{
			Node:               raw.GetStmt(),
			Line:               line,
			Col:                baseCol,
			InfeasibleBaseRisk: risks.infeasibleBase,
			ClosureEscapeRisk:  risks.closureEscape,
		})
	}
	return stmts, nil, nil
}

// byteOffsetOfChar converts a 0-based character (rune) offset into text to the
// 0-based byte offset of that rune. A negative offset clamps to 0; an offset
// at or past the end of text clamps to len(text), which is what an
// end-of-input error cursor reports.
func byteOffsetOfChar(text string, chars int) int {
	if chars <= 0 {
		return 0
	}
	n := 0
	for off := range text { // ranges over rune-start byte offsets
		if n == chars {
			return off
		}
		n++
	}
	return len(text)
}

// skipInsignificant advances off past whitespace and SQL comments (-- line
// comments and /* */ block comments, which nest in PostgreSQL) starting at off.
// It runs only over the region before a statement's first token (the file
// prefix or an inter-statement gap), which contains no string literals — so it
// needs no string-literal awareness. An unterminated comment advances to end.
func skipInsignificant(text string, off int) int {
	if off < 0 {
		off = 0
	}
	for off < len(text) {
		c := text[off]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			off++
		case c == '-' && off+1 < len(text) && text[off+1] == '-':
			off += 2
			for off < len(text) && text[off] != '\n' {
				off++
			}
		case c == '/' && off+1 < len(text) && text[off+1] == '*':
			depth := 1
			off += 2
			for off < len(text) && depth > 0 {
				switch {
				case off+1 < len(text) && text[off] == '/' && text[off+1] == '*':
					depth++
					off += 2
				case off+1 < len(text) && text[off] == '*' && text[off+1] == '/':
					depth--
					off += 2
				default:
					off++
				}
			}
		default:
			return off
		}
	}
	return off
}
