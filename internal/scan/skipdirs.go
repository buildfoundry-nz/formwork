// skipdirs.go — the built-in skip set and the predicates that answer "would
// the walk have descended here?". Split from scan.go, which the 750-line
// vendor cap bounds; same package, same precedent as restrict.go.
package scan

import (
	"sort"
	"strings"
)

// Directories never scanned: VCS internals and formwork's own config/fixtures.
//
// Membership here makes a NAME eligible; it is not on its own the prune. The
// two members prune at DIFFERENT DEPTHS, and builtinSkipDir below is the only
// place that difference is decided.
var skipDirs = map[string]bool{".git": true, ".formwork": true}

// rootOnlySkipDir is the member of skipDirs that prunes only where it is a
// direct child of the walk root. Named once, here, because it is the whole of
// the asymmetry and every consult reads it from builtinSkipDir.
const rootOnlySkipDir = ".formwork"

// BuiltinSkipDirs returns the engine-level skip set, sorted. Exported so
// `formwork lint`'s escape-hatch census can NAME it rather than restate it in
// prose (#56): a hand-written "directories named .git, .formwork" drifts the
// moment this set changes, and it is the one exemption channel no rule can
// declare, so the census is the only place it is auditable at all.
//
// It reports the NAMES only, and since #268 the DEPTH differs by name — a
// caller rendering these into prose owns saying so. Widening the strings
// themselves would break the callers that compare them to a path segment.
func BuiltinSkipDirs() []string {
	out := make([]string, 0, len(skipDirs))
	for d := range skipDirs {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// builtinSkipDir reports whether the walk prunes a DIRECTORY at this
// walk-root-relative, slash-separated path. It is the single source of the
// depth rule: the walk's d.IsDir() branch, its symlink subtree-hiding refusal,
// NotScannedBy's ancestor loop and the two exported Under* predicates all
// decide here, because four copies of a depth test is how the walk and its own
// attribution come to disagree about whether a path was ever looked at.
//
// `.git` prunes AT ANY DEPTH. It is VCS internals wherever it appears — a
// submodule's, a vendored clone's, a linked worktree's — and none of it is
// governed source.
//
// `.formwork` prunes ONLY AS A DIRECT CHILD OF THE WALK ROOT. At the root it is
// the engine's own config and fixtures, and skipping it is load-bearing:
// .formwork/fixtures holds deliberately-broken fire fixtures whose job is to
// contain violations, so scanning them would make every rule fail on its own
// test data and `formwork test` could not exist. Deeper in the tree it is
// something else — a ported corpus under examples/, a vendored subproject —
// and it is ordinary governed content. The fixture argument does not reach it,
// because the run that owns those fixtures is the one rooted AT that directory
// (`formwork test -C examples/<corpus>`, which `make selftest` and `make lint`
// both loop), where the same directory is a root child again and skipped again.
//
// Pruning by basename at any depth conflated the two and cost this repository
// the gate it rests on (#268): 2,474 of the 2,492 tracked files under examples/
// live in examples/<corpus>/.formwork/, so the permanent vocabulary rule scoped
// `examples/**` — the one #126 promised would fail `make check` "forever" on a
// reintroduced token — could see 18 of them, and none at all of the 2,399-file
// corpus where a newly ported rule is actually written.
func builtinSkipDir(rel string) bool {
	base := rel[strings.LastIndexByte(rel, '/')+1:]
	if !skipDirs[base] {
		return false
	}
	if base == rootOnlySkipDir {
		return rel == rootOnlySkipDir
	}
	return true
}

// ScopeRootedUnderSkipDir reports whether EVERY glob is rooted at a literal
// first segment the engine never scans — so no path the walk can produce will
// ever match, whatever else is in the repository.
//
// A literal LEADING segment is the one position where both members of the skip
// set prune alike (`.git` prunes anywhere, `.formwork` at the root), so testing
// the set directly here is exact rather than approximate.
//
// One-sided by construction: it decides only on a literal leading segment, so
// `**/.git/**` (which also can never match) is not detected. That is
// deliberate. This exists to explain an already-reported empty scope, and a
// wrong "the skip set is why" on a rule with an ordinary broken glob would send
// the reader somewhere there is nothing to find. `**/.formwork/**` is NOT in
// that category since #268 — it matches every ported corpus's rules — and
// reporting it would be the opposite error.
func ScopeRootedUnderSkipDir(globs []string) bool {
	if len(globs) == 0 {
		return false
	}
	for _, g := range globs {
		seg := g
		if i := strings.IndexAny(g, "/"); i >= 0 {
			seg = g[:i]
		}
		if !skipDirs[seg] {
			return false
		}
	}
	return true
}

// UnderBuiltinSkip reports whether any component of the slash-separated,
// root-relative path is a built-in skip directory — by the same predicate, at
// the same depths, that Walk prunes with before scan.ignore is consulted.
// Callers reasoning about paths the walk never enumerated (e.g. lint's
// tracked-path cross-check, #90) use this to avoid attributing a built-in skip
// to an operator glob.
//
// Each ANCESTOR PREFIX is tested, not each bare segment: `.formwork` prunes
// only at the walk root since #268, and a bare segment carries no depth. A
// nested corpus's `.formwork` is scanned content, and reporting it skipped here
// would excuse it from the very check that asks whether an operator glob is
// hiding tracked files.
func UnderBuiltinSkip(path string) bool {
	for _, prefix := range ancestorPrefixes(path) {
		if builtinSkipDir(prefix) {
			return true
		}
	}
	return false
}

// UnderBuiltinSkipDir reports whether an ANCESTOR of path is a built-in skip
// directory — the question a caller reasoning about a FILE path must ask, and
// the one UnderBuiltinSkip above answers too loosely.
//
// The leaf is deliberately excluded. Walk consults the skip set in its
// d.IsDir() branch and again in the symlink refusal (where the answer is to
// skip a linked .git/.formwork rather than refuse it) — never in its
// REGULAR-FILE branch, so a regular file named .git or .formwork is scanned and
// enforced on like any other file; calling it "under a built-in skip"
// contradicts the walk's own verdict. NotScannedBy has written the `!leaf`
// guard for exactly this reason since #119, and a caller that reached for
// UnderBuiltinSkip instead reintroduced the contradiction as a fail-open: a
// staged .formwork FILE the walk would have failed on was excused from #158's
// accounting.
//
// Both predicates are kept because they answer different questions. Lint's
// tracked-path cross-check (#90) asks about paths it has not classified as files
// or directories, where the looser test is the conservative one; this asks about
// a path already known to be a file in git's index or a commit.
func UnderBuiltinSkipDir(path string) bool {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return false // a bare leaf has no ancestors
	}
	return UnderBuiltinSkip(path[:i])
}
