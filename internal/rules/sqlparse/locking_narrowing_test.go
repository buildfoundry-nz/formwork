// locking_narrowing_test.go — #337, the narrowing-direction measurement itself.
//
// locking_decision_test.go ties the DECISION to the run that produces it in both
// directions. This file ties the other half of that record — the sentence that
// says acting on the escape cannot be done quietly — to the tree it describes.
//
// WHY IT NEEDED ITS OWN GATE. That sentence was the one measurement in the
// decision record not read at runtime: a bare count of tests that redden when
// `scanCalls` is patched to treat a bare identifier in argument position as an
// invocation. A count taken in a throwaway snapshot cannot be re-derived by
// anybody reading it, and it rots on the next test either package gains — which
// is exactly what happened. The revision that added internal/sqlextract's three
// carried this package's "eleven" forward unmeasured while the tree produced
// twelve, in the same commit whose subject said the count was measured and not
// inherited. That is the #312/#313 false-claim shape the decision record exists
// to close, reproduced inside the decision record.
//
// SO THE GUARDS ARE NAMED, NOT COUNTED. A name is greppable, survives a test
// being moved between files, and reddens here the moment it is renamed or
// deleted; an integer says nothing a reader can check. The numerals stay beside
// the names, but they are derived from them — this test partitions the names it
// finds by the package that actually defines them and requires each part's size
// and the total to appear in the record, so the 11-against-12 disagreement this
// file was written for cannot be written again.
//
// WHAT THIS CANNOT HOLD, stated plainly rather than implied. That the list is
// EXHAUSTIVE is not machine-checked: proving it takes applying the patch and
// running both packages, which a test cannot do to its own tree. So the record's
// numerals are there to be diffed against a real run, not trusted — and every
// name in the list is real, which is the half that rots on its own.
package sqlparse_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// narRecordFile is the document this test governs. It is a sibling, so the
// package directory the test already runs in is the whole path.
const narRecordFile = "locking_decision_test.go"

// narLeadIn opens the paragraph. The header separates its paragraphs with bare
// `//` lines and opens each with a capitalised lead-in, so this is the record's
// address rather than a quotation of anybody's prose.
const narLeadIn = "THE NARROWING DIRECTION"

// narPackageDirs are the packages the record may name guards from, keyed by the
// name the record has to use for each. internal/sqlextract is where the fold
// lives, and the escape is noticed there and reported here, so a record that
// could only name one of them would be describing half the change.
var narPackageDirs = map[string]string{
	"internal/rules/sqlparse": ".",
	"internal/sqlextract":     "../../sqlextract",
}

var (
	narTestNameRE = regexp.MustCompile(`\bTest[A-Za-z0-9_]+\b`)
	narNumeralRE  = regexp.MustCompile(`\b[0-9]+\b`)
	narIssueRE    = regexp.MustCompile(`#[0-9]+`)

	// narSectionRE matches a paragraph lead-in: a run of capitals long enough
	// that ordinary prose starting with a capitalised word cannot reach it.
	narSectionRE = regexp.MustCompile(`^[A-Z][A-Z0-9 ,'-]{3,}`)
)

// TestTheNarrowingDirectionRecordNamesGuardsThatExist is the gate.
func TestTheNarrowingDirectionRecordNamesGuardsThatExist(t *testing.T) {
	rec := narRecord(t)

	names := narNames(rec)
	if len(names) == 0 {
		t.Fatalf("the %q paragraph of %s states what happens when somebody acts on the "+
			"escape but names no test that would stop them: a bare count cannot be "+
			"re-derived by a reader and rots on the next test either package gains, "+
			"which is how this paragraph came to claim eleven against a tree "+
			"producing twelve. Name the guards.", narLeadIn, narRecordFile)
	}

	defined := narDefinedTests(t)
	byPkg := map[string]int{}
	for _, n := range names {
		pkg, ok := defined[n]
		if !ok {
			t.Errorf("the %q paragraph of %s names %s as a guard that reddens, and no "+
				"test function by that name exists in %s. A record naming a test the "+
				"tree does not have is the false claim this file exists to catch — "+
				"either it was renamed and the record was not, or it never ran.",
				narLeadIn, narRecordFile, n, narPackageList())
			continue
		}
		byPkg[pkg]++
	}
	if t.Failed() {
		return
	}

	numerals := narNumerals(rec)
	for pkg, n := range byPkg {
		if !numerals[n] {
			t.Errorf("the %q paragraph of %s names %d guard(s) defined in %s and does not "+
				"state %d beside them. The numeral is the part a reader diffs against "+
				"their own run of the patch, so it has to be the size of the list "+
				"actually written here, not a number carried forward from an older one.",
				narLeadIn, narRecordFile, n, pkg, n)
		}
	}
	if !numerals[len(names)] {
		t.Errorf("the %q paragraph of %s names %d guards in total and does not state %d. "+
			"The total is what somebody re-running the patch compares their own "+
			"failure count against; a total that disagrees with the list under it "+
			"tells them the record was edited without being measured.",
			narLeadIn, narRecordFile, len(names), len(names))
	}
}

// narRecord returns the record's paragraph, flattened to one line.
//
// Delimited by the header's own shape: the lead-in opens it, and it runs to the
// next paragraph that opens with a lead-in of its own. A bare `//` alone does
// NOT close it, because the record has to be able to carry an indented list of
// guard names and a list is preceded by a blank comment line — a reader that
// stopped at the first `//` would stop exactly where the names begin and report
// the record empty no matter what it said.
func narRecord(t *testing.T) string {
	t.Helper()
	lines := strings.Split(decRead(t, narRecordFile), "\n")

	start := -1
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "//") && strings.Contains(trimmed, narLeadIn) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no %q paragraph: the decision record holds the KEPT status and "+
			"its measurement, and this is the other half of it — that acting on the "+
			"escape reddens named guards rather than passing unnoticed.",
			narRecordFile, narLeadIn)
	}

	var out []string
	for i, l := range lines[start:] {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "//") {
			break
		}
		if trimmed == "//" {
			if narOpensSection(lines[start+i+1:]) {
				break
			}
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
	}
	return strings.Join(out, " ")
}

// narOpensSection reports whether the next comment line in lines begins a new
// paragraph of the header rather than continuing this one.
//
// The header opens every paragraph with a capitalised lead-in, so that is the
// boundary. It is only ever consulted after a blank comment line, which is why a
// record whose own second line happens to start in capitals — `COUNTED.` — is
// not mistaken for the start of the next paragraph.
func narOpensSection(rest []string) bool {
	for _, l := range rest {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "//") {
			return true
		}
		if trimmed == "//" {
			continue
		}
		return narSectionRE.MatchString(strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
	}
	return true
}

// narNames returns the test names the record cites, in order and deduplicated.
func narNames(record string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range narTestNameRE.FindAllString(record, -1) {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// narNumerals returns the numbers the record states.
//
// Issue references go first: `#337` is an address, not a measurement, and a
// record that satisfied its count check with an issue number would be exactly
// the accident this test is here to make impossible.
func narNumerals(record string) map[int]bool {
	out := map[int]bool{}
	for _, s := range narNumeralRE.FindAllString(narIssueRE.ReplaceAllString(record, " "), -1) {
		n, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		out[n] = true
	}
	return out
}

// narDefinedTests maps every test function in the governed packages to the
// package that defines it.
//
// Read out of the trees rather than listed here: a list in this file would be a
// second record to keep in step with the tree, which is the defect being fixed.
func narDefinedTests(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for pkg, dir := range narPackageDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s is a package the narrowing record names guards from: %v", pkg, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s, which may define a guard the record names: %v", path, err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				out[fn.Name.Name] = pkg
			}
		}
	}
	return out
}

// narPackageList renders the governed packages for a failure message, so a
// reader who has just renamed a test is told where it was looked for.
func narPackageList() string {
	var names []string
	for pkg := range narPackageDirs {
		names = append(names, pkg)
	}
	sortStrings(names)
	return strings.Join(names, " or ")
}

// sortStrings orders a small slice in place. The standard library's sort would
// do, but the failure message has to be stable across runs and this keeps the
// reason for that visible at the call site.
func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
