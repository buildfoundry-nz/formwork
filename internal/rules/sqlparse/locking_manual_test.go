// locking_manual_test.go — #337, at the document an adopter actually reads.
//
// THE FINDING CITES AN ISSUE NUMBER AT AN OPERATOR WHOSE MANUAL SAYS NOTHING
// ABOUT IT. `closureNameEscapeNote` tells the reader holding this finding, in
// the message itself, that it "may be the disclosed #337 world", names the
// escape as written, and hands over a two-outcome decision procedure — read the
// callee, CALLS means this reports a hazard the code does not have, STORES means
// the hazard is real — with a `formwork:allow` marker as the cure for the first.
// Every word of that was written down in exactly two places, both of them Go
// source: the COVERAGE LIMIT block in locking.go and the fold design spec under
// docs/specs/. The manual an adopter reads carried a four-line blockquote
// pointing AT that Go file, so the reader the tool addressed by issue number had
// nowhere to go and read it.
//
// A disclosure an operator can only find by reading the analyzer's source is not
// a disclosure — escapehint_test.go's own words about why the note exists at
// all, one channel further out. So the manual carries the residual itself: the
// machine name the block discloses it under, the program it fires on verbatim,
// the issue the finding cites, both outcomes of the procedure, and the marker.
//
// NOTHING HERE IS SPELLED. Every token required of the manual is read at
// runtime out of the live finding, out of the fixture that produces it, or out
// of the COVERAGE LIMIT block — the locking_specclaim_test.go precedent. Reword
// the note and this test asks the manual for the new wording rather than going
// on checking a quotation that has drifted; rename the helper in the fixture and
// the manual is wrong here rather than wrong in somebody's transcription of it.
//
// AND IT FAILS IN BOTH DIRECTIONS, which is the census_wiring precedent and the
// half #312/#313 were filed about one document over: the section the block
// points at has to exist under the name the block gives it, so deleting the
// disclosure reddens here rather than leaving a pointer into nothing.
package sqlparse_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// manualPath is the adopter-facing manual, from this package's directory.
const manualPath = "../../../docs/reference.md"

var (
	// manPointer is the `<doc>.md's <Section> section` shape
	// internal/repoproof/doc_section_reference_test.go resolves against the
	// real headings. Reading the section name out of the block rather than
	// spelling it here means a rename that moves both keeps this test pointed
	// at the right place, and a rename that moves only one fails there.
	manPointer = regexp.MustCompile(`([A-Za-z0-9._/-]+\.md)'s ([^.,;:]+?) section`)

	// The three vocabularies the note is built from, matched by shape so that
	// the values stay the finding's to choose.
	manIssueRE  = regexp.MustCompile(`#[0-9]+`)
	manShoutRE  = regexp.MustCompile(`[A-Z]{4,}`)
	manMarkerRE = regexp.MustCompile(`[a-z]+:[a-z-]+`)

	manHeadingRE = regexp.MustCompile(`^(#{1,6}) +(.*?)\s*$`)
)

// TestTheManualCarriesTheEscapeTheFindingCitesAtAnOperator is the gate.
func TestTheManualCarriesTheEscapeTheFindingCitesAtAnOperator(t *testing.T) {
	key, _, _ := manEscapeShape(t)
	msg := manOnlyFinding(t, covShapes[key])

	name := manPointedSection(t, coverageBlock(t))
	manual := manRead(t)
	sect, ok := manSectionText(manual, name)
	if !ok {
		t.Fatalf("the COVERAGE LIMIT block sends adopters to %s's %q section and the "+
			"manual has no such heading: the finding tells an operator this may be a "+
			"disclosed world and the document they read to check that is where the "+
			"disclosure has to be", manualPath, name)
	}
	flat := covFlat(sect)

	issues := manDistinct(manIssueRE.FindAllString(msg, -1))
	shouts := manDistinct(manShoutRE.FindAllString(msg, -1))
	markers := manDistinct(manMarkerRE.FindAllString(msg, -1))
	// The non-vacuity floor, per vocabulary. A note reworded until one of these
	// matches nothing would leave this test asking the manual for a shorter and
	// shorter list and still passing, which is the shape of check this whole
	// epic is about.
	switch {
	case len(issues) < 1:
		t.Fatalf("the finding cites no issue, so this test would require nothing of the "+
			"manual on that count: %q", msg)
	case len(shouts) < 3:
		t.Fatalf("the finding's decision procedure turns on %d shouted word(s); it is a "+
			"two-outcome check announced as a NOTE, so fewer than three means the "+
			"procedure has gone out of the message and this test is asking the manual "+
			"to carry a procedure nobody is handed: %q", len(shouts), msg)
	case len(markers) < 1:
		t.Fatalf("the finding names no `tool:marker` cure, so the manual is not being "+
			"asked to carry one — and the cure is the half that makes the disclosure "+
			"actionable: %q", msg)
	}

	want := map[string]string{
		key: "the machine name the COVERAGE LIMIT block discloses this shape under, " +
			"which is what joins a manual entry to a line in that block",
		covFlat(covShapes[key]): "the program the finding fires on, verbatim — an " +
			"adopter has to be able to compare their own code against it",
	}
	for _, s := range issues {
		want[s] = "the issue the finding cites at the operator in the message"
	}
	for _, s := range shouts {
		want[s] = "a word the finding's own decision procedure turns on"
	}
	for _, s := range markers {
		want[s] = "the cure the finding names"
	}

	var missing []string
	for tok, why := range want {
		if !strings.Contains(flat, tok) {
			missing = append(missing, "\n  "+strconvQuote(tok)+" — "+why)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%s's %q section is where an operator holding this finding goes to "+
			"read what it is telling them, and it does not carry %d of the things the "+
			"finding sends them to look up:%s\n\nThe finding, verbatim: %q",
			manualPath, name, len(missing), strings.Join(missing, ""), msg)
	}
}

// manEscapeShape finds, among the compositions the COVERAGE LIMIT block
// discloses, the one whose program hands a closure's NAME to a call. The KEY is
// a result rather than an input: the machine name is one of the things the
// manual is required to carry, so spelling it here would let the manual and the
// block agree with this test while disagreeing with each other.
func manEscapeShape(t *testing.T) (key, call, helper string) {
	t.Helper()
	keys := make([]string, 0, len(covShapes))
	for k := range covShapes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var found []string
	for _, k := range keys {
		c, h, ok := manEscapeSpelling(t, covShapes[k])
		if !ok {
			continue
		}
		found = append(found, k)
		key, call, helper = k, c, h
	}
	if len(found) != 1 {
		t.Fatalf("exactly one disclosed composition hands a closure's NAME to a call — "+
			"that escape is the whole shape #337 is about, and a run finding %d of them "+
			"(%v) is reading a fixture set this test can no longer identify the shape "+
			"in", len(found), found)
	}
	return key, call, helper
}

// manEscapeSpelling reports whether one composition binds a func literal to a
// name and then hands that name to a call, and if so renders the call and the
// declaration of its callee. Whether that callee invokes f is the fact this pass
// cannot reach, so the fixture has to show it.
func manEscapeSpelling(t *testing.T, src string) (call, helper string, ok bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "q.go", src, 0)
	if err != nil {
		t.Fatalf("every disclosed composition is real Go: %v", err)
	}

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
	if len(closures) == 0 {
		return "", "", false
	}

	var callee string
	var site *ast.CallExpr
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
				site, callee = c, fn.Name
			}
		}
		return true
	})
	if site == nil {
		return "", "", false
	}

	for _, d := range f.Decls {
		if fd, isFunc := d.(*ast.FuncDecl); isFunc && fd.Name.Name == callee {
			return covFlat(covRender(t, fset, site)), covFlat(covRender(t, fset, fd)), true
		}
	}
	t.Fatalf("a composition hands a closure to %s and declares no such function — "+
		"whether it invokes f is the fact this pass cannot reach, so the fixture has "+
		"to show it", callee)
	return "", "", false
}

// manOnlyFinding runs the gate over one composition and returns the single
// finding's message. It fails rather than defaulting when the count is not one:
// "no finding" and "a finding whose note went missing" are the two failures this
// file exists to tell apart.
func manOnlyFinding(t *testing.T, src string) string {
	t.Helper()
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("q.go", src))
	if len(ms) != 1 {
		t.Fatalf("precondition: the escaped closure's world still fires — #337 is a "+
			"disclosed trade, not a fix: %+v", ms)
	}
	return ms[0].Message
}

// manPointedSection reads the manual section the COVERAGE LIMIT block names.
// The block and the manual point at each other, so neither can be edited into
// agreement alone: this test resolves the pointer, and
// internal/repoproof/doc_section_reference_test.go resolves it against the real
// headings from the other end.
func manPointedSection(t *testing.T, block string) string {
	t.Helper()
	flat := covFlat(strings.ReplaceAll(block, "//", " "))
	for _, m := range manPointer.FindAllStringSubmatch(flat, -1) {
		if strings.HasSuffix(manualPath, m[1]) {
			return strings.TrimSpace(m[2])
		}
	}
	t.Fatalf("the COVERAGE LIMIT block names no section of %s. It is the list the "+
		"manual sends adopters to, and the finding it produces cites an issue at an "+
		"operator: the block has to name where that operator reads about it, or the "+
		"only copy of this residual is Go source again", manualPath)
	return ""
}

func manRead(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("%s is the manual adopters read: %v", manualPath, err)
	}
	return string(b)
}

// manSectionText returns the body of a heading, up to the next heading at the
// same level or shallower, the heading line included. Fenced blocks are opaque,
// so a `#` at column zero inside an example is not read as a heading — the same
// distinction doc_path_reference_test.go and doc_section_reference_test.go draw.
func manSectionText(manual, name string) (string, bool) {
	var out []string
	level, inFence, collecting := 0, false, false
	for _, ln := range strings.Split(manual, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			if collecting {
				out = append(out, ln)
			}
			continue
		}
		if !inFence {
			if m := manHeadingRE.FindStringSubmatch(ln); m != nil {
				switch {
				case collecting && len(m[1]) <= level:
					return strings.Join(out, "\n"), true
				case !collecting && manSameName(m[2], name):
					level, collecting = len(m[1]), true
				}
			}
		}
		if collecting {
			out = append(out, ln)
		}
	}
	if collecting {
		return strings.Join(out, "\n"), true
	}
	return "", false
}

// manSameName compares a heading to a cited name ignoring case and code spans,
// exactly as doc_section_reference_test.go does.
func manSameName(heading, cited string) bool {
	strip := func(s string) string { return strings.ReplaceAll(s, "`", "") }
	return strings.EqualFold(strip(heading), strip(cited))
}

func manDistinct(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// strconvQuote keeps a long program readable in a failure: the whole
// composition is one required token, and %q on it inside a joined list is
// unreadable without the delimiters being obvious.
func strconvQuote(s string) string {
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return "`" + s + "`"
}
