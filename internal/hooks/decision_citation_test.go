// decision_citation_test.go — #338. A decision comment's own pointer resolves.
//
// hooks.go's checkCommand records the #81/#338 decision: `hooks install` does
// not emit --skip-escapes and the flag keeps its false default. That decision
// only stands up because it names the lever that DOES bound the parallel-
// checkout case — per-lane `cost:` filters — and tells the reader where that
// lever is written down. A reader who follows the pointer and lands somewhere
// that says nothing about `cost:` is left with a refusal and no alternative,
// which is the shape the decision was written to avoid.
//
// The pointer was measurably wrong when this test was written: the comment sent
// the reader to docs/reference.md's "File-set modes" subsection, and `cost:` is
// documented one subsection earlier, under "Lanes". Nothing could see that —
// internal/repoproof/doc_path_reference_test.go judges cited FILES in published
// markdown, so the file resolved and the subsection inside it was never read.
//
// WHY THIS IS NOT THE PROSE ASSERTION skip_escapes_decision_test.go REFUSES.
// That file declines to assert the comment's text, because a test over prose
// teaches people to edit the test. This one asserts nothing about what the
// comment says. It reads whatever section the comment happens to cite and
// checks that the section exists and contains the thing cited for. Rewrite the
// paragraph however you like; the pointer just has to land somewhere that
// documents `cost:`. That is referential integrity, not fixity — the same
// distinction doc_path_reference_test.go draws for file paths, one altitude
// down, at the subsection.
//
// It lives beside the decision rather than in repoproof for the reason
// skip_escapes_decision_test.go gives for its own placement: the thing being
// held is this package's decision, and `cost:` is the lever THIS package's
// argument turns on. The half that generalises does not live here — every Go
// comment in the tree that names a section of a document is judged by
// internal/repoproof's TestGoCommentsCiteASectionThatExists, which resolves the
// heading and stops there. This test is the semantic half on top of it: not
// that the section exists, but that it is the one carrying the lever the
// refusal above sends the reader to find.
package hooks_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// costLever is what checkCommand's comment sends the reader to find: the
// per-lane cost filter, spelled as it is in a lane declaration.
const costLever = "cost:"

// docSectionCitation matches a "docs/<file>.md's <Section> section" pointer.
// The section name stops at sentence punctuation so a citation can never run
// past the end of its own sentence and match a later "section".
var docSectionCitation = regexp.MustCompile(`(docs/[A-Za-z0-9._/-]+\.md)'s ([^.,;:]+?) section`)

func TestDecisionCommentCitesASectionThatDocumentsTheCostLever(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("cannot locate the module root: %v", err)
	}

	doc := funcDoc(t, filepath.Join(root, "internal", "hooks", "hooks.go"), "checkCommand")
	cites := docSectionCitation.FindAllStringSubmatch(strings.Join(strings.Fields(doc), " "), -1)
	if len(cites) == 0 {
		t.Fatalf("checkCommand's decision comment cites no docs/<file>.md section.\n"+
			"The decision refuses --skip-escapes on the grounds that per-lane `cost:` filters are the "+
			"lever that bounds the multi-checkout case without narrowing what the gate judges. A reader "+
			"who is refused one lever and pointed at no other has been given half a decision, so the "+
			"pointer is load-bearing: restore it rather than deleting it to quiet this test.\n"+
			"comment was:\n%s", doc)
	}

	for _, cite := range cites {
		docFile, wanted := cite[1], strings.TrimSpace(cite[2])
		path := filepath.Join(root, filepath.FromSlash(docFile))
		body, headings, ok := markdownSection(t, path, wanted)
		if !ok {
			t.Errorf("checkCommand's decision comment cites %s's %q section, and %s has no such heading.\n"+
				"headings in that file: %s", docFile, wanted, docFile, strings.Join(headings, ", "))
			continue
		}
		if !strings.Contains(body, costLever) {
			t.Errorf("checkCommand's decision comment sends the reader to %s's %q section for the "+
				"per-lane cost lever, and that section never mentions %q.\n"+
				"The lever is real and it is documented — the pointer names the wrong subsection, so the "+
				"reader who follows it finds the refusal and not the alternative. Cite the subsection "+
				"that carries the `cost: fast | heavy` schema.\nsection %q reads:\n%s",
				docFile, wanted, costLever, wanted, body)
		}
	}
}

// funcDoc returns the doc comment of the named top-level function.
//
// It parses rather than greps because the association being read is "the
// comment attached to THIS function": a grep for the citation would still pass
// if the paragraph were cut loose from checkCommand and left floating in the
// file, which is a state a reader of checkCommand cannot see.
func funcDoc(t *testing.T, path, name string) string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv != nil {
			continue
		}
		if fn.Doc == nil {
			t.Fatalf("%s has no doc comment in %s, so the decision it carried is gone", name, path)
		}
		return fn.Doc.Text()
	}
	t.Fatalf("no func %s in %s", name, path)
	return ""
}

// markdownSection returns the body of the named heading, which runs to the next
// heading at the same or a shallower level — so citing a parent section counts
// its subsections, and citing a leaf counts only the leaf. That is what makes
// the check a subsection check: "Lanes and file-set modes" would resolve past
// both of its children, while "File-set modes" resolves to its own text alone.
//
// Fenced blocks are skipped when looking for headings: a `#` at column zero
// inside a fence is a shell prompt or a YAML comment, not a heading.
//
// Names are matched with code spans stripped, because half this file's headings
// are spelled with backticks (`### command`, "### `formwork scope` file-set
// modes") and prose cites them either way. Rejecting an honest citation over a
// backtick would teach authors to write around this check rather than fix the
// pointer it exists to keep true.
func markdownSection(t *testing.T, path, name string) (body string, headings []string, found bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	heading := regexp.MustCompile(`^(#{1,6}) +(.*?)\s*$`)

	var collected []string
	level := 0
	fenced, capturing, ended := false, false, false
	for _, line := range strings.Split(string(raw), "\n") {
		wasFenced := fenced
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if m := heading.FindStringSubmatch(line); m != nil && !wasFenced && !fenced {
			headings = append(headings, m[2])
			switch {
			case capturing && len(m[1]) <= level:
				// The cited section ended. Scanning continues so the failure
				// message can list every heading the file does have.
				capturing, ended = false, true
			case !capturing && !ended && sameHeading(m[2], name):
				capturing, level, collected = true, len(m[1]), nil
				continue // the heading line is the name, not the body
			}
		}
		if capturing {
			collected = append(collected, line)
		}
	}
	return strings.Join(collected, "\n"), headings, capturing || ended
}

// sameHeading compares a heading to a cited name, ignoring case and code spans.
func sameHeading(title, cited string) bool {
	strip := func(s string) string { return strings.ReplaceAll(s, "`", "") }
	return strings.EqualFold(strip(title), strip(cited))
}
