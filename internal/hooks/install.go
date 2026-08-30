// Install and its pre-flight. hooks.go keeps the vocabulary — what a hook is,
// and what one lane's shim says — and answers no question about the filesystem.
// This file is the side that touches it: it creates a directory, writes files,
// and changes git's configuration.
//
// That is the boundary. Everything here can fail for reasons that have nothing
// to do with the config it was handed — a read-only tree, a directory where a
// file is expected, a git that will not take a setting — so this is where the
// error returns live, and hooks.go stays a place where the only way to be wrong
// is to describe a hook incorrectly.
package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Install writes a shim per git-hook lane into root/.formwork/hooks and points
// core.hooksPath at it. The shims are machine-independent (see shim) and meant
// to be committed. Idempotent.
//
// overrideGlobal is `formwork hooks install --override-global`: the operator
// saying this repository is different from whatever the machine has set. It
// answers ONE of the pre-flight's refusals, the one about a wiring wider than
// this repository, and it never makes formwork write outside the repository —
// what it unlocks is a repo-local core.hooksPath. Wiring the project itself
// declared is not something it clears; preflight.go argues which is which.
func Install(root string, cfg *config.Config, overrideGlobal bool) ([]string, error) {
	expected := Expected(cfg)
	if len(expected) == 0 {
		return nil, errors.New("no lanes are named after git hooks (pre-commit, pre-push, pre-merge-commit); nothing to install")
	}
	// The pre-flight comes before the first write and before the first setting,
	// and its refusals abort the whole install — which is NOT what the empty-lane
	// diagnosis below does. See preflight.go for why the two are different
	// shapes.
	if err := preflight(root, cfg, overrideGlobal); err != nil {
		return nil, err
	}
	// A git-hook lane selecting no rules gets NO shim, and the run reports it —
	// but the healthy lanes are still installed first.
	//
	// The shim's `check --lane` exits 2 on an empty lane, so writing one would
	// abort every commit with nothing the developer can act on from the hook's
	// output. Skipping it is not enough on its own either: a lane silently
	// missing its hook is a gate that looks wired and is not, so the error below
	// is what makes it loud.
	//
	// The ordering is the correction to this guard's first cut, which refused
	// the WHOLE install when any lane was empty. A repo with a healthy
	// pre-commit and a stale pre-push then got neither — bootstrap scripts run
	// `hooks install` and continue, so the developer committed with no gate at
	// all, where before they had one. Never let a diagnosis about lane B remove
	// the protection lane A was providing.
	skip := map[string]bool{}
	for _, name := range emptyLanes(cfg) {
		skip[name] = true
	}
	var wired []string
	for _, lane := range expected {
		if !skip[lane] {
			wired = append(wired, lane)
		}
	}
	dir := filepath.Join(root, filepath.FromSlash(hooksDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, lane := range wired {
		p := filepath.Join(dir, lane)
		if err := os.WriteFile(p, []byte(shim(lane)), 0o755); err != nil {
			return nil, err
		}
		if err := ensureExecutable(p); err != nil {
			return nil, err
		}
	}
	// Set core.hooksPath whenever at least one shim was written, so the healthy
	// gates take effect even though the run reports a problem below.
	if len(wired) > 0 {
		if err := vcs.SetConfig(root, "core.hooksPath", hooksDir); err != nil {
			return nil, err
		}
	}
	// AFTER the config is set, so a directory formwork cannot list or a file it
	// cannot remove costs the operator a report rather than the wiring: the
	// healthy lanes' shims are on disk and pointed at by the time anything here
	// can go wrong.
	probs := orphanCleanup(dir, expected)
	if len(wired) > 0 {
		probs = append(probs, repositoryWideScopeProblems(root, expected)...)
	}
	if len(skip) > 0 {
		probs = append([]string{fmt.Sprintf("git-hook lane(s) %s select no rules, so no shim was written for them — a hook running zero rules is a gate that only looks like one; give those lanes rules or matching tags, or rename them so they are not git hooks",
			strings.Join(emptyLanes(cfg), ", "))}, probs...)
	}
	if len(probs) > 0 {
		// Non-nil installed list AND an error: the caller prints both, so the
		// operator sees what is now protecting them and what is not.
		return wired, errors.New(strings.Join(probs, "\n"))
	}
	return wired, nil
}

// repositoryWideScopeProblems names the OTHER worktrees the setting install just
// wrote now governs and which of them git will find no shims in (#172).
//
// core.hooksPath IS REPOSITORY-WIDE AND INSTALL'S SUCCESS LINE IS NOT. Run from a
// linked worktree, install reported the lanes it wired at exit 0 while `hooks
// verify` at the same root exited 1 over the state install had just created —
// install certifying what verify refuses, which is the pattern #146 exists to
// remove. The over-claim is in the reporting: the other worktree had no
// protection before either, so nothing is newly ungated here.
//
// IT REPORTS AND DOES NOT WRITE, WHICH IS R8 KEPT RATHER THAN REVERSED. R8
// decided install runs no worktree loop that ACTS — the shims are a committed
// artifact (D4) that arrives with a checkout, an install-time write loop would
// act on prunable and bare entries, and it could not be atomic across N
// worktrees. Reading is a different act: this asks git for the list it already
// walks in verify, asks one question per entry, and writes nothing.
//
// IT FIRES ONLY WHERE A WORKTREE IS REALLY UNWIRED, and that is the difference
// between a diagnosis and a nag. The shims are committed, so a healthy
// multi-worktree repository is the ORDINARY state; a line printed for every
// worktree that merely EXISTS would be an exit 1 on every install nobody can
// clear, which is how the command stops being read.
//
// THE QUESTION IS shimProblems AND NOT checkHooksDir. An orphan in another
// worktree's hooks directory is a real finding and it is verify's to report;
// treating it as "this worktree is not wired" would make install refuse over a
// file that has nothing to do with the setting it just wrote.
//
// WHAT IT DOES NOT COVER IS NAMED IN THE MESSAGE RATHER THAN LEFT IMPLIED: a
// worktree git will not answer for — a deleted directory, a prunable
// registration — is reported as unchecked, not as wired, and `hooks verify`
// stays the authority for the repository as a whole. Install is not growing a
// second verify here; it is refusing to describe as installed a repository it
// has only half looked at.
func repositoryWideScopeProblems(root string, expected []string) []string {
	wts, err := vcs.Worktrees(root)
	if err != nil {
		return []string{fmt.Sprintf("core.hooksPath is repository-wide, and formwork cannot ask git which worktrees this repository has (%v), so it cannot say whether the setting it just wrote leaves another worktree without shims; run `formwork hooks verify`", err)}
	}
	rootKey := resolvedRootKey(root)
	var unwired, unchecked []string
	for _, wt := range wts {
		// A bare entry is not a working tree and no client hook runs a commit
		// there — the reading verify's own loop takes.
		if wt.Bare || resolvedRootKey(wt.Path) == rootKey {
			continue
		}
		hp, err := vcs.HooksPath(wt.Path)
		if err != nil {
			unchecked = append(unchecked, wt.Path)
			continue
		}
		if len(shimProblems(hp, expected, "")) > 0 {
			unwired = append(unwired, wt.Path)
		}
	}
	var probs []string
	if len(unwired) > 0 {
		probs = append(probs, fmt.Sprintf("core.hooksPath is repository-wide: this install set it for every worktree of this repository, and git now looks for formwork's shims under %s in each — %s do not have them, so a commit there runs no gate. The shims are meant to be committed, so a worktree gets them from its own checkout; commit %s and check the branch each worktree is on. `formwork hooks verify` is the authority for the whole repository",
			hooksDir, strings.Join(unwired, ", "), hooksDir))
	}
	if len(unchecked) > 0 {
		probs = append(probs, fmt.Sprintf("core.hooksPath is repository-wide, and git would not say where it runs hooks in %s, so formwork could not check whether the setting it just wrote leaves those worktrees without shims; run `formwork hooks verify`",
			strings.Join(unchecked, ", ")))
	}
	return probs
}

// ensureExecutable makes a shim that is already on disk runnable again.
//
// os.WriteFile's mode applies ON CREATE ONLY — rewriting a file that exists
// leaves whatever mode it has — so the 0o755 above states install's intent for a
// fresh write and repairs nothing. A shim whose execute bit was lost (a broad
// chmod, an archive or editor that rewrote it, a filesystem that does not carry
// the bit) therefore survived every re-install unrunnable, while install
// reported the lane as wired and git ran nothing.
//
// IT ASKS THE KERNEL, NOT THE MODE BITS. `mode&0o111 != 0` is "some execute bit
// is set somewhere", which is a different question from the one git asks:
// access(2) X_OK consults the owner's bit alone once the caller owns the file,
// so a shim at 0655 carries two execute bits and still cannot be run by the
// person committing. executableByCaller (foreign.go) is that call, and it is
// also what verify's verdict asks — repairing on a different question would
// leave a state install called fixed and verify goes on reporting.
//
// It repairs only when the answer is not a definite yes, so a mode that already
// works is left alone rather than widened: a shim the operator tightened to 0700
// is executable by its owner, and rewriting it 0755 would hand execute to
// everyone on the machine over a bit that was never broken. A failure to ASK is
// repaired, because "formwork could not find out" is not "it is fine" — chmod is
// idempotent, and the other direction ships a gate git declines to run.
//
// AND THE KERNEL IS ASKED AGAIN AFTERWARDS. os.Chmod returning nil says the mode
// write landed, which is a different claim from "the user who will commit can now
// run this file": a deny-execute ACL, a filesystem that does not carry the bit,
// and the noexec mount named above each accept the write and change nothing.
// Taking the write as proof made install exit 0 naming the lane while verify went
// on reporting the same shim as unrunnable — and verify's cure is `formwork hooks
// install`, so the operator loops between the two commands. Only a definite no
// fails: an errno that is not EACCES is still "could not find out", which is the
// state the repair above exists for rather than one to abort on.
func ensureExecutable(path string) error {
	if ok, err := executableByCaller(path); err == nil && ok {
		return nil
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return err
	}
	if ok, err := executableByCaller(path); err == nil && !ok {
		return fmt.Errorf("%s is still not executable by the user who will commit after chmod 0755; git prints a hint and runs nothing, so this lane would be reported as wired and gate nothing — check for an ACL denying execute, or a filesystem mounted noexec", path)
	}
	return nil
}

// orphanCleanup removes formwork's own leftovers from the managed hooks
// directory and reports the files in it formwork did not write.
//
// An orphan is a file git executes as a hook (gitClientHookNames, foreign.go)
// that this config declares no lane for — the shim left behind when a git-hook
// lane is deleted is the case that makes them. Nothing in this directory is
// inert once core.hooksPath points at it: a stale pre-push shim runs `check
// --lane pre-push` for a lane that no longer exists, which exits 2, so every
// push aborts.
//
// THE SET IS THE DECLARED LANES, NOT THE ONES THIS RUN WIRED. A declared lane
// that selects no rules gets no shim (Install argues why), and verify describes
// its leftover as an empty lane rather than as an orphan; judging by the wired
// list here would delete a file the other command is still reporting on. Both
// take Expected(cfg).
//
// A FOREIGN FILE IS REPORTED AND LEFT WHERE IT IS. What makes a file formwork's
// own is isGenerated, the predicate verify's orphan report reads too; everything
// else is somebody's hook, and deleting an operator's script is worse than the
// orphan that motivated this. A file that cannot be READ is treated as foreign
// for the same reason: a deletion rests on the claim that formwork wrote the
// file, and an unanswered question is not that claim.
//
// Removals are silent and reports are not, which is a deliberate asymmetry. A
// removal reaches exactly the state verify's orphan line already tells the
// operator to reach ("delete it, or restore the lane"), and the loop above
// overwrites a stale shim's bytes without announcing that either. A foreign file
// is the opposite case: formwork will not act on it, so saying nothing would
// leave a hook in place that nothing accounts for.
func orphanCleanup(dir string, expected []string) []string {
	declared := map[string]bool{}
	for _, l := range expected {
		declared[l] = true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		// Reported, never swallowed. The directory was created moments ago, so
		// this is a genuine fault — and mode 0111 is the shape of it: an EXISTING
		// shim above rewrites fine by name while the listing fails, which read as
		// "no orphans" would be an install that certified a directory it never
		// saw. Measured: at 0111 a rewrite succeeds and creating a NEW file does
		// not, because creation needs `w` on the directory — so a first install
		// fails in the loop above and never arrives here. This arm is the
		// re-install case.
		return []string{fmt.Sprintf("cannot list %s (%v), so formwork could not check it for hooks git runs and this config does not declare", dir, err)}
	}
	var probs []string
	for _, e := range ents {
		name := e.Name()
		if declared[name] || !gitClientHookNames[name] {
			// A name git never executes is inert here — the directory's README
			// is not a hook, and reporting it would be an error nobody can clear.
			continue
		}
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: cannot read %s (%v), so formwork could not tell whether it wrote that file and has left it alone", name, p, err))
			continue
		}
		if !isGenerated(b) {
			probs = append(probs, fmt.Sprintf("%s: %s is in the hooks directory formwork manages and this config declares no %s lane; git executes files with that name as hooks, and formwork did not write this one, so it will not remove it — move it elsewhere, or rename it to a name git does not run", name, p, name))
			continue
		}
		if err := os.Remove(p); err != nil {
			probs = append(probs, fmt.Sprintf("%s: cannot remove the stale shim %s (%v); git still runs it, so every %s aborts", name, p, err, name))
		}
	}
	return probs
}
