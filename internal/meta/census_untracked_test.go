// census_untracked_test.go — #80.
//
// A file can be present on disk, matched by a rule's scope, and outside version
// control, with every instrument in the toolchain reading clean. Nothing
// reported it: the walk reads the filesystem (correctly — deferring to the index
// would be a fail-open change, since an untracked .go file is still compiled by
// `go build ./...`), and the index was consulted nowhere.
//
// So the information is reported, and the walk is untouched. Detector, never
// authority — the census gains a line, no file gains or loses a scan. That
// direction is the whole design: a skip could be used to hide something, a
// census line cannot.
//
// It also supplies the discriminator #55 named and could not settle: a
// scope.exclude matching zero files is ambiguous between "dead because the tree
// is unscanned" and "dead because the premise rotted", and the index is what
// tells those apart.
package meta_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

const untrackedRule = "rules:\n" +
	"  - id: no-ghost\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {pattern: 'Ghost'}\n" +
	"    cure: \"drop it\"\n"

func untrackedRepo() map[string]string {
	return map[string]string{
		".formwork/formwork.yaml":                 "version: 1\n",
		".formwork/rules/r.yaml":                  untrackedRule,
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"tracked.go": "package p\n",
	}
}

// The signal: an in-scope file nobody committed.
func TestCensusReportsAnInScopeUntrackedFile(t *testing.T) {
	files := untrackedRepo()
	files["stray.go"] = "package p\n" // present, in scope, never added
	_, out := lintTracked(t, files, "tracked.go")

	if !strings.Contains(out, "untracked") {
		t.Fatalf("an in-scope file outside version control must be reported:\n%s", out)
	}
	if !strings.Contains(out, "stray.go") {
		t.Fatalf("the census must name the file, not just count it:\n%s", out)
	}
}

// A reported file is still SCANNED. This is the assertion that keeps the
// feature a detector: if reporting ever became skipping, the rule would stop
// firing on the very file the census just pointed at — turning an
// accountability line into a bypass. Asserted against the walk itself, which is
// the narrowest form of the claim.
func TestUntrackedFileIsStillScanned(t *testing.T) {
	files := untrackedRepo()
	files["stray.go"] = "package p // Ghost\n"
	root := writeRepo(t, files)
	gitTrack(t, root, "tracked.go")

	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range fset.Files {
		if f.Path() == "stray.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("the untracked file must still be in the scanned set; reporting it " +
			"must not exempt it from evaluation")
	}
}

// A tracked in-scope file is not reported. Without this the line fires on every
// file in every repo and means nothing.
func TestCensusDoesNotReportTrackedFiles(t *testing.T) {
	_, out := lintTracked(t, untrackedRepo(), "tracked.go", ".formwork/rules/r.yaml")
	// The substring must match the line the census actually emits. An earlier
	// version of this assertion looked for "tracked.go: untracked", which the
	// real format ("untracked in-scope file: tracked.go") can never contain — so
	// it could not fail, and the mutation that drops the isTracked gate survived
	// it. That is the tautological-test shape, caught by mutation rather than by
	// reading.
	if strings.Contains(out, "untracked in-scope file: tracked.go") {
		t.Fatalf("a tracked file must not be reported as untracked:\n%s", out)
	}
}

// Out-of-scope files are not reported either: a file no rule governs is not a
// coverage gap, and reporting it would bury the signal in build output.
func TestCensusDoesNotReportOutOfScopeUntrackedFiles(t *testing.T) {
	files := untrackedRepo()
	files["notes.txt"] = "no rule includes this\n"
	_, out := lintTracked(t, files, "tracked.go")
	if strings.Contains(out, "notes.txt") {
		t.Fatalf("an untracked file no rule governs is not a coverage gap:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// The degradation, which #80 required to be fail-closed and pinned by a test.
//
// "Reporting '0 untracked' when the index could not be read is a false clean —
// this repo's signature defect. The no-index case must say 'could not
// determine', distinctly from 'none', and a test must pin that distinction."
//
// Before these tests the census answered a git failure with silence, so the
// affirmative "escape hatches: none" was printed at exit 0 over a repository
// nobody had been able to ask — output BYTE-IDENTICAL to a tree that is not a
// repository at all, which is precisely the distinction the criterion asked to
// exist.

// lintBrokenIndex builds a real git repository over files, breaks its index so
// git cannot answer, and lints it.
//
// The index is CORRUPTED rather than chmod'ed to 000: a mode-000 file is still
// readable by uid 0, so on an image whose tests run as root the degradation
// would silently not happen and the test would pass without ever exercising the
// branch it exists for. Bad bytes fail for every uid.
//
// The TrackedUnder precondition below is what keeps that honest — if the index
// were still readable this helper fails loudly instead of handing back an
// output that proves nothing.
func lintBrokenIndex(t *testing.T, files map[string]string, track ...string) string {
	t.Helper()
	root := writeRepo(t, files)
	gitTrack(t, root, track...)
	idx := filepath.Join(root, ".git", "index")
	if _, err := os.Stat(idx); err != nil {
		t.Fatalf("no index to break: %v", err)
	}
	if err := os.WriteFile(idx, []byte("this is not a git index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vcs.TrackedUnder(root); err == nil {
		t.Fatal("the index is still readable: this test cannot observe the degradation it exists for")
	}
	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, devOptOutActive, false); err != nil {
		t.Fatalf("lint: %v\n%s", err, sb.String())
	}
	return sb.String()
}

// A repository whose index git cannot read says so, and does not say "none".
func TestCensusSaysCouldNotDetermineWhenTheIndexCannotBeRead(t *testing.T) {
	files := untrackedRepo()
	files["stray.go"] = "package p\n"
	out := lintBrokenIndex(t, files, "tracked.go")

	if !strings.Contains(out, "untracked in-scope files: could not determine") {
		t.Fatalf("a repository whose index git cannot read must say so in the census, "+
			"in the idiom the same binary already uses for scan.gitignore:\n%s", out)
	}
	if strings.Contains(out, "escape hatches: none") {
		t.Fatalf("\"escape hatches: none\" asserts the census was TAKEN and found nothing; "+
			"it must be unreachable when git could not answer:\n%s", out)
	}
}

// The half of the guard's reasoning that is correct is preserved: in a tree
// that is not a repository the question has no answer, so printing "could not
// determine" there would assert a gap that is not there. Without this control
// the fix is a line on every non-git corpus — `formwork test -C examples/...`
// among them — and the new distinction means nothing.
func TestCensusStaysSilentOutsideARepository(t *testing.T) {
	files := untrackedRepo()
	files["stray.go"] = "package p\n"
	_, out := lint(t, files) // writeRepo with no git init: not a repository

	if strings.Contains(out, "could not determine") {
		t.Fatalf("outside a repository the question has no answer, so there is no gap to "+
			"report; the undetermined line must not fire here:\n%s", out)
	}
	if !strings.Contains(out, "escape hatches: none") {
		t.Fatalf("a clean non-repository tree keeps its \"escape hatches: none\" contract:\n%s", out)
	}
}

// The decisive form of the same claim, and the one that does not depend on any
// wording: the two situations must not READ ALIKE. Before the fix `diff`
// reported no difference between a repository whose index cannot be read and a
// tree with no repository at all — an operator could not tell "I asked and
// there are none" from "I could not ask".
func TestUnreadableIndexDoesNotReadLikeANonRepository(t *testing.T) {
	files := untrackedRepo()
	files["stray.go"] = "package p\n"

	broken := lintBrokenIndex(t, files, "tracked.go")
	_, absent := lint(t, files)

	if broken == absent {
		t.Fatalf("a repository whose index git cannot read renders identically to a tree "+
			"that is not a repository; the census cannot tell \"I could not ask\" from "+
			"\"there are none\":\n%s", broken)
	}
}

// A corpus root BELOW the repository top-level is covered too. This is the
// branch that carries #80's own example: `formwork test -C examples/...` runs
// "with a root that is not a repo root", and TrackedUnder is deliberately built
// for it — `git -C root ls-files` resolves to the nearest ancestor repository
// (#89). A discriminator that looked only at root/.git would call that corpus a
// non-repository, take the silent branch, and reproduce the whole defect in the
// one scenario the issue named by hand.
func TestCensusUndeterminedReachesACorpusBelowTheRepositoryRoot(t *testing.T) {
	files := map[string]string{}
	for rel, content := range untrackedRepo() {
		files["corpus/"+rel] = content
	}
	files["corpus/stray.go"] = "package p\n"

	top := writeRepo(t, files)
	gitTrack(t, top, "corpus/tracked.go") // the repository is the PARENT of the corpus
	if err := os.WriteFile(filepath.Join(top, ".git", "index"), []byte("this is not a git index"), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus := filepath.Join(top, "corpus")
	if _, err := os.Stat(filepath.Join(corpus, ".git")); err == nil {
		t.Fatal("the corpus must not be a repository in its own right, or the ancestor walk is not what is under test")
	}
	if _, err := vcs.TrackedUnder(corpus); err == nil {
		t.Fatal("the index is still readable: this test cannot observe the degradation it exists for")
	}

	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	if _, err := meta.Lint(mustLoad(t, corpus), corpus, &sb, devOptOutActive, false); err != nil {
		t.Fatalf("lint: %v\n%s", err, sb.String())
	}
	out := sb.String()

	if !strings.Contains(out, "untracked in-scope files: could not determine") {
		t.Fatalf("a corpus inside a repository whose index git cannot read must say so, "+
			"even when the repository is an ancestor rather than the corpus root:\n%s", out)
	}
	if strings.Contains(out, "escape hatches: none") {
		t.Fatalf("\"escape hatches: none\" must be unreachable here too:\n%s", out)
	}
}
