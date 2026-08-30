// coverage_internal_test.go — #313, the sqlextract half.
//
// coverage.go's five reasons are the single copy of a list that three places
// have to agree about: this package's classifiers, the COVERAGE LIMIT block in
// internal/rules/sqlparse/locking.go, and the census that reports unanalysable
// compositions to an operator. Two of those three had drifted into declaring
// closed defects open and naming behaviour the shipped rule does not have.
//
// A list is only a single copy while the code PRODUCES it. If the reasons were
// a table nothing stamped, editing coverage.go would be editing a comment with
// a struct around it, and the cross-package test that reads the block would be
// checking one piece of prose against another. So this file pins the stamping:
// for each reason, a fixture whose refusal must carry exactly that value, and
// the assertion that no classifier can return a value the table does not list.
//
// Inside the package because the stamp is not observable from outside — an
// untracked variable emits nothing, and "nothing" looks the same whichever
// analysis refused it. That indistinguishability at the output is precisely why
// the reason has to be carried explicitly rather than inferred by a reader.
package sqlextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

// bodyOf parses one function's source and returns its body, plus the signature
// fixedArrays needs.
func bodyOf(t *testing.T, src string) (*ast.FuncType, *ast.BlockStmt) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "db.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "load" {
			return fd.Type, fd.Body
		}
	}
	t.Fatalf("no func load in %q", src)
	return nil, nil
}

// loadSrc wraps a body in `func load(params) string`, the shape every fixture
// below uses.
func loadSrc(params, body string) string {
	return "package db\n\nfunc load(" + params + ") string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" + body + "\treturn q\n}\n"
}

// Each block-wide analysis must stamp its OWN reason. A refusal that carries
// the wrong one is worse than an unlabelled refusal: locking.go's block and the
// census both disclose the reason to a reader deciding whether a clean run on
// this file means anything.
func TestUnfoldableStampsTheAnalysisThatRefused(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want UntrackReason
	}{
		{"deref-write", loadSrc("", "\tp := &q\n\t*p += \" FOR UPDATE\"\n"),
			reasonDerefWrite},
		{"called-closure", loadSrc("",
			"\tadd := func() { q += \" ORDER BY id\" }\n\tadd()\n\tq += \" FOR UPDATE\"\n"),
			reasonCalledClosure},
		{"address-escape", loadSrc("",
			"\torderIt(&q)\n\tq += \" FOR UPDATE\"\n"),
			reasonAddressEscape},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, body := bodyOf(t, c.src)
			got, ok := unfoldable(body)["q"]
			if !ok {
				t.Fatalf("q must not be tracked at all here: unfoldable = %v",
					unfoldable(body))
			}
			if got.reason != c.want {
				t.Fatalf("unfoldable stamped %+v, want %+v — the reason a variable "+
					"went unanalysed is what locking.go's COVERAGE LIMIT and the "+
					"census both disclose", got, c.want)
			}
		})
	}
}

// The three analyses overlap, so the merge needs an order or a consumer sees a
// different reason run to run. This source is found by aliasUnsafe (a deref
// write with q's address taken) AND by escapedNames (the address handed to a
// call at a provably-run position); the deref write is the more specific answer
// and must win.
func TestUnfoldableOverlapResolvesToTheMostSpecificReason(t *testing.T) {
	src := loadSrc("", "\tp := &q\n\t*p += \" ORDER BY id\"\n\tlockIt(&q)\n")
	_, body := bodyOf(t, src)
	// Both analyses must genuinely find it, or the order below is untested.
	if aliasUnsafe(body)["q"] == token.NoPos || escapedNames(body)["q"] == token.NoPos {
		t.Fatalf("this fixture no longer overlaps — aliasUnsafe=%v escapedNames=%v",
			aliasUnsafe(body), escapedNames(body))
	}
	if got := unfoldable(body)["q"].reason; got != reasonDerefWrite {
		t.Fatalf("overlapping analyses stamped %+v, want %+v — first match in a "+
			"fixed order, so the disclosed reason does not change between runs",
			got, reasonDerefWrite)
	}
}

// The per-statement classifier's two arms are two different disclosures — one
// says a construct this walk does not fold wrote the name, the other says a
// range clause certainly overwrote it — so they must not collapse to one value.
func TestUnmodelledWritesStampsTheFormThatWrote(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want UntrackReason
	}{
		{"switch-arm", loadSrc("n int",
			"\tswitch n {\n\tcase 1:\n\t\tq = \"x\"\n\t}\n"), reasonUnmodelledWrite},
		{"range-clause-over-an-array", loadSrc("",
			"\tvar arr [2]string\n\tfor _, q = range arr {\n\t}\n"), reasonRangeClause},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sig, body := bodyOf(t, c.src)
			arrays := fixedArrays(sig, body)
			got, ok := unmodelledWrites(body, arrays)["q"]
			if !ok {
				t.Fatalf("this form writes q and the fold does not model it, so it "+
					"must be named: %v", unmodelledWrites(body, arrays))
			}
			if got.reason != c.want {
				t.Fatalf("unmodelledWrites stamped %+v, want %+v", got, c.want)
			}
		})
	}
}

// The table is the single copy only while it is COMPLETE. A classifier that can
// stamp a value UntrackReasons does not list puts a reason in front of an
// operator that no disclosure covers — the drift this whole change is about,
// pointed the other way.
func TestEveryStampedReasonIsInTheTable(t *testing.T) {
	listed := map[UntrackReason]bool{}
	for _, r := range UntrackReasons() {
		listed[r] = true
	}
	if len(listed) != len(UntrackReasons()) {
		t.Fatalf("UntrackReasons has duplicate entries: %+v", UntrackReasons())
	}
	// Every fixture above, in one source per classifier, plus the two forms the
	// per-statement arm distinguishes.
	srcs := []string{
		loadSrc("", "\tp := &q\n\t*p += \" FOR UPDATE\"\n"),
		loadSrc("", "\tadd := func() { q += \" ORDER BY id\" }\n\tadd()\n\tq += \" FOR UPDATE\"\n"),
		loadSrc("", "\torderIt(&q)\n\tq += \" FOR UPDATE\"\n"),
		loadSrc("n int", "\tswitch n {\n\tcase 1:\n\t\tq = \"x\"\n\t}\n"),
		loadSrc("", "\tvar arr [2]string\n\tfor _, q = range arr {\n\t}\n"),
	}
	seen := map[UntrackReason]bool{}
	for _, src := range srcs {
		sig, body := bodyOf(t, src)
		for _, m := range []map[string]unread{
			unfoldable(body), unmodelledWrites(body, fixedArrays(sig, body)),
		} {
			for name, u := range m {
				r := u.reason
				if !listed[r] {
					t.Fatalf("%s was refused with %+v, which UntrackReasons does not "+
						"list — an undisclosed reason reaches an operator as an "+
						"unexplained silence", name, r)
				}
				seen[r] = true
			}
		}
	}
	// And COMPLETE in the other direction: a listed reason no classifier
	// produces is a disclosure of a silence that cannot happen.
	for _, r := range UntrackReasons() {
		if !seen[r] {
			t.Fatalf("UntrackReasons lists %+v but no fixture here produces it — "+
				"either the classifier stopped stamping it or the entry is prose "+
				"with a struct around it", r)
		}
	}
}

// Every field is read by a consumer outside this package: the Key is what they
// switch on, the Issue is what an operator greps, and the Detail is the text the
// census renders inside "could not be read (…)". None may be blank, the Issue
// has to look like one, and the Detail has to be the phrase that fits there —
// a sentence or a bare key would render as a broken line to an operator (#311).
//
// Over UnreadableReasons, not UntrackReasons: a Site can carry any of the
// eight, so all eight reach a reader.
func TestUntrackReasonFieldsAreWellFormed(t *testing.T) {
	issueRE := regexp.MustCompile(`^#[0-9]+$`)
	keys := map[string]bool{}
	all := UnreadableReasons()
	if len(all) < len(UntrackReasons()) {
		t.Fatalf("UnreadableReasons must include every untrack reason: %+v", all)
	}
	for _, r := range all {
		if r.Key == "" {
			t.Fatalf("a reason with no key cannot be disclosed: %+v", r)
		}
		if keys[r.Key] {
			t.Fatalf("duplicate key %q — consumers key on it", r.Key)
		}
		keys[r.Key] = true
		if !issueRE.MatchString(r.Issue) {
			t.Fatalf("reason %q cites %q, which is not an issue reference", r.Key, r.Issue)
		}
		if r.Detail == "" {
			t.Fatalf("reason %q has no operator-facing phrase; the census renders "+
				"one inside \"could not be read (…)\" and an empty one is a refusal "+
				"with no explanation", r.Key)
		}
		if r.Detail == r.Key || strings.HasSuffix(r.Detail, ".") {
			t.Fatalf("reason %q renders as %q, which does not read as the noun "+
				"phrase that slot takes", r.Key, r.Detail)
		}
	}
}

// The untrack half is a strict subset, and it is the half locking.go must
// disclose SILENT. A reason that drifted from one list to the other would move a
// census line between "nothing was analysed" and "part of it was", which are the
// two things #311 is about telling apart.
func TestUntrackReasonsAreTheNonPartialHalf(t *testing.T) {
	untracks := map[string]bool{}
	for _, r := range UntrackReasons() {
		untracks[r.Key] = true
		if r.Partial {
			t.Fatalf("%q untracks the variable, so nothing is emitted for it and "+
				"there is no part that was read", r.Key)
		}
	}
	partials := 0
	for _, r := range UnreadableReasons() {
		if r.Partial {
			partials++
			if untracks[r.Key] {
				t.Fatalf("%q is in both halves", r.Key)
			}
		}
	}
	if partials == 0 {
		t.Fatal("no partial-read reason — the assertion above would be vacuous")
	}
}
