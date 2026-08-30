// fileset.go — the --staged/--range accounting: which of the paths git named
// the walk could produce, and what became of the rest — a declared prune
// channel hid it (#151 row 9), or it never arrived at all (#158).
// Split from cli.go, which the 750-line vendor cap bounds; same package.
package cli

import (
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// scannablePaths counts the paths a file-set flag named that the walk could ever
// have produced — everything except paths beneath a built-in skip DIRECTORY
// (.git, .formwork).
//
// The count exists to be compared against how many files were actually scanned,
// so anything that was never scannable must not enter it: a config-only commit
// stages nothing BUT .formwork/ paths, and counting those reported "1 path(s)
// requested, 0 file(s) scanned" on the most ordinary commit in this repo. A gap
// that fires on a benign everyday case is one readers learn to skip, which is
// the failure the summary block exists to avoid.
//
// The exclusion is ANCESTOR-ONLY, and the looser spelling was a defect here too
// (#158 review round 1): a regular file NAMED .formwork is scanned and enforced
// on, so leaving it out of the count suppressed the requested-vs-scanned
// indicator for the one path where it had something to report.
//
// This is a NARROWER exclusion than requestedButAbsent's, and deliberately so.
// That function also declines to refuse a symlink and a gitlink, which ARE
// requested paths a caller asked about and which the walk genuinely considered —
// counting them keeps the headline's arithmetic honest, and
// TestCheckStagedDistinguishesRequestedFromScanned reads exactly that gap.
func scannablePaths(requested []string) int {
	n := 0
	for _, p := range requested {
		if !scan.UnderBuiltinSkipDir(p) {
			n++
		}
	}
	return n
}

// trackedFileSet restricts fset to the paths git reports tracked. It is the one
// place a file-set run narrows the walk to the committed tree, and it has two
// consumers that must not diverge: the whole-tree invariants (#4 — an untracked
// file must not false-fail a pre-commit invariant) and an armed scope floor
// (#23 — an untracked file must not SATISFY one, which is the same asymmetry
// read from the other side; untracking a corpus was passing the pre-commit shim
// while failing a fresh clone of the identical commit).
//
// A git failure is returned, never swallowed: silently widening back to the
// working tree is what both consumers exist to prevent.
func trackedFileSet(root string, fset *scan.FileSet) (*scan.FileSet, error) {
	tracked, err := vcs.TrackedPaths(root)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]bool, len(tracked))
	for _, p := range tracked {
		allow[p] = true
	}
	return fset.Restrict(allow), nil
}

// hiddenPath is one path a file-set flag named that a declared prune channel
// removed from the scan, with the channel that did it.
type hiddenPath struct {
	path    string
	channel string
}

// absentPath is one path a file-set flag named that the scan did not produce
// and that NO declared prune channel accounts for, with the reason it was not
// there. The reason is an observation of the filesystem, not a hypothesis about
// the config — which is what separates this list from hiddenPath.
type absentPath struct {
	path   string
	reason string
}

// The reasons an unexplained path can have. They are separate because the cures
// are: put the file back, fix the spelling, or replace the worktree entry with
// the file git thinks is there.
const (
	reasonNotOnDisk  = "named by git but not present in the working tree"
	reasonUnscanned  = "present in the working tree but not produced by the scan under this spelling"
	reasonNotRegular = "git has it as a regular file, but the working-tree entry is not one — rules read the working tree, so nothing read it"
)

// requestedButAbsent partitions the paths a file-set flag named that the scan
// did not produce, into the ones a DECLARED prune channel explains and the ones
// nothing explains. Both lists are sorted by path; both mean exit 2, and they
// are kept apart only so each can print its own cure.
//
// PRECEDENCE: OBSERVATION BEATS HYPOTHESIS. Channel attribution is
// scan.NotScannedBy, which decides from the CONFIGURED GLOBS in the walk's own
// order — it is not a lookup of what the walk recorded, and it cannot be, since
// FileSet.Restrict builds its result from Files alone and carries no census to
// consult. So for a path that never reached the walk at all, a matching glob is
// a coincidence: removing the glob would not make that path scannable, and
// "narrow the glob" is an inert cure for it. os.Lstat answers the same question
// by looking, so it is asked FIRST and a path that is not on disk is reported as
// absent even when a glob would also have matched it. This changes only the
// MESSAGE — every one of these paths was exit 2 before and still is.
//
// WHAT LICENSES THE CARVE-OUTS BELOW: this guard fires only where a FILE-SET
// run would cover LESS than a WHOLE-TREE run of the same repository. A file-set
// run is a cheaper stand-in for the whole-tree run CI does, so a path the walk
// declines in BOTH modes opens no coverage gap and is not this function's
// business. Read literally, "the walk did not produce it and no channel explains
// it" says the opposite and would refuse the two entries below — do not
// re-widen it on that reading. Making the pre-commit gate stricter than the CI
// gate it stands in for blocks developers on paths CI would ignore, and buys
// nothing, because the whole-tree run will not check them either.
//
// GIT DECIDES WHAT IS A POINTER, NOT THE WORKING TREE. The oracle used to be
// os.Lstat, and that was a fail-open (#158 review round 1): stage a regular
// blob, replace the path on disk with a directory or a symlink, and the
// worktree excused a blob that commits unread. Each mode therefore asks git
// about the tree it stands in for — the INDEX under --staged, since that is
// what the commit will carry, and the RANGE'S END TREE under --range, since
// that is the tree the range is about. os.Lstat is left to DESCRIBE the
// absence, never to license it.
//
// Both modes reach the same verdict for the same entity, which is the point
// (#158 review round 2). A gitlink is a pointer whichever flag was passed, and
// formwork reads a submodule's contents in no mode at all — so refusing one
// under --range while excusing it under --staged was an asymmetry with no fact
// behind it, and it hard-failed every CI range over a submodule bump (a
// checkout without --recurse-submodules leaves the directory absent) with a
// `git restore` cure for something that was never a file.
//
// Three kinds of absence are therefore not reported at all:
//
//   - A BUILT-IN SKIP ANCESTOR (.git, .formwork) was never scannable, and every
//     config-only commit stages nothing else. Refusing there would make
//     `check --staged` unusable for the commits that edit formwork's own rules.
//     ANCESTOR is load-bearing: Walk consults skipDirs only for directories, so
//     a regular FILE named .formwork is scanned and enforced on, and excusing it
//     here contradicted the walk. scan.UnderBuiltinSkipDir is the
//     ancestor-only predicate; scan.UnderBuiltinSkip is not.
//
//   - AN ENTRY GIT ITSELF CALLS A POINTER, not a file — a symlink (120000) or a
//     submodule gitlink (160000), in either mode. scan.WalkWith appends only
//     regular files to
//     FileSet.Files, so a whole-tree `formwork check` reads these no more than a
//     file-set run does: no gap, nothing to refuse.
//
//     This carve-out DOES reach one dangerous-looking entry, and the narrowing
//     is deliberate rather than overlooked. A committed symlink whose own name
//     ends in a source extension normally fails the walk (#54) long before this
//     — but WalkWith consults ignore globs BEFORE that refusal, a trade pinned by
//     scan/ignore_test.go's TestWalkIgnoringSourceSymlinkInsideIgnoredTreeDoes
//     NotError, so inside a declared scan.ignore tree the walk succeeds and the
//     path does arrive here. Before #158 the channel guard refused it at exit 2
//     naming the glob; it is carved out now. The licensing invariant holds — the
//     same glob prunes it in a whole-tree run, so no coverage is lost — and
//     TestCheckStagedSourceSymlinkInsideAnIgnoredTreeIsNotRefused pins it so the
//     change stays a decision rather than a drift.
//
//   - A path git named that the scan DID produce. Nothing to explain.
//
// A stat that fails for any other reason (a permission error on an ancestor) is
// reported as absent rather than skipped: this function's job is to leave no
// requested path unaccounted for, and "I could not tell" is not an account.
//
// The scan.gitignore arm has not been shown to be reachable from a file-set
// mode, and no test here exercises it: vcs.IgnoredUnder asks git for UNTRACKED
// ignored paths, while everything in a changeset is in the index or a commit. It
// is kept because the contract is "any declared channel", and a channel silently
// dropped from a fail-closed check is the more expensive mistake. Its
// reachability is deliberately not asserted either way.
func requestedButAbsent(root string, requested []string, got *scan.FileSet, opts scan.Opts, gitModes map[string]string) ([]absentPath, []hiddenPath) {
	present := make(map[string]bool, len(got.Files))
	for _, f := range got.Files {
		present[f.Path()] = true
	}
	var absent []absentPath
	var hidden []hiddenPath
	for _, p := range requested {
		// EXACT MATCH FIRST, THEN THE SCAN'S OWN ANSWER (#308). `present` is a
		// string map over the walk's paths, and git hands this function the NFC
		// spelling of a filename readdir returned decomposed — so on macOS/APFS
		// a file FileSet.Restrict's fold DID produce read as unproduced here and
		// was refused at exit 2 under reasonUnscanned, whose message ("present in
		// the working tree but not produced by the scan under this spelling")
		// was false in both halves and carries no cure. scan.Produced is that
		// fold asked as a question, on the same filesystem oracle and with the
		// same narrowing; it is consulted second so an exactly-matched path costs
		// nothing and no ASCII verdict can move.
		if present[p] || got.Produced(p) || scan.UnderBuiltinSkipDir(p) {
			continue
		}
		// Lstat, not Stat: WalkDir classifies from the directory entry, so a
		// symlink is judged by the link and never by its target here either.
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
		nonRegular := err == nil && !info.Mode().IsRegular()
		if pointerEntry(gitModes[p]) {
			continue
		}
		switch {
		case errors.Is(err, iofs.ErrNotExist):
			absent = append(absent, absentPath{path: p, reason: reasonNotOnDisk})
			continue
		case err != nil:
			absent = append(absent, absentPath{path: p, reason: "could not be examined on disk: " + err.Error()})
			continue
		case nonRegular:
			// Reached only when git called this a regular file and the worktree
			// disagrees. The carve-out above has already
			// excused everything git itself calls a pointer.
			absent = append(absent, absentPath{path: p, reason: reasonNotRegular})
			continue
		}
		switch v := scan.NotScannedBy(p, opts); {
		case v.Glob != "":
			hidden = append(hidden, hiddenPath{path: p, channel: "scan.ignore (" + v.Glob + ")"})
		case v.GitRule != "":
			hidden = append(hidden, hiddenPath{path: p, channel: "scan.gitignore (" + v.GitRule + ")"})
		default:
			absent = append(absent, absentPath{path: p, reason: reasonUnscanned})
		}
	}
	sort.Slice(absent, func(i, j int) bool { return absent[i].path < absent[j].path })
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].path < hidden[j].path })
	return absent, hidden
}

// pointerEntry reports whether the mode git recorded names something git treats
// as a pointer rather than a file — which the walk declines to produce in every
// mode, so excusing it loses no coverage.
//
// The EMPTY mode is deliberately NOT a pointer. "git recorded nothing for this
// path" and "git recorded a symlink" must not read alike in a fail-closed
// check, and only the second is a reason to excuse anything; the first falls
// through to be refused.
func pointerEntry(mode string) bool {
	switch mode {
	case vcs.ModeSymlink, vcs.ModeGitlink:
		return true
	}
	return false
}

// refuseUnaccountedPaths writes the refusal for every requested path the scan
// did not produce, and reports whether the run must exit 2.
//
// BOTH blocks are emitted when both are non-empty. They name disjoint sets of
// paths, so printing one and returning would hide half of what went wrong on a
// run that is failing anyway.
//
// The accounting is PER PATH, not over the restricted set's size. A set-level
// test (`len(requested) > 0 && len(got.Files) == 0`) fires only when the
// changeset is unaccounted for in its entirety, so one visible staged file
// alongside a missing one would leave the missing one exactly as green as
// before — TestCheckStagedHiddenPathAmongVisibleOnesExits2 is that case for the
// channel arm.
func refuseUnaccountedPaths(stderr io.Writer, root, flagName, rangeSpec string, staged bool, requested []string, got *scan.FileSet, opts scan.Opts) (bool, error) {
	// git is asked ONLY when something is unaccounted for. In the ordinary case
	// every requested path was scanned, candidates is empty, and neither mode
	// makes a git call at all — which is why this costs a healthy run nothing.
	present := make(map[string]bool, len(got.Files))
	for _, f := range got.Files {
		present[f.Path()] = true
	}
	candidates := make([]string, 0, len(requested))
	for _, p := range requested {
		// The inverse of the test requestedButAbsent applies below, and it has to
		// stay the inverse (#308). A path the scan produced under a divergent
		// spelling is accounted for, so it is not a candidate — and while
		// counting it as one changes no ordinary verdict (git answers the mode
		// question fine and the classifier now skips the path anyway), it costs
		// the healthy run the invariant this comment opens with: on a git that
		// cannot answer, a phantom candidate is the difference between exit 0 and
		// a refusal naming a path count that should have been zero.
		if !present[p] && !got.Produced(p) && !scan.UnderBuiltinSkipDir(p) {
			candidates = append(candidates, p)
		}
	}
	var gitModes map[string]string
	if len(candidates) > 0 {
		var err error
		what := "end-of-range"
		if staged {
			// Scoped to the candidates: the answer is only consulted for those,
			// and an unscoped ls-files over a large index is work with no reader.
			what = "staged"
			gitModes, err = vcs.StagedModes(root, candidates)
		} else {
			// NOT scoped by pathspec, unlike the arm above: a range string may
			// legally carry its own `-- <pathspec>` tail and appending a second
			// one is malformed. The diff is already bounded by the range.
			gitModes, err = vcs.RangeModes(root, rangeSpec)
		}
		if err != nil {
			// Fail closed, per the vcs package contract. Without git's answer the
			// carve-out cannot be taken safely, and taking it anyway on a guess is
			// the fail-open being fixed.
			return false, fmt.Errorf("could not read the %s mode of %d path(s) git named: %w", what, len(candidates), err)
		}
	}
	absent, hidden := requestedButAbsent(root, requested, got, opts, gitModes)
	if len(absent) == 0 && len(hidden) == 0 {
		return false, nil
	}
	if len(hidden) > 0 {
		fmt.Fprintf(stderr, "formwork: %s named %d path(s) that no rule could see:\n", flagName, len(hidden))
		for _, h := range hidden {
			fmt.Fprintf(stderr, "  %s — hidden by %s\n", h.path, h.channel)
		}
		fmt.Fprintln(stderr, "formwork: the file set you asked for is not the file set that would be checked; narrow the glob, or drop the path from the changeset")
	}
	if len(absent) > 0 {
		fmt.Fprintf(stderr, "formwork: %s named %d path(s) the scan never produced:\n", flagName, len(absent))
		for _, a := range absent {
			fmt.Fprintf(stderr, "  %s — %s\n", a.path, a.reason)
		}
		fmt.Fprintln(stderr, "formwork: rules are evaluated over the working tree, so these paths were checked by nothing and this run cannot speak for them")
		// The cure below is "put the file back", so it is printed only when at
		// least one path is genuinely NOT THERE. reasonUnscanned means the file IS
		// on disk under some spelling and a stat error means we could not tell —
		// telling either of those operators to restore a file they already have
		// would send them looking for the wrong problem.
		missing := false
		for _, a := range absent {
			if a.reason == reasonNotOnDisk {
				missing = true
				break
			}
		}
		switch {
		case missing && staged:
			// Only under --staged is the missing content also about to be
			// committed: vcs.StagedPaths lists the index (`git diff --cached
			// --name-only --diff-filter=ACMR`), while every rule reads the
			// worktree. ACMR is why the ordinary case is quiet — a removal staged
			// with the file leaves the path out of the list entirely.
			fmt.Fprintln(stderr, "formwork: --staged takes the file list from the index and the content from the working tree — a path staged and then removed from the worktree would commit unchecked; restore it, or stage its removal")
		case missing:
			// --range commits nothing, so the index framing above would be false
			// here. What a range run hits instead is a DIRTY WORKING TREE: the
			// range names content from history that local edits have since removed.
			// The cure is to give the working tree that content back, or to run
			// the range where it already has it.
			fmt.Fprintf(stderr, "formwork: %s takes the file list from a commit range and the content from the working tree — these paths are not on disk, so restore them (git restore, or git stash) or run the range against a clean checkout\n", flagName)
		}
	}
	return true, nil
}
