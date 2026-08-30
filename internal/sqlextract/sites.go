package sqlextract

import (
	"bytes"
	"go/ast"
	"go/token"
	"sort"
)

// sites.go — #311. THE FOLD'S SILENCE, AS A COUNTABLE RECORD.
//
// The fold refuses to model a composition in eight distinct situations
// (coverage.go), and in each of them it emits NOTHING for that variable. That
// refusal is deliberate — a world assembled from only the writes the walk
// happened to see is a query no execution path produces, which is the whole
// #72/#73/#74/#310/#337/#314 class — but its cost is a silence that reads
// exactly like a pass.
//
// #75 built the channel that is supposed to carry that cost to an operator, and
// asked the WRONG EXTRACTOR for it. It calls sqlextract.FromGo, whose unreadable
// set is exactly two shapes (unresolvedReason: a fmt.Sprint{,f,ln} call with a
// literal first argument, and a one-sided '+' chain) — and FromGoReassembled
// RESOLVES both of those into fw_expr placeholder text and analyses them. So for
// a rule sourcing through this function the census was wrong in both directions
// at once: every line it printed denied analysis of a composition the rule had
// just read, and none of the fold's real limits produced a Site at all. One
// repo, one run, `formwork check` failing on db/q.go:6 and `formwork lint`
// calling that same line "not analysed by this rule".
//
// So the sites come from the walk that does the declining. Two properties make
// them usable rather than merely present:
//
// ANCHORED AT THE WRITE, NOT AT THE SEED. A site names the construct that made
// the query unreadable — the escaping `&q`, the closure's append, the range
// clause — and never the `q := …` line. That is where an operator has to look,
// and it also keeps the claim TRUE: the expression walk emits every seed literal
// whatever the fold does, so a seed that is itself an unordered locking SELECT
// FIRES at its own line. A site anchored there would say "not analysed" about a
// line the same run just failed on, which is the defect this file closes,
// reproduced by its own fix.
//
// CARRYING THE TEXT. A tracked variable is any string-literal-seeded name, most
// of which hold no SQL at all. Text is the seed (or, for a builder, the literal
// text it accumulates), so a consumer can filter to the SQL-bearing ones —
// sqlparse.UnreadableSites does, through its own looksLikeSQL — rather than
// flooding the census with every untracked path and format string in the repo.

// unread is one name's refusal: the reason, and the position of the construct
// that caused it. The position is why this is a struct rather than the bare
// UntrackReason it replaced — a reason with no site is a disclosure an operator
// cannot act on.
type unread struct {
	reason UntrackReason
	pos    token.Pos
}

// siteKey collapses two refusals that would print as one census line. The fold
// can reach the same construct twice — a name refused at two `:=` sites, a
// scope walked as both a FuncDecl body and its own FuncLit — and a channel that
// prints the same line twice is a channel an operator stops reading.
type siteKey struct {
	line int
	key  string
	text string
}

// siteSink collects the unreadable compositions of ONE file. Nil-safe on add so
// the fold's internal tests can drive foldBlock without one.
type siteSink struct {
	path string
	fset *token.FileSet
	seen map[siteKey]bool
	out  []Site
	// scanBuilders is a PRE-PARSE GATE, not a policy: builderNames recognises a
	// builder only through the selector names below, so a file holding neither
	// cannot contain one and its per-function walk is skipped outright. Same
	// shape as lockingStatements' "update"/"share" gate, and for the same
	// reason — FromGoReassembled runs on every `check` over every .go file a
	// locking rule scopes, so a walk added for `lint`'s benefit is paid by every
	// run of the gate.
	scanBuilders bool
	// pkgSeeds is the file's package-level `const`/`var` literal text, keyed by
	// name (builder.go). It lives on the sink rather than being recomputed per
	// function because it is a fact about the FILE, and because the only walk
	// that reads it is the builder one — which is skipped entirely when
	// scanBuilders is false, so a file with no builder never pays for it.
	pkgSeeds map[string]string
}

// readPackageSeeds records the file-scope literal declarations builderOperand
// resolves names against. A no-op unless this file holds a builder at all, for
// the reason scanBuilders exists: FromGoReassembled runs on every .go file a
// locking rule scopes on every `check`, and this walk is for `lint`'s benefit.
func (s *siteSink) readPackageSeeds(file *ast.File) {
	if s == nil || !s.scanBuilders {
		return
	}
	s.pkgSeeds = fileLiteralSeeds(file)
}

// builderTypeNames are the SELECTOR names isBuilderType matches — deliberately
// the bare type names and not the qualified spellings.
//
// "strings.Builder" would be the tighter gate and it would be UNSOUND: a
// selector may break after its dot, so `var sb strings.<newline>Builder` parses,
// isBuilderType matches it, and a substring search for the qualified form does
// not. The identifier itself cannot be split, whatever surrounds the dot, so
// gating on it is sound for every spelling the recogniser accepts. The cost is
// scanning a file that has a Builder of its own, which is one AST walk of a file
// that was going to be walked anyway.
var builderTypeNames = [][]byte{
	[]byte("Builder"), []byte("Buffer"),
}

func newSiteSink(path string, fset *token.FileSet, src []byte) *siteSink {
	s := &siteSink{path: path, fset: fset, seen: map[siteKey]bool{}}
	for _, name := range builderTypeNames {
		if bytes.Contains(src, name) {
			s.scanBuilders = true
			break
		}
	}
	return s
}

// add records that the composition whose SQL-bearing text is text was not read,
// because of the construct at pos.
//
// TEXTLESS REFUSALS ARE DROPPED, deliberately: a refusal with no text cannot be
// told from a refusal about an import path, and a consumer filtering for SQL
// would have to either report it blind or drop it anyway. The one place that
// can happen is a name refused before any literal seed reached it, which is a
// name this pass has no evidence ever held SQL.
func (s *siteSink) add(pos token.Pos, r UntrackReason, text string) {
	if s == nil || text == "" {
		return
	}
	line := s.fset.Position(pos).Line
	k := siteKey{line: line, key: r.Key, text: text}
	if s.seen[k] {
		return
	}
	s.seen[k] = true
	s.out = append(s.out, Site{
		Path:   s.path,
		Line:   line,
		Key:    r.Key,
		Reason: r.Detail,
		Text:   text,
	})
}

// collected returns the sites in source order. Order is fixed because the
// census renders them in the order it receives them, and a diagnostic channel
// whose output permutes between runs cannot be diffed.
func (s *siteSink) collected() []Site {
	if s == nil || len(s.out) == 0 {
		return nil
	}
	out := append([]Site(nil), s.out...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// appendCollector gathers, for one construct, the first append it makes to each
// TRACKED variable — and nothing else. flush turns that into one Site per
// variable, because a literal appending three times to one query is one query
// the fold read only in part, and three census lines about it would triple-count
// a single coverage gap and rank a wordy composition above a hazardous one. Two
// SEPARATE constructs appending to the same variable do get two lines: each is
// somewhere different for an operator to look.
type appendCollector struct {
	sc     *foldScope
	reason UntrackReason
	order  []string
	first  map[string]token.Pos
}

func newAppendCollector(sc *foldScope, r UntrackReason) *appendCollector {
	if sc.sites == nil || len(sc.vars) == 0 {
		return nil
	}
	return &appendCollector{sc: sc, reason: r, first: map[string]token.Pos{}}
}

// direct records the appends node makes ITSELF — every assignment in it that is
// not inside a nested function literal.
//
// A NESTED LITERAL REACHED AS A VALUE HAS NOT RUN. `pick(xs, func(){ q += … })`
// hands a closure to a call that may never invoke it, so the world without its
// append is a real execution path, the fold is right to emit it, and a line
// saying the query was read only in part is an invented coverage gap — the same
// false claim #311 closed, pointed the other way.
func (c *appendCollector) direct(node ast.Node) {
	if c == nil {
		return
	}
	ast.Inspect(node, func(x ast.Node) bool {
		if _, isLit := x.(*ast.FuncLit); isLit {
			return false
		}
		as, ok := x.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				c.note(id)
			}
		}
		return true
	})
}

// invoked records the appends made by every literal node CALLS, at any depth
// reachable without passing through one that is merely a value. Those bodies
// provably run, so their text is text the fold did not read.
func (c *appendCollector) invoked(node ast.Node) {
	if c == nil {
		return
	}
	for _, lit := range invokedLiterals(node) {
		c.direct(lit.Body)
	}
}

func (c *appendCollector) note(id *ast.Ident) {
	if _, tracked := c.sc.vars[id.Name]; !tracked {
		return
	}
	at, seen := c.first[id.Name]
	switch {
	case !seen:
		c.first[id.Name] = id.Pos()
		c.order = append(c.order, id.Name)
	case id.Pos() < at:
		c.first[id.Name] = id.Pos()
	}
}

func (c *appendCollector) flush() {
	if c == nil {
		return
	}
	for _, name := range c.order {
		c.sc.sites.add(c.first[name], c.reason, c.sc.vars[name].seed)
	}
}

// recordUnreadAppends records the appends of a construct whose OWN statements
// provably run — a disqualified IIFE's body — plus those of every literal it
// invokes.
//
// It is the half of #311 that is NOT an untrack. A disqualified IIFE runs
// unconditionally where it sits and has its appends dropped while the variable
// outside stays TRACKED, so the fold emits a world assembled from the appends it
// happened to see and part of the query was never read. Nothing untracks, so
// nothing else in this package would ever notice.
func recordUnreadAppends(node ast.Node, sc *foldScope, r UntrackReason) {
	c := newAppendCollector(sc, r)
	c.direct(node)
	c.invoked(node)
	c.flush()
}

// recordHeaderLiterals records what a statement's HEADER reads only in part: the
// appends of every literal the header invokes.
//
// INVOKED LITERALS ONLY, and the asymmetry with recordUnreadAppends is the
// point. A header can hold an assignment of its own — `if q = "x"; b {` — and
// that write belongs to untrackAssigned, which untracks the variable and reports
// it as the unmodelled write it is. Collecting it here as well printed the same
// write twice, the second time under a reason naming a closure that is not
// there.
//
// WHICH PARTS OF WHICH STATEMENTS is headerParts' question, and the reason this
// is one function rather than a case in each arm of foldStmts: #72 disclosed the
// `if` condition, this pass has since found the same fabrication through an `if`
// Init, a `for` header, a `range` source, a `switch` tag and a `select`'s channel
// operands, and patching those one syntax form at a time is exactly how the
// class got here.
func recordHeaderLiterals(st ast.Stmt, sc *foldScope) {
	parts := headerParts(st)
	if len(parts) == 0 {
		return
	}
	c := newAppendCollector(sc, reasonHeaderLiteral)
	for _, part := range parts {
		c.invoked(part)
	}
	c.flush()
}

// headerParts is the pieces of st that Go evaluates unconditionally on reaching
// it, and nothing else.
//
// A `for`'s Post is excluded and that is not an oversight: it runs after an
// iteration, so a loop whose body never runs never evaluates it, and the world
// without its append is real. Every clause body is excluded for the same reason.
// A `select`'s channel operands ARE included — the spec evaluates every one of
// them exactly once on entering the statement, whichever clause is then chosen —
// while the clause bodies are not.
func headerParts(st ast.Stmt) []ast.Node {
	var out []ast.Node
	addStmt := func(s ast.Stmt) {
		if s != nil {
			out = append(out, s)
		}
	}
	addExpr := func(e ast.Expr) {
		if e != nil {
			out = append(out, e)
		}
	}
	switch s := st.(type) {
	case *ast.IfStmt:
		addStmt(s.Init)
		addExpr(s.Cond)
	case *ast.ForStmt:
		addStmt(s.Init)
		addExpr(s.Cond)
	case *ast.RangeStmt:
		addExpr(s.X)
	case *ast.SwitchStmt:
		addStmt(s.Init)
		addExpr(s.Tag)
	case *ast.TypeSwitchStmt:
		addStmt(s.Init)
		addStmt(s.Assign)
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, clause := range s.Body.List {
				if cc, ok := clause.(*ast.CommClause); ok {
					addStmt(cc.Comm) // nil for `default:`
				}
			}
		}
	}
	return out
}

// invokedLiterals returns every function literal node CALLS — one appearing as a
// call's Fun, at any depth reachable without passing through a literal that is
// merely a value.
//
// The distinction it draws is the one the whole recording turns on. `func(){…}()`
// runs; `pick(func(){…})` is a closure handed to a call, and whether the callee
// invokes it is cross-function flow this pass declines outright (spec §2), so
// the world without its append is a real path. Descending into an invoked
// literal's body is right for the same reason it is wrong for the other: that
// body provably runs.
func invokedLiterals(node ast.Node) []*ast.FuncLit {
	var out []*ast.FuncLit
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(x ast.Node) bool {
			switch v := x.(type) {
			case *ast.FuncLit:
				return false // reached as a value: it has not run here
			case *ast.CallExpr:
				if lit, ok := ast.Unparen(v.Fun).(*ast.FuncLit); ok {
					out = append(out, lit)
					walk(lit.Body)
				} else {
					walk(v.Fun)
				}
				for _, a := range v.Args {
					walk(a)
				}
				return false
			}
			return true
		})
	}
	walk(node)
	return out
}
