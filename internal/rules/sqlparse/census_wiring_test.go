// census_wiring_test.go — #311, the claim that #311 is closed.
//
// This package holds the whole of #311's answer: `UnreadableSites` sources
// through the extractor the locking rules actually use, `CensusSites` owns the
// rule-type → extractor mapping the census used to read off a type-name prefix,
// and `ErrExtractorUnknown` refuses a type neither table names rather than
// handing it somebody else's answer. Every one of those is pinned, and none of
// them is reached by `formwork lint`: `internal/meta`'s `enumerateEscapeHatches`
// still calls `sqlextract.FromGo` itself, so a repo with a
// `sql/locking-select-order` rule still gets "not analysed by this rule" about
// the line `formwork check` failed on in the same run, and still gets an empty
// census for four files that each hide an unordered locking SELECT.
//
// The two documents an adopter reads had stopped saying so. `locking.go`'s
// COVERAGE LIMIT block said the contradiction was what the census did "until
// #311"; the fold spec said the channel "asked `sqlextract.FromGo`" in the past
// tense under a heading declaring that disclosure now means counted. A reader
// holding the reproduction was being told by both documents that it could not
// happen — which is the false-claim shape #311 itself is about, rebuilt by
// #311's own fix one layer up, and the shape #312, #313 and #337's disclosure
// half were each filed for.
//
// So the disclosure is tied to the fact rather than to an author's memory of
// it. This test finds every non-test caller of the seam, and requires the two
// documents to describe the state that scan actually finds: while nothing on
// the lint path calls `CensusSites`, both must tell the reader where the
// unfixed call still lives, and once something does, both must stop.
package sqlparse_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// censusSeam is the function a census must source through. A caller that picks
// an extractor itself has re-derived the mapping #311 is about, so this is the
// name the scan looks for and not `UnreadableSites`.
const censusSeam = "sqlparse.CensusSites"

// censusSeamCallers returns every non-test Go file in THIS MODULE'S TRACKED
// SOURCE, outside this package, that calls the seam.
//
// TRACKED, VIA git ls-files, and not a filesystem walk with a directory
// exclusion list. `make sync` materialises the validating targets under
// projects/, they VENDOR formwork's own internal source, and a walk therefore
// finds CensusSites in a copy of this very package and reports the wiring
// present on a tree where nothing calls it. git ls-files excludes every
// gitignored path by construction rather than by a list that rots — the
// Makefile's `fmt` target records the same trap and takes the same answer, for
// the same reason.
//
// Non-test only, and that is the whole point of the scan: #311 shipped as a
// merged PR with the seam fully pinned by tests in this package and no caller
// at all, so a scan that counted test files would report the wiring present on
// the exact tree where it is missing. examples/ is tracked and still excluded:
// those corpora are fixture material for the gate, never source the binary
// links.
func censusSeamCallers(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	selfAbs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("this package's directory: %v", err)
	}
	selfRel, err := filepath.Rel(root, selfAbs)
	if err != nil {
		t.Fatalf("this package's path within the repository: %v", err)
	}
	self := filepath.ToSlash(selfRel) + "/"

	listed, err := exec.Command("git", "-C", root, "ls-files", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files failed, so the tracked file set is unknown — a scan "+
			"that reported \"no caller\" from a file set it never read would delete "+
			"the #311 disclosure on no evidence: %v", err)
	}
	var out []string
	read := 0
	for _, rel := range strings.Split(string(listed), "\n") {
		rel = strings.TrimSpace(rel)
		if !onTheLintPath(rel, self) {
			continue
		}
		read++
		b, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("reading tracked file %s: %v", rel, rerr)
		}
		if strings.Contains(string(b), censusSeam+"(") {
			out = append(out, rel)
		}
	}
	if read < 50 {
		t.Fatalf("the scan read %d tracked non-test Go file(s); this module has far "+
			"more, so a read that small is finding nothing rather than finding no "+
			"caller — which would report the wiring missing whatever the tree says",
			read)
	}
	return out
}

// onTheLintPath reports whether a tracked path is source the `formwork` binary
// is built from, and therefore a place a call to the seam would put it on the
// lint path.
//
// A SEPARATE PREDICATE BECAUSE TWO OF ITS THREE REFUSALS ARE OTHERWISE
// UNFALSIFIABLE. Inside censusSeamCallers they are exercised only by whatever
// the repository happens to contain: today nothing outside this package names
// CensusSites at all, so deleting the _test.go arm changed no result and the
// arm asserted nothing. Each arm is now driven directly by
// TestOnlyThisModulesLinkedSourceIsScannedForTheSeam.
//
// The _test.go arm is the load-bearing one: #311 shipped as a merged PR with
// the seam fully pinned by tests and no caller, so a scan counting a test file
// reports the wiring present on the exact tree where it is missing. examples/
// is tracked and excluded because those corpora are fixture material for the
// gate rather than source the binary links, and this package is excluded
// because a definition is not a caller.
func onTheLintPath(rel, self string) bool {
	switch {
	case rel == "",
		!strings.HasSuffix(rel, ".go"),
		strings.HasSuffix(rel, "_test.go"),
		strings.HasPrefix(rel, "examples/"),
		strings.HasPrefix(rel, self):
		return false
	}
	return true
}

// Every arm of that predicate, driven by path rather than by whatever the tree
// happens to hold.
func TestOnlyThisModulesLinkedSourceIsScannedForTheSeam(t *testing.T) {
	const self = "internal/rules/sqlparse/"
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"internal/meta/census.go", true},
		{"internal/meta/lint.go", true},
		{"cmd/formwork/main.go", true},
		{"tools/x/main.go", true},
		// A test caller is not the lint path, and this is the arm #311's own
		// history is about.
		{"internal/meta/census_sqlsites_test.go", false},
		// Corpus material, tracked but never linked.
		{"examples/palletra-port-full/x/y.go", false},
		// The definitions themselves.
		{"internal/rules/sqlparse/unreadable.go", false},
		{"internal/rules/sqlparse/census_wiring_test.go", false},
		// git ls-files lists more than Go source.
		{"docs/reference.md", false},
		{"", false},
	} {
		t.Run(tc.rel, func(t *testing.T) {
			if got := onTheLintPath(tc.rel, self); got != tc.want {
				t.Fatalf("onTheLintPath(%q) = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}

// censusGapTokens are what a paragraph disclosing the open wiring has to name:
// the package holding the unfixed call, the extractor it still calls, and a
// present-tense word, so that a paragraph recounting the history of the defect
// after it is fixed does not read as a disclosure of it.
var censusGapTokens = []string{"internal/meta", "FromGo", "still"}

// The disclosure and the tree, checked against each other.
func TestTheLintCensusClaimMatchesWhatCallsTheSeam(t *testing.T) {
	callers := censusSeamCallers(t)
	spec, err := os.ReadFile(foldSpecPath)
	if err != nil {
		t.Fatalf("the fold design spec is the document fold.go points readers at: %v", err)
	}
	docs := []struct{ name, text, sep string }{
		{"locking.go's COVERAGE LIMIT block", coverageBlock(t), "\n//\n"},
		{"the fold design spec's §9", string(spec), "\n\n"},
	}
	for _, doc := range docs {
		discloses := false
		for _, para := range strings.Split(doc.text, doc.sep) {
			flat := covFlat(para)
			all := true
			for _, tok := range censusGapTokens {
				if !strings.Contains(flat, tok) {
					all = false
					break
				}
			}
			if all {
				discloses = true
				break
			}
		}
		switch {
		case len(callers) == 0 && !discloses:
			t.Errorf("nothing outside this package calls %s, so `formwork lint` "+
				"still asks sqlextract.FromGo and still prints \"not analysed by this "+
				"rule\" about the line `formwork check` just failed on — and %s says "+
				"nowhere that it does. A paragraph naming %v is what tells a reader "+
				"holding that reproduction it is real (#311).",
				censusSeam, doc.name, censusGapTokens)
		case len(callers) > 0 && discloses:
			t.Errorf("%v now call %s, so the lint census sources through this "+
				"package's mapping — and %s still tells a reader the contradiction "+
				"reproduces. A disclosure of a gap that has been closed misdirects "+
				"triage exactly as the missing one did.",
				callers, censusSeam, doc.name)
		}
	}
}

// A CLONE UNDER projects/ IS NOT THE LINT PATH. `make sync` materialises the
// validating targets there, and they VENDOR formwork's own internal source — so
// a plain filesystem walk finds `CensusSites` in a copy of this very package
// and reports the wiring present on a tree where nothing calls it. That is the
// one direction of this scan that fails toward "already fixed", which is the
// #311 shape again: a channel answering about something it never read.
//
// The Makefile's `fmt` target records the same trap and the same answer —
// "`gofmt -l .` is a plain filesystem walk with no notion of modules or
// .gitignore, so it descends into projects/" — and keys off `git ls-files`,
// "which excludes every gitignored path by construction, not by an exclusion
// list that would rot".
func TestTheSeamScanReadsTrackedSourceOnly(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	base := filepath.Join(root, "projects", "zz-seam-scan-probe")
	dir := filepath.Join(base, "internal", "meta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("planting a clone: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	planted := filepath.Join(dir, "census.go")
	src := "package meta\n\nfunc probe() { _, _, _ = " + censusSeam + "(\"\", \"\", nil) }\n"
	if err := os.WriteFile(planted, []byte(src), 0o600); err != nil {
		t.Fatalf("planting a clone: %v", err)
	}

	// The premise. If projects/ stopped being gitignored the plant would BE
	// tracked source, and the assertion below would be asserting the opposite of
	// what it means.
	if err := exec.Command("git", "-C", root, "check-ignore", "-q", planted).Run(); err != nil {
		t.Fatalf("projects/ is no longer gitignored, so a clone under it is part "+
			"of this module's tracked source and this test's premise is gone: %v", err)
	}

	// The scan may legitimately report the REAL caller (internal/meta/census.go
	// wires the seam as of #311). What it must never report is the plant: a
	// vendored copy inside a cloned validating target is not the lint path, and
	// counting it would say #311 is wired on a tree where nothing calls it.
	rel, err := filepath.Rel(root, planted)
	if err != nil {
		t.Fatalf("relative plant path: %v", err)
	}
	for _, got := range censusSeamCallers(t) {
		if filepath.ToSlash(got) == filepath.ToSlash(rel) || strings.Contains(filepath.ToSlash(got), "projects/") {
			t.Fatalf("the scan reported %q — a vendored copy of this package inside a "+
				"cloned validating target is not the lint path, and counting it says "+
				"#311 is wired on a tree where nothing calls the seam", got)
		}
	}
}
