// locking_specclaim_test.go — #337.
//
// The fold spec is the normative document `fold.go` points readers at, and for
// this one shape it says the opposite of what the tool does. §9: "A **named
// closure called unconditionally** … used to head this list, and does not any
// more: it is untracked and emits nothing." §9 item 2: "widened across every
// binding and call spelling by #337." Both are true of every spelling the pass
// can SEE being called, and false of the one it cannot — a closure whose name is
// handed to a call, which still fabricates and still fires on a query ordered on
// every real path.
//
// That is the same defect #312 and #313 were filed about, one document over: a
// claim of closure that nothing executes, drifting in the direction that tells
// an adopter holding a real finding to dismiss it. #313 gave `locking.go`'s
// block a machine-checked half; this gives the spec's version of the same
// sentence one.
//
// The spelling is READ OUT OF THE FIXTURE, never spelled here. covShapes
// carries the composition that measures FIRES; this file parses it, finds the
// call that hands the closure's name away, and requires the spec to name that
// call wherever it declares the class closed. Rename the helper in the fixture
// and the spec is wrong here rather than wrong in somebody's quotation of it.
package sqlparse_test

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	// The class, by the name both documents give it.
	covClassRE = regexp.MustCompile(`(?i)named closure`)
	// The claim of closure, matched on what it asserts rather than on one
	// wording of it: a paragraph about this class that says the variable is
	// untracked, or that nothing is emitted, is telling a reader the class no
	// longer fabricates.
	covClosedRE = regexp.MustCompile(`(?i)untrack|emits? nothing|nothing is emitted`)
)

// The spec must not claim this class is closed without naming the spelling that
// still fires.
func TestFoldSpecDoesNotClaimTheCalledClosureClassIsClosed(t *testing.T) {
	b, err := os.ReadFile(foldSpecPath)
	if err != nil {
		t.Fatalf("the fold design spec is the document fold.go points readers at: %v", err)
	}
	spec := string(b)
	call, helper := covEscapeSpelling(t)

	if !strings.Contains(covFlat(spec), helper) {
		t.Errorf("the composition `locking.go` discloses as FIRES is built on %q "+
			"and the fold spec never mentions it — the only place this residual is "+
			"written down would then be a package comment in internal/sqlextract, "+
			"while the two documents an adopter reads say the class is closed",
			helper)
	}

	claims := 0
	for i, para := range strings.Split(spec, "\n\n") {
		if !covClassRE.MatchString(para) || !covClosedRE.MatchString(para) {
			continue
		}
		claims++
		if strings.Contains(covFlat(para), call) {
			continue
		}
		t.Errorf("spec paragraph %d says the named-closure class is untracked and "+
			"emits nothing, and does not name %s — the spelling that is still "+
			"tracked, still emits, and still fires on a query ordered on every "+
			"real path (locking.go: SHAPE closure-name-escape FIRES). First line: %q",
			i, call, covFirstLine(para))
	}
	if claims < 3 {
		t.Fatalf("only %d spec paragraph(s) both name this class and claim it is "+
			"closed — the spec carries that claim in §4.2, §9 item 2, §9's "+
			"false-positive list and §10's revision note, so a run finding fewer "+
			"than three is matching prose that has been rewritten out from under "+
			"this test rather than checking it", claims)
	}
}

// covEscapeSpelling reads the still-firing call and its callee out of the
// FIXTURE, so the spec cannot be brought into agreement with a renamed helper by
// editing prose alone. The composition is IDENTIFIED by that shape rather than
// looked up by key (manEscapeShape) — the machine name is itself one of the
// things the disclosure has to carry, so a key spelled here would let this file
// agree with a fixture the block no longer discloses under that name.
func covEscapeSpelling(t *testing.T) (call, helper string) {
	t.Helper()
	_, call, helper = manEscapeShape(t)
	return call, helper
}

func covRender(t *testing.T, fset *token.FileSet, n ast.Node) string {
	t.Helper()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// covFlat collapses every run of whitespace to one space, so a rendering
// go/printer tabs and a spec sentence markdown wraps compare as the same text.
func covFlat(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func covFirstLine(para string) string {
	if i := strings.IndexByte(para, '\n'); i >= 0 {
		return para[:i]
	}
	return para
}

// §9 IS A DISCLOSURE LIST, NOT A BACKLOG, and one entry was written as both.
//
// "7. **`strings.Builder`** — unchanged from before (separate follow-up)."
// was the whole of what the spec said about the composition #311 half 2 names
// FIRST among the four whose unordered locking SELECT the gate cannot see. It
// was wrong in two ways at once by the time it was read: the shape IS counted
// now — `locking.go` discloses `SHAPE strings-builder SILENT #311`,
// `FromGoReassembled` returns a Site for it and `sqlparse.UnreadableSites`
// puts it in the census — and "separate follow-up" tells a reader auditing a
// clean run that there is nothing here to look for yet, which is the direction
// that misdirects triage.
//
// A coverage limit in this list is in one of three states: closed, kept with
// the measurement that keeps it, or counted by the census. There is no fourth
// state in which it is owed to a later pass, so a promise of one is a claim
// about this repository that nothing executes.
func TestFoldSpecCoverageLimitsDeferNothing(t *testing.T) {
	b, err := os.ReadFile(foldSpecPath)
	if err != nil {
		t.Fatalf("the fold design spec is the document fold.go points readers at: %v", err)
	}
	items := covSection9Items(t, string(b))
	if len(items) < 7 {
		t.Fatalf("§9's coverage-limit list parsed as %d item(s); the section carries "+
			"seven, so a shorter read means this test is matching a heading that has "+
			"moved rather than checking the list", len(items))
	}
	for i, item := range items {
		if m := covDeferralRE.FindString(item); m != "" {
			t.Errorf("§9 item %d defers a coverage limit (%q): every shape in this "+
				"list is closed, kept with the measurement that keeps it, or counted "+
				"by the census — `locking.go` discloses each SILENT one and "+
				"census_sites_test.go runs it — so a promise of a later pass tells a "+
				"reader auditing a clean run there is nothing here to look for. "+
				"First line: %q", i+1, m, covFirstLine(item))
		}
	}
}

// covDeferralRE is the vocabulary of work owed to a later pass.
var covDeferralRE = regexp.MustCompile(`(?i)separate follow-?up|follow-?up|future work|\bTODO\b|\bFIXME\b|for now|revisit later|left (as|to) a|deferred to|to be (done|fixed|addressed)`)

// covSection9Items splits §9's top-level numbered list into whole items,
// continuation lines and nested bullets included. Reading the section by
// heading rather than by line number means a spec edit that moves the list
// keeps this test pointed at it, and a spec edit that removes the heading fails
// the premise above rather than passing vacuously.
func covSection9Items(t *testing.T, spec string) []string {
	t.Helper()
	start := strings.Index(spec, "\n## 9. ")
	if start < 0 {
		t.Fatal("no `## 9.` heading in the fold spec: §9 is the coverage-limit " +
			"section every SILENT shape in locking.go cites")
	}
	body := spec[start+1:]
	if end := strings.Index(body[1:], "\n## "); end >= 0 {
		body = body[:end+1]
	}
	itemStart := regexp.MustCompile(`^[0-9]+\. `)
	var items []string
	var cur []string
	for _, line := range strings.Split(body, "\n") {
		if itemStart.MatchString(line) {
			if len(cur) > 0 {
				items = append(items, strings.Join(cur, "\n"))
			}
			cur = []string{line}
			continue
		}
		if len(cur) == 0 {
			continue
		}
		// A blank line ends the item only when what follows is not indented
		// continuation, which the next iteration decides; keep collecting
		// indented lines and stop at the first flush-left prose line.
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			items = append(items, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		items = append(items, strings.Join(cur, "\n"))
	}
	return items
}
