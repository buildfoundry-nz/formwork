// doc_section_reference_test.go — when a comment in this repository's Go source
// sends a reader to a named section of a document, that section is there.
//
// WHY THIS EXISTS. #338 turned on a pointer. The decision recorded at
// internal/hooks/hooks.go's checkCommand refuses `--skip-escapes`, and because a
// refusal with no alternative is half a decision it names the lever that does
// bound the multi-checkout case — per-lane `cost:` filters — and tells the
// reader where that lever is written down. The pointer was wrong: it named a
// subsection of docs/reference.md that says nothing about `cost:`, one
// subsection past the one that carries it. A reader who followed it found the
// refusal and not the alternative.
//
// Nothing in the repository could see that, which is the part worth fixing.
// formwork registers a `doc-path-exists` rule type, three rules in examples/ use
// it, and doc_path_reference_test.go carries the same idea for the published
// documents — but every one of them resolves the FILE a citation names. The file
// existed. The heading inside it was never read. This gate is that resolution
// one altitude in, over the citations Go comments make.
//
// WHAT IS JUDGED, AND WHAT IS NOT.
//
//   - COMMENTS, NOT STRING LITERALS. This parses rather than greps, because a
//     `//`-prefixed line inside a raw string is fixture bytes — this package's
//     own test corpora are full of them — and judging one would be judging an
//     input, not a claim the author made.
//
//   - THE HEADING, NOT THE PROSE UNDER IT. The document must be tracked here and
//     must carry a heading of the cited name. Whether that section documents the
//     particular thing it was cited FOR is a claim only the citing package can
//     make, and internal/hooks/decision_citation_test.go makes it for the
//     citation above. Referential integrity generalises; the semantics do not.
//
//   - CODE SPANS ARE NOT PART OF THE NAME. Half of docs/reference.md's headings
//     are spelled with backticks and prose cites them either way. Refusing an
//     honest citation over a backtick would teach authors to write around this
//     gate rather than fix the pointer it exists to keep true.
//
// THIS IS A TEST RATHER THAN A FORMWORK RULE, for the reason
// doc_path_reference_test.go gives for its own shape: a rule's predicate reads
// ONE file, and this claim spans two — the citing Go file, and the markdown file
// named by data INSIDE the citation, which no scope can declare ahead of time.
// `doc-path-exists` resolves such a path as far as a stat; nothing registered
// reads a heading inside the file it just resolved.
package repoproof_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// citedSection is one section pointer a Go comment makes, kept with its origin
// so an unresolved citation reports the citing file:line.
type citedSection struct {
	file    string // repo-relative path of the commenting Go file
	line    int
	doc     string // repo-relative path of the cited markdown document
	section string // the heading name as the comment spells it
}

// sectionCitation matches a `<doc>.md's <Section> section` pointer.
//
// The name stops at sentence punctuation so a citation can never run past the
// end of its own sentence and swallow a later "section", and the space before
// `section` is load-bearing: it keeps "subsection" — how prose refers to a
// nested heading in general rather than pointing at a particular one — out of
// the judged set.
var sectionCitation = regexp.MustCompile(`([A-Za-z0-9._/-]+\.md)'s ([^.,;:]+?) section`)

// goCommentCitations reads every tracked Go file's comments and returns the
// section pointers they make.
//
// Each comment group is unwrapped onto one line before matching, so a citation
// is not hostage to where the author's paragraph happened to wrap — which is
// exactly where the #338 pointer sat.
func goCommentCitations(t *testing.T, root string) []citedSection {
	t.Helper()
	var found []citedSection
	for _, rel := range trackedFiles(t, root, "cmd/*.go", "internal/*.go", "tools/*.go") {
		path := filepath.Join(root, filepath.FromSlash(rel))
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			// Fail, never skip: a Go file this gate cannot parse is a file
			// whose citations go unjudged, and a gate that quietly judges a
			// smaller tree reports the same green as one that judged it all.
			t.Fatalf("cannot parse %s, so its comments cannot be read: %v", rel, err)
		}
		for _, group := range file.Comments {
			flat := strings.Join(strings.Fields(group.Text()), " ")
			for _, m := range sectionCitation.FindAllStringSubmatch(flat, -1) {
				found = append(found, citedSection{
					file:    rel,
					line:    fset.Position(group.Pos()).Line,
					doc:     m[1],
					section: strings.TrimSpace(m[2]),
				})
			}
		}
	}
	return found
}

// markdownHeadings lists every heading in a markdown file, in order.
//
// A `#` at column zero inside a fenced block is a shell prompt or a YAML
// comment, not a heading, so fences are skipped — the same distinction
// doc_path_reference_test.go draws for path citations.
func markdownHeadings(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, so the sections cited from it cannot be resolved: %v", path, err)
	}
	heading := regexp.MustCompile(`^#{1,6} +(.*?)\s*$`)
	var headings []string
	fenced := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if m := heading.FindStringSubmatch(line); m != nil && !fenced {
			headings = append(headings, m[1])
		}
	}
	return headings
}

// sameSectionName compares a heading to a cited name, ignoring case and code
// spans.
func sameSectionName(heading, cited string) bool {
	strip := func(s string) string { return strings.ReplaceAll(s, "`", "") }
	return strings.EqualFold(strip(heading), strip(cited))
}

const sectionCitationCure = "" +
	"A comment that sends a reader to a named section is a pointer the reader is expected to\n" +
	"follow. Correct the section name, or move the paragraph the citation points at — never\n" +
	"delete the pointer to quiet this gate: a decision that refuses one lever and misdirects\n" +
	"the reader to the other is half a decision, which is the defect #338 was filed for."

// TestGoCommentsCiteASectionThatExists is the gate. It reads the real tree.
func TestGoCommentsCiteASectionThatExists(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	cites := goCommentCitations(t, root)
	if len(cites) == 0 {
		t.Fatal("no section citation was found in any tracked Go comment — this gate would pass over nothing")
	}

	headings := map[string][]string{}
	var dangling []string
	for _, c := range cites {
		if _, seen := headings[c.doc]; !seen {
			headings[c.doc] = markdownHeadings(t, filepath.Join(root, filepath.FromSlash(c.doc)))
		}
		resolved := false
		for _, h := range headings[c.doc] {
			if sameSectionName(h, c.section) {
				resolved = true
				break
			}
		}
		if !resolved {
			dangling = append(dangling, "  "+c.file+":"+strconv.Itoa(c.line)+
				"  cites "+c.doc+"'s "+strconv.Quote(c.section)+" section, and that document has no such heading\n"+
				"    headings in "+c.doc+": "+strings.Join(headings[c.doc], ", "))
		}
	}
	if len(dangling) > 0 {
		t.Fatalf("%d Go comment citation(s) name a section that is not there:\n%s\n\n%s",
			len(dangling), strings.Join(dangling, "\n"), sectionCitationCure)
	}
}

// sectionCitationFloor is the non-vacuity floor. Each entry is a pointer a
// comment makes into a document, and they are the reason the gate is worth
// running: if the scanner stops finding them it is judging a smaller tree than
// it was written to judge, and the green above stops meaning anything. Follow a
// moved citation here; do not delete the entry.
var sectionCitationFloor = []citedSection{
	{file: "internal/cli/scope_format_test.go", doc: "docs/reference.md", section: "Introspection"},
	{file: "internal/hooks/hooks.go", doc: "docs/reference.md", section: "Lanes"},
}

func TestSectionCitationScannerSeesTheCitationsThisGateJudges(t *testing.T) {
	needBinary(t, "git")
	got := map[citedSection]bool{}
	for _, c := range goCommentCitations(t, repoRoot(t)) {
		got[citedSection{file: c.file, doc: c.doc, section: c.section}] = true
	}
	var missing []string
	for _, want := range sectionCitationFloor {
		if !got[want] {
			missing = append(missing, want.file+" -> "+want.doc+" "+strconv.Quote(want.section))
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the scanner no longer sees %d citation(s) this gate is meant to judge:\n  %s\n"+
			"If the pointer moved, follow it in sectionCitationFloor. If the scanner stopped seeing it, that is the bug.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// --- the scanner's own judgements, each exercised ---------------------------

// The vectors live in a synthetic file rather than the real tree because they
// are statements about what the scanner does, not about what this repository
// currently says. Driving them through parser.ParseFile is what keeps the
// comment/string-literal distinction honest — a grep-based scanner would pass
// the first three of these and fail the fourth.
func TestSectionCitationScannerReadsCommentsAndNotLiterals(t *testing.T) {
	const src = `package p

// A wrapped pointer: docs/reference.md's Lanes
// section, which the author's paragraph split.
func A() {}

// Two in one sentence: docs/a.md's One section and docs/b.md's Two section.
func B() {}

// Stops at punctuation: docs/c.md's Three section. Not the section after it.
func C() {}

// Not a pointer at a particular heading: docs/d.md's own subsection.
func D() {}

func E() string { return "// docs/e.md's Literal section" }
`
	dir := t.TempDir()
	path := filepath.Join(dir, "vectors.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("cannot write the scanner vectors: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("cannot parse the scanner vectors: %v", err)
	}
	var got []string
	for _, group := range file.Comments {
		flat := strings.Join(strings.Fields(group.Text()), " ")
		for _, m := range sectionCitation.FindAllStringSubmatch(flat, -1) {
			got = append(got, m[1]+" "+strings.TrimSpace(m[2]))
		}
	}

	want := []string{
		"docs/reference.md Lanes", // read across the line the author wrapped on
		"docs/a.md One",           // both citations in one sentence
		"docs/b.md Two",
		"docs/c.md Three", // stopped at the full stop, so "after it" is not a name
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the scanner read %v; want %v.\n"+
			"A citation missing here is one the gate would never judge; an extra one is a "+
			"sentence the gate would fail over without an author having pointed anywhere.", got, want)
	}
	for _, g := range got {
		if strings.Contains(g, "docs/e.md") {
			t.Error("the scanner read a citation out of a string literal — those are fixture bytes, not a pointer an author made")
		}
	}
}

// The heading reader's two judgements: a `#` inside a fence is not a heading,
// and a code-spanned heading is the same heading as its bare spelling.
func TestMarkdownHeadingReaderSkipsFencesAndCodeSpans(t *testing.T) {
	const doc = "# Top\n" +
		"prose\n" +
		"```sh\n" +
		"# not a heading, a shell comment\n" +
		"```\n" +
		"## `formwork scope` file-set modes\n" +
		"more prose\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatalf("cannot write the heading vectors: %v", err)
	}

	got := markdownHeadings(t, path)
	want := []string{"Top", "`formwork scope` file-set modes"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("markdownHeadings read %v; want %v", got, want)
	}
	if !sameSectionName(got[1], "formwork scope file-set modes") {
		t.Error("a backticked heading did not match its bare spelling, so an honest citation would be refused")
	}
	if sameSectionName(got[0], "not a heading, a shell comment") {
		t.Error("a `#` line inside a fence was read as a heading")
	}
}
