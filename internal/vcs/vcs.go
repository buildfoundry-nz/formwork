// Package vcs is formwork's thin, fail-closed git seam: it turns a working
// tree plus a change selector (staged, or a commit range) into the set of
// changed paths a lane should scan (spec §8). Every git failure is an error —
// callers must never fall back to scanning the whole tree, which could let a
// gate pass by looking at the wrong file set.
package vcs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// StagedPaths returns the repo-root-relative, slash-separated paths staged for
// commit (git index vs HEAD), added/copied/modified/renamed only — a deleted
// file cannot be scanned. root must be the repository top-level.
//
// The ACMR filter answers "which paths can be read", which is the question a
// scanner asks. A caller that classifies a CHANGE rather than reading one wants
// StagedPathsAnyStatus instead: deleting a source file is a change, and the
// filter hides it (#147).
func StagedPaths(root string) ([]string, error) {
	return stagedPaths(root, "--diff-filter=ACMR")
}

// StagedPathsAnyStatus is StagedPaths without the status filter, and with
// rename detection off: every path git reports as differing between index and
// HEAD, deletions and type changes included, and for a rename BOTH the source
// and the destination.
//
// Unfiltered is the fail-closed direction for its callers, and that is why the
// filter is absent rather than widened to a longer letter list. Every status
// this admits and ACMR excludes is a difference in the INDEX against HEAD — a
// deletion, a typechange, or an unmerged entry, which git reports even where the
// "ours" stage matches HEAD byte for byte. A caller deciding how much to check
// from this set can only be made more cautious by seeing more of them, never
// less, and that is the property this rests on.
//
// --no-renames is the same argument reached by a different mechanism, and it is
// why dropping the filter alone was not enough. With detection on, git reports a
// rename as ONE path — the destination — so the source is not filtered out, it
// is never emitted: `git mv src/api.go docs/api.md` came back as docs/api.md
// alone, and the renamed-away .go file went unseen. Worse, detection is operator
// configuration (diff.renames), so identical bytes gave different answers on
// different machines; --no-renames pins the widening spelling.
func StagedPathsAnyStatus(root string) ([]string, error) {
	return stagedPaths(root, "--no-renames")
}

func stagedPaths(root string, extra ...string) ([]string, error) {
	if err := EnsureTopLevel(root); err != nil {
		return nil, err
	}
	return diffPaths(root, append([]string{"diff", "--cached", "--name-only"}, extra...))
}

// RangePaths returns the paths changed across a commit range (e.g. "A..B" or
// "origin/main HEAD"), added/copied/modified/renamed only. The range string is
// tokenized into separate git arguments by splitRange (rangespec.go), which
// honours shell-style quoting so a `-- <pathspec>` tail can name a path
// containing a space; an unclosed quote is an error, never a guess (#99). root
// must be the top-level.
//
// Same split as StagedPaths: this is the readable-paths answer. See
// RangePathsAnyStatus for the classify-a-change one.
func RangePaths(root, rng string) ([]string, error) {
	return rangePaths(root, rng, "--diff-filter=ACMR")
}

// RangePathsAnyStatus is RangePaths without the status filter and with rename
// detection off — the range analogue of StagedPathsAnyStatus, widened both ways
// for that function's reasons.
func RangePathsAnyStatus(root, rng string) ([]string, error) {
	return rangePaths(root, rng, "--no-renames")
}

func rangePaths(root, rng string, extra ...string) ([]string, error) {
	fields, err := splitRange(rng)
	if err != nil {
		return nil, err
	}
	if err := EnsureTopLevel(root); err != nil {
		return nil, err
	}
	args := append(append([]string{"diff", "--name-only"}, extra...), fields...)
	return diffPaths(root, args)
}

// TrackedPaths returns every file git tracks under root (git ls-files),
// repo-relative and forward-slashed. Whole-tree-invariant rules are evaluated
// over this tracked set under a --staged/--range scan, so a pre-commit hook
// never false-fails on an untracked working-tree file the developer is not
// committing (#4). root must be the repository top-level.
func TrackedPaths(root string) ([]string, error) {
	if err := EnsureTopLevel(root); err != nil {
		return nil, err
	}
	return diffPaths(root, []string{"ls-files"})
}

// TrackedUnder returns every git-tracked file at or below root, relative to
// root and forward-slashed. Unlike TrackedPaths it does not require root to
// be the repository top-level: git ls-files reports paths relative to its
// cwd, which is exactly the root-relative frame lint's scan uses (and keeps
// the check usable if lint ever runs over a corpus that is a repo subdir,
// #89). git -C resolves to the NEAREST ancestor repository, so a root that
// is not itself a repo answers for the repo that contains it — truthfully
// empty when nothing is tracked under root.
//
// The output is read with -z (NUL-terminated): default core.quotePath
// C-quotes any path carrying a non-ASCII or control byte, and a line-split
// parser would return the quoted spelling — a string that can never match a
// scan path, which for this function's caller means a silently passed
// bypass (#90 review). --stage exposes the mode so submodule gitlinks
// (160000) are excluded — they are pointers, not files of this repo — and
// merge-conflict stage duplicates collapse via the dedup.
//
// Index semantics: a staged-but-uncommitted file is tracked. Any git failure
// is an error; callers must fail, never fall back to "nothing tracked"
// (package contract).
//
// WHICH index, though, is decided by GIT_INDEX_FILE — the #175 defect, and this
// is its third seam alongside IgnoredUnder and CheckIgnored. Under an index that
// tracks less than the repository's, a committed file drops out of this answer
// and scan-ignore-tracked (#90) reports clean over it. Where the variable is
// set, the question is asked again with it removed and the two answers are
// UNIONED (unionTracked, indexguard.go) — not intersected as the prune side is;
// see there for why the fail-closed direction is opposite. Unset, nothing is
// compared and no extra git call is made.
func TrackedUnder(root string) ([]string, error) {
	ambient, err := trackedUnder(root, ambientIndex)
	if err != nil {
		return nil, err
	}
	if !indexMoved() {
		return ambient, nil
	}
	repoSide, err := trackedUnder(root, repositoryIndex)
	if err != nil {
		return nil, err
	}
	return unionTracked(ambient, repoSide), nil
}

// trackedUnder is TrackedUnder's single-frame answer, which is the unit the #175
// cross-check runs twice.
func trackedUnder(root string, frame indexFrame) ([]string, error) {
	out, err := frame.git(root, "ls-files", "-z", "--stage")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			return nil, fmt.Errorf("git ls-files --stage: malformed record %q", rec)
		}
		if strings.HasPrefix(meta, "160000 ") {
			continue // submodule gitlink: a pointer, not a file of this repo
		}
		p := filepath.ToSlash(path)
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// INDEX ENTRY modes, as git records them. Those four are the whole set an index
// entry can carry. A TREE is a wider vocabulary — it also holds 040000 for a
// subtree — so this is deliberately not stated as "what git stores"; RangeModes
// below reads tree-side modes and would make that sentence false.
const (
	ModeBlob       = "100644"
	ModeExecutable = "100755"
	ModeSymlink    = "120000"
	ModeGitlink    = "160000"
)

// StagedModes returns the mode git has recorded in the INDEX for each of paths,
// keyed by path. Paths with no index entry are absent from the result rather
// than defaulted, so a caller can tell "git says symlink" from "git says
// nothing" — those must not read alike in a fail-closed check.
//
// It exists because the worktree and the index disagree, and under --staged it
// is the INDEX that is about to become a commit. A caller deciding whether a
// path is a file worth scanning cannot ask os.Lstat: replace a staged regular
// blob with a directory or a symlink on disk and the worktree answers for an
// entry that will never be committed (#158 review round 1).
//
// A merge conflict lists one record per stage, and the fold below keeps the LAST
// non-pointer mode seen — so stages [100644, 100755] yield 100755, not the
// regular blob. What it guarantees is only that a pointer stage never displaces
// a file stage, which is the direction that matters. THAT BRANCH DOES NOT FIRE THROUGH
// TODAY'S ONLY CALLER, and the comment says so rather than implying otherwise:
// StagedPaths filters --diff-filter=ACMR, which excludes unmerged (U) entries,
// so a conflicted path never reaches this function. It is kept because this is
// an exported helper whose contract is "the mode git recorded", a second caller
// need not filter the same way, and the fold is the fail-closed direction — it
// can only keep a caller scanning, never excuse a path. It is untested for the
// same reason it is unreachable.
//
// The pathspec is scoped to the caller's paths and terminated with `--`, so a
// path that begins with a dash cannot be read as an option. Empty input makes no
// git call: `ls-files` with an empty pathspec after `--` lists the whole index,
// which would be a silently much larger answer than was asked for.
//
// root must be the repository top-level, checked here rather than assumed —
// like the other CHANGESET helpers (StagedPaths, RangePaths, TrackedPaths,
// RangeModes, Diff), whose output is intersected with repo-root-relative scan
// paths. Not like the ...Under family, which is root-relative by design and
// must stay that way; see TrackedUnder.
//
// The production caller has already run StagedPaths, which checks it — but
// relying on that makes correctness depend on call ORDER in another package,
// and a future caller reaching this first would get subdir-relative keys that
// match no scan path and silently look like "git recorded nothing".
func StagedModes(root string, paths []string) (map[string]string, error) {
	out := make(map[string]string, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	if err := EnsureTopLevel(root); err != nil {
		return nil, err
	}
	args := append([]string{"ls-files", "-z", "--stage", "--"}, paths...)
	raw, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	for _, rec := range strings.Split(raw, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			return nil, fmt.Errorf("git ls-files --stage: malformed record %q", rec)
		}
		mode, _, ok := strings.Cut(meta, " ")
		if !ok {
			return nil, fmt.Errorf("git ls-files --stage: no mode in %q", meta)
		}
		p := filepath.ToSlash(path)
		if prev, seen := out[p]; seen && (mode == ModeSymlink || mode == ModeGitlink) {
			// A conflicted path whose other stage is a real blob stays a real
			// blob: the fold must never turn a scannable entry unscannable.
			out[p] = prev
			continue
		}
		out[p] = mode
	}
	return out, nil
}

// RangeModes returns the mode each path changed by rng has AT THE RANGE'S END,
// keyed by path — the analogue of StagedModes for a commit range, and the same
// question asked of the tree that range is about.
//
// It reads `diff --raw`, whose destination mode is exactly that, rather than
// `ls-tree` at an endpoint this package would have to derive. A range string is
// arbitrary — "A..B", "A...B", "origin/main HEAD", any of them with a legal
// `-- <pathspec>` tail — so parsing out "the endpoint" is a guess, and a wrong
// guess here silently reports the wrong mode. --raw hands the whole spec to git
// and reads the answer git resolved, exactly as RangePaths does.
//
// The filter matches RangePaths, so the paths reported here are the paths
// reported there. Records are `:<srcmode> <dstmode> <srcsha> <dstsha>
// <status>\0<path>\0`, and a rename or copy carries TWO path fields — source
// then destination. The destination is what is keyed, because it is what
// --name-only reports and therefore what the caller is holding.
func RangeModes(root, rng string) (map[string]string, error) {
	fields, err := splitRange(rng)
	if err != nil {
		return nil, err
	}
	if err := EnsureTopLevel(root); err != nil {
		return nil, err
	}
	args := append([]string{"diff", "-z", "--raw", "--diff-filter=ACMR"}, fields...)
	raw, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	recs := strings.Split(raw, "\x00")
	out := make(map[string]string, len(recs)/2)
	for i := 0; i < len(recs); i++ {
		meta := recs[i]
		if !strings.HasPrefix(meta, ":") {
			continue // trailing empty field from the final NUL
		}
		parts := strings.Fields(meta)
		if len(parts) != 5 {
			return nil, fmt.Errorf("git diff --raw: malformed record %q", meta)
		}
		dstMode, status := parts[1], parts[4]
		// R and C carry <src>\0<dst>; every other status carries one path.
		npaths := 1
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			npaths = 2
		}
		if i+npaths >= len(recs) {
			return nil, fmt.Errorf("git diff --raw: record %q has no path", meta)
		}
		i += npaths
		out[filepath.ToSlash(recs[i])] = dstMode
	}
	return out, nil
}

// Diff returns the raw unified diff for a range (e.g. "A..B" or
// "origin/main HEAD"), or an error on any git failure. root must be the
// repository top-level.
func Diff(root, rng string) (string, error) {
	fields, err := splitRange(rng)
	if err != nil {
		return "", err
	}
	if err := EnsureTopLevel(root); err != nil {
		return "", err
	}
	args := append([]string{"diff", "--no-color"}, fields...)
	return git(root, args...)
}

// TopLevel reports the repository top level git resolves for root, and the
// prefix — the subdirectory git landed in, which is empty if and only if root
// IS that top level.
//
// ASK GIT, IN GIT'S OWN FRAME. `--show-prefix` is empty if and only if the
// directory git ran in IS the top level. A subdirectory, a `..` that traverses
// a symlink, or GIT_WORK_TREE pointing elsewhere all come back with a non-empty
// prefix naming where git actually landed.
//
// Both answers come from ONE invocation so they cannot disagree about which
// repository they describe — which is also why this is a function rather than
// two callers each asking their own half. `--show-toplevel` stays in the
// command because a bare repository fails it outright (exit 128) while
// `--show-prefix` alone reports "" for one and would wave it through.
//
// It is the verdict EnsureTopLevel is built on, and it is exported because
// internal/hooks needs the same predicate for a different reason: install
// refuses a subdirectory root because git resolves core.hooksPath from the top
// level, not because a file-set intersection has to align. Sharing the
// invocation keeps one parse of git's answer; sharing EnsureTopLevel's message
// would have put "file-set modes require" in front of an operator running
// `hooks install`, which is a sentence about a command they did not type.
func TopLevel(root string) (top, prefix string, err error) {
	out, err := git(root, topLevelArgs...)
	if err != nil {
		return "", "", err
	}

	// NO TRIMMING BEYOND THE LINE TERMINATOR. This package's parser contract
	// already says so — diffPaths: "no per-record trimming, so trailing-space
	// and control-character names survive intact", pinned by
	// TestTrackedPathsSurvivesTrailingSpaceName. EnsureTopLevel used
	// strings.TrimSpace here, which strips the newline git appends AND any
	// trailing space belonging to the directory name. A root legally named
	// `foo ` was compared as `foo`; where a sibling `foo` existed and pointed
	// into the repo, both paths resolved to one directory and a SUBDIRECTORY
	// was accepted as the root — exit 0 over an unscanned staged changeset.
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("git: rev-parse did not report both the top level and the prefix for -C %s: %q", root, out)
	}
	return strings.TrimSuffix(lines[0], "\r"), strings.TrimSuffix(lines[1], "\r"), nil
}

// EnsureTopLevel errors unless root resolves to the git repository top-level.
// File-set modes align git's repo-relative paths with the scan's root-relative
// paths only when root IS the top-level; enforcing it keeps the intersection
// honest instead of silently matching nothing (a fail-open hazard).
func EnsureTopLevel(root string) error {
	top, prefix, err := TopLevel(root)
	if err != nil {
		return err
	}
	if prefix != "" {
		return fmt.Errorf("git: file-set modes require -C to be the repository root (%s), not %s — git resolves it to the subdirectory %q", top, root, prefix)
	}

	// Belt and braces: also compare directory identity. os.Stat follows
	// symlinks, so this asks whether -C and git's reported top level are one
	// directory. A symlinked root passes, which is correct — it names the top
	// level and git agrees, so the path frames align. (`check` over such a root
	// is nevertheless a vacuous pass today for an unrelated reason in the scan
	// layer: #143.)
	//
	// This is deliberately NOT the load-bearing check. os.SameFile has no error
	// path on unix — Dev == Dev && Ino == Ino — and on Windows
	// GetFileInformationByHandle succeeds with all-zero file indices on
	// filesystems that do not support file IDs. On such a substrate (SMB/9p/
	// FUSE, container bind mounts, overlayfs without xino) every directory
	// compares equal, so an identity-only guard does not fail closed, it goes
	// silently inert. The prefix check above is immune because it never leaves
	// git's frame. TestEnsureTopLevelRefusesSubdirWhenIdentityCannotDistinguish
	// forces the degenerate answer and pins that.
	//
	// The string comparisons this replaced each exposed the next: raw equality
	// refused the default "." from the repo root and broke every git hook
	// (#142); filepath.Abs before EvalSymlinks was worse than the bug, because
	// Abs calls Clean, which strips `x/..` lexically while the kernel follows
	// `x` first, reporting a subdirectory as the top level; EvalSymlinks then
	// Abs fixed that but still compared spelling, so a case-variant root on a
	// case-insensitive filesystem was refused.
	topInfo, statErr := os.Stat(top)
	if statErr != nil {
		return fmt.Errorf("git: cannot stat the repository root %s reported by git: %w", top, statErr)
	}
	rootInfo, statErr := os.Stat(root)
	if statErr != nil {
		return fmt.Errorf("git: cannot stat -C %s: %w", root, statErr)
	}
	if !sameFile(topInfo, rootInfo) {
		return fmt.Errorf("git: file-set modes require -C to be the repository root (%s), not %s", top, root)
	}
	return nil
}

// sameFile is os.SameFile behind a variable so a test can force the degenerate
// answer a filesystem without usable file IDs would give. See
// SetSameFileForTest in export_test.go.
var sameFile = os.SameFile

// diffPaths runs a path-listing git command and parses its output. It
// inserts -z (NUL-terminated, supported by both `diff --name-only` and
// `ls-files`) because line-splitting is unsound (#96): with default
// core.quotePath git C-quotes any path holding a non-ASCII or control byte,
// and the quoted spelling can never intersect the scanner's paths — the file
// silently drops out of the --staged/--range file set, a green gate over an
// unscanned file. -z disables quoting unconditionally; no per-record
// trimming, so trailing-space and control-character names survive intact
// (same parser contract as TrackedUnder).
//
// -z goes directly after the subcommand, BEFORE caller-supplied args: a
// range string may legally carry a `-- <pathspec>` tail, and a trailing -z
// lands after the separator where git silently swallows it as a pathspec —
// newline output again, fused by the NUL split into one garbage record and
// an empty intersection (#96 review blocker).
func diffPaths(root string, args []string) ([]string, error) {
	out, err := git(root, append([]string{args[0], "-z"}, args[1:]...)...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, rec := range strings.Split(out, "\x00") {
		if rec != "" {
			paths = append(paths, filepath.ToSlash(rec))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// GetConfig returns a git config value, or an error when the key is unset.
func GetConfig(root, key string) (string, error) {
	out, err := git(root, "config", "--get", key)
	return strings.TrimSpace(out), err
}

// GetConfigBool returns a git config value normalized through git's own
// boolean parser (--type=bool prints "true" for yes/on/1/True and "false"
// for their negations), or an error when the key is unset. Callers gating
// behavior on a boolean key must use this, not GetConfig: --get returns the
// value AS SPELLED, so `ignorecase = yes` would compare unequal to "true"
// and silently flip the gate (#90 review — proven fail-open).
func GetConfigBool(root, key string) (bool, error) {
	out, err := git(root, "config", "--get", "--type=bool", key)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

// RepoConfig answers a different question from GetConfig: not "what value does
// git resolve for key" but whether the CONFIG FILE BODY of this repository's
// local (or worktree) scope sets one. It returns the value, whether that scope
// sets it at all, and an error when git could not answer.
//
// THAT IS NARROWER THAN "DID THIS REPOSITORY DECLARE ONE", and the difference is
// NOT closed here. `git config --local --get` defaults to --no-includes, so a
// key this repository declares in its own .git/config through `include.path`
// reads back unset from this function while git resolves and acts on it —
// measured on git 2.50.1 with core.hooksPath: `rev-parse --git-path hooks`
// answers the included value, this function answers unset. Folding --includes in
// would make this function report as "declared here" settings that scoping was
// chosen to exclude: an include may name a file anywhere on the machine.
// RepoConfigWithIncludes below is the other half of the answer instead — the two
// together tell a caller that a declaration exists AND that it arrives through
// an include, which is what internal/hooks needs to stop attributing the
// project's own wiring to something outside the repository (#173).
//
// It exists because GetConfig's `--get` returns the EFFECTIVE value across
// system, global and local scope, so a machine-global core.hooksPath equal to
// formwork's own directory answers "yes, that value is in force" for a
// repository that never mentioned it. A caller deciding who OWNS a piece of
// wiring — this project, or a machine-wide default it must not silently take
// over — cannot ask that question with GetConfig at all.
//
// THE THREE-VALUED ANSWER IS LOAD-BEARING. Unset is a normal answer here, not
// a fault: measured on git 2.50.1, `--get` on an unset key exits 1 while a
// repository git cannot read (unparseable config, no repository) exits 128, and
// only the second is a failure. A (string, error) signature has to spend one of
// its two states on unset, and either choice loses information the caller needs
// — an unset key is not an error, and the empty string is a value a repository
// can genuinely declare (`hooksPath =` is exit 0 with empty output, measured).
//
// PRECONDITION: key must be a well-formed `section.name`. git spends exit 1 on
// an invalid key spelling too — `bogus`, `core.`, `core..x`, measured — so such
// a key arrives here indistinguishable from unset, and the last of those prints
// nothing on stderr, which rules out splitting the two on git's diagnostics.
//
// SCOPE IS local PLUS worktree, and the second is not redundant. With
// extensions.worktreeConfig enabled the declaration lives in
// .git/config.worktree, where `--local --get` reports it unset and git
// nevertheless runs hooks from it (measured: `rev-parse --git-path hooks`
// answers the config.worktree value) — so local-only would call a repository's
// own wiring somebody else's. Worktree scope also WINS over local, which is why
// it is asked first.
//
// The worktree read is GATED rather than unconditional, on a measurement: with
// the extension absent and more than one working tree registered, `git config
// --worktree --get` is `fatal: --worktree cannot be used with multiple working
// trees`, exit 128. Unconditional, that turns every ordinary repository that
// has run `git worktree add` into one whose config cannot be read. The gate is
// read from `--local` scope because that is where git honours it: a global
// extensions.worktreeConfig is ignored, measured — git answered `.git/hooks`
// with one set and a .git/config.worktree present.
func RepoConfig(root, key string) (string, bool, error) {
	wtScope, err := worktreeConfigEnabled(root)
	if err != nil {
		return "", false, err
	}
	if wtScope {
		if val, set, err := scopedConfig(root, "--worktree", "--get", key); err != nil || set {
			return val, set, err
		}
	}
	return scopedConfig(root, "--local", "--get", key)
}

// RepoConfigWithIncludes asks RepoConfig's question with git's `include.path`
// directives FOLLOWED, and reports only whether the answer exists — never what
// it is.
//
// THE PAIR IS THE POINT. `--local --get` defaults to --no-includes, so this
// function answering where RepoConfig does not means one thing: the key is
// declared through an include from this repository's own config. That two-answer
// test is all its caller needs, and it needs no policy about WHERE an include
// points, which is why the value is deliberately not returned — measured on git
// 2.50.1, an include reaches outside the repository through both spellings, an
// absolute path to a file elsewhere on the machine and a relative `../../up.cfg`
// out of .git. Returning the value would make this a reader of configuration
// formwork was never pointed at, and hand a caller a string to act on.
//
// THE SCOPES MIRROR RepoConfig, worktree-first and worktree GATED, for that
// function's reasons: with more than one working tree registered and the
// extension absent, `--worktree` is `fatal: --worktree cannot be used with
// multiple working trees` (exit 128), and adding --includes does not change
// that — measured. The second scope is not
// redundant here either, and it is what a fix written to the local scope alone
// misses: an include inside .git/config.worktree reads back UNSET from `--local
// --includes --get` while git runs hooks from it (measured on git 2.50.1;
// `rev-parse --git-path hooks` answers the included value).
//
// The gate itself reads WITHOUT includes, as RepoConfig's does, and that matches
// git rather than merely reusing a helper: measured on git 2.50.1, an
// extensions.worktreeConfig arriving through an include is not honoured at all —
// .git/config.worktree stayed unread and `rev-parse --git-path hooks` answered
// the default. Following includes for the gate would open a worktree scope git
// itself does not.
func RepoConfigWithIncludes(root, key string) (bool, error) {
	wtScope, err := worktreeConfigEnabled(root)
	if err != nil {
		return false, err
	}
	if wtScope {
		if _, set, err := scopedConfig(root, "--worktree", "--includes", "--get", key); err != nil || set {
			return set, err
		}
	}
	_, set, err := scopedConfig(root, "--local", "--includes", "--get", key)
	return set, err
}

// worktreeConfigEnabled reports whether this repository turned on git's
// per-worktree config file.
//
// --type=bool for GetConfigBool's reason (#90): `--get` returns the value AS
// SPELLED, and `worktreeConfig = yes` compared against "true" would read as
// off, dropping the worktree scope RepoConfig documents. It cannot use
// GetConfigBool itself, which asks git for the effective value across every
// scope — the exact thing RepoConfig exists not to do.
func worktreeConfigEnabled(root string) (bool, error) {
	val, set, err := scopedConfig(root, "--local", "--get", "--type=bool", "extensions.worktreeConfig")
	if err != nil || !set {
		return false, err
	}
	return val == "true", nil
}

// scopedConfig runs one `git config` read and splits git's exit status into the
// three answers RepoConfig documents: exit 0 is a value, exit 1 is unset, and
// anything else is an error carrying git's stderr.
//
// It strips only the line terminator. TrimSpace would be wrong for the same
// reason it is wrong in HooksPath (hooks.go): a directory legally named
// "hooks " must not become "hooks", and this package's parsers do not trim
// beyond the terminator.
func scopedConfig(root string, args ...string) (string, bool, error) {
	out, code, err := gitExit(root, append([]string{"config"}, args...)...)
	if code == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r"), true, nil
}

// SetConfig sets a key in the local config of the repository git RESOLVES for
// root — which is not the same as "the repository at root".
//
// GIT_DIR MOVES IT OUT FROM UNDER THIS FUNCTION. --local names the local scope
// of whatever repository git resolved, and GIT_DIR beats -C at that resolution.
// Measured on git 2.50.1: with GIT_DIR pointing at repository B, `git -C A
// config --local core.hooksPath …` writes to B and leaves A's config unset, so a
// caller that wrote shims into A reports a wiring it made somewhere else. That
// is #167.
//
// --local IS EXPLICIT, and it is not only a spelling. Measured on git 2.50.1:
// with GIT_CONFIG set in the environment, a bare `git config <key> <val>`
// writes to THAT file and exits 0, leaving the repository's config untouched —
// a caller reports a wiring it never made. `--local` refuses the ambiguity
// instead (`error: only one config file at a time`, exit 129), which is the
// loud direction. It also pins the scope against a future git changing where an
// unqualified write lands: repo-local is the only scope this function may write,
// and saying so in the argv is cheaper than trusting a default.
func SetConfig(root, key, val string) error {
	_, err := git(root, "config", "--local", key, val)
	return err
}

// git runs `git -C root <args>` and returns stdout, turning a non-zero exit
// into an error that carries git's stderr. Every non-zero exit is one error:
// callers that must tell git's exit codes apart use gitExit.
func git(root string, args ...string) (string, error) {
	out, _, err := gitExit(root, args...)
	return out, err
}

// gitExit is git plus the process exit code, which it returns alongside the
// error. It exists for `git config --get`, where exit 1 means "unset" — a
// normal answer a caller must not read as a failure — and gitStdin (ignored.go)
// carries the same code for check-ignore's own exit 1. A caller with only the
// error cannot make that distinction, because the plain helper collapses every
// non-zero exit into one.
//
// The code is -1 whenever there is no exit status to report — git never ran
// (not on PATH, unexecutable), or, per os/exec's ExitCode, it was terminated by
// a signal. The error is set in every such case, and 0 would say "success". A
// refused FORMWORK_GIT_ENV is one more such case: git never ran.
//
// This is one of THIS PACKAGE's two exec sites, and the environment policy is
// applied at both. gitStdinEnv (ignored.go) builds its own exec.Command and does
// not route through here, so a guard placed only here would leave CheckIgnored
// inheriting the ambient environment. The wrapper `git` is deliberately not the
// site either: scopedConfig calls this function directly.
//
// "TWO" IS SCOPED TO THIS PACKAGE ON PURPOSE. internal/rules/command runs
// operator-supplied argv through a third exec.Command with no cmd.Env at all,
// so a `command` rule shelling out to git still inherits the environment this
// package removes. It is not reachable from here and arguably should not be (it
// needs PATH and HOME), which is why this says where the boundary is rather than
// claiming there isn't one.
//
// WHAT CLOSED THE CONSEQUENCE THERE IS A REFUSAL, NOT THIS SCRUB (#177 and #213,
// both closed COMPLETED 2026-08-24). An earlier version of this paragraph ended
// "and can therefore answer from a repository the engine refused. Filed
// separately as #177", which was true when it was written and is not now:
// ensureRepositoryAgreement asks vcs.EnsureNoInheritedHistoryEnv for the
// object-store six and vcs.CommonDir for the pointer three, so such a rule is
// exit 2 naming the variable rather than a pass over another repository's
// history (measured both ways, #335). The INHERITANCE stands — that is what this
// paragraph is about, and it is why the two texts env.go emits now carve command
// rules out instead of claiming the scrub reaches them.
func gitExit(root string, args ...string) (string, int, error) {
	env, envErr := gitEnvFor(root)
	if envErr != nil {
		return "", -1, envErr
	}
	return gitExitEnv(env, root, args...)
}

// gitExitEnv is gitExit with the environment already decided, which is the one
// thing a caller can vary here. Four sites vary it, and every one of them runs
// the SAME question under two environments and compares — which is why the
// environment is a parameter rather than something resolved inside:
//
//   - configenv.go:178-179   the config-override measurement (#167)
//   - env.go:503-504         the pointer-family scrub comparison (#167)
//   - hatch.go:176-177       the FORMWORK_GIT_ENV hatch comparison (#173)
//   - indexguard.go:66       the #175 moved-index cross-check, via indexFrame.git
//
// hatch.go:255 also calls it directly, with nil, to ask one unscrubbed question.
// Everything else goes through gitExit and gets gitEnv's answer. This list is
// the whole set as of #175/#176 — an earlier version of this comment said "two
// callers", which was already false when it was written.
//
// A nil env is os/exec's own meaning, "inherit the parent's unchanged" — which
// is what gitEnv returns for the FORMWORK_GIT_ENV hatch.
func gitExitEnv(env []string, root string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", ee.ExitCode(), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", -1, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), 0, nil
}
