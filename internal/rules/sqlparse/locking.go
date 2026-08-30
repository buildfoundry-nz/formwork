package sqlparse

import (
	"errors"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
	pg "github.com/pganalyze/pg_query_go/v6"
	"gopkg.in/yaml.v3"
)

type lockingParams struct {
	UniqueKeyColumns       []string `yaml:"unique_key_columns"`
	OrderRequiresUniqueKey *bool    `yaml:"order_requires_unique_key"`
}

type lockingOrder struct {
	uniqueCols             map[string]bool
	orderRequiresUniqueKey bool
}

func newLockingOrder(node *yaml.Node) (rules.Checker, error) {
	var p lockingParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	cols := p.UniqueKeyColumns
	if len(cols) == 0 {
		cols = []string{"id"}
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		if c == "" {
			return nil, errors.New("sql/locking-select-order: unique_key_columns entries must be non-empty")
		}
		// PostgreSQL folds unquoted identifiers to lowercase, so the parsed AST
		// never carries the config's original case; fold here to match.
		set[strings.ToLower(c)] = true
	}
	orderStrict := true
	if p.OrderRequiresUniqueKey != nil {
		orderStrict = *p.OrderRequiresUniqueKey
	}
	return &lockingOrder{uniqueCols: set, orderRequiresUniqueKey: orderStrict}, nil
}

// CheckFile flags every locking SELECT that is neither ordered by a total order
// (per order_requires_unique_key) nor a single-row unique-key lookup on the
// locked relation.
//
// COVERAGE LIMIT — READ THIS BEFORE TRUSTING A CLEAN RUN. In a .go file this
// rule sees a query where the whole statement is one expression (a literal, a
// '+' chain, or a fmt.Sprint{,f,ln} call — #12) AND, since #36, one composed
// across straight-line or if-branch assignment flow:
//
//	q := `SELECT ... FROM t WHERE ...`   // seed
//	if lockRows {
//	    q += " FOR UPDATE"               // folded in: this now fires if unordered
//	}
//
// sqlextract.FromGoReassembled folds that flow: every tracked variable yields
// its `full` and `base` values, plus the branch worlds of each complementary
// guard pair it has, up to three — so a lock and an order split across
// `if !x` / `if x` IS analyzed, including when both sit under a shared guard
// like `if useTx { … }` (see fold.go).
//
// THE DISCLOSED SHAPES, AND WHY THIS HALF IS A TABLE. docs/reference.md sends
// adopters here for "the current list" and calls it a checkable claim rather
// than prose. It was prose, and it drifted in the direction that misdirects
// triage: it declared #72 and #73 OPEN and unmodelled — seven and fourteen
// minutes before each of them landed — and told its reader that a forward
// `goto`'s skipped append is "silently included", which is the opposite of what
// this rule does with one. Somebody holding a real forward-goto finding was
// being told to dismiss it as a known artifact.
//
// Every line below is now RUN, by locking_coverage_test.go, through this rule
// and through the fold underneath it. A verdict that stops being true reddens;
// so does a shape with no composition behind it; so does an issue cited
// anywhere in this block with no line of its own; and so does an untrack reason
// sqlextract can report that no line here discloses. Nothing in this comment is
// maintained by remembering to maintain it.
//
// AND EVERY LINE IS ALSO A CLAIM ABOUT THE CENSUS (#311), checked by
// census_sites_test.go against the same compositions. A shape disclosed SILENT
// must produce a census line saying this rule read nothing of it; a shape
// disclosed FIRES or PASSES must produce none, because saying "not analysed"
// about a composition this rule reads is a lint answer contradicting a check
// answer in the same run — which is what a census asking sqlextract.FromGo says
// about both locking types, neither of which uses that extractor.
// The two shapes below that the rule reads only IN PART are the third case, and
// they say so in their own vocabulary rather than in the vocabulary of silence.
//
// THAT CLAIM IS CHECKED AT sqlparse.CensusSites, AND `formwork lint` NOW ASKS
// IT. internal/meta's enumerateEscapeHatches routes through CensusSites rather
// than calling sqlextract.FromGo behind a `sql/` type-name prefix, so the
// census answers about the extractor each rule actually sources through. That
// closed #311: `formwork check` failing on a fmt.Sprintf-composed locking
// SELECT no longer comes with a `formwork lint` calling that same line "not
// analysed by this rule", and a repo whose files each hide an unordered locking
// SELECT behind a strings.Builder, a loop, a called closure and `lockIt(&q)`
// gets a census line per file instead of silence. census_wiring_test.go scans
// for the call and fails this package if these sentences and the tree stop
// agreeing, in either direction — including if the wiring is removed.
//
// THREE VERDICTS, and the last two are the reason to read this at all — both
// are zero findings, and only one of them is an answer:
//
//	FIRES   this rule reports the hazard on this composition.
//	PASSES  a world was folded and this rule judged it safe.
//	SILENT  no world was folded at all. NOT ANALYZED — never read as passed.
//
//	SHAPE sprintf-composed           FIRES   #12
//	      the whole query is one fmt.Sprintf call: reassembled through
//	      placeholders and checked, non-literal operands and all.
//	SHAPE if-branch-append           FIRES   #36
//	      an append under an `if` is folded in, and `base` — the world where no
//	      optional append fired — is emitted beside `full`.
//	SHAPE complementary-base         FIRES   #42
//	      under `if a` / `if !a` that `base` world is emitted although no path
//	      ends there. A disclosed false positive; the finding carries a NOTE.
//	SHAPE iife-modellable            FIRES   #72
//	      an immediately-invoked literal whose body is FULLY MODELLABLE is
//	      folded inline where it sits, appends interleaved in source order.
//	SHAPE called-closure             SILENT  #72
//	      `add := func(){ q += … }; add()` untracks q. NOTHING is emitted —
//	      not the world without add's append, which is what this block used to
//	      claim and what the pre-#72 build did.
//	SHAPE called-closure-alias       SILENT  #337
//	      the same unconditional call written any other way the pass can see:
//	      bound by `var add = …`, called inside a bare block, called through an
//	      alias (`g := add; g()`), called for a result that is assigned or sent,
//	      or called inside a literal that itself provably runs. Each of those
//	      fabricated until #337.
//	SHAPE closure-name-escape        FIRES   #337
//	      the call spelling the pass CANNOT see, and the reason the line above
//	      stops where it does: the closure's NAME is handed to a call rather
//	      than called — `run(add)` against `func run(f func()) { f() }`. `run`
//	      invokes it, so the ORDER BY runs on every real path and the world
//	      emitted here is one no path produces. #337 — DECIDED: KEPT, NOT
//	      FIXED. To a per-file parse-only pass this is the same text as
//	      `register(add)` against `func register(f func()) {}`, where nothing
//	      calls the closure and the unordered locking SELECT is the REAL
//	      value — `run(add)` and `register(add)` both measure 1 finding at
//	      `unique_key_columns: [id]`, so untracking on the escape deletes the
//	      second to delete the first. Because the two are one program here, the
//	      finding carries a NOTE naming the escape and the one check that
//	      separates them — what the callee does with f — on BOTH. Item 5 below.
//	SHAPE forward-goto               FIRES   #73
//	      the jumped-over statements are OPTIONAL, so the skipped world is
//	      emitted beside the fall-through one and an unordered lock on the goto
//	      path fires. A label carrying an append of its own is untracked, so
//	      `lock: q += …` is the one spelling this does not reach.
//	SHAPE backward-goto              SILENT  #73
//	      a label a `goto` jumps back to is a loop head, and the LabeledStmt is
//	      untracked exactly as `for` is. Nothing is emitted — not a doubled
//	      append, which is what this block used to claim.
//	SHAPE deref-write                SILENT  #74
//	      a scope that writes through a dereference and takes q's address
//	      untracks q. UNTRACKED, NOT RESOLVED: `*p op= …` is not read back as
//	      `q op= …`, and nothing is emitted, deliberately, because a fabricated
//	      world is worse here than a missing one.
//	SHAPE alias-read-only            PASSES  #74
//	      an address taken only to READ through (`p := &q; len(*p)`) leaves
//	      every append visible, and the query is analyzed as written.
//	SHAPE address-escape             SILENT  #310
//	      `orderIt(&q)` — an address handed to a call, stored, sent, or put in
//	      a composite literal at a position that provably runs — untracks q.
//	      `readOnly(&q)` costs a world too: nothing here tells a reader from a
//	      writer across a call.
//	SHAPE escape-under-branch        FIRES   #310
//	      an escape inside an `if` arm, a loop body, a switch case, a select
//	      clause, a `defer`/`go` call or a `return` expression does NOT untrack:
//	      the path where it did not run is real, and deleting those worlds was
//	      measured at eight true positives.
//	SHAPE range-clause               SILENT  #314
//	      `for _, q = range arr` over a source that PROVABLY iterates — an
//	      array, a non-empty composite literal — certainly overwrites q, so the
//	      pre-loop world is one no path produces.
//	SHAPE range-clause-empty-source  FIRES   #314
//	      over a map, slice, channel or iterator function the zero-iteration
//	      path is real: q survives the loop, the value built around it IS
//	      produced, and untracking it would delete a true positive.
//	SHAPE unmodelled-write           SILENT  #314
//	      a write from a statement form foldStmts does not fold — a switch or
//	      select arm, a loop header or body, a bare block, an if/else whose
//	      arms both write, a labelled statement, a multi-target assignment —
//	      untracks the name it writes.
//	SHAPE strings-builder            SILENT  #311
//	      a builder composes through method calls on a value that never holds a
//	      string-literal seed, so nothing is tracked: no world to emit and none
//	      to fabricate.
//	SHAPE disqualified-iife          FIRES   #72
//	      an immediately-invoked literal whose body holds anything iifeModellable
//	      refuses — one plain function call is enough — is left OPAQUE while the
//	      variable outside it stays TRACKED. Its own appends are dropped and the
//	      world built from the ones around it is emitted, so an ORDER BY inside
//	      fires on code that is ordered on every path. Item 3 below; the verdict
//	      inverts with the appends, and with the LOCK inside nothing is emitted
//	      at all.
//	SHAPE header-literal             FIRES   #72
//	      a literal invoked in a statement HEADER runs unconditionally too, and
//	      arrives through a part no write-detection path inspects — an `if` or
//	      `for` Init or Cond, a `range` source, a `switch` Init or tag, a
//	      `select`'s channel operands (sqlextract's headerParts). Same
//	      fabrication as the line above, reached by every one of those routes.
//	      Item 4 below.
//
// WHAT THE TABLE DOES NOT COVER, because it is not one shape: a hazard
// reachable only by a mix of INDEPENDENT optional appends that neither "all
// applied" nor "none applied" shows, complementary pairs past the third on one
// variable, a pair whose two appends sit under DIFFERENT guard prefixes, an
// address ESCAPING in BOTH arms of an if/else (which provably runs and is still
// not untracked, because telling it from a one-arm escape means proving the two
// arms escape the same name), and a query used mid-block before a later append.
// Treat a clean run on a .go file that composes SQL by those shapes as
// unproven, not as passed.
//
// TWO THINGS THE TABLE DOES NOT MEAN. First, the seed literal is analyzed
// whatever the fold does — the expression walk emits every `:=` RHS — so an
// unmodellable composition is checked as if its seed were the whole query, which
// can fire on a hazard a later append cures. Second, a closure that this rule
// cannot model does not make the query invisible: the enclosing walk keeps
// folding and reports on the value built from the appends outside the closure,
// which is the real value whenever that closure is conditionally called, never
// called, or created after the query is used — NOT whenever it is a
// disqualified IIFE or a literal invoked in a statement header, both of which
// run unconditionally right where they sit; see items 3 and 4 below for what
// dropping those actually leaves behind. AND NOT whenever the closure's NAME is
// handed to a call — `run(add)` — where whether it runs is decided in another
// function and this pass reads one file: item 5 below. Those three are why that
// list of three justifying cases is a list of what this rule can SEE, never a
// statement that the outside-appends world is otherwise real.
//
// THE FOLD'S OTHER SILENT DROP — an append concatenated onto the accumulated
// value with nothing checking whether the seam parses (lockingStatements,
// below, drops the unparseable result in silence) — IS NOW MEASURED, NOT
// MERELY NAMED. A differential across 2,035 generated files plus examples/
// and 8,662 files of the validating port found that failure on exactly three
// append shapes, fifteen files total, none of them in examples/ and none in
// the port — all fifteen are in the generated files. The
// three share a SYMPTOM, not one cause: an ORDER BY built from two
// non-literal pieces joined by a bare space renders "ORDER BY fw_expr
// fw_expr" — which fails identically with two DISTINCT real column names in
// the placeholders' place, because ORDER BY needs a comma there and a space
// was never going to supply one, so no placeholder is involved; a Sprintf'd
// ORDER BY glued straight onto a preceding digit renders "1ORDER" — a
// digit-then-keyword lexer collision that fails the same way with a real
// column name, well before the placeholder that follows it; and a literal
// "%%" append glued onto quoted text renders a bare "%", with no placeholder
// anywhere in the rendered text. All three are pre-existing — nothing #72
// introduced — and this is the miss's first measured bound, not a new one
// (spec §9).
//
// THE FOLD CAN ALSO FIRE ON A QUERY THAT IS ORDERED ON EVERY REAL PATH, in
// five disclosed shapes. The first two share one defect: this pass cannot
// prove a relationship between two values, so it keeps a world rather than
// deleting one. Items 3 and 4 are a different defect and go BOTH ways. Item 5
// is a third defect and goes only this way: the fact that would settle it is in
// another function, and a wrong guess there deletes the hazard this rule is for.
//
//  1. `base`, the "no optional append taken" value. Under a complementary pair
//     at least one branch always fires, so `base` is unreachable — and it is
//     emitted anyway. That is #42, open and disclosed.
//  2. A world that fixes ONE flag while another flag is a copy of it
//     (`b := a`), whose minimal rendering turns off appends the copy forces on.
//  3. AN IMMEDIATELY-INVOKED CLOSURE WHOSE BODY IS DISQUALIFIED — a single
//     plain function call in the body is enough. A different defect: the
//     literal is left opaque, so its OWN appends are dropped while the
//     variable outside it stays tracked. When the dropped append was the
//     query's only `ORDER BY`, what remains fires as a false positive on a
//     query that IS ordered on every real path. Unlike 1 and 2, this one does
//     not only over-fire: when the dropped append was the query's only lock,
//     the rule reports NOTHING on a genuinely unordered locking SELECT instead
//     — the #72 hazard itself, reopened by any disqualifying statement in the
//     body (spec §9).
//  4. A FUNCTION LITERAL INVOKED IN A STATEMENT HEADER —
//     `if func() bool { q += " ORDER BY id"; return true }() {`, and equally
//     `if func() { q += " ORDER BY id" }(); b {`. Not a
//     disqualified body: iifeBody refuses the first on its RESULT before shape
//     is considered and never sees the second at all, and both arrive via
//     IfStmt.Cond or IfStmt.Init, which no write-detection
//     path inspects (foldStmts reads Cond for guards only; untrackAssigned
//     stops at *ast.FuncLit). The `if` condition is the form #72 named and not
//     the only one: sqlextract's headerParts reads the same fabrication out of
//     a `for` Init or Cond, a `range` source, a `switch` Init or tag and a
//     `select`'s channel operands, which is why the shape above is disclosed
//     by header and not by keyword. A header is evaluated unconditionally,
//     so like item 3 it goes both ways: the ORDER BY inside and a lock outside
//     emits the unordered value and fires on ordered code; the lock inside
//     emits NO folded candidate, so a genuinely unordered locking SELECT is
//     never analyzed. Undisclosed throughout #72 — named in its first comment,
//     lost when that comment was retracted wholesale (spec §9, §10).
//  5. A CLOSURE WHOSE NAME IS HANDED TO A CALL — `add := func(){ q += " ORDER
//     BY id" }` then `run(add)`, against `func run(f func()) { f() }`. `run`
//     invokes it, so the ORDER BY runs on every real path and the world built
//     without it is one no path produces: a finding on ordered code, the
//     #337 headline. #337 — DECIDED: KEPT, and the measurement is the reason.
//     To a pass that reads one file and never resolves a callee, that program
//     is the SAME TEXT as `register(add)` against `func register(f func()) {}`
//     — `run(add)` and `register(add)` both measure 1 finding at
//     `unique_key_columns: [id]` — and in the register case nothing ever calls
//     the closure, the value really is `… FOR UPDATE` unordered, and the
//     finding is the deadlock hazard this rule exists for. Untracking on the
//     escape deletes that one to delete this one. It would also split
//     `run(add)` from `run(func(){ … })`, which folds today and is pinned green
//     as spec §10's rejected design. This one does NOT go both ways: with the
//     LOCK inside the closure the rule is silent for EVERY call spelling,
//     `add()` included, which is item 2's own pre-existing miss and not this
//     shape's (spec §9). So unlike 3 and 4 it is a false positive and nothing
//     else, and the marker below is a real answer for it.
//     AND THE FINDING SAYS SO, which is the half that reaches an operator.
//     Like item 1 it carries a NOTE (closureNameEscapeNote, below), naming the
//     escape as written and the one fact that decides it: read the callee, and
//     if it CALLS the closure this reports a hazard the code does not have,
//     while if it only STORES the closure the hazard is real. The note is
//     attached to `register(add)` too and says exactly the same thing there,
//     because to this pass that IS the same program — a note that softened the
//     finding would delete the true positive in the message instead of in the
//     rule, which is the trade this whole entry refuses.
//     AND IT IS WRITTEN DOWN WHERE THE OPERATOR HOLDING IT READS. A finding
//     that cites an issue number is citing it at somebody whose manual has to
//     be able to answer it, and a disclosure reachable only by reading this
//     file is not one: docs/reference.md's Known limits section carries the
//     program above verbatim, the `register(add)` measurement beside it, both
//     outcomes of the procedure and the marker — not a pointer back here.
//
// Suppressing 1 and 2 needs proof that two reads see ONE value; four
// adversarial review rounds of four different proofs each found the proof
// WRONG on ordinary Go — a helper call handed an options struct, a method
// value, a pointer in a composite literal, an embedded field, an alias made in
// a closure — and a wrongly proven pair deletes a reachable world and passes a
// real deadlock hazard in silence. Even a correct proof would not cover a
// query observed BETWEEN the two branches (`run(q)`, an early return, a
// db.Query), where `base` is what the caller gets. Fixing 3 the same way needs
// widening `iifeShapesAdmissible`'s allowlist to admit more per-statement
// shapes — exactly what six review rounds on that predicate (fold_iife.go)
// already measured the cost of: the all-or-nothing refusal is what makes a
// disqualified body provably base-equal at all. Fixing 5 needs the callee's
// body, which is the cross-function flow spec §2 declines outright so that this
// pass works on a tree that does not compile — and a same-file-only version of
// it decides the verdict by where the helper happens to be declared, folding for
// a helper in a sibling file of the same package and untracking for the same
// helper moved into this one.
//
// So emission only ever grows here on what this pass DOES emit, and all five
// false positives stand: if a finding has no reachable path, the answer is a
// `formwork:allow` marker — enumerated by formwork lint, so the suppression
// stays visible — not a query rewritten to please the analyzer. The silent
// directions of 3 AND 4 have no such answer; they are misses like any other in
// this file, not findings to suppress. Those two are what a reader auditing a
// clean run on a closure-composed query should hold onto — 5 has no silent
// direction of its own, so the marker settles it.
func (c *lockingOrder) CheckFile(f *scan.File) ([]rules.Match, error) {
	// lockingStatements (not statements()): unlike sql/parses, this rule needs
	// visibility into concatenated/Sprintf-composed .go queries (#12), so it
	// sources via sqlextract.FromGoReassembled's placeholder reassembly
	// instead of FromGo's Partial-skipped fragments. Parse failures (including
	// placeholder artifacts that don't parse) are silently dropped inside
	// lockingStatements — an unparseable statement has no tree to analyze, so
	// this rule cannot flag it. Surfacing SQL syntax errors is sql/parses' job.
	//
	// That division of labour is COMPLETE for .sql files and for whole-literal
	// .go queries, and it has a hole for composed .go queries: a syntax error
	// in a query built by concatenation or Sprintf is reported by NEITHER rule.
	// This rule drops it (above), and sql/parses sources via sqlextract.FromGo,
	// which marks each fragment of an unresolvable composition Partial and
	// skips it. The hole is deliberate, not an oversight: the only text that
	// could be checked is the placeholder reassembly, and a reassembled
	// fragment routinely fails to parse for reasons that say nothing about the
	// real query ("WHERE a IN (fw_expr)" is not a statement), so reporting
	// those failures would trade a silent gap for a stream of false positives.
	// Composed .go SQL is syntax-checked at runtime, or not at all.
	stmts, err := lockingStatements(f)
	if err != nil {
		return nil, err
	}
	var ms []rules.Match
	// cols[i] is the seed column of ms[i], parallel by construction. It exists
	// only for the dedup below (#44) — no reporter renders it, and putting it on
	// rules.Match would widen a type every rule shares for one rule's benefit.
	var cols []int
	var risks []bool
	var escapes []string
	for _, s := range stmts {
		for _, sel := range lockingSelects(s.Node) {
			if c.compliant(sel) {
				continue
			}
			cols = append(cols, s.Col)
			// NEITHER hint is put in the Message here, deliberately. The dedup
			// below keys on (Line, Col, Message) to collapse the walk/fold
			// duplicate of one statement, so a message that differs between the
			// two copies stops them collapsing and the operator sees the same
			// violation twice. Measured — that is exactly what happened when
			// this was written the obvious way, and
			// TestLockingComplementaryGuardOneBranchUnorderedFires caught it.
			// The flags ride alongside and are applied after dedup.
			//
			// #337's flag is the same shape and not the same measurement.
			// The walk's copy of a statement can NEVER carry it — only the
			// fold knows a closure escaped — so wherever the walk and the fold
			// do surface one statement, the two copies differ by construction
			// rather than by luck. Putting this note in the message here does
			// not risk the #238 regression; it reproduces it, measured on
			// TestTheEscapeNoteSurvivesWhenTheFlaggedCopyIsDeduped.
			risks = append(risks, s.InfeasibleBaseRisk)
			escapes = append(escapes, s.ClosureEscapeRisk)
			ms = append(ms, rules.Match{
				Line:    s.Line,
				Message: "locking SELECT over sibling rows has no deterministic ORDER BY (deadlock risk)",
			})
		}
	}
	// The expression walk and the assignment-fold pass (#36) can surface the
	// same .go locking statement as byte-identical Matches at the seed line
	// (e.g. a seed that already locks, then `+= ";"`). That walk/fold overlap is
	// .go-only — a .sql file is parsed once — so dedup ONLY .go findings.
	// Collapsing .sql findings would wrongly merge two distinct locking
	// statements that share one physical source line. A residual .go case —
	// The key is (Line, Col, Message) — Message is a package constant, so
	// without the column it was effectively line-only and ANY two distinct
	// locking findings on one physical .go line collapsed, not just a seed/fold
	// duplicate (#44). The seed column separates two query variables declared on
	// one line, while the walk/fold duplicate of ONE statement shares both line
	// and column and still collapses, which is the case this dedup exists for.
	//
	// That was an under-count rather than a miss — at least one finding always
	// survived to fail the check — but the operator saw one thing to fix where
	// there were two, and found the second only by iterating.
	if len(ms) > 1 && sqlextract.FileKind(f.Path()) == "go" {
		type key struct {
			line int
			col  int
			msg  string
		}
		seen := make(map[key]int, len(ms))
		// In-place compaction: ms is this function's own fresh slice, and the
		// forward filter never lets the write index overtake the read index, so
		// reusing the backing array (ms[:0], not ms[:0:0]) is safe and avoids a
		// fresh allocation. (Unlike cli.go's rule filter, which must keep :0:0
		// because its slice aliases the shared cfg.Rules backing array.)
		deduped := ms[:0]
		dedupedRisks := risks[:0]
		dedupedEscapes := escapes[:0]
		for i, m := range ms {
			k := key{m.Line, cols[i], m.Message}
			if at, ok := seen[k]; ok {
				// OR both flags across the collapsed copies (#238, #337). The
				// walk and the fold surface the same statement, and only the
				// fold's copy knows it came from a base world or that a
				// closure's name escaped — dropping the duplicate must not drop
				// what it knew. For the escape the OR is "keep the non-empty
				// one"; two copies of ONE statement come from one variable, so
				// they can only ever disagree by one of them being empty.
				dedupedRisks[at] = dedupedRisks[at] || risks[i]
				if dedupedEscapes[at] == "" {
					dedupedEscapes[at] = escapes[i]
				}
				continue
			}
			seen[k] = len(deduped)
			deduped = append(deduped, m)
			dedupedRisks = append(dedupedRisks, risks[i])
			dedupedEscapes = append(dedupedEscapes, escapes[i])
		}
		ms = deduped
		risks = dedupedRisks
		escapes = dedupedEscapes
	}
	// #238 and #337: applied after dedup, for the reason recorded at the emit
	// site. The order is fixed rather than incidental — a finding can be both
	// worlds at once, and a message whose two halves swap between runs is one
	// nobody can recognise twice or grep for.
	for i := range ms {
		if i < len(risks) && risks[i] {
			ms[i].Message += infeasibleBaseNote
		}
		if i < len(escapes) && escapes[i] != "" {
			ms[i].Message += closureNameEscapeNote(escapes[i])
		}
	}
	return ms, nil
}

// compliant reports whether a locking SELECT is safe: it uses SKIP LOCKED
// (which never waits, so it cannot join a lock-wait cycle — #41), it has an
// ORDER BY that totally orders the locked set, or it is a single-row unique-key
// lookup (exactly one locked relation pinned by a conjunctive unique-key
// equality). Task 8 tightens the ORDER BY arm.
//
// The SKIP LOCKED exemption is unconditional: a SKIP-LOCKED transaction skips
// rows others hold rather than waiting, so it can never be the waiting edge of a
// deadlock, and its ORDER BY is irrelevant to that hazard. NOWAIT is NOT
// exempted here — it also cannot deadlock (it errors instead of waiting) but
// the reasoning differs and is decided separately (#41 scope).
//
// A dynamic ORDER BY column (or WHERE-key operand) sourced from Go string
// composition reassembles to the "fw_expr" placeholder (#12), which never
// matches a configured unique_key_columns name; under the strict default
// (order_requires_unique_key: true) this is therefore treated as not a total
// order and fires — conservative by design, not a false positive.
func (c *lockingOrder) compliant(sel *pg.SelectStmt) bool {
	if allLocksSkip(sel) {
		return true // SKIP LOCKED never waits → cannot join a lock-wait cycle (#41)
	}
	locked := lockedRelations(sel)
	single := len(baseRelationIDs(sel.GetFromClause())) == 1
	if sort := sel.GetSortClause(); len(sort) > 0 {
		if !c.orderRequiresUniqueKey {
			return true
		}
		// Relation-aware: every locked relation needs a unique-key witness in
		// the sort clause. This is criterion (1) of the spec and is independent
		// of the single-locked-relation precondition that criterion (2)'s
		// WHERE-clause exemption below carries — a bare FOR UPDATE over a join
		// locks both relations, and ORDER BY both their keys is a total order
		// over the locked set.
		if sortIsTotalOrder(sel, c.uniqueCols, locked, single) {
			return true
		}
	}
	if len(locked) != 1 {
		return false
	}
	return whereIsSingleRowKeyLookup(sel.GetWhereClause(), c.uniqueCols, locked[0], single)
}

func init() {
	rules.Register("sql/locking-select-order", newLockingOrder)
}

// infeasibleBaseNote explains a finding that MAY be the disclosed #42 world —
// the one where an ORDER BY appended under complementary guards (`if a` /
// `if !a`) is absent from both, which no path produces at the END of the
// function (#238).
//
// "may be", never "is": complementarity constrains only the FINAL value. A
// query observed BETWEEN the two branches — run(q), an early return, a
// db.Query — genuinely is that world on a real path, so softening the finding
// into a maybe-ignore-me would trade a disclosed false positive for a missed
// deadlock. It is attached only where a pair exists; a note on every finding
// would say nothing and would teach the reader to skip it on the true
// positives too.
const infeasibleBaseNote = " — NOTE: this may be the disclosed #42 world, where an ORDER BY " +
	"appended under complementary guards (`if a` / `if !a`) is absent " +
	"from both. If every real path orders this query, clear it with a " +
	"formwork:allow marker; if any path can observe the query between " +
	"those branches, the finding is real"

// closureNameEscapeNote explains a finding that MAY be the disclosed #337 world
// — the one where a closure appending to this query had its NAME handed to a
// call (`run(add)`, or the inline `run(func(){…})`), so this pass could not
// follow it and the value reported here is missing that closure's appends.
//
// IT IS A DECISION PROCEDURE AND NEVER A VERDICT, and that is the whole design.
// `run(add)` against `func run(f func()) { f() }` and `register(add)` against
// `func register(f func()) {}` are THE SAME TEXT to a parse-only pass (spec §2
// "Out" declines cross-function flow so the extractor works on a tree that does
// not compile), so this note is attached to both — and for the register one the
// finding is a genuinely unordered locking SELECT, the deadlock hazard this rule
// exists for. A note that said "this is probably fine" would delete that hazard
// in the message instead of in the rule, which is the trade the whole #337
// decision refuses. So it names the escape, states what follows in EACH
// direction, and hands the reader the one fact that settles it: what the callee
// does with f.
//
// The cure named for the false direction is the marker, not a rewrite. A
// formwork:allow marker stays enumerated by `formwork lint`, so a suppression
// remains visible; reshaping the query to please the analyzer does not.
//
// escape is rendered by sqlextract (invoked.go) and is the spelling as written —
// `h.run(add)`, or `…(add)` where no name this pass resolves says which function
// runs. It is interpolated rather than described because the check is "go read
// that callee", and a note with no callee in it asks a question nobody can act
// on.
func closureNameEscapeNote(escape string) string {
	return " — NOTE: this may be the disclosed #337 world: the closure in `" + escape +
		"` appends to this query, and this pass cannot follow a name into a callee, so " +
		"those appends are absent from the value reported here. Read that callee. If it " +
		"CALLS the closure, every real path orders this query and this finding reports a " +
		"hazard the code does not have — clear it with a formwork:allow marker, which " +
		"formwork lint keeps enumerated. If it only STORES the closure, nothing runs " +
		"those appends, the unordered locking SELECT is the real value and this finding " +
		"is the deadlock hazard"
}
