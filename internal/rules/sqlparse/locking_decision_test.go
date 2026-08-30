// locking_decision_test.go — #337, the decision record itself.
//
// #337's headline behaviour is KEPT: `run(add)` against `func run(f func()) {
// f() }` fires an unordered locking SELECT on a query every real path orders,
// and it goes on doing so. The reason is a MEASUREMENT, not a preference — one
// identifier away, `register(add)` against `func register(f func()) {}` is the
// same text to a pass that reads one file and never resolves a callee, and
// there the closure never runs, `… FOR UPDATE` really is the value, and the
// finding is the deadlock hazard this rule exists for. Both measure the same
// count at the same gate config. Untracking on the escape deletes the second to
// delete the first.
//
// THE NARROWING DIRECTION IS ALREADY HELD, AND THE HOLD IS NAMED RATHER THAN
// COUNTED. Patch `scanCalls` to treat a bare identifier in argument position as
// an invocation — the one edit that would silence `run(add)` — and these guards
// redden, 17 of them, 12 in internal/rules/sqlparse and 5 in internal/sqlextract:
//
//   - TestNoShapeTheRuleReadsIsReportedAsUnreadable
//   - TestLockingClosureNameHandedToAHelperFires
//   - TestLockingOrderExplainsTheDisclosedClosureNameEscape
//   - TestTheEscapeNoteSaysTheSameThingAboutTheTruePositive
//   - TestTheEscapeNoteSurvivesWhenTheFlaggedCopyIsDeduped
//   - TestTheEscapeNoteSurvivesWhenTheFlaggedCopyIsKept
//   - TestAFindingCanCarryBothNotes
//   - TestEveryNotePromisedIsAttachedAndEveryNoteAttachedIsPromised
//   - TestCoverageLimitDisclosesWhatTheRuleDoes
//   - TestFoldSpecMeasurementMatchesTheGate
//   - TestTheEscapeDecisionRecordStatesWhatTheGateMeasures
//   - TestTheManualCarriesTheEscapeTheFindingCitesAtAnOperator
//   - TestClosureNameHandedToAHelperKeepsFolding
//   - TestEscapedClosureIsDisclosedOnTheWorldThatDropsIt
//   - TestNoticingTheEscapeEmitsExactlyWhatItEmittedBefore
//   - TestFoldContractNamesTheCallSpellingThatFabricates
//   - TestFoldContractQualifiesTheNotCalledJustification
//
// Nobody can act on this escape quietly. The guards are NAMED because the count
// alone was prose no reader could re-derive, and it rotted here first: an
// earlier revision of this paragraph carried "eleven" for this package forward
// unmeasured while the tree produced twelve, in the commit whose subject said
// the count was measured and not inherited. locking_narrowing_test.go reads this
// list at runtime, resolves each name against the package that defines it, and
// requires the three numerals above to be the sizes of that partition — so the
// record cannot disagree with itself again. That the list is EXHAUSTIVE is the
// one part no test holds, because proving it means applying the patch and
// running both packages: the numerals are for diffing against a real run, not
// for trusting.
//
// THE OTHER DIRECTION WAS NOT HELD AT ALL, and that is what this file is. In a
// throwaway snapshot the "KEPT" status and the whole `register(add)`
// measurement were deleted out of `locking.go`'s item 5, out of the fold spec's
// §4.2 and §9, and out of the manual's Known limits section:
// `go test ./internal/rules/sqlparse/... ./internal/sqlextract/...
// ./internal/repoproof/... ./internal/meta/... -count=1` came back ok on all
// four. A decision record could lose its status and its number with every
// behavioural test green — which is the #312/#313 false-claim shape one
// document over, and the reason #337 stayed open after the code half landed.
//
// SO THE RECORD IS TIED TO THE RUN THAT PRODUCES IT, in both directions at
// once, on census_wiring_test.go's precedent — the test whose two-way form is
// why closing a gap while the documents still described it open broke the
// build. This test runs BOTH programs at the real gate, measures each, and then
// requires every PASSAGE that exhibits the decision to state the number that run
// just produced:
//
//   - Act on the escape and both programs drop to zero findings. Every passage
//     still claiming the measurement is now claiming a number the gate does not
//     produce, and this reddens.
//   - Delete the status or the measurement from any one passage and it reddens
//     against a gate that still produces it.
//
// PASSAGES AND NOT DOCUMENTS, which is the second thing this file got wrong. The
// per-document form of the check let item 5 lose its status while the SHAPE
// stanza fourteen entries above it kept one, and let §4.2 lose its number while
// §9 and §10 kept theirs — so the drift it exists to catch was unguarded exactly
// where the records are thickest, and the SHAPE stanza an adopter is sent to
// first recorded the verdict with no number in it at all. decRecords carries the
// rule that decides what counts as a passage.
//
// None can be edited into agreement one file at a time, because every one of
// them carries ONE sentence built here out of the run.
//
// WHAT IS SPELLED HERE, AND WHY THAT SHORT LIST. The status VOCABULARY —
// `DECIDED` — is spelled, exactly as `locking_coverage_test.go` spells
// `FIRES`/`PASSES`/`SILENT`: a state name has to be a fixed word or the check is
// vacuous. The gate CONFIG is spelled, because it is the configuration this
// file measures at. Everything else is read at runtime: the issue number comes
// out of the live finding, the call spellings come out of the fixture the
// coverage block discloses and out of the program the manual exhibits, and the
// count comes out of the gate. Following internal/hooks/skip_escapes_decision_test.go
// (#338), no sentence of anybody's prose is quoted — a test over prose teaches
// people to edit the test.
package sqlparse_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

// decParams is the gate configuration every count in this file is taken at, and
// the one the three documents have to name beside the number. A measurement
// with no configuration on it is not re-runnable, which is what #312 was filed
// about.
const decParams = "unique_key_columns: [id]"

// decStatusWord is the status vocabulary. #338 recorded a decision by reasoning
// it out at the code it governs and pinning its consequences; this repository
// has no word for "considered, and settled this way" that a reader can grep,
// and item 5, §4.2, §9 and the manual each reached for a different one. One
// word, cited against the issue that settled it.
const decStatusWord = "DECIDED"

// decCalleePlaceholder stands in for the helper's name when two compositions
// are compared. `run(add)` and `register(add)` differing in nothing else is the
// decision's whole premise, so it is checked rather than asserted in prose.
const decCalleePlaceholder = "CALLEE"

var decIssueRE = regexp.MustCompile(`#[0-9]+`)

// TestTheEscapeDecisionRecordStatesWhatTheGateMeasures is the gate.
func TestTheEscapeDecisionRecordStatesWhatTheGateMeasures(t *testing.T) {
	key, escapeCall, escapeHelper := manEscapeShape(t)
	escapeSrc := covShapes[key]

	sectName, sect := decManualSection(t)
	twinSrc := decNonCallingTwin(t, sectName, sect)
	twinCall, twinHelper, ok := manEscapeSpelling(t, twinSrc)
	if !ok {
		t.Fatalf("the program the manual exhibits beside %s does not hand a closure's "+
			"NAME to a call, so it is not the same shape and measuring it proves "+
			"nothing:\n%s", escapeCall, twinSrc)
	}

	// The premise, checked rather than asserted: one program's callee invokes
	// the closure and the other's never does, and the two are otherwise the
	// same text. That is what makes the second finding a true positive and the
	// pair indistinguishable here.
	if !decInvokesItsFuncParam(t, escapeSrc) {
		t.Fatalf("the composition disclosed as %q is built on %q, which never invokes "+
			"its function parameter — then the world this rule emits for it is the real "+
			"one, there is no false positive, and there is nothing to decide",
			key, escapeHelper)
	}
	if decInvokesItsFuncParam(t, twinSrc) {
		t.Fatalf("the twin the manual exhibits is built on %q, which DOES invoke its "+
			"function parameter — the decision turns on a helper that only STORES the "+
			"closure, where the unordered locking SELECT is the value that really "+
			"executes. A twin that calls it measures nothing", twinHelper)
	}
	if a, b := decWithoutCallee(t, escapeSrc), decWithoutCallee(t, twinSrc); a != b {
		t.Fatalf("the two programs differ in more than which helper they hand the "+
			"closure to, so \"the same text to this pass\" is no longer what is being "+
			"measured:\n  %s\n  %s", a, b)
	}

	nEscape := decFindings(t, escapeSrc)
	nTwin := decFindings(t, twinSrc)
	switch {
	case nEscape == 0:
		t.Fatalf("the gate no longer fires on %s at %s. Somebody acted on the escape: "+
			"that is a real change and may be the right one, but the three documents "+
			"still record this shape as kept BECAUSE it is indistinguishable from %s, "+
			"and the first thing to check is what happened to the finding on THAT "+
			"program (measured here at %d). Move the record before moving the rule",
			escapeCall, decParams, twinCall, nTwin)
	case nEscape != nTwin:
		t.Fatalf("%s measures %d finding(s) and %s measures %d at %s. The decision "+
			"recorded in all three documents rests on those two being one program to "+
			"this pass; if the gate can now tell them apart, the reason the false "+
			"positive is kept is gone and the record is describing a trade nobody is "+
			"making", escapeCall, nEscape, twinCall, nTwin, decParams)
	}

	msg := manOnlyFinding(t, escapeSrc)
	issues := manDistinct(decIssueRE.FindAllString(msg, -1))
	if len(issues) != 1 {
		t.Fatalf("the finding cites %v; the status line names the ONE issue that "+
			"settled this shape, so a note citing none leaves nothing to cite and a "+
			"note citing several leaves this test choosing: %q", issues, msg)
	}
	status := issues[0] + " — " + decStatusWord + ":"
	measurement := decMeasurement(escapeCall, twinCall, nEscape)

	for _, doc := range decRecords(t, escapeCall, twinCall, sectName, sect) {
		flat := decFlat(doc.text)
		if !strings.Contains(flat, status) {
			t.Errorf("%s records this shape and does not carry the status %q.\n"+
				"Every place a reader can land on this residual has to say that it was "+
				"settled and by what, in ONE vocabulary — item 5, the spec and the "+
				"manual each reached for a different word, and a record whose status is "+
				"a matter of wording is a record a later pass reads as an oversight.",
				doc.name, status)
		}
		if !strings.Contains(flat, measurement) {
			t.Errorf("%s does not state the measurement this gate just produced.\n"+
				"  wanted, verbatim: %s\n"+
				"The number is the whole reason the false positive is kept: %s is the "+
				"same text here as %s, and untracking on the escape deletes the second "+
				"finding to delete the first. A document that records the decision "+
				"without the number records a preference.\n"+
				"Measured in this run: %s -> %d finding(s), %s -> %d finding(s).",
				doc.name, strconvQuote(measurement), escapeCall, twinCall,
				escapeCall, nEscape, twinCall, nTwin)
		}
	}
}

// decMeasurement renders the one sentence all three documents carry. Nothing in
// it is this file's to choose: the two call spellings come out of the fixture
// and out of the manual's own program, the count out of the gate, the
// configuration out of the checker this file builds.
func decMeasurement(escapeCall, twinCall string, n int) string {
	unit := "findings"
	if n == 1 {
		unit = "finding"
	}
	return fmt.Sprintf("%s and %s both measure %d %s at %s",
		escapeCall, twinCall, n, unit, decParams)
}

// decManualSection resolves the manual section the COVERAGE LIMIT block sends
// adopters to, and returns its heading and its body.
//
// RESOLVED THROUGH THE POINTER THE BLOCK ITSELF MAKES, never by a name spelled
// here: the block and the manual point at each other, so a rename that moves
// only one of them fails rather than leaving this test reading a section
// nobody is sent to.
func decManualSection(t *testing.T) (name, text string) {
	t.Helper()
	name = manPointedSection(t, coverageBlock(t))
	text, ok := manSectionText(manRead(t), name)
	if !ok {
		t.Fatalf("the COVERAGE LIMIT block sends adopters to %s's %q section and the "+
			"manual has no such heading: the document an operator holding this finding "+
			"reads is where the two programs sit side by side, and one of the three "+
			"records that has to carry the decision", manualPath, name)
	}
	return name, text
}

// decRecord is one PASSAGE that records this decision.
type decRecord struct{ name, text string }

// decEntryStart opens a record inside the COVERAGE LIMIT block: a SHAPE stanza
// or a numbered item. The block is a list of independent entries, and a reader
// arrives at ONE of them — from the finding's issue number, from the manual's
// pointer, or from the shape's machine name — never at the block as a whole.
var decEntryStart = regexp.MustCompile(`^//\s+(SHAPE\s|[0-9]+\.\s)`)

// decBlockEntries splits the COVERAGE LIMIT block into those entries. A blank
// comment line also ends one, so the block's connecting prose does not fall
// into the last item above it.
func decBlockEntries(block string) []string {
	var out, cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, ln := range strings.Split(block, "\n") {
		blank := strings.TrimSpace(ln) == "//"
		if blank || decEntryStart.MatchString(ln) {
			flush()
		}
		if blank {
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return out
}

// decRecords returns every PASSAGE that exhibits this decision, across the
// three documents that carry it.
//
// PASSAGES AND NOT DOCUMENTS, and that is the correction this function is. The
// per-document form of this check passed while the status word was deleted out
// of item 5, because the SHAPE stanza fourteen entries above it still carried
// one and both live in the same block — so the drift this file exists to catch
// was unguarded inside a document, which is where the block keeps two entries
// on one shape and the spec keeps three. Nobody reads a block; they read the
// entry the finding's issue number sent them to, and an entry that records this
// residual without saying it was settled and by what number reads as an
// oversight to the next pass, wherever else in the same file the words survive.
//
// A PASSAGE QUALIFIES BY EXHIBITING BOTH PROGRAMS, which is what presenting the
// decision means: `run(add)` alone is a cross-reference — §9 item 2 points
// forward to the full entry that way and is not asked to repeat it — while the
// two together are the measurement's premise, and stating a premise without its
// number is the shape #312 was filed about. Both spellings are read out of the
// fixture and the manual by the caller, never spelled here.
func decRecords(t *testing.T, escapeCall, twinCall, name, sect string) []decRecord {
	t.Helper()
	var out []decRecord
	for _, doc := range []struct {
		name    string
		entries []string
	}{
		{"locking.go's COVERAGE LIMIT block", decBlockEntries(coverageBlock(t))},
		{"the fold design spec (" + foldSpecPath + ")", strings.Split(decRead(t, foldSpecPath), "\n\n")},
		{manualPath + "'s " + strconvQuote(name) + " section", []string{sect}},
	} {
		found := 0
		for _, entry := range doc.entries {
			flat := decFlat(entry)
			if !strings.Contains(flat, escapeCall) || !strings.Contains(flat, twinCall) {
				continue
			}
			found++
			out = append(out, decRecord{doc.name + ", the passage beginning " + strconvQuote(decOpening(entry)), entry})
		}
		if found == 0 {
			t.Fatalf("%s exhibits %s and %s together in no passage at all. That pair IS "+
				"the decision — one program to this pass, one of them a true positive — "+
				"so a document recording this residual without them has stopped "+
				"recording why it is kept, and this test would go on passing against "+
				"the other two", doc.name, escapeCall, twinCall)
		}
	}
	return out
}

// decOpening is a passage's first non-empty line, trimmed of comment and
// markdown furniture, so a failure names the entry a reader would recognise
// rather than quoting it whole.
func decOpening(entry string) string {
	for _, ln := range strings.Split(entry, "\n") {
		if flat := decFlat(ln); flat != "" {
			if len(flat) > 72 {
				return flat[:72] + "…"
			}
			return flat
		}
	}
	return entry
}

// decNonCallingTwin returns the program the manual exhibits beside the escape:
// the same composition handed to a helper that never invokes it.
//
// READ OUT OF THE MANUAL RATHER THAN BUILT HERE. That program is the evidence
// the decision rests on, and a program in a manual that nothing runs is a claim
// wearing a code fence. Reading it back means the manual's counter-example is
// the thing measured, so an edit that turns it into a program which does not
// fire is caught here instead of misleading the adopter it was written for.
func decNonCallingTwin(t *testing.T, name, sect string) string {
	t.Helper()
	var calling, storing []string
	for _, prog := range decFencedGo(sect) {
		if _, _, isEscape := manEscapeSpelling(t, prog); !isEscape {
			continue
		}
		if decInvokesItsFuncParam(t, prog) {
			calling = append(calling, prog)
			continue
		}
		storing = append(storing, prog)
	}
	if len(calling) != 1 || len(storing) != 1 {
		t.Fatalf("%s's %q section exhibits %d program(s) whose helper CALLS the closure "+
			"and %d whose helper only STORES it. It has to show exactly one of each: "+
			"the decision is that those two are one program to this pass, and a section "+
			"showing only the false positive tells an adopter to go and silence it",
			manualPath, name, len(calling), len(storing))
	}
	return storing[0]
}

// decFencedGo returns every ```go block in a markdown section, verbatim.
func decFencedGo(section string) []string {
	var out []string
	var cur []string
	in := false
	for _, ln := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case !in && trimmed == "```go":
			in, cur = true, nil
		case in && strings.HasPrefix(trimmed, "```"):
			out = append(out, strings.Join(cur, "\n")+"\n")
			in = false
		case in:
			cur = append(cur, ln)
		}
	}
	return out
}

// decCallee returns the name of the function a composition hands a closure's
// NAME to — the callee this pass cannot resolve, and the whole subject of the
// decision.
func decCallee(t *testing.T, f *ast.File) string {
	t.Helper()
	closures := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign {
			return true
		}
		for i, rhs := range as.Rhs {
			if _, isLit := rhs.(*ast.FuncLit); !isLit || i >= len(as.Lhs) {
				continue
			}
			if id, isIdent := as.Lhs[i].(*ast.Ident); isIdent {
				closures[id.Name] = true
			}
		}
		return true
	})
	name := ""
	ast.Inspect(f, func(n ast.Node) bool {
		c, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		fn, isIdent := c.Fun.(*ast.Ident)
		if !isIdent {
			return true
		}
		for _, arg := range c.Args {
			if id, isIdent := arg.(*ast.Ident); isIdent && closures[id.Name] {
				name = fn.Name
			}
		}
		return true
	})
	if name == "" {
		t.Fatal("no closure name is handed to a call in this composition — the escape " +
			"is the shape #337 is about, and a composition without one measures a " +
			"different program than the one the record describes")
	}
	return name
}

// decInvokesItsFuncParam reports whether the callee a composition hands its
// closure to ever calls its own function parameter. CALLS and STORES are the
// two outcomes of the decision procedure the finding hands an operator, so
// which one a fixture exhibits is read off the fixture, never assumed from its
// name.
func decInvokesItsFuncParam(t *testing.T, src string) bool {
	t.Helper()
	_, f := decParse(t, src)
	callee := decCallee(t, f)
	for _, d := range f.Decls {
		fd, isFunc := d.(*ast.FuncDecl)
		if !isFunc || fd.Name.Name != callee || fd.Body == nil {
			continue
		}
		params := map[string]bool{}
		if fd.Type.Params != nil {
			for _, field := range fd.Type.Params.List {
				if _, isFuncType := field.Type.(*ast.FuncType); !isFuncType {
					continue
				}
				for _, id := range field.Names {
					params[id.Name] = true
				}
			}
		}
		invokes := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			c, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if id, isIdent := c.Fun.(*ast.Ident); isIdent && params[id.Name] {
				invokes = true
			}
			return true
		})
		return invokes
	}
	t.Fatalf("this composition hands its closure to %s and declares no such function; "+
		"what that callee does with f is the one fact the decision turns on, so the "+
		"fixture has to show it", callee)
	return false
}

// decWithoutCallee renders a composition with the callee's declaration removed
// and every mention of its name replaced by a placeholder. Two programs that
// differ only in which helper they hand the closure to render as one text,
// which is the executable form of "the same text to a parse-only pass".
func decWithoutCallee(t *testing.T, src string) string {
	t.Helper()
	fset, f := decParse(t, src)
	callee := decCallee(t, f)
	ast.Inspect(f, func(n ast.Node) bool {
		if id, isIdent := n.(*ast.Ident); isIdent && id.Name == callee {
			id.Name = decCalleePlaceholder
		}
		return true
	})
	var parts []string
	for _, d := range f.Decls {
		if fd, isFunc := d.(*ast.FuncDecl); isFunc && fd.Name.Name == decCalleePlaceholder {
			continue
		}
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, d); err != nil {
			t.Fatalf("render: %v", err)
		}
		parts = append(parts, buf.String())
	}
	return covFlat(strings.Join(parts, "\n"))
}

func decParse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "q.go", src, 0)
	if err != nil {
		t.Fatalf("every composition this decision is measured on is real Go:\n%s\n%v", src, err)
	}
	return fset, f
}

// decFindings runs one composition through the real rule at decParams.
func decFindings(t *testing.T, src string) int {
	t.Helper()
	c := checker(t, "sql/locking-select-order", decParams+"\n")
	return len(matches(t, c, file("q.go", src)))
}

func decRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is one of the three documents this decision is recorded in: %v", path, err)
	}
	return string(b)
}

// decFlat normalises a document for containment: comment markers, code spans
// and markdown emphasis go, and every run of whitespace collapses to one space.
// One sentence therefore reads the same wrapped in a Go comment at eighty
// columns, set in backticks in a spec, and bolded in the manual — so all three
// can carry the same words without this test being a check on typography.
func decFlat(s string) string {
	s = strings.ReplaceAll(s, "//", " ")
	s = strings.NewReplacer("`", "", "*", "").Replace(s)
	return covFlat(s)
}
