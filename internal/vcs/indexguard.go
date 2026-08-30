package vcs

import (
	"os"
	"sort"
	"strings"
)

// IndexEnvVar is the variable that decides which index git answers from. It is
// deliberately NOT in scrubbedGitVars: git points it at a temporary index during
// a partial commit and then runs pre-commit, so removing it would make --staged
// judge a file set that is not the one being committed (env.go records the
// measurement, TestGitCallsPreserveGitIndexFile pins it).
//
// This file is the other half of that decision. Preserving the variable is right
// for the CHANGESET question — "what is about to be committed" — and wrong for
// the two questions that are about what the REPOSITORY tracks: the PRUNE
// question, "what may the walk decline to read" (IgnoredUnder, CheckIgnored),
// and the TRACKED-SET question the scan-ignore-tracked check asks (TrackedUnder,
// vcs.go). #175 is that difference: with GIT_INDEX_FILE naming a well-formed
// empty index, git reports nothing tracked, `check-ignore` calls a COMMITTED
// file ignored, and `check` exits 0 over it.
//
// Those three are every answer in this package that reads an index; the
// remaining GIT_INDEX_FILE readers (StagedPaths, StagedModes and their callers)
// are the changeset question and must keep the ambient answer. The two verdicts
// below run in OPPOSITE directions — keepAgreedPrunes intersects, unionTracked
// unions — because fail-closed follows the consumer, not the variable; each
// says why at its own doc.
const IndexEnvVar = "GIT_INDEX_FILE"

// indexFrame says which index the git calls behind one ignore question read
// from. It exists so the #175 cross-check can ask the SAME question twice, and
// it is a type with methods rather than an environment slice passed around so
// that gitEnvFor still runs on every call — the exec-site altitude env.go argues
// for, which a caller holding a prepared environment could quietly skip.
type indexFrame bool

const (
	// ambientIndex is the environment as the caller has it, GIT_INDEX_FILE
	// included: git's answer about the index that is about to become the commit.
	ambientIndex indexFrame = false
	// repositoryIndex removes GIT_INDEX_FILE, so git answers from the index the
	// repository itself has.
	repositoryIndex indexFrame = true
)

// indexMoved reports whether anything has pointed git at another index. Where
// nothing has, the two frames are the same environment and the cross-check is
// skipped entirely — no extra git calls on the ordinary run, which is every
// whole-tree `check` and every CI invocation.
func indexMoved() bool {
	_, set := os.LookupEnv(IndexEnvVar)
	return set
}

// git runs one git command in this frame.
func (f indexFrame) git(root string, args ...string) (string, error) {
	if f == ambientIndex {
		return git(root, args...)
	}
	env, err := repoIndexEnvFor(root)
	if err != nil {
		return "", err
	}
	out, _, err := gitExitEnv(env, root, args...)
	return out, err
}

// gitStdin runs one git command with input on stdin in this frame, returning
// check-ignore's exit code alongside the error for confirmIgnored's sake.
func (f indexFrame) gitStdin(root, input string, args ...string) (string, int, error) {
	if f == ambientIndex {
		return gitStdin(root, input, args...)
	}
	env, err := repoIndexEnvFor(root)
	if err != nil {
		return "", -1, err
	}
	return gitStdinEnv(env, root, input, args...)
}

// repoIndexEnvFor is gitEnvFor plus the removal of GIT_INDEX_FILE, so the
// environment policy still decides everything it decided before and this changes
// exactly one variable on top of it.
//
// A nil answer from gitEnvFor is os/exec's "inherit the parent's environment",
// which is what the FORMWORK_GIT_ENV hatch asks for — so the variable has to be
// removed from a materialised copy of os.Environ() there rather than from nil.
// The hatch is about the repository-pointer family; it is not an instruction to
// prune a path this repository tracks.
func repoIndexEnvFor(root string) ([]string, error) {
	base, err := gitEnvFor(root)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base))
	for _, kv := range base {
		if name, _, ok := strings.Cut(kv, "="); ok && name == IndexEnvVar {
			continue
		}
		out = append(out, kv)
	}
	return out, nil
}

// keepAgreedPrunes is the verdict: a path may be pruned only where BOTH indexes
// call it ignored. It returns the records from the repository's own index that
// the ambient answer also covers.
//
// NEITHER ANSWER ALONE IS RIGHT, and that is why this is an intersection rather
// than a choice. Each direction was measured on git 2.50.1:
//
//   - The AMBIENT answer alone is #175. Under an index that tracks less than the
//     repository's — a well-formed empty one, or the temporary index of a
//     partial commit, which holds HEAD plus the named paths and so omits a
//     force-added file — git calls a tracked path untracked, `ls-files
//     --directory` collapses the whole subtree, and check-ignore confirms the
//     collapsed directory. A committed file is pruned and never scanned.
//   - The REPOSITORY'S answer alone fails the other way. A path in HEAD that has
//     been `git rm --cached`ed is absent from the real index and present in a
//     partial commit's temporary one — so the repository's index calls it
//     ignored while it is still going into the commit, and taking that answer
//     would prune a file the changeset carries.
//
// THE INTERSECTION IS OVER COVERED PATHS, NOT OVER RECORDS, because the two runs
// do not describe the same tree at the same granularity: `ls-files --directory`
// collapses a wholly-untracked directory to one entry, so the ambient side of
// the #175 shape is a single `build` record while the repository's side is the
// individual files under it. Comparing records would drop both and prune nothing
// — fail-closed, but it would also throw away every legitimate prune in any
// repository holding a force-added file. Asking whether the ambient answer
// COVERS each repository-side record keeps the finer answer and drops exactly
// the paths one index calls ignored and the other calls tracked.
//
// The kept record is the REPOSITORY'S, so the ignore rule reported for a prune
// is the one that decided it under the index the repository actually has.
func keepAgreedPrunes(repoSide, ambient []IgnoredPath) []IgnoredPath {
	if len(repoSide) == 0 || len(ambient) == 0 {
		return nil
	}
	var kept []IgnoredPath
	for _, r := range repoSide {
		if coveredBy(ambient, r.Path) {
			kept = append(kept, r)
		}
	}
	return kept
}

// unionTracked is the verdict for the OTHER question this variable moves: a path
// either index calls tracked is tracked. Both inputs are sorted and deduped
// (TrackedUnder's parser), and so is the result.
//
// THE DIRECTION IS OPPOSITE TO keepAgreedPrunes ABOVE, and deliberately, because
// the fail-closed direction follows the consumer rather than the function. A
// prune DECLINES to read a file, so agreement is what makes declining safe; the
// tracked set is what scan-ignore-tracked (#90) reports a violation FROM, so the
// safe answer is the larger one. Intersecting here would reproduce #175 in this
// seam exactly: under an empty ambient index the intersection is empty, no path
// is tracked, and lint reports clean over a committed file a scan.ignore glob
// hides. Each index catches what the other misses — the repository's holds a
// force-added file the temporary index of a partial commit omits, the ambient
// one holds a path in HEAD that `git rm --cached` dropped from the real index
// and the commit still carries.
//
// The cost of the union is a false POSITIVE, not a false pass: a path only one
// index tracks is reported, and the operator is told about a real committed path
// their scan.ignore hides. That is the trade this package takes everywhere.
func unionTracked(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, p := range list {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// coveredBy reports whether recs already prune path — as that exact path, or as
// a directory record above it. Directory records are why this is not a set
// membership test: a `build` record prunes `build/a.txt` without naming it.
//
// The prefix is built with an explicit "/" so `buildx.txt` is not read as being
// under `build`. Paths here are slash-separated and carry no trailing slash
// (IgnoredPath's contract), so that is the only separator that can appear.
func coveredBy(recs []IgnoredPath, path string) bool {
	for _, r := range recs {
		if r.Path == path {
			return true
		}
		if r.Dir && strings.HasPrefix(path, r.Path+"/") {
			return true
		}
	}
	return false
}
