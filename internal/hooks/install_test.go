package hooks_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/hooks"
)

// Install's WRITE path, after the pre-flight has already decided this repository
// may be wired at all. Two states live here and they are not alike: a shim
// formwork owns that has gone wrong, which install repairs, and a file in the
// managed directory formwork does NOT own, which it reports and leaves.
//
// Helpers (repo, laneCfg, managedDir, mustInstall, mustVerify, writeShimFile)
// live in hooks_test.go and verify_test.go.

// A re-install must make an existing shim runnable again.
//
// os.WriteFile's mode applies on create only, so `0o755` states install's intent
// and repairs nothing: a shim that lost its execute bit stayed unrunnable
// through every re-install while install reported the lane as wired and git
// printed a hint and ran nothing.
//
// TWO MODES, AND ONLY THE SECOND SAYS WHICH QUESTION THE REPAIR ASKS. At 0644
// every reading agrees the file is not executable, so a repair keyed on the mode
// bits fixes it too. At 0655 (rw-r-xr-x) the readings diverge: two execute bits
// are set, and the owner — the user git asks access(X_OK) about — still cannot
// run it. TestVerifyAgreesWithGitAboutAShimGitWillNotRun in commit_test.go pins
// that this exact mode lets a violating commit through, which is what makes the
// row worth repairing rather than merely counting bits.
//
// Verify is the oracle rather than a mode assertion, because "is this file
// executable" is the question the mode bits get wrong; verify asks the kernel
// the same way git does.
func TestInstallRepairsAShimThatLostItsExecuteBit(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o655} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			if mode&0o100 == 0 && mode&0o111 != 0 && os.Geteuid() == 0 {
				t.Skip("root executes on any execute bit, so this mode is not broken for root")
			}
			dir := repo(t)
			cfg := laneCfg("pre-commit")
			mustInstall(t, dir, cfg)
			shim := filepath.Join(managedDir(dir), "pre-commit")
			if err := os.Chmod(shim, mode); err != nil {
				t.Fatal(err)
			}
			// The state this test is about: broken BEFORE the re-install. A test
			// that installs into a fresh directory cannot tell a repair from
			// WriteFile's create-time mode.
			if probs := mustVerify(t, dir, cfg); len(probs) == 0 {
				t.Fatalf("fixture is not broken: verify called a %04o shim wired", mode)
			}

			mustInstall(t, dir, cfg)

			if probs := mustVerify(t, dir, cfg); len(probs) != 0 {
				t.Fatalf("re-install left a shim git will not run: %#v", probs)
			}
			if got := readShim(t, dir, "pre-commit"); !strings.Contains(got, "check --lane pre-commit") {
				t.Fatalf("the repair changed the shim's contents:\n%s", got)
			}
		})
	}
}

// The arm no chmod can produce: access(2) failing for a reason that is not
// EACCES, which means formwork could not perform the executable test at all
// rather than that the answer is no. The repair runs anyway — "could not find
// out" is not "it is fine", and the other direction ships a gate git declines to
// run. The seam is the only way in, which is the reason foreign.go gives for it
// existing at all.
//
// Verify cannot be the oracle here, because the stub makes verify's own
// executable test unanswerable too; the mode is what says the chmod happened.
func TestInstallRepairsWhenItCannotTellWhetherTheShimIsExecutable(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	shim := filepath.Join(managedDir(dir), "pre-commit")
	if err := os.Chmod(shim, 0o644); err != nil {
		t.Fatal(err)
	}
	defer hooks.SetAccessForTest(func(string, uint32) error { return syscall.EIO })()

	mustInstall(t, dir, cfg)

	fi, err := os.Stat(shim)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("install left the shim at %04o when it could not tell whether git could run it", fi.Mode().Perm())
	}
}

// The other arm of the same seam, and the one the repair was taking on trust: a
// definite NO after the chmod. `os.Chmod` returning nil says the mode write
// landed, which is not the same claim as "the user who will commit can now run
// this file" — a deny-execute ACL, a filesystem that does not carry the bit, and
// a noexec mount each accept the write and change nothing. foreign.go's own
// argument for asking the kernel is that no arithmetic over Mode() can see a
// macOS ACL; a repair that reads a successful chmod as proof is that same
// arithmetic one level up.
//
// Measured before this guard, with a deny-execute ACL on the shim: `hooks
// install` exited 0 naming the lane, and `hooks verify` immediately reported the
// shim as not executable — with `formwork hooks install` as its cure. The
// operator loops between a command that says it fixed it and one that says it is
// broken.
//
// EACCES rather than EIO, because those are the two different answers: EACCES is
// the kernel saying no, and that is what must abort. An errno that is not EACCES
// means the test could not be performed at all, which the test above pins as
// still-repaired — "could not find out" is not "it is broken" either.
func TestInstallRefusesAShimItCouldNotMakeExecutable(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	defer hooks.SetAccessForTest(func(string, uint32) error { return syscall.EACCES })()

	_, err := hooks.Install(dir, cfg, false)

	if err == nil {
		t.Fatal("install reported a wired lane over a shim the kernel says cannot be executed; verify then reports it, and its cure is this command")
	}
	shim := filepath.Join(managedDir(dir), "pre-commit")
	if !strings.Contains(err.Error(), shim) {
		t.Errorf("the refusal must name the shim it could not repair: %v", err)
	}
	if !strings.Contains(err.Error(), "not executable by the user who will commit") {
		t.Errorf("the refusal must say what is wrong in verify's own words: %v", err)
	}
}

// A shim formwork wrote for a lane the config no longer declares is formwork's
// own litter, and install clears it.
//
// Under core.hooksPath nothing in that directory is inert: the stale shim runs
// `check --lane pre-push` for a lane that no longer exists, which exits 2, so
// every push aborts forever. Verify already reports it and tells the operator to
// delete it; install performing that deletion is the pairing.
func TestInstallRemovesItsOwnShimForALaneTheConfigNoLongerDeclares(t *testing.T) {
	dir := repo(t)
	mustInstall(t, dir, laneCfg("pre-commit", "pre-push"))
	stale := filepath.Join(managedDir(dir), "pre-push")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("fixture: no pre-push shim to orphan: %v", err)
	}

	only := laneCfg("pre-commit")
	wired, err := hooks.Install(dir, only, false)
	if err != nil {
		t.Fatalf("removing formwork's own leftover is not a problem to report: %v", err)
	}
	if len(wired) != 1 || wired[0] != "pre-commit" {
		t.Fatalf("wired = %v, want [pre-commit]", wired)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the shim for the deleted lane is still there (%v); git runs it and every push aborts", err)
	}
	if probs := mustVerify(t, dir, only); len(probs) != 0 {
		t.Errorf("verify still reports the directory install just tidied: %#v", probs)
	}
}

// THE DESTRUCTIVE-ACTION GUARD. Install removes formwork's own leftovers, and
// the same loop sees files formwork never wrote — an operator's commit-msg,
// dropped into the managed directory so core.hooksPath would pick it up. Those
// are REPORTED and left exactly where they are.
//
// The mutation this test exists for is to drop the marker test and remove every
// undeclared hook file: the survival assertion below is what goes red, and
// without it deleting someone's script would be a silent, passing behaviour.
//
// The non-hook file is the other half. `README` is not a name git executes, so
// reporting it would be a permanent error over a file that does nothing —
// verify skips those names for the same reason.
func TestInstallReportsAForeignHookFileAndNeverDeletesIt(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)

	const theirs = "#!/bin/sh\necho theirs\n"
	foreign := filepath.Join(managedDir(dir), "commit-msg")
	writeShimFile(t, foreign, theirs, 0o755)
	readme := filepath.Join(managedDir(dir), "README")
	writeShimFile(t, readme, "hooks live here\n", 0o644)

	wired, err := hooks.Install(dir, cfg, false)

	// NEVER DELETED — asserted FIRST, and with no Fatal above it. Ordered the
	// other way round, a mutation that removes unmarked files also removes the
	// report, so the run dies on the missing report and the destructive step is
	// never examined at all. The guard has to be the assertion that fails.
	b, readErr := os.ReadFile(foreign)
	if readErr != nil {
		t.Errorf("install deleted a file it did not write: %v", readErr)
	} else if string(b) != theirs {
		t.Errorf("the operator's hook was rewritten:\n%s", b)
	}
	if _, err := os.Stat(readme); err != nil {
		t.Errorf("install deleted a file that is not even a hook: %v", err)
	}

	// Reported.
	if err == nil {
		t.Fatal("install said nothing about a hook file in its own directory that it did not write")
	}
	if !strings.Contains(err.Error(), "commit-msg") {
		t.Errorf("the report must name the file: %v", err)
	}
	if strings.Contains(err.Error(), "README") {
		t.Errorf("a name git never executes is not a hook, and reporting it is an error nobody can clear: %v", err)
	}
	// And the healthy lane is still wired, exactly as the empty-lane path does
	// it: a diagnosis about a file formwork does not own must not cost the
	// operator the gate it does.
	if len(wired) != 1 || wired[0] != "pre-commit" {
		t.Errorf("wired = %v, want [pre-commit]", wired)
	}
}

// --- #172: what install SAYS when the setting it writes is repository-wide ----

// core.hooksPath is REPOSITORY-WIDE, and install's success line describes the
// worktree it ran in. Run from a linked worktree it reported the lanes it wired
// and exited 0, while `hooks verify` at the same root immediately exited 1 over
// the state install had just created — the third place in this subsystem where
// install certifies what verify refuses, which is the pattern #146 exists for.
//
// THE FIX IS TO WHAT INSTALL SAYS, NOT TO WHAT IT DOES. R8 decided deliberately
// that install runs no worktree loop that ACTS: a relative core.hooksPath
// resolves per worktree, the shims are a committed artifact (D4) that arrives
// with a checkout, and an install-time write loop could not be atomic across N
// worktrees. So the other worktree's missing shims are still not install's to
// create; they are install's to stop calling installed.
//
// The assertion is paired with Verify deliberately: what must not happen is
// install reading as an unqualified success WHILE verify at the same root
// returns problems, and asserting either half alone would let the pair drift
// apart again.
func TestInstallFromALinkedWorktreeDoesNotReportAnUnqualifiedSuccess(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir) // a HEAD for `worktree add`; the shims do not exist yet
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")

	wired, err := hooks.Install(wt, cfg, false)
	if len(wired) == 0 {
		t.Fatal("install wired nothing, so this test is not measuring the reporting of a successful install")
	}
	probs := mustVerify(t, wt, cfg)
	if len(probs) == 0 {
		t.Fatal("the fixture no longer reproduces: verify certifies the repository install just wired")
	}
	if err == nil {
		t.Fatalf("install reported an unqualified success while verify at the same root reports: %#v", probs)
	}
	// The scope of the setting, and the worktree that does not have the shims —
	// the two facts install's own output was missing.
	wantErrContains(t, err, "core.hooksPath", resolved(t, dir))
}

// The other direction, without which the line above is a permanent nag: an
// ordinary single-worktree install says nothing about worktrees, and neither
// does one whose other worktrees all carry the shims. The second case is the one
// that matters here — the shims are committed, so a healthy multi-worktree
// repository is the NORMAL state, and a message that fires there would be an
// exit 1 on every install nobody can clear.
func TestInstallSaysNothingAboutWorktreesThatAlreadyCarryTheShims(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	// Tracked, so `worktree add` checks them out into the new worktree. Committed
	// with --no-verify because the shim install just wired execs a `formwork`
	// that is not on this test's PATH, and every commit here would exit 127.
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "--no-verify", "-m", "seed")
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	if _, err := os.Stat(filepath.Join(wt, ".formwork", "hooks", "pre-commit")); err != nil {
		t.Fatalf("the fixture no longer reproduces: the new worktree did not get the committed shims: %v", err)
	}

	wired, err := hooks.Install(dir, cfg, false)
	if err != nil {
		t.Fatalf("install reported a problem over a repository whose every worktree is wired: %v", err)
	}
	if len(wired) != 1 || wired[0] != "pre-commit" {
		t.Fatalf("wired = %v, want [pre-commit]", wired)
	}
	wantWired(t, mustVerify(t, dir, cfg))
}

// The same control, over the state that actually reproduced the defect: a
// linked worktree that carries every shim AND a hook file git runs that this
// config does not declare — an operator's own `commit-msg`, which is nothing to
// do with the setting install just wrote.
//
// install asks shimProblems, and shimProblems used to end by appending
// orphanProblems, so install inherited the orphan judgement anyway and called a
// fully-wired worktree unwired: "so a commit there runs no gate", exit 2, over a
// worktree that runs the gate.
//
// The assertion is paired with Verify deliberately. Install saying nothing must
// not be bought by nobody saying anything: the undeclared hook is a real
// finding and verify is the command that owns it, so the second half pins that
// the judgement moved out of install rather than out of the codebase.
func TestInstallSaysNothingAboutAWiredWorktreeThatAlsoHoldsAnUndeclaredHook(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	// Tracked, so `worktree add` checks the shims out into the new worktree.
	// --no-verify because the shim just wired execs a `formwork` that is not on
	// this test's PATH.
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "--no-verify", "-m", "seed")
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	if _, err := os.Stat(filepath.Join(wt, ".formwork", "hooks", "pre-commit")); err != nil {
		t.Fatalf("the fixture no longer reproduces: the new worktree did not get the committed shims: %v", err)
	}
	// Only in the linked worktree: root's own directory holds nothing but the
	// shim, so anything install says here is about the OTHER worktree.
	writeShimFile(t, filepath.Join(wt, ".formwork", "hooks", "commit-msg"), "#!/bin/sh\necho mine\n", 0o755)

	wired, err := hooks.Install(dir, cfg, false)
	if err != nil {
		t.Fatalf("install called a worktree that carries every shim unwired, over a hook file it does not own: %v", err)
	}
	if len(wired) != 1 || wired[0] != "pre-commit" {
		t.Fatalf("wired = %v, want [pre-commit]", wired)
	}
	// And the finding is still made, by the command that owns it.
	wantProblem(t, mustVerify(t, dir, cfg), "commit-msg")
}
