// skipdirs.go — the built-in skip set and the predicates that answer "would
// the walk have descended here?". Split from scan.go, which the 750-line
// vendor cap bounds; same package, same precedent as restrict.go.
package scan

import (
	"strings"
)

// Directories never scanned: VCS internals, and the walk root's fire and pass
// fixtures.
//
// `.git` prunes AT ANY DEPTH. It is VCS internals wherever it appears — a
// submodule's, a vendored clone's, a linked worktree's — and none of it is
// governed source.
var skipDirs = map[string]bool{".git": true}

// fixturesDir prunes ONLY AT THE WALK ROOT: the repository's own rule fixtures.
// A fire fixture exists to contain a violation, so scanning it would fail every
// rule on its own test data, and each fixture is already judged — by the one
// rule it belongs to, under `formwork test`, rooted at the fixture.
//
// IT IS THE ONLY PART OF .formwork THE WALK SKIPS (#24467 item 9). The rest —
// formwork.yaml, the rule YAML, mutation specs — is the repository's governance,
// and a rule may now read it like any other file, which is what a check over
// the corpus itself had to shell out for (`type: command`) while the walk could
// not see it. Before, the whole root `.formwork` was pruned (#268 made that
// root-only: a ported corpus's `.formwork` under examples/ is ordinary content,
// and its fixtures with it, because the run that owns those fixtures is the one
// rooted at that corpus).
const fixturesDir = ".formwork/fixtures"

// BuiltinSkipDirs returns the engine-level skip set, sorted: the name `.git`,
// pruned at any depth, and the root path `.formwork/fixtures`. Exported so
// `formwork lint`'s escape-hatch census can NAME it rather than restate it in
// prose (#56): it is the one exemption channel no rule can declare, so the
// census is the only place it is auditable at all. BuiltinSkipSummary says
// which depth each prunes at.
func BuiltinSkipDirs() []string {
	return []string{fixturesDir, ".git"}
}

// BuiltinSkipSummary renders the skip set with the depth each member prunes
// at, for prose that must not restate it by hand.
func BuiltinSkipSummary() string {
	return "directories named .git (any depth) and the root's " + fixturesDir
}

// builtinSkipDir reports whether the walk prunes a DIRECTORY at this
// walk-root-relative, slash-separated path. It is the single source of the
// depth rule: the walk's d.IsDir() branch, its symlink subtree-hiding refusal,
// NotScannedBy's ancestor loop and the two exported Under* predicates all
// decide here, because four copies of a depth test is how the walk and its own
// attribution come to disagree about whether a path was ever looked at.
func builtinSkipDir(rel string) bool {
	if rel == fixturesDir {
		return true
	}
	return skipDirs[rel[strings.LastIndexByte(rel, '/')+1:]]
}

// ScopeRootedUnderSkipDir reports whether EVERY glob is rooted at a literal
// prefix the engine never scans — a leading `.git` segment, or a leading
// `.formwork/fixtures` — so no path the walk can produce will ever match,
// whatever else is in the repository.
//
// One-sided by construction: it decides only on a literal leading prefix, so
// `**/.git/**` (which also can never match) is not detected. That is
// deliberate. This exists to explain an already-reported empty scope, and a
// wrong "the skip set is why" on a rule with an ordinary broken glob would send
// the reader somewhere there is nothing to find.
func ScopeRootedUnderSkipDir(globs []string) bool {
	if len(globs) == 0 {
		return false
	}
	for _, g := range globs {
		seg := g
		if i := strings.IndexByte(g, '/'); i >= 0 {
			seg = g[:i]
		}
		if !skipDirs[seg] && g != fixturesDir && !strings.HasPrefix(g, fixturesDir+"/") {
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
// Each ANCESTOR PREFIX is tested, not each bare segment: `.formwork/fixtures`
// prunes only at the walk root, and a bare segment carries no depth. A nested
// corpus's fixtures are scanned content, and reporting them skipped here would
// excuse them from the very check that asks whether an operator glob is hiding
// tracked files.
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
// skip a linked .git or root fixtures directory rather than refuse it) — never in its
// REGULAR-FILE branch, so a regular file at a skipped path is scanned and
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
