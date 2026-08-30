// Verify and the checks behind it. hooks.go keeps the vocabulary and install.go
// keeps Install; this file answers one question, and the whole of #146 is that
// it used to answer a different one.
//
// THE QUESTION IS "WILL GIT RUN FORMWORK'S GATE HERE", NOT "IS THE CONFIG
// STRING THE ONE INSTALL WRITES". Verify used to read core.hooksPath, compare
// it to a constant, and read shims under formwork's own root. Git decides by
// different rules: it resolves core.hooksPath from the repository top level
// across every config scope, applies its own default, resolves it again per
// worktree, and then runs whatever executable file it finds by name. Twelve
// states came apart between those two answers — ten where verify printed "hooks
// wired" over a gate that could not run, two where it reported a healthy
// repository unwired.
//
// So the verdict here is FILE-LEVEL, at the directory git names: the shim
// exists, is a regular file, is executable, and byte-equals what install would
// write. There is deliberately no path comparison in the decision, which is why
// an absolute core.hooksPath and one spelled with a trailing slash stop being
// special cases rather than becoming two more of them.
package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Verify reports every reason git will not run formwork's gate in the
// repository containing root. An empty result means the wiring is intact.
//
// The error return is not a wiring problem. A git failure means verify could
// not find out what git will do; reporting "not wired" for a question nobody
// answered would turn a tool fault into a layout diagnosis, and the exit-code
// contract puts it at 2 rather than 1 (R6).
//
// R6 HAS TWO STATED EXCEPTIONS, both below and both for a git failure that is
// not a tool fault: shadowedHookProblems' defaultHooksDir call, which is a diagnostic
// arm reached only after the verdict has already run, and the HooksPath call for
// a worktree git has itself reported prunable, where the failure IS the
// finding. Each is argued at its site. Everywhere else a git failure is an
// error, and neither exception may be widened to a git call whose answer the
// verdict depends on.
//
// THE SECOND EXCEPTION IS FOR A FAILURE GIT ITSELF PRODUCED, and it excludes
// vcs.ErrGitEnv on that ground — an error formwork's environment policy raised
// before running git at all is not something git diagnosed, and it is argued at
// the site.
//
// Every other error lands IN the result. Nothing below moves past a failed
// stat, read or listing without appending a problem first — the loops do
// `continue`, but only after reporting — because each of those was a state where
// the check could not be performed at all and the old code read that as nothing
// to report. The two `fs.ErrNotExist` arms below are not that class: each is a
// state where there is genuinely nothing to check, and each is argued at its
// site.
func Verify(root string, cfg *config.Config) ([]string, error) {
	expected := Expected(cfg)
	if len(expected) == 0 {
		return []string{"no lanes are named after git hooks; nothing to verify"}, nil
	}

	var probs []string
	// R7, and it is now a measurement rather than a list of variable names:
	// gitenv.go asks git each question twice and reports the ones the ambient
	// configuration environment moves. A failure to ask is a git failure, so it
	// takes the error return like every other one here (R6 above).
	envProbs, err := configEnvProblems(root)
	if err != nil {
		return nil, err
	}
	probs = append(probs, envProbs...)
	// Wiring that is present but guaranteed to abort every commit is not wired
	// correctly, and this command replaces the shell system's "hooks-wired" CI
	// gate — so reporting green here is the same fail-open as the gate it
	// replaced going quiet. Checked before the file inspection because it holds
	// whatever those files say.
	for _, lane := range emptyLanes(cfg) {
		probs = append(probs, lane+": lane selects no rules, so the installed shim aborts every commit (`check --lane "+lane+"` exits 2)")
	}

	live, err := vcs.HooksPath(root)
	if err != nil {
		return nil, err
	}
	probs = append(probs, checkHooksDir(live, expected, "")...)
	probs = append(probs, shadowedHookProblems(root, live)...)

	wtProbs, err := worktreeProblems(root, live, expected)
	if err != nil {
		return nil, err
	}
	probs = append(probs, wtProbs...)

	if len(probs) > 0 {
		// Context first, so the operator reads WHY before WHAT. Gated on there
		// being a problem already: see hooksDirDiagnostic.
		probs = append([]string{hooksDirDiagnostic(root, live)}, probs...)
	}
	return probs, nil
}

// hooksDirDiagnostic renders where git will run hooks from and where formwork
// manages them.
//
// IT RETURNS A DISPLAY STRING. Not a bool, not a problem, not a verdict — and
// the type is the point. Comparing those two directories is exactly the check
// this rewrite removed from the decision: it reported a healthy repository
// unwired whenever core.hooksPath was spelled absolutely or with a trailing
// slash, and #142 r2 broke a case-variant root the same way. Promoting this
// back to a verdict now requires changing its signature, which a reviewer sees;
// as a bool it would be one `if` away.
// BOTH HALVES ARE PRINTED ABSOLUTELY, because the sentence is built to be read
// as a comparison. `live` comes back absolute from vcs.HooksPath, while the
// managed half is joined onto root — which is "." on the CLI's default
// invocation. Measured before this: `formwork hooks verify` with no -C printed
// "git runs hooks from /abs/repo/.formwork/hooks; formwork manages
// .formwork/hooks" for a repository where those are the SAME directory, which
// reads as the mismatch this line exists to reveal.
//
// The Abs failure keeps the joined spelling rather than raising: this is a
// display string with no verdict in it (above), so a degraded rendering is the
// right trade and an error return would give the function the shape the comment
// above says it must not have.
func hooksDirDiagnostic(root, live string) string {
	managed := filepath.Join(root, filepath.FromSlash(hooksDir))
	if abs, err := filepath.Abs(managed); err == nil {
		managed = abs
	}
	return fmt.Sprintf("git runs hooks from %s; formwork manages %s", live, managed)
}

// checkHooksDir is the verdict for one hooks directory: the shims below, plus
// the files in it git runs that this config does not declare.
//
// THE TWO HALVES ARE SPLIT BECAUSE ONE OF THEM HAS A SECOND CALLER AND THE OTHER
// MUST NOT. install.go asks shimProblems about the OTHER worktrees a
// repository-wide core.hooksPath now governs (#172), and that question is "does
// this worktree have formwork's shims" — an orphan there is a real finding and
// it is verify's to report, not a reason for install to call the worktree
// unwired. Splitting the function is what keeps install from either duplicating
// the shim verdict or inheriting a judgement it did not ask for.
func checkHooksDir(dir string, expected []string, prefix string) []string {
	return append(shimProblems(dir, expected, prefix), orphanProblems(dir, expected, prefix)...)
}

// shimProblems is the shim half: for each expected lane, the file at dir must
// exist, be a regular file, be executable, and byte-equal shim(lane). prefix
// labels the problems when dir belongs to a linked worktree rather than root.
//
// IT REPORTS NOTHING ABOUT FILES IT DID NOT EXPECT, and that boundary is the
// whole reason the function exists separately. The #172 split left the old
// `append(probs, orphanProblems(...))` tail on this function as well, so
// checkHooksDir ran the orphan half twice (every orphan reported doubled, per
// worktree) and install — which asks this half precisely to avoid the orphan
// judgement — called a worktree carrying every shim "runs no gate" over an
// operator's own hook file. A caller that wants both halves calls checkHooksDir.
//
// The byte-compare replaces a substring search for `check --lane <name>`, which
// was satisfied by a shim with `exit 0` above the exec line and by one rewritten
// with CRLF. Both were certified here, and they fail in OPPOSITE directions at
// commit time — measured on git 2.50.1: the `exit 0` shim lets `git commit`
// succeed at exit 0 with the gate never run, while the CRLF shim blocks every
// commit (git: `cannot exec`, because the CR becomes part of the interpreter
// name; run from a shell it is `bad interpreter`, exit 126). So the substring
// search certified one silent bypass and one gate nobody could get past. It is
// possible only because the shim's bytes depend on nothing but the lane (D4).
func shimProblems(dir string, expected []string, prefix string) []string {
	var probs []string
	for _, lane := range expected {
		p := filepath.Join(dir, lane)
		fi, err := os.Lstat(p)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s%s: no shim at %s (%v)", prefix, lane, p, err))
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			// Deliberately NOT phrased as "git will not run it": measured, git
			// follows the link and runs the target perfectly. What formwork
			// cannot do is certify the content, because install writes a
			// regular file and the link's target is not that file.
			probs = append(probs, fmt.Sprintf("%s%s: %s is a symlink; `formwork hooks install` writes a regular file, so formwork cannot certify what git would run through it", prefix, lane, p))
			continue
		}
		if !fi.Mode().IsRegular() {
			probs = append(probs, fmt.Sprintf("%s%s: %s is not a regular file, so it is not the shim `formwork hooks install` writes", prefix, lane, p))
			continue
		}
		// The kernel's answer, not the mode bits: see executableByCaller. A
		// failure to ask is reported rather than read as "executable".
		switch ok, err := executableByCaller(p); {
		case err != nil:
			probs = append(probs, fmt.Sprintf("%s%s: cannot tell whether %s is executable (%v), so formwork cannot certify that git would run it", prefix, lane, p, err))
		case !ok:
			probs = append(probs, fmt.Sprintf("%s%s: %s is not executable by the user who will commit (mode %04o); git prints a hint and runs nothing (run: formwork hooks install)", prefix, lane, p, fi.Mode().Perm()))
		}
		b, err := os.ReadFile(p)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s%s: cannot read %s (%v)", prefix, lane, p, err))
			continue
		}
		if string(b) != shim(lane) {
			probs = append(probs, fmt.Sprintf("%s%s: %s is not the shim `formwork hooks install` writes — it does not match byte for byte, so it was edited or written by another formwork version (run: formwork hooks install)", prefix, lane, p))
		}
	}
	return probs
}

// orphanProblems reports files in the hooks directory that git will execute and
// this config does not declare.
//
// This is the inverted row of #146: a shim left behind for a deleted lane is not
// a missing gate, it is a hook that aborts every push forever, and verify never
// looked at anything but the lanes it expected. A file formwork wrote is named
// as removable; a file it did not write is REPORTED and nothing more, because
// deleting an operator's script is worse than the bug.
func orphanProblems(dir string, expected []string, prefix string) []string {
	declared := map[string]bool{}
	for _, l := range expected {
		declared[l] = true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// A directory that does not exist holds no orphans, and every
			// expected lane above already reported its shim missing at this
			// same path. Any OTHER failure is reported: mode 111 is
			// traversable but not listable, so each shim reads fine by name
			// while the listing fails — swallowing that is "hooks wired", exit
			// 0, over an orphan that aborts every push.
			return nil
		}
		return []string{fmt.Sprintf("%scannot list %s (%v), so formwork could not check it for hooks git runs and this config does not declare", prefix, dir, err)}
	}
	var probs []string
	for _, e := range ents {
		name := e.Name()
		if declared[name] || !gitClientHookNames[name] {
			// A name git never executes is inert here — the directory's README
			// is not a hook, and reporting it would be a permanent exit 1 over
			// nothing.
			continue
		}
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s%s: cannot read %s (%v), so formwork could not tell whether it wrote it", prefix, name, p, err))
			continue
		}
		// isGenerated, not a marker search spelled here: install's orphan
		// cleanup asks the same question of the same files and REMOVES the ones
		// it answers yes for (hooks.go argues why the predicate is shared).
		if isGenerated(b) {
			probs = append(probs, fmt.Sprintf("%s%s: %s is a formwork shim for a lane this config does not declare; git still runs it, so every %s aborts (delete it, or restore the lane)", prefix, name, p, name))
			continue
		}
		// NOT "in formwork's hooks directory". dir is whatever `rev-parse
		// --git-path hooks` returned, which before any install is git's own
		// default — so that phrasing put a false sentence under a true finding
		// in every repository with real hooks and no formwork install.
		probs = append(probs, fmt.Sprintf("%s%s: git runs %s as a %s hook and this config does not declare it; formwork did not write that file, so it will not remove it", prefix, name, p, name))
	}
	return probs
}

// shadowedHookProblems names the hooks core.hooksPath is currently switching
// off.
//
// core.hooksPath overrides the WHOLE default hooks directory, not the three
// names formwork models, so pointing it anywhere silently disables every hook
// that was there — commit-msg, prepare-commit-msg, an operator's own pre-commit.
// Install no longer walks into it: the pre-flight refuses where git is running
// real hooks from its default directory (D2), and refuses a wiring wider than
// this repository unless --override-global (D7). This is what finds the state
// wherever it already exists — an older binary, a hand-set config, another
// tool's installer — whoever created it. What install refuses is a statement
// about installs formwork performed, never about how the state got here, so it
// is not a reason to trust the state unchecked.
//
// A filesystem failure is a problem, not an error return: only git failures are
// errors here.
func shadowedHookProblems(root, live string) []string {
	// defaultHooksDir, not a git directory joined here: install's detector and
	// this report must look in the same place, and the reason that place is the
	// SHARED git directory is argued there (R5).
	def, err := defaultHooksDir(root)
	if err != nil {
		// Not an error return, because this is a diagnostic arm rather than the
		// verdict: reaching it means the verdict above already ran, and losing
		// it would report the repository healthy on a git fault. defaultHooksDir
		// fails only on the git call it makes, which is what the wording names.
		return []string{fmt.Sprintf("cannot ask git which hooks directory it would use by default (%v), so formwork could not check whether core.hooksPath is switching hooks off", err)}
	}
	// VERIFY KEEPS THE OTHER POLARITY, AND NOW SAYS SO IN WORDS (#171). An
	// unresolvable comparison used to reach here as "not the same directory",
	// which sent this report on to list what core.hooksPath was shadowing — a
	// spurious report at worst, which is why the failure direction was safe on
	// this side and a fail-open on install's. It is still a report rather than an
	// error return, on this function's own rule that only git failures are errors
	// here; what changes is that it names the question formwork could not answer
	// instead of asserting the two directories differ.
	same, err := sameDirPath(def, live)
	if err != nil {
		return []string{fmt.Sprintf("cannot tell whether core.hooksPath sends hooks somewhere other than %s (%v), so formwork could not check which hooks it is switching off", def, err)}
	}
	if same {
		return nil // nothing is being shadowed: git is using the default
	}
	names, err := realHooksIn(def)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no default hooks directory, so nothing to shadow
		}
		return []string{fmt.Sprintf("cannot list %s (%v), so formwork could not check which hooks core.hooksPath is switching off", def, err)}
	}
	if len(names) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("core.hooksPath sends every hook to %s, so git no longer runs %s in %s — chain them from formwork's shims or from your own hook runner", live, strings.Join(names, ", "), def)}
}

// worktreeProblems checks every worktree git will run hooks in, not just the
// one root is in.
//
// Each state git reports gets its own line. Calling a worktree git cannot use
// "unwired" is a permanent exit 1 nobody can clear, which is how a verify
// command gets ignored — but SKIPPING a worktree whose directory is missing is
// the fail-open, because the same skip hides a worktree that exists on a branch
// without the shims. A bare entry is not a working tree and no client hook runs
// a commit there.
//
// THE DIRECTORY IS TESTED BEFORE GIT'S `prunable` FLAG, and the order is the
// point. `prunable` is git's verdict on the REGISTRATION, not on the directory:
// measured, deleting a worktree's `.git` file makes the entry prunable with
// every file still in place, and MOVING a worktree makes it prunable while the
// worktree keeps working at its new path. Branching on the flag first therefore
// skipped the hooks of a directory that was right there, and asserted "its
// directory is gone" — false in the first case, and in the second attached to
// advice that deregisters a worktree `git worktree repair` would fix. git's own
// reason is reported instead of one invented here.
//
// THE DEDUPE KEY IS THE RESOLVED HOOKS DIRECTORY, ABSOLUTE, JOINED AGAINST THAT
// WORKTREE'S OWN PATH. Under D4 core.hooksPath is the relative
// `.formwork/hooks`, so `rev-parse --git-path hooks` returns the IDENTICAL
// string in the main worktree and in every linked one — measured. A seen-set
// keyed on git's raw answer therefore collapses them all on the first check and
// every linked worktree goes unchecked, restoring the exact row this loop
// exists for. vcs.HooksPath applies the join, so it must be handed the
// worktree's path and never root.
//
// Deduping at all is load-bearing rather than cosmetic: `worktree list` reports
// the main worktree too, so without it a subdirectory root reports the same
// single missing shim twice.
func worktreeProblems(root, rootHooks string, expected []string) ([]string, error) {
	wts, err := vcs.Worktrees(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{resolvedKey(rootHooks): true}
	// THE MEASUREMENT HAS ITS OWN SET, KEYED ON THE DIRECTORY ASKED ABOUT rather
	// than on the hooks directory answered — the two dedupe different things, and
	// collapsing them is the defect the measurement's own comment below records.
	// `worktree list` reports the main worktree as an entry, and Verify has already
	// measured HooksPathQuestion at root (configEnvProblems, gitenv.go, which asks
	// it among its three); without this the ordinary case where the environment
	// moves ROOT's answer printed the same finding twice, once unlabelled and once
	// labelled with the main worktree's path — measured against this loop before
	// the set was added. A key collision here means the same question at the same
	// directory under the same environment, which is why skipping it reports
	// nothing this run has not already said.
	measured := map[string]bool{resolvedRootKey(root): true}
	var probs []string
	for _, wt := range wts {
		if wt.Bare {
			continue
		}
		if _, err := os.Stat(wt.Path); err != nil {
			if wt.Prunable {
				probs = append(probs, fmt.Sprintf("worktree %s: %s, and formwork cannot find that directory, so it cannot check its hooks%s", wt.Path, prunableNote(wt), worktreeCure))
				continue
			}
			locked := ""
			if wt.Locked {
				locked = " — it is locked, so git will not prune it either"
			}
			probs = append(probs, fmt.Sprintf("worktree %s: cannot be inspected (%v), so formwork cannot check its hooks%s", wt.Path, err, locked))
			continue
		}
		hp, err := vcs.HooksPath(wt.Path)
		if err != nil {
			if wt.Prunable && !errors.Is(err, vcs.ErrGitEnv) {
				// R6's one further exception, and it is narrow: git has already
				// diagnosed this registration itself, at exit 0, so its refusal
				// to answer for the directory is that diagnosis rather than a
				// tool fault. Returning the error would make one dead worktree
				// registration exit 2 for the whole repository. A worktree git
				// does NOT call prunable keeps R6 exactly.
				//
				// THE ARGUMENT IS ABOUT AN ANSWER GIT GAVE, so it cannot cover an
				// error raised BEFORE git ran. vcs.ErrGitEnv marks formwork's own
				// environment policy refusing to ask at all — measured, an ambient
				// GIT_DIR that is coherent at root diverges at a linked worktree, and
				// a worktree that is ALSO prunable took this arm and rendered that
				// refusal as a wiring problem at exit 1 (gitenv_test.go). Nothing
				// about a dead registration makes an unusable environment less of an
				// engine error, and no prune or repair clears it.
				probs = append(probs, fmt.Sprintf("worktree %s: %s, and git will not say where it would run hooks in that directory (%v), so formwork cannot check them%s", wt.Path, prunableNote(wt), err, worktreeCure))
				continue
			}
			return nil, err
		}
		if wt.Prunable {
			// The directory is there and git answered for it, so its hooks are
			// checked below like any other worktree's. The registration is still
			// reported: `git worktree prune` will drop it.
			probs = append(probs, fmt.Sprintf("worktree %s: %s%s", wt.Path, prunableNote(wt), worktreeCure))
		}
		label := "worktree " + wt.Path + ": "
		// The measurement is taken HERE, at this worktree's path, because that is
		// where the answer above was taken. Verify's other measurement is at root,
		// and root's is not this one: under extensions.worktreeConfig a linked
		// worktree carries a core.hooksPath of its own, so a variable whose value
		// EQUALS root's own local setting leaves root's two answers byte-identical
		// while moving this worktree's. Measured on git 2.50.1, that state reported
		// "hooks wired" at exit 0 over a worktree running no gate at all
		// (gitenv_test.go).
		//
		// IT IS TAKEN BEFORE THE DEDUPE, WHICH IS THE ONLY ORDER THAT MEASURES THE
		// STATE IT EXISTS FOR. The key is built from hp, and hp is the AMBIENT
		// answer — so an environment that moves this worktree's answer ONTO root's
		// resolved hooks directory makes the key collide and `continue` skip the
		// measurement that would have reported it. With an absolute core.hooksPath
		// there is one such directory for every worktree, so that collision is not
		// exotic: measured on git 2.50.1, verify printed "hooks wired" at exit 0
		// over a worktree with no shim at all. The cost of the order is two forks
		// per deduped worktree, which gitenv.go's measurement records as ~2.5ms.
		//
		// A failure to ask is a git failure, so it takes the error return — R6 at
		// the top of this file, and neither exception above covers it: git has
		// already answered for this directory at the HooksPath call the measurement
		// asks the same question as.
		if mkey := resolvedRootKey(wt.Path); !measured[mkey] {
			measured[mkey] = true
			envProbs, err := configEnvProblemsAt(wt.Path, hooksPathOnly, label)
			if err != nil {
				return nil, err
			}
			probs = append(probs, envProbs...)
		}
		key := resolvedKey(hp)
		if seen[key] {
			continue
		}
		seen[key] = true
		probs = append(probs, checkHooksDir(hp, expected, label)...)
	}
	return probs, nil
}

// worktreeCure names both cures for a prunable entry, in the order that keeps a
// working worktree working.
//
// Prune alone was the advice, and for one of the states behind this line it is
// destructive: a moved worktree is listed at its old path and still commits from
// its new one, so pruning deregisters something that works. Verify cannot tell
// that state from a deleted worktree — measured on git 2.50.1, both report
// `prunable gitdir file points to non-existent location`, and the old path is
// all either leaves behind — so it must not choose for the operator.
const worktreeCure = " — if it was moved, run `git worktree repair` from its new location; if it is really gone, run `git worktree prune`"

// prunableNote renders git's verdict on a registration in git's own words.
func prunableNote(wt vcs.Worktree) string {
	if wt.PrunableReason == "" {
		return "git lists it as prunable"
	}
	return fmt.Sprintf("git lists it as prunable (%s)", wt.PrunableReason)
}

// resolvedKey is a dedupe key for a directory that may not exist yet.
//
// git reports worktree paths canonically while root is whatever the operator
// typed, so on macOS the same directory arrives as /var/… and /private/var/…
// and a raw-string seen-set never matches — measured, one missing shim reported
// twice. filepath.EvalSymlinks cannot resolve a path that does not exist, which
// is precisely the case here (the missing hooks directory IS the finding), so
// this resolves the longest existing prefix and rejoins the remainder.
//
// Unresolvable falls back to the lexical path: two spellings then compare
// unequal and the directory is checked twice. That is the harmless direction —
// this key only decides whether to REPEAT a check, never whether to perform
// one, so no arrangement of it can hide a worktree.
func resolvedKey(p string) string {
	p = filepath.Clean(p)
	cur, rest := p, ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// resolvedRootKey is resolvedKey for a directory the operator may have spelled
// relatively, which root routinely is: the CLI's `-C` defaults to ".", the
// spelling hooksDirDiagnostic above takes the same precaution for. git reports
// worktree paths absolutely, so without the Abs a relative root can never
// compare equal to the entry `worktree list` reports for it.
//
// An Abs failure keeps the spelling it was given, on resolvedKey's own argument:
// two spellings then compare unequal and the directory is measured twice, which
// is the harmless direction — this key decides whether to REPEAT a measurement,
// never whether to take one.
func resolvedRootKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return resolvedKey(p)
}
