package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #308: #98's fold landed inside the SCAN and stopped there. `FileSet.Restrict`
// folds git's NFC spelling onto the NFD directory entry the walk carried, and
// `FileSet.Produced` (internal/scan/restrict.go) asks that same fold as a
// question — but internal/cli's file-set accounting still rebuilt its own answer
// by exact string equality, so a file the scan really did read was reported as
// one the scan "never produced".
//
// Measured on macOS/APFS with the real binary at 90e01d7c, a clean NFD-named
// file with no violation of any kind in it:
//
//	$ formwork check --staged; echo exit=$?
//	formwork: --staged named 1 path(s) the scan never produced:
//	  naïve.ts — present in the working tree but not produced by the scan under this spelling
//	formwork: rules are evaluated over the working tree, so these paths were
//	checked by nothing and this run cannot speak for them
//	exit=2
//
// Both sentences are false, no cure line is printed for that reason at all
// (fileset.go withholds "restore the file" precisely because the file IS on
// disk), and a real `git commit` through formwork's own installed pre-commit
// hook is refused. The same refusal fires on the --range arm.
//
// EVERY TEST HERE DRIVES runCLI, NOT THE ACCOUNTING FUNCTIONS. The bug is the
// seam between two layers that each work on their own: internal/scan's fold is
// pinned by internal/scan/produced_test.go and internal/cli's classifier is
// pinned by fileset_internal_test.go, which hand-feeds it a scan that omits a
// file. Neither can see a spelling that diverges between them, which is why
// nothing caught this and why these rows run the whole command.

// The two byte sequences for one visible filename, written as \u escapes rather
// than as literal characters: a Go source file is saved NFC by every editor
// involved, so a literal would make both sides agree by construction — which is
// exactly how #97's tests missed this. Deliberately a second copy of the pair in
// internal/scan/restrict_test.go: those live in package scan_test and are not
// reachable from here, and one shared spelling is not worth exporting test
// fixtures from a production package.
const (
	cliNFCName = "na\u00efve.ts"  // precomposed U+00EF: what git reports on macOS
	cliNFDName = "nai\u0308ve.ts" // 'i' + combining U+0308: what readdir returns
)

// normalizationInsensitiveFS reports whether this filesystem resolves the NFC
// spelling of a file created with NFD bytes — i.e. whether #98's divergence can
// arise here at all. macOS/APFS: yes. Linux/ext4: no, and there the divergence
// does not arise either, because git reports the bytes readdir gave it.
//
// Probed rather than keyed on GOOS: the property belongs to the filesystem, not
// to the operating system. Mirrors the probe of the same name in package
// scan_test, which this package cannot see.
func normalizationInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "probe-"+cliNFDName)
	mustWrite(t, probe, "probe\n")
	_, err := os.Stat(filepath.Join(dir, "probe-"+cliNFCName))
	if rmErr := os.Remove(probe); rmErr != nil {
		t.Fatal(rmErr)
	}
	return err == nil
}

// nfdRepo builds the repository #308 was filed against: one rule scoping
// **/*.ts, and one source file whose DIRECTORY ENTRY carries the NFD bytes while
// git's index carries the NFC ones. Nothing else is staged that a rule can see,
// so the counts the summary reports are unambiguous — one path requested, one
// file scanned.
func nfdRepo(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if !normalizationInsensitiveFS(t, root) {
		t.Skip("filesystem is normalization-sensitive: git and the walk report the same bytes, so #98's divergence cannot arise here")
	}
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	// The fixture token is WIDGET rather than an annotation marker: this file
	// lives under internal/, where this repo's own marker-comment gate reads it.
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.ts']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, cliNFDName), content)
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	return root
}

// assertGitSpelledItNFC fails the test if this git did NOT report the precomposed
// spelling — the precondition the whole file rests on (core.precomposeunicode,
// default true on macOS). Without it a green row here would mean "the two
// spellings happened to agree", which is not the thing under test.
func assertGitSpelledItNFC(t *testing.T, root string) {
	t.Helper()
	// -z, so git emits the raw bytes: without it core.quotepath renders every
	// non-ASCII byte as a C escape and the comparison below reads a quoted
	// spelling rather than the one the index carries.
	named := gitOut(t, root, "diff", "--cached", "--name-only", "-z")
	if !strings.Contains(named, cliNFCName) {
		t.Fatalf("git did not report the precomposed spelling %+q, so this fixture is not #98's shape; git named:\n%+q", cliNFCName, named)
	}
}

// nfdRangeRepo is nfdRepo one commit later: the .formwork corpus is committed
// first and the NFD-named source file arrives in the SECOND commit, so
// HEAD~1..HEAD names exactly that one path.
func nfdRangeRepo(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if !normalizationInsensitiveFS(t, root) {
		t.Skip("filesystem is normalization-sensitive: git and the walk report the same bytes, so #98's divergence cannot arise here")
	}
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.ts']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "corpus")
	mustWrite(t, filepath.Join(root, cliNFDName), content)
	gitRun(t, root, "add", "-A")
	assertGitSpelledItNFC(t, root)
	gitRun(t, root, "commit", "-qm", "add the nfd-named source file")
	return root
}

// ---------------------------------------------------------------------------
// The accounting must answer for the file. Both arms, both contents.
// ---------------------------------------------------------------------------

// A clean file is a clean run. This is the row that blocks real work today: the
// operator has committed nothing wrong, and the engine's own installed
// pre-commit hook refuses the commit at exit 2 with no next step.
func TestCheckStagedCleanNFDNamedFileIsScannedNotRefused(t *testing.T) {
	root := nfdRepo(t, "const clean = 1;\n")
	assertGitSpelledItNFC(t, root)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the scan produced that file under the spelling readdir gave it\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// The count is the disclosure that makes the exit code mean something: a
	// run that scanned nothing would also be exit 0.
	if !strings.Contains(out, "scan: 1 path(s) requested by --staged, 1 file(s) scanned") {
		t.Errorf("the summary must say the requested path was scanned:\n%s", out)
	}
	if strings.Contains(errOut, "never produced") {
		t.Errorf("the scan DID produce that file; refusing to speak for it is false:\n%s", errOut)
	}
}

// #98's acceptance criterion, verbatim: "an NFD-named file is scanned and
// enforced on under --staged/--range". Exit 1, and the finding names the file.
//
// This is the half a fix cannot fake by loosening the accounting alone — the
// rule has to have READ the file for a finding to exist at all.
func TestCheckStagedViolationInAnNFDNamedFileIsReported(t *testing.T) {
	root := nfdRepo(t, "const w = WIDGET;\n")
	assertGitSpelledItNFC(t, root)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the violation in the NFD-named file must be reported, not refused\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// The report names the file under the spelling the WALK carried, which is
	// the NFD one: that is the entry on disk, and it is what the operator's
	// editor and `ls` show. Asserted as bytes rather than as glyphs so a report
	// that silently substituted git's spelling would be caught.
	if !strings.Contains(out, cliNFDName+":1: forbidden pattern matched: WIDGET") {
		t.Errorf("the finding must name %+q at its line:\n%s", cliNFDName, out)
	}
}

// The --range arm carries the same accounting and had the same defect. It is
// the CI-side spelling of the same run.
func TestCheckRangeCleanNFDNamedFileIsScannedNotRefused(t *testing.T) {
	root := nfdRangeRepo(t, "const clean = 1;\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "scan: 1 path(s) requested by --range HEAD~1..HEAD, 1 file(s) scanned") {
		t.Errorf("the summary must say the requested path was scanned:\n%s", out)
	}
	if strings.Contains(errOut, "never produced") {
		t.Errorf("the scan DID produce that file; refusing to speak for it is false:\n%s", errOut)
	}
}

func TestCheckRangeViolationInAnNFDNamedFileIsReported(t *testing.T) {
	root := nfdRangeRepo(t, "const w = WIDGET;\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the violation in the NFD-named file must be reported, not refused\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, cliNFDName+":1: forbidden pattern matched: WIDGET") {
		t.Errorf("the finding must name %+q at its line:\n%s", cliNFDName, out)
	}
}

// ---------------------------------------------------------------------------
// The control. Widening the accounting must not reopen #158.
// ---------------------------------------------------------------------------

// #158's fail-closed refusal, asked of a NON-ASCII path so the fold's own gate
// is not what is doing the work. A path git named and the worktree no longer has
// was read by nothing, and answering for it because some other file shares a
// spelling would turn the loud refusal back into the silent exit 0 #158 closed.
//
// This row is GREEN before the fix as well as after. It is here because it is
// the assertion a fix that simply answered "produced" for everything would fail,
// and nothing else in this package asks it under a divergent spelling.
func TestCheckStagedNFDNamedFileDeletedFromTheWorktreeIsStillRefused(t *testing.T) {
	root := nfdRepo(t, "const clean = 1;\n")
	assertGitSpelledItNFC(t, root)
	mustRemove(t, filepath.Join(root, cliNFDName))

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the staged content commits unchecked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "named by git but not present in the working tree") {
		t.Errorf("stderr must give the arrival reason:\n%s", errOut)
	}
	if !strings.Contains(errOut, "would commit unchecked") {
		t.Errorf("stderr must still carry the --staged cure:\n%s", errOut)
	}
}

// ---------------------------------------------------------------------------
// A healthy run asks git nothing. The second call site, and its own proof.
// ---------------------------------------------------------------------------

// refuseUnaccountedPaths builds its own `candidates` list — the requested paths
// the scan did not produce — and asks git for their modes ONLY when that list is
// non-empty. Its doc comment states the resulting invariant outright: "in the
// ordinary case every requested path was scanned, candidates is empty, and
// neither mode makes a git call at all — which is why this costs a healthy run
// nothing."
//
// That list was built by the same exact string match, so an NFD-named file made
// itself a candidate even once the classifier beside it had been taught to fold.
// The consequence is not visible in ordinary output — git answers the question
// fine — so it is proved where the invariant is load-bearing: on a git that
// CANNOT answer, a phantom candidate turns a clean run into exit 2 with an error
// naming a path count that should have been zero.

// gitRefusingScopedStagedModes puts a git on PATH that answers every question
// the real git does EXCEPT the scoped staged-mode question — `ls-files --stage
// -- <paths>`, the argv only vcs.StagedModes builds. The two other `--stage`
// callers in internal/vcs (TrackedUnder, Submodules) pass no `--`, so the
// conjunction is what makes this shim exclusive to the call under test rather
// than a blanket break of git.
//
// A pass-through shim rather than a stub, and installed after the fixture is
// built, so the run under test is a real run in every other respect. The same
// device is used by internal/vcs/hatch_test.go's gitRefusingPathFormat.
func gitRefusingScopedStagedModes(t *testing.T) {
	t.Helper()
	installGitShim(t, "stage=0; dashdash=0\n"+
		"for a in \"$@\"; do\n"+
		"  case \"$a\" in --stage) stage=1;; --) dashdash=1;; esac\n"+
		"done\n"+
		"if [ \"$stage\" = 1 ] && [ \"$dashdash\" = 1 ]; then\n"+
		"  echo \"shim: refusing the scoped staged-mode question\" >&2\n"+
		"  exit 129\n"+
		"fi\n")
}

// gitRefusingRawDiff is the --range arm's equivalent: `diff --raw` is the argv
// only vcs.RangeModes builds, and the range arm asks it for the same reason —
// because the candidate list was not empty.
func gitRefusingRawDiff(t *testing.T) {
	t.Helper()
	installGitShim(t, "for a in \"$@\"; do\n"+
		"  case \"$a\" in --raw) echo \"shim: refusing the end-of-range mode question\" >&2; exit 129;; esac\n"+
		"done\n")
}

// installGitShim writes a pass-through git wrapper whose refusal clause is
// `guard` and puts it first on PATH.
func installGitShim(t *testing.T, guard string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	shim := "#!/bin/sh\n" + guard + "exec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The shim is a fixture, so its own reach is asserted before it is relied on:
// it must break the question under test and leave the rest of the run intact.
// Without this a green row above could mean "the shim never fired".
func TestGitShimRefusesOnlyTheScopedStagedModeQuestion(t *testing.T) {
	root := nfdRepo(t, "const clean = 1;\n")
	gitRefusingScopedStagedModes(t)

	if got := gitOut(t, root, "ls-files", "-z", "--stage"); got == "" {
		t.Fatalf("the shim broke the unscoped index question, which this run also asks")
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--stage", "--", cliNFCName).Output()
	if err == nil {
		t.Fatalf("the shim did not refuse the scoped staged-mode question; it answered %q", out)
	}
}

// #158's accounting asks git for a mode only about paths it could not account
// for. A clean NFD-named file is accounted for, so this run must complete
// without that question being asked at all — on a git that would refuse it.
func TestCheckStagedCleanNFDNamedFileAsksGitForNoStagedModes(t *testing.T) {
	root := nfdRepo(t, "const clean = 1;\n")
	assertGitSpelledItNFC(t, root)
	gitRefusingScopedStagedModes(t)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — nothing was unaccounted for, so the staged-mode question must never have been asked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "could not read the staged mode") {
		t.Errorf("a file the scan produced was still counted as a candidate:\n%s", errOut)
	}
}

// The same invariant on the --range arm, where the question git is asked is
// `diff --raw` rather than a scoped `ls-files`.
func TestCheckRangeCleanNFDNamedFileAsksGitForNoEndOfRangeModes(t *testing.T) {
	root := nfdRangeRepo(t, "const clean = 1;\n")
	gitRefusingRawDiff(t)

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — nothing was unaccounted for, so the end-of-range mode question must never have been asked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "could not read the end-of-range mode") {
		t.Errorf("a file the scan produced was still counted as a candidate:\n%s", errOut)
	}
}
