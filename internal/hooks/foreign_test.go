package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/hooks"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Hook wiring that is not formwork's: the worktrees git will run hooks in, and
// the hooks core.hooksPath is currently shadowing. Split from verify_test.go to
// stay clear of the repo's own 750-line cap (file-size-vendor-cap), whose cure
// is "split the file".

// --- row 5: a linked worktree ------------------------------------------------

// core.hooksPath is relative, so `rev-parse --git-path hooks` returns the
// IDENTICAL string in every worktree while resolving to a different directory in
// each. A linked worktree checked out on a branch without the shims therefore
// commits ungated while the main worktree is perfectly wired — and a dedupe
// keyed on git's raw answer collapses them all and never looks.
func TestVerifyChecksLinkedWorktreeWithoutShims(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	// Commit BEFORE install: once core.hooksPath is set, the shim execs a
	// formwork that is not on this test's PATH and every commit exits 127.
	commitEverything(t, dir) // the shims stay untracked, so the branch below lacks them
	mustInstall(t, dir, cfg)
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified the repository while a linked worktree commits ungated")
	}
	wantProblem(t, probs, wt)
}

// R9 at VERIFY's altitude. `worktree list --porcelain` writes the path raw, so
// a worktree at a path containing a newline spills across lines and a line
// parser reports a truncated, possibly real, directory. internal/vcs pins the
// parser; this pins what verify does with the answer, which is where the
// consequence lands — dropping -z left the whole of this package green while
// verify named a directory nobody has.
//
// The assertion is the newline-bearing basename rather than the whole path: git
// reports worktree paths as the kernel resolves them, which is not always the
// spelling the test handed it.
func TestVerifyNamesAWorktreeAtAPathContainingANewline(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir) // the shims stay untracked, so the branch below lacks them
	mustInstall(t, dir, cfg)
	wt := filepath.Join(t.TempDir(), "nl\nwt")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified the repository while a linked worktree commits ungated")
	}
	wantProblem(t, probs, "nl\nwt")
}

// git LISTS a worktree whose directory is gone, at exit 0. Reporting it as
// "unwired" is a permanent exit 1 nobody can clear, which is how a verify
// command gets ignored; skipping it silently is the fail-open, because the same
// skip hides a worktree that exists without shims. It gets its own line, with
// the cure.
func TestVerifyReportsAPrunableWorktreeWithItsCure(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir)
	mustInstall(t, dir, cfg)
	wt := filepath.Join(t.TempDir(), "gone")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, wt)
	wantProblem(t, probs, "git worktree prune")
}

// A prunable worktree is not necessarily a worktree that is gone. `prunable` is
// git's verdict on the REGISTRATION: delete only the worktree's `.git` file and
// git reports the entry prunable — measured, reason "gitdir file points to
// non-existent location" — while the directory and every file in it are right
// where they were. The old order tested the flag before the directory, so this
// state was reported as "its directory is gone", which the operator can see is
// false, alongside advice that deregisters the worktree.
//
// It also pins the exit code: git cannot answer for that directory any more, and
// turning that into an error would make one broken registration exit 2 for the
// whole repository. mustVerify fails the test on an error.
func TestVerifyReportsAPrunableWorktreeWhoseDirectoryIsStillThere(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir)
	mustInstall(t, dir, cfg)
	wt := filepath.Join(t.TempDir(), "orphaned")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	if err := os.Remove(filepath.Join(wt, ".git")); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, wt)
	// git's own reason, read from git rather than hard-coded: the wording is a
	// property of the installed git, and inventing one is what this fixes.
	wantProblem(t, probs, prunableReason(t, dir))
	for _, p := range probs {
		if strings.Contains(p, "its directory is gone") {
			t.Errorf("the reason is false — the directory is still there: %q", p)
		}
	}
}

// A worktree that was MOVED is listed at its OLD path and reported prunable,
// while the worktree itself is alive at the new one — measured: `git commit`
// there succeeds and git resolves its hooks normally. `git worktree prune` is
// the wrong cure for that state; it deregisters a working worktree. The line has
// to offer the repair as well, because verify cannot tell the two apart from the
// old path alone.
func TestVerifyOffersRepairForAWorktreeThatMayHaveMoved(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir)
	mustInstall(t, dir, cfg)
	parent := t.TempDir()
	wt := filepath.Join(parent, "here")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	if err := os.Rename(wt, filepath.Join(parent, "there")); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, wt) // the path git names, which is the old one
	wantProblem(t, probs, "git worktree repair")
}

// prunableReason returns the reason git gives for the one prunable worktree of
// the repository at dir.
func prunableReason(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if r, ok := strings.CutPrefix(line, "prunable "); ok && r != "" {
			return r
		}
	}
	t.Fatalf("no prunable worktree with a reason in:\n%s", out)
	return ""
}

// --- requirement 6 / row 7: hooks the managed directory shadows ---------------

// core.hooksPath overrides the WHOLE default hooks directory, so pointing it at
// formwork's silently disables every hook the operator already had — including
// hook names formwork does not model. Install no longer creates that state, and
// nothing detected it where it already existed.
//
// THE OPERATOR'S HOOK ARRIVES AFTER THE INSTALL, and the order is now the only
// one that constructs this state: install refuses over a hook git is already
// running (#146 D2), so writing commit-msg first makes the install fail instead
// of shadowing it. Later is not a contrivance — a repository picks up a
// commit-msg hook whenever someone adds one, and the point of this test is that
// nothing tells them it stopped running.
func TestVerifyReportsHooksShadowedByTheManagedDir(t *testing.T) {
	dir := repo(t)
	theirs := filepath.Join(dir, ".git", "hooks")
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	writeShimFile(t, filepath.Join(theirs, "commit-msg"), "#!/bin/sh\necho theirs\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified wiring that silently disabled an operator's hook")
	}
	// The hook that stopped running, and the directory it is in: a report that
	// named only one of them would leave the operator hunting.
	wantProblem(t, probs, "commit-msg")
	wantProblem(t, probs, theirs)
}

// The three axes of "is this a real hook file", each pinned by a file that has
// two of them. Without the NAME axis a healthy repository reports the 14
// executable `*.sample` files git ships as shadowed hooks — the `.sample`
// suffix is not what excludes `lib.sh`, and `init.templateDir` replaces the
// samples anyway, so suffix-matching does not characterise "fresh".
func TestVerifyDoesNotReportInertFilesAsShadowedHooks(t *testing.T) {
	dir := repo(t)
	hd := filepath.Join(dir, ".git", "hooks")
	if n := len(sampleHookNames(t, hd)); n == 0 {
		t.Fatal("this git init shipped no *.sample hooks, so the name axis is unexercised here")
	}
	writeShimFile(t, filepath.Join(hd, "pre-push"), "#!/bin/sh\necho theirs\n", 0o644)    // not executable
	writeShimFile(t, filepath.Join(hd, "lib.sh"), "#!/bin/sh\necho helper\n", 0o755)      // name git never runs
	writeShimFile(t, filepath.Join(hd, "pre-commit.bak"), "#!/bin/sh\necho old\n", 0o755) // ditto
	if err := os.MkdirAll(filepath.Join(hd, "pre-merge-commit"), 0o755); err != nil {     // not a file
		t.Fatal(err)
	}
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)

	wantWired(t, mustVerify(t, dir, cfg))
}

// The executable axis has to be git's own test, not the mode bits. A hook at
// 0655 has two execute bits set and the owner — the person whose commit it would
// gate — cannot run it, so git prints a hint and runs nothing. Reporting it as
// protection core.hooksPath is switching off is a finding about a file that was
// never running, and the same expression on the other side of #146 certifies a
// formwork shim nobody can execute.
func TestVerifyDoesNotReportAHookTheOwnerCannotExecuteAsShadowed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root passes access(X_OK) on any execute bit, so the state cannot be constructed")
	}
	dir := repo(t)
	writeShimFile(t, filepath.Join(dir, ".git", "hooks", "commit-msg"), "#!/bin/sh\necho theirs\n", 0o655)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)

	wantWired(t, mustVerify(t, dir, cfg))
}

// A failure to ASK is not a yes. Every errno that is not EACCES means formwork
// could not perform the executable test, and reading that as "executable" is the
// fail-open one level below the mode-bit one: verify would certify a shim it
// never tested, and the shadowed-hooks report would count a file it could not
// examine as live protection.
//
// The errno is injected because no chmod produces one: EACCES is the only answer
// a test can construct, and it is the arm that already works.
func TestVerifyDoesNotCertifyAShimItCouldNotTestForExecutability(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg)) // healthy while the kernel answers

	restore := hooks.SetAccessForTest(func(string, uint32) error { return syscall.EIO })
	defer restore()

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim whose executability it could not test")
	}
	wantProblem(t, probs, "cannot tell whether")
}

// gitClientHookNames is a version-dependent fact about git, not a constant, and
// the failure direction of a missing name is that formwork treats a real hook
// as inert. Every name git ships as a sample must be in it — the cheapest
// available check that the extraction from githooks(5) dropped nothing.
func TestGitClientHookNamesCoversEverySampleGitShips(t *testing.T) {
	dir := repo(t)
	names := sampleHookNames(t, filepath.Join(dir, ".git", "hooks"))
	if len(names) == 0 {
		t.Fatal("no *.sample hooks to cross-check against")
	}
	set := hooks.GitClientHookNamesForTest()
	for _, n := range names {
		if !set[n] {
			t.Errorf("git ships %s.sample but gitClientHookNames does not list %q", n, n)
		}
	}
}

// --- D2's detector: hook wiring install must not take over --------------------

// mustPreexisting runs the detector the way install's pre-flight will: against
// the directory git says it will run hooks from.
//
// It fails the test on an error, so an assertion about the hooks found can never
// read "formwork could not find out" as "there is nothing there".
func mustPreexisting(t *testing.T, root string) (string, []string) {
	t.Helper()
	hp, err := vcs.HooksPath(root)
	if err != nil {
		t.Fatalf("HooksPath(%s): %v", root, err)
	}
	dir, names, err := hooks.PreexistingHooksForTest(root, hp)
	if err != nil {
		t.Fatalf("preexisting hooks: %v", err)
	}
	return dir, names
}

// wantSameDir fails unless two paths name the same directory. git answers some
// questions with a path the kernel has resolved, so the spelling a test handed
// it is not always the spelling it gets back — on macOS a temporary directory
// under /var arrives as /private/var.
func wantSameDir(t *testing.T, got, want string) {
	t.Helper()
	rg, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("resolve %s: %v", got, err)
	}
	rw, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve %s: %v", want, err)
	}
	if rg != rw {
		t.Errorf("detector looked in %s, want %s", got, want)
	}
}

// The state D2 refuses over: an operator's own hook, in the directory git is
// running hooks from right now. Installing there sets core.hooksPath, which
// overrides that whole directory — so this file stops running with nothing said.
func TestPreexistingHooksFindsAHookGitIsRunning(t *testing.T) {
	dir := repo(t)
	theirs := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(theirs, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)

	found, names := mustPreexisting(t, dir)
	if !slices.Contains(names, "pre-commit") {
		t.Fatalf("detector found %v, want the operator's pre-commit", names)
	}
	// The directory as well as the names: a refusal that cannot say WHERE the
	// hook is leaves the operator hunting for it.
	wantSameDir(t, found, theirs)
}

// R3, and it is the specific regression this detector must not have. Once
// core.hooksPath points somewhere else, git does not run these files at all —
// they are inert, not protection. Reading them as a reason to refuse means a
// repository whose shim was deleted cannot have it put back, which leaves NO
// gate running: R1's mistake arriving by another route.
func TestPreexistingHooksIgnoresHooksGitNoLongerRuns(t *testing.T) {
	dir := repo(t)
	writeShimFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	// Written with git rather than by installing, so the fixture states what it
	// means — a repository already wired elsewhere — and does not depend on
	// install's own behaviour in the state install is about to start refusing.
	gitT(t, dir, "config", "core.hooksPath", ".formwork/hooks")

	if _, names := mustPreexisting(t, dir); len(names) != 0 {
		t.Fatalf("detector reported %v as wiring to refuse over, but git runs hooks from .formwork/hooks — those files are inert", names)
	}
}

// R5. In a linked worktree the per-worktree git directory has no hooks of its
// own; the ones git will run live under the directory every worktree SHARES.
// A detector reading the per-worktree answer concludes the repository has no
// pre-existing hooks, installs, and silently overrides the operator's real
// pre-commit — wrong in the dangerous direction.
func TestPreexistingHooksFindsTheSharedDirsHooksFromALinkedWorktree(t *testing.T) {
	dir := repo(t)
	commitEverything(t, dir)
	theirs := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(theirs, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")

	found, names := mustPreexisting(t, wt)
	if !slices.Contains(names, "pre-commit") {
		t.Fatalf("from the linked worktree the detector found %v, want the pre-commit under the shared git directory", names)
	}
	wantSameDir(t, found, theirs)
}

// "No hooks here" and "formwork could not find out" are the two answers a
// refusal must never confuse: only the first says it is safe to write. A
// directory formwork cannot list is the second, and folding it into an empty
// list is this repo's signature defect — install would take over wiring it
// never saw.
func TestPreexistingHooksIsAnErrorWhenTheDirectoryCannotBeListed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lists a directory with no permissions, so the state cannot be constructed")
	}
	dir := repo(t)
	hd := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(hd, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	if err := os.Chmod(hd, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hd, 0o755) }) // t.TempDir cannot remove it otherwise

	hp, err := vcs.HooksPath(dir)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	_, names, err := hooks.PreexistingHooksForTest(dir, hp)
	if err == nil {
		t.Fatalf("a hooks directory formwork cannot list reported %v and no error", names)
	}
}

// The one listing failure that is an honest empty answer: nothing can be running
// from a directory that is not there. Reading it as an error would refuse to
// install in a repository whose .git/hooks was simply deleted — a state with no
// hook in it at all.
func TestPreexistingHooksTreatsAMissingHooksDirectoryAsNoHooks(t *testing.T) {
	dir := repo(t)
	if err := os.RemoveAll(filepath.Join(dir, ".git", "hooks")); err != nil {
		t.Fatal(err)
	}

	if _, names := mustPreexisting(t, dir); len(names) != 0 {
		t.Fatalf("detector reported %v in a directory that does not exist", names)
	}
}

// ...and the arm that reads like that honest answer and is not. The directory is
// there; a single FILE in it could not be examined, and the ErrNotExist that
// comes back is realHookFile's — os.Lstat on a name ReadDir listed a moment ago,
// or the access(2) behind the executable axis. Folded into "no hooks", ONE
// vanishing file empties the answer for the WHOLE directory, and the refusal
// built on it finds nothing to refuse over: install proceeds and every hook the
// operator had stops running.
//
// Race-only in practice, which is why the errno is injected — but the two states
// are still the two a refusal must not confuse, and only the directory's own
// absence is the safe one.
func TestPreexistingHooksIsAnErrorWhenAHookFileCannotBeStatted(t *testing.T) {
	dir := repo(t)
	writeShimFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	defer hooks.SetAccessForTest(func(string, uint32) error { return syscall.ENOENT })()

	hp, err := vcs.HooksPath(dir)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	_, names, err := hooks.PreexistingHooksForTest(dir, hp)
	if err == nil {
		t.Fatalf("a hook file formwork could not examine reported %v and no error; the directory it is in exists, so this is not the empty answer", names)
	}
}

func sampleHookNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if n, ok := strings.CutSuffix(e.Name(), ".sample"); ok {
			out = append(out, n)
		}
	}
	return out
}

// --- #171: an unresolvable comparison is not "those files are inert" ---------

// sameDirPath answers "do these two paths name the same directory", and its two
// callers read the answer with OPPOSITE polarity: verify's shadowed-hooks report
// treats "not the same" as a reason to report, install's detector treats it as a
// reason NOT to refuse. So the single failure direction it used to have — false —
// was a spurious report on one side and, on the other, install taking over hook
// wiring it never looked at.
//
// THE FIXTURE IS THE ONE ROUTE THE OTHER GATES DO NOT ALREADY COVER, which #171
// names: --override-global. The comparison has to be unsettleable lexically for
// EvalSymlinks to be reached at all, so git's hooks directory is spelled here
// through a symlink, which a wider-scope core.hooksPath names — measured on git
// 2.50.1, `rev-parse --git-path hooks` then answers `hookslink` while
// `--git-common-dir` answers `.git`. Both name one directory; only a resolution
// says so.
//
// The control runs FIRST and asserts the fixture still reproduces: without the
// forced failure install refuses over the operator's live pre-commit, so a
// refusal below cannot come from the fixture having gone stale.
func TestInstallRefusesWhenItCannotTellWhetherGitRunsTheHooksItFound(t *testing.T) {
	dir := repo(t)
	theirs := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(theirs, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	if err := os.Symlink(filepath.Join(".git", "hooks"), filepath.Join(dir, "hookslink")); err != nil {
		t.Fatal(err)
	}
	globalHooksPath(t, "hookslink")
	cfg := laneCfg("pre-commit")

	if _, err := hooks.Install(dir, cfg, true); err == nil {
		t.Fatal("the fixture no longer reproduces: install accepted a repository whose live pre-commit it would have switched off")
	}

	defer hooks.SetEvalSymlinksForTest(func(string) (string, error) { return "", syscall.EACCES })()
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(dir, cfg, true)
	if err == nil {
		t.Fatal("install took over the operator's hook wiring on a comparison it could not make")
	}
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	// THE WORDING, NOT ONLY THE PRESENCE. The control run above refuses too, so
	// `err != nil` alone is satisfied by any refusal at all — including a future
	// unrelated one arriving while the unanswerable comparison quietly went back
	// to being read as "those files are inert". This names the resolution
	// failure that is the whole subject of #171.
	wantErrContains(t, err, "cannot resolve")
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// The other polarity, kept rather than flattened. Verify reports; it does not
// take the exit-2 install takes, because a spurious report is the safe direction
// on the side whose verdict cannot switch a gate off.
//
// The assertion is on the WORDING as well as the exit: reaching here with the
// old `false` would have sent this on to list the hooks core.hooksPath shadows,
// which asserts a comparison nobody made.
func TestVerifyReportsRatherThanErrorsWhenItCannotCompareTheHooksDirectories(t *testing.T) {
	dir := repo(t)
	if err := os.Symlink(filepath.Join(".git", "hooks"), filepath.Join(dir, "hookslink")); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "config", "core.hooksPath", "hookslink")
	cfg := laneCfg("pre-commit")

	defer hooks.SetEvalSymlinksForTest(func(string) (string, error) { return "", syscall.EACCES })()

	probs, err := hooks.Verify(dir, cfg)
	if err != nil {
		t.Fatalf("verify turned a filesystem question it could not answer into an engine error: %v", err)
	}
	wantProblem(t, probs, "could not check which hooks it is switching off")
}
