package vcs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The argv of the three rev-parse questions PAIRED WITH AN EXPORTED READER —
// HooksPath and CommonDir below, TopLevel in vcs.go — named once. "Three" counts
// those pairings and not every layout question this package puts to git: env.go
// asks two more of its own, the repoIdentity and bareRepoIdentity variants of
// `rev-parse --path-format=absolute --git-dir --git-common-dir [--show-toplevel]`,
// whose argv is spelled there because nothing exported returns it.
//
// configenv.go's HooksPathQuestion, TopLevelQuestion and CommonDirQuestion — the
// ConfigEnvQuestion values a caller names — carry these same slices, so a
// measurement of "did the environment move this answer" is provably about the
// answer the function beside it returns rather than about a second spelling of a
// similar question.
var (
	hooksPathArgs = []string{"rev-parse", "--git-path", "hooks"}
	topLevelArgs  = []string{"rev-parse", "--show-toplevel", "--show-prefix"}
	commonDirArgs = []string{"rev-parse", "--git-common-dir"}
)

// HooksPath returns the absolute directory git will actually run hooks from in
// the repository containing root.
//
// It asks `rev-parse --git-path hooks` rather than reading core.hooksPath,
// because the question a caller has is "what will git DO", not "what string is
// configured". git resolves the key across every scope, applies its own default
// when nothing is set, and answers for the repository it lands in. There is no
// unset case to disambiguate: measured on git 2.50.1, a repository that never
// set core.hooksPath answers `.git/hooks` rather than failing.
//
// THE filepath.IsAbs BRANCH IS LOAD-BEARING, not defensive. git's answer is
// sometimes relative and sometimes absolute, and the absolute case arrives
// without anyone configuring an absolute path — measured on git 2.50.1, inside a
// linked worktree with core.hooksPath unset the answer is the MAIN repository's
// absolute `.git/hooks`, while the main worktree returns the relative
// `.git/hooks`. filepath.Join(root, "/abs/path") concatenates the two into a
// directory that cannot exist, so a caller reading files there is looking
// somewhere that was never real.
//
// `--path-format=absolute` would remove the branch and is deliberately not used:
// it needs git >= 2.31, and rev-parse ECHOES an option it does not recognise
// rather than failing, so on an older git the flag would come back as part of
// the answer instead of announcing the version floor.
//
// Any git failure is an error (package contract): a caller must never fall back
// to a guessed directory, since certifying hooks at the wrong directory is the
// whole defect this exists to close.
//
// The result is a path to OPEN, and it is absolute in BOTH arms — the relative
// one through absJoin, which is there because callers do compare these strings
// even though they cannot be compared as identities.
//
// AN IDENTITY IS WHAT THIS IS NOT. Both arms normalise lexically only, so a `..`
// component traversing a symlink resolves differently here than the kernel
// resolves it for git. That is the hazard EnsureTopLevel documents in vcs.go at
// its filepath.Abs/EvalSymlinks note. Normalising makes the two arms agree on
// one spelling, which a diagnostic line needs; a caller that needs to know
// whether two of these name the same directory resolves symlinks itself, as
// internal/hooks does in resolvedKey.
func HooksPath(root string) (string, error) {
	out, err := git(root, hooksPathArgs...)
	if err != nil {
		return "", err
	}
	// Strip only the line terminator. No TrimSpace: this package's parser
	// contract keeps trailing-space names intact (vcs.go: diffPaths, and
	// EnsureTopLevel's "NO TRIMMING BEYOND THE LINE TERMINATOR"), and a
	// directory legally named "hooks " must not become "hooks".
	p := strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
	if p == "" {
		// Not reachable through git today — measured, rev-parse either answers
		// or exits non-zero — and therefore untested, for the same reason
		// StagedModes' conflict fold in vcs.go is. Kept because the fallthrough is a
		// fail-open of the exact shape this function exists to close:
		// filepath.Join(root, "") is root, so an empty answer would certify the
		// repository root as the hooks directory.
		return "", fmt.Errorf("git rev-parse --git-path hooks: empty answer for -C %s", root)
	}
	if filepath.IsAbs(p) {
		// Clean normalises the trailing separator git echoes back when
		// core.hooksPath was spelled with one; absJoin does the same for the
		// relative arm.
		return filepath.Clean(p), nil
	}
	return absJoin(root, p)
}

// absJoin joins git's relative answer onto root and makes the result absolute.
//
// ABSOLUTE IS NOT WHAT filepath.Join GIVES: `Join(root, p)` is relative whenever
// root is, and root is relative on the commonest invocation there is — the CLI's
// default root is ".", which is what `formwork hooks verify` passes when nobody
// types -C. Measured, in a repository with one shim deleted: the caller's root
// check reported `.formwork/hooks/pre-commit` while its worktree loop, which
// asks with git's own absolute worktree path, reported the same file absolutely.
// Those two strings are what internal/hooks dedupes on, so one missing shim was
// reported twice — a doc comment promising "absolute" and an implementation that
// did not, disagreeing where nothing failed loudly.
//
// filepath.Abs, never EvalSymlinks: this changes the SPELLING and not the target,
// so the path still names what the caller's working directory said it named.
// Resolving symlinks here would be a different path with different `..`
// semantics — the hazard the note above describes.
//
// A Getwd failure is an error. Handing back a path the caller will treat as
// absolute when it is not is the defect this function exists to close.
func absJoin(root, p string) (string, error) {
	return filepath.Abs(filepath.Join(root, p))
}

// CommonDir returns the absolute path of the git directory SHARED by every
// worktree of the repository containing root — the directory holding the hooks
// git falls back to when core.hooksPath is unset.
//
// `--git-common-dir`, never `--git-dir`. In a linked worktree the latter names
// the per-worktree directory, whose `hooks/` does not exist, while the hooks git
// would actually run live under the common directory — measured on git 2.50.1,
// where `--git-common-dir` from a linked worktree answers the MAIN repository's
// absolute `.git`. A caller reading the per-worktree answer concludes the
// repository has no hooks of its own, which is the wrong answer in the
// dangerous direction: it is a decision about whether someone else's gate is
// about to be overridden.
//
// The same IsAbs branch as HooksPath, for the same reason and with the same
// measurements behind it: from a subdirectory the answer is the relative
// `../.git`, from a linked worktree it is absolute. It absolutises through the
// same absJoin, so "the absolute path" above is a promise both arms keep rather
// than one the relative arm quietly broke.
func CommonDir(root string) (string, error) {
	out, err := git(root, commonDirArgs...)
	if err != nil {
		return "", err
	}
	// Strip only the line terminator — see HooksPath.
	p := strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
	if p == "" {
		// Unreachable through git and therefore untested, on the same measurement
		// as HooksPath's guard above: rev-parse either answers or exits non-zero.
		// Kept for the same class of reason — absJoin(root, "") is root, so an
		// empty answer would hand back the repository root as the shared git
		// directory, and internal/hooks' shadowedHookProblems joins "hooks" onto
		// it and compares root/hooks against the directory git names.
		return "", fmt.Errorf("git rev-parse --git-common-dir: empty answer for -C %s", root)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return absJoin(root, p)
}

// Worktree is one entry of `git worktree list`, reduced to the path plus the
// three states that decide whether it is a hook site at all.
//
// Path is the path exactly as git reported it — no cleaning, no case folding,
// no separator rewriting — so it can be compared against and joined with git's
// other answers without a second normalisation disagreeing with the first.
type Worktree struct {
	// Path is the worktree's root directory.
	Path string
	// Bare marks a repository with no working tree.
	Bare bool
	// Prunable marks an entry git would drop on `git worktree prune`. git still
	// LISTS it, at exit 0 — it is not the same thing as a worktree that exists
	// but is missing files.
	//
	// IT IS A VERDICT ON THE REGISTRATION, NOT ON THE DIRECTORY, and a caller
	// that reads it as "the directory is gone" is wrong in both directions.
	// Measured on git 2.50.1: deleting a worktree's `.git` file makes the entry
	// prunable while every file in it stays where it was; moving the whole
	// worktree makes it prunable while the worktree keeps working — git lists
	// the OLD path, and a commit in the new location succeeds.
	Prunable bool
	// PrunableReason is git's own words for why, empty when git gave none.
	//
	// Carried rather than discarded because the states above have opposite
	// cures — prune deregisters a worktree that `git worktree repair` would
	// fix — and a caller that has only the flag has to invent a reason.
	PrunableReason string
	// Locked marks an entry the operator locked with `git worktree lock`.
	Locked bool
}

// Worktrees returns every worktree git reports for the repository containing
// root, in git's own order. It does not require root to be the top level: the
// question is about the repository, and a caller diagnosing a subdirectory root
// still needs the answer.
//
// Any git failure is an error, and so is any record that does not parse — see
// parseWorktrees.
func Worktrees(root string) ([]Worktree, error) {
	out, err := git(root, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out)
}

// parseWorktrees decodes `worktree list --porcelain -z`.
//
// -z IS REQUIRED, and the plain porcelain format is not merely inconvenient
// without it. Measured on git 2.50.1: the porcelain writer emits the worktree
// path raw, so a worktree at a path containing a newline spills across lines. A
// line-splitting parser does not notice — it reads the first line as the whole
// path and treats the remainder as an attribute it does not recognise — and
// silently reports a DIFFERENT, possibly existing, directory. -z terminates
// every attribute with a NUL instead, which a path cannot contain, so the path
// arrives intact. This package already treats that class as load-bearing rather
// than exotic: see diffPaths (#96) and TrackedUnder.
//
// The -z grammar, measured: each attribute is NUL-terminated, and an EMPTY
// attribute ends a record. Every record's first attribute is `worktree <path>`;
// the rest are `HEAD <sha>`, `branch <ref>`, `detached`, `bare`, `locked
// [reason]`, `prunable <reason>`. A bare repository's record carries only
// `worktree <path>` and `bare`.
//
// FAIL CLOSED: a record whose first attribute is not a worktree path is an
// error, never a skipped entry. Skipping is the fail-open — the dropped entry
// may be a worktree that exists on a branch without hooks installed, and a
// caller that never sees it reports the repository wired.
//
// Attributes beyond the four this struct models are ignored rather than
// rejected, so a future git adding one does not turn every call into an error.
func parseWorktrees(out string) ([]Worktree, error) {
	var wts []Worktree
	cur := -1 // index into wts of the record being read; -1 = between records
	for _, attr := range strings.Split(out, "\x00") {
		if attr == "" {
			// End of record — also the trailing field the final NUL leaves.
			cur = -1
			continue
		}
		if cur < 0 {
			path, ok := strings.CutPrefix(attr, "worktree ")
			if !ok || path == "" {
				return nil, fmt.Errorf("git worktree list --porcelain -z: record does not begin with a worktree path: %q", attr)
			}
			wts = append(wts, Worktree{Path: path})
			cur = len(wts) - 1
			continue
		}
		// `locked` and `prunable` may carry a reason after a space; `bare` never
		// does. Key on the attribute name either way.
		name, rest, _ := strings.Cut(attr, " ")
		switch name {
		case "bare":
			wts[cur].Bare = true
		case "prunable":
			wts[cur].Prunable = true
			wts[cur].PrunableReason = rest // "" when git gave no reason
		case "locked":
			wts[cur].Locked = true
		}
	}
	return wts, nil
}
