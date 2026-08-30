// Package scan walks a repository tree once and serves cached file content
// to rule checkers. It knows nothing about rules (spec §4).
package scan

import (
	"bytes"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
)

// ancestorPrefixes returns path and each ancestor prefix, ordered SHALLOWEST
// first — the order the walk's descent encounters them.
func ancestorPrefixes(path string) []string {
	deepFirst := []string{path}
	for i := strings.LastIndexByte(path, '/'); i > 0; i = strings.LastIndexByte(path[:i], '/') {
		deepFirst = append(deepFirst, path[:i])
	}
	out := make([]string, 0, len(deepFirst))
	for i := len(deepFirst) - 1; i >= 0; i-- {
		out = append(out, deepFirst[i])
	}
	return out
}

// IgnoredBy reports whether a scan.ignore glob hides path from the walk, and
// which one, attributing exactly as WalkIgnoring does: the walk prunes at the
// SHALLOWEST matching ancestor, testing each level in config order — so this
// loop is level-outer (shallowest first), glob-inner. A glob-outer loop names
// a different (deeper) glob than the walk whenever overlapping globs match
// different ancestor depths (#95 review). Built-in skips are deliberately not
// consulted here (lint's fallback excludes them with its own guard);
// consumers needing full walk attribution use NotScannedBy. doublestar cannot
// case-fold; callers owning case-insensitive concerns handle them above this.
func IgnoredBy(path string, globs []string) (string, bool) {
	for _, prefix := range ancestorPrefixes(path) {
		for _, g := range globs {
			// Match's only error is ErrBadPattern; config validated every
			// glob at load, same as the walk.
			if ok, _ := doublestar.Match(g, prefix); ok {
				return g, true
			}
		}
	}
	return "", false
}

// NotScannedVerdict says whether the walk never yields a path, and which
// channel hides it. At most one of Builtin, Glob, GitRule is set; the zero
// value is "scanned". Hidden is DERIVED from the channel fields rather than
// stored, so a verdict claiming a channel without claiming hidden — the
// silent-governed fail-open — is unrepresentable (#125 review).
type NotScannedVerdict struct {
	Builtin bool   // a .git/.formwork ancestor — the built-in skip set
	Glob    string // the scan.ignore glob that pruned it, when that channel decided
	GitRule string // the gitignore rule ("<file>:<line>:<pattern>"), when that channel decided
}

// Hidden reports whether any channel hides the path from the walk.
func (v NotScannedVerdict) Hidden() bool {
	return v.Builtin || v.Glob != "" || v.GitRule != ""
}

// NotScannedBy reports whether WalkWith(opts) never yields path (treated as a
// FILE path — the walk's file frame), and which trigger hides it, in the
// walk's own attribution order: descent hits the SHALLOWEST trigger first,
// and at each directory level the built-in skip set is consulted before
// scan.ignore globs before the gitignore prune (WalkWith's pinned dir-branch
// ordering). So a .git nested under an operator-ignored tree attributes the
// operator glob — the walk pruned above it and never saw that .git — while a
// root-level .git directory attributes the built-in skip. The built-in set
// applies to ANCESTOR segments only: WalkWith consults builtinSkipDir in its
// d.IsDir() branch and in the symlink refusal, never in its regular-file
// branch, so a regular file NAMED .git (a linked-worktree gitdir pointer is
// exactly this) is scanned and enforced on — reporting it hidden would
// contradict check's verdict (#119 third-pass finding 2). Ignore globs
// and the gitignore files set, by contrast, do match at the leaf: the walk's
// file branch checks both; the gitignore dirs set, like the built-in skip,
// applies to ancestors only. Getting any of this wrong makes trusted surfaces
// (lint's census, rules-for's annotation, #108) disagree about who hid a path —
// or whether it is hidden at all.
//
// The ancestor test is the whole PREFIX, not its last segment: `.formwork`
// prunes only at the walk root since #268, and a bare segment cannot say how
// deep it sits. Answering "hidden by the built-in skip" for a nested corpus's
// .formwork would be this function claiming a prune the walk does not perform.
func NotScannedBy(path string, opts Opts) NotScannedVerdict {
	prefixes := ancestorPrefixes(path)
	for i, prefix := range prefixes {
		leaf := i == len(prefixes)-1
		if !leaf && builtinSkipDir(prefix) {
			return NotScannedVerdict{Builtin: true}
		}
		for _, g := range opts.Ignore {
			if ok, _ := doublestar.Match(g, prefix); ok {
				return NotScannedVerdict{Glob: g}
			}
		}
		if rule, ok := opts.GitIgnored.lookup(prefix, !leaf); ok {
			return NotScannedVerdict{GitRule: rule}
		}
		// An ANCESTOR can also live in the files set: git lists a symlink
		// (even one pointing at a directory) as a non-dir entry, and the
		// walk meets it in its FILE branch — pruning it at the gitignore
		// check before the non-regular skip, censused as SourceGitignore.
		// Attribution must name that same channel, not fall through to a
		// non-regular diagnosis the census would contradict (#125 round-2).
		if !leaf {
			if rule, ok := opts.GitIgnored.lookup(prefix, false); ok {
				return NotScannedVerdict{GitRule: rule}
			}
		}
	}
	return NotScannedVerdict{}
}

// File is one regular file under the scanned root. Content is read at most
// once and cached; File is safe for concurrent use.
type File struct {
	relPath string
	absPath string

	contentOnce sync.Once
	content     []byte
	contentErr  error

	linesOnce sync.Once
	lines     []string

	variantMu sync.Mutex
	variants  map[string]*File
}

// NewMemFile returns an in-memory File. For tests and fixture runners.
func NewMemFile(relPath string, content []byte) *File {
	f := &File{relPath: relPath, content: append([]byte(nil), content...)}
	f.contentOnce.Do(func() {})
	return f
}

// Path returns the repo-relative, slash-separated path.
func (f *File) Path() string { return f.relPath }

// Content returns the file bytes, reading from disk on first call only.
func (f *File) Content() ([]byte, error) {
	f.contentOnce.Do(func() {
		f.content, f.contentErr = os.ReadFile(f.absPath)
	})
	return f.content, f.contentErr
}

// Lines returns the file content split into lines, without a trailing empty
// line for newline-terminated files.
//
// The returned strings ALIAS the cached content — they are not copies. This is
// the whole point: content is already retained for the run, and materialising
// `string(content)` to split made every file cost its own size a second time.
// Measured over a 10k-file tree, that took a file from 1.06x its size (Content
// alone) to 2.55x, and applied again for every preprocessor variant, which is
// the single largest term in the engine's steady-state footprint (#66).
//
// THE INVARIANT THIS DEPENDS ON: cached content is never mutated after it is
// read. Content() reads once under contentOnce and no caller writes through the
// returned slice; every preprocess.Transform copies its input before modifying
// it (`out := append([]byte(nil), src...)`) rather than editing in place, and
// Variant stores the transform's OUTPUT as a separate File with its own content.
// A transform that ever mutated its argument would corrupt the shared cache for
// every other rule regardless of this aliasing — but it would also silently
// change strings already handed out here, so the two tests in
// lines_alias_test.go pin the invariant rather than leaving it to a comment.
func (f *File) Lines() ([]string, error) {
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	f.linesOnce.Do(func() {
		b := content
		if n := len(b); n > 0 && b[n-1] == '\n' {
			b = b[:n-1] // drop exactly one trailing newline, as TrimSuffix did
		}
		if len(b) == 0 {
			return
		}
		// unsafe.String views the cached bytes as a string without copying;
		// strings.Split then returns substrings that alias that same memory, so
		// what is retained is the line index alone.
		f.lines = strings.Split(unsafe.String(unsafe.SliceData(b), len(b)), "\n")
	})
	return f.lines, nil
}

// Variant returns a view of the file whose content is transformed by the
// named preprocessor, computed lazily and cached per (file, variant) —
// spec §6. "raw" and "" return the file itself. Variants share the file's
// path, so findings against a variant report original positions (transforms
// are line-preserving).
func (f *File) Variant(name string) (*File, error) {
	t, ok := preprocess.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("%s: unknown preprocessor %q", f.relPath, name)
	}
	if t == nil {
		return f, nil
	}
	f.variantMu.Lock()
	defer f.variantMu.Unlock()
	if v, ok := f.variants[name]; ok {
		return v, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	transformed := t(content)
	wantLines, gotLines := bytes.Count(content, []byte("\n")), bytes.Count(transformed, []byte("\n"))
	if gotLines != wantLines {
		return nil, fmt.Errorf("%s: preprocessor %q is not line-preserving: input has %d newline(s), output has %d",
			f.relPath, name, wantLines, gotLines)
	}
	v := NewMemFile(f.relPath, transformed)
	if f.variants == nil {
		f.variants = map[string]*File{}
	}
	f.variants[name] = v
	return v, nil
}

// FileSet is every scannable file under Root, sorted by Path.
type FileSet struct {
	Root  string
	Files []*File
	// Ignored is every path a scan.ignore glob removed from this walk,
	// sorted by Path. Empty unless the walk ran with ignore globs.
	Ignored []Ignored
}

// Ignored is one path removed from the walk. A Dir record means the subtree
// was pruned WITHOUT descending — nothing below it was enumerated, which is
// why counts of "files under an ignored dir" are deliberately not offered
// anywhere: producing them would cost the walk the prune bought.
type Ignored struct {
	Path string // repo-relative, slash-separated
	Dir  bool
	Glob string // the scan.ignore glob that matched ("" for a gitignore prune)
	By   Source
	Rule string // gitignore only: "<file>:<line>:<pattern>" that decided it
}

// Source names the channel that removed a path from the walk. The census must
// be able to tell them apart: they are declared in different files, justified
// differently, and audited differently, so reporting one as the other would
// send a reader to the wrong place to change it.
type Source int

const (
	// SourceIgnore is a scan.ignore glob. It is the zero value because it was
	// the only channel when Ignored was introduced, which keeps every existing
	// record's literal shape unchanged.
	SourceIgnore Source = iota
	// SourceGitignore is a path git itself refuses to track.
	SourceGitignore
	// SourceSymlink is a symlink the walk declined to follow (#143). Unlike the
	// other two it is not DECLARED anywhere — no glob, no .gitignore line — so
	// it is the one channel a reader cannot find by looking at config. That is
	// exactly why it has to be censused: the operator sees a rule matching
	// nothing and is told their glob is wrong, when the truth is that the walk
	// declined to look.
	SourceSymlink
)

// GitIgnored is a set of paths git confirmed ignored, keyed for O(1) lookup
// during the walk. Callers build it from their git seam; this package holds no
// git dependency of its own, so the walk stays testable without a repository
// and the seam stays fail-closed in one place rather than two.
type GitIgnored struct {
	dirs  map[string]string // path (no trailing slash) -> responsible rule
	files map[string]string
}

// NewGitIgnored returns an empty prune set.
func NewGitIgnored() *GitIgnored {
	return &GitIgnored{dirs: map[string]string{}, files: map[string]string{}}
}

// Add records one confirmed-ignored path. rule is the ignore line responsible
// for it, carried through to the census so a prune can always be traced to the
// declaration that caused it.
func (g *GitIgnored) Add(path string, dir bool, rule string) {
	if dir {
		g.dirs[path] = rule
	} else {
		g.files[path] = rule
	}
}

// Len is the number of recorded paths.
func (g *GitIgnored) Len() int { return len(g.dirs) + len(g.files) }

// Lookup reports the responsible rule for rel in the given frame (dir or
// file), if this set covers it — the exported read the guidance layer's
// ghost overlay uses to pick up existing-ancestor collapse levels. A nil set
// covers nothing.
func (g *GitIgnored) Lookup(rel string, dir bool) (string, bool) {
	return g.lookup(rel, dir)
}

// lookup reports the responsible rule for rel, if this set covers it. A nil
// set covers nothing, so an undeclared repo takes the same path as before.
//
// Matching is EXACT, deliberately, and this is not the gap #90 closed. That
// one needed case folding because it compared git's INDEX spelling against the
// walk's on-disk spelling, and on a case-folding filesystem those genuinely
// differ. Both sides here come from the filesystem instead: git populates this
// set from `ls-files --others`, which reads the working tree, and
// `check-ignore -v` echoes back the spelling it was given. So the two agree by
// construction, and folding would only widen matching beyond what git
// actually said. A caller that ever builds this set from the index rather than
// the working tree reopens #90's question and must revisit this.
func (g *GitIgnored) lookup(rel string, dir bool) (string, bool) {
	if g == nil {
		return "", false
	}
	if dir {
		r, ok := g.dirs[rel]
		return r, ok
	}
	r, ok := g.files[rel]
	return r, ok
}

// sourceExts are filename extensions the toolchains this engine guards will
// compile or interpret as source. A committed symlink whose name ends in one
// of these is a silent bypass of every rule: Walk used to skip non-regular
// files, so the symlink never entered the FileSet, while go/dart/etc. followed
// it and built the target (formwork#54).
//
// We refuse those symlinks with a Walk error rather than following them:
// following needs cycle detection and a repo-containment check, and the right
// first move is to make the bypass loud. Non-source symlinks (config/docs
// aliases such as formwork.yaml → .formwork/formwork.yaml) stay skipped —
// they are not a compile-time blind spot.
var sourceExts = map[string]bool{
	".go": true, ".dart": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true,
	".m": true, ".mm": true, ".swift": true,
	".java": true, ".kt": true, ".rs": true,
	".py": true, ".rb": true, ".php": true,
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".sh": true, ".bash": true, ".zsh": true,
	".sql": true, ".proto": true, ".awk": true,
}

// sourceNames are EXTENSIONLESS filenames the same toolchains read as source or
// build input. sourceExts cannot reach them — filepath.Ext("Makefile") is "" —
// which is #143 row 3: `Makefile -> ../shared/Makefile` was skipped in silence
// even where a rule's scope named `**/Makefile`, for want of a dot.
//
// This is a name list, with a name list's limits: it closes the shapes a
// toolchain compiles or executes, not every path some rule might scope. A
// symlink named neither like source nor like one of these is still skipped
// (TestWalkSkipsNonSourceSymlinks pins that this is deliberate for config and
// doc aliases), so a rule scoping such a name still has a hole. Closing that
// one needs the walk to know the rule set, which it does not and must not
// (spec §4). Extend this the way sourceExts is extended.
var sourceNames = map[string]bool{
	"Makefile": true, "makefile": true, "GNUmakefile": true,
	"Dockerfile": true, "Containerfile": true,
	"Justfile": true, "justfile": true,
	"Rakefile": true, "Gemfile": true, "Vagrantfile": true,
	"Procfile": true, "Jenkinsfile": true,
	"BUILD": true, "WORKSPACE": true,
}

// isSourceSymlinkName reports whether basename looks like source the toolchains
// would compile — keyed on the symlink's own name, not the target's.
func isSourceSymlinkName(name string) bool {
	if sourceNames[name] {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return sourceExts[ext]
}

// hidesASubtree reports whether a symlink at absPath leads to a directory that
// this walk reaches by no other path — the shape in which the link's silent
// skip costs coverage (#143 row 2). Two questions, in order:
//
// IS IT A DIRECTORY? os.Stat FOLLOWS, which is the part that cannot be answered
// from the directory entry: WalkDir reports d.IsDir() false for a
// symlink-to-directory, so the entry lands in the non-regular branch and the
// whole subtree behind it leaves the walk with no error and no census record. A
// stat is not a traversal — nothing under the link is enumerated either way, so
// no cycle semantics are taken on.
//
// DOES IT LEAVE THE TREE? `lib -> ../shared/lib` puts content in the walk's
// reach that appears under no other walked path; `alias -> src` puts nothing
// there, because the walk already enumerates src by its own name, and refusing
// that would fail repositories over a link that hides nothing. The comparison is
// between two already-resolved absolute paths — it decides one containment
// question about this entry, and takes on no repo-containment contract for the
// walk at large.
//
// CAN IT BE LOOKED AT? Only a DANGLING link — ENOENT, nothing at the other end
// — answers false on an error. That is the one case where "there is nothing
// here" is a true answer rather than an unexamined one; it names no content, so
// none goes unscanned. Every other error means the opposite: EACCES behind an
// unsearchable parent, EPERM (macOS denies a Go process ~/Documents until TCC
// grants it), ELOOP. There the subtree exists and the walk cannot look at it,
// and answering false would report "I cannot look" as "nothing there" — #143's
// own signature, inside the guard written to close it. So an unreadable link is
// REFUSED, which is what the root arm of the same shape (resolveRoot) already
// does for a permission error.
//
// That refusal reaches one case beyond directories, deliberately: a
// non-source-named link to an unreadable FILE is refused too, because at this
// point the walk cannot tell file from directory. Refusing the thing it cannot
// classify is the answer consistent with the issue.
func hidesASubtree(absPath, walkRoot string) bool {
	info, err := os.Stat(absPath)
	if err != nil {
		return !errors.Is(err, iofs.ErrNotExist)
	}
	if !info.IsDir() {
		return false
	}
	target, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return !errors.Is(err, iofs.ErrNotExist)
	}
	rel, err := filepath.Rel(walkRoot, target)
	if err != nil {
		return true // not expressible relative to the root: not under it
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveRoot returns the directory WalkDir should enumerate for the given
// root, and is #143 row 1's mechanism at the only altitude this package owns.
//
// filepath.WalkDir LSTATS its root, so a symlinked root (`-C alias`, a checkout
// at ~/code/app -> /Volumes/dev/app, anything under /tmp on macOS) arrived at
// the callback as a non-directory entry, took the `p == root` early return, and
// ended the walk having enumerated nothing — reported as a clean tree at exit 0.
// `-C alias/` scanned that identical tree correctly, because the trailing slash
// made lstat resolve the link.
//
// RESOLVE, NOT REFUSE. PR #148 refused this root and was closed unmerged: the
// walk can serve a symlinked root, it only needs the final component resolved,
// and refusing broke ordinary checkouts with no override. Resolving costs no
// traversal semantics — symlinks INSIDE the tree are still never followed, and
// a directory symlink inside it is refused rather than descended (above).
//
// It is NOT the root contract #143 asks the -C seam to decide. Subcommands that
// answer without walking (rules-for, explain, scope) do not pass through here,
// so making them agree remains that seam's job; this makes every consumer that
// DOES walk agree with `-C alias/`.
//
// An lstat that fails is handed back to WalkDir untouched, so a missing root
// still produces exactly the error it produced before — this is a refinement of
// the walk, not a new gate in front of it. A root that resolves to something
// that is not a directory is refused for the same reason the rest of this
// exists: there is no tree to enumerate anywhere, and an empty FileSet reads as
// a clean one.
//
// The root is resolved WHOLE, not just its final component, so that the paths
// WalkDir hands back and the paths a symlink resolves to are spelled the same
// way. Without that the containment test in hidesASubtree cannot be trusted on
// macOS, where every t.TempDir() sits under /var -> /private/var and an
// in-tree link resolves to a root-relative path of "../../..".
func resolveRoot(root string) (string, error) {
	if _, err := os.Lstat(root); err != nil {
		return root, nil
	}
	walkRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("scan: root %s does not resolve: %w", root, err)
	}
	info, err := os.Stat(walkRoot)
	if err != nil {
		return "", fmt.Errorf("scan: root %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scan: root %s is not a directory, so there is no tree to enumerate", root)
	}
	return walkRoot, nil
}

// Walk builds the FileSet for root, skipping .git and .formwork — by name, in
// either spelling, a real directory or a symlink to one — and skipping
// non-regular files. Two shapes of symlink are REFUSED (returned as an error)
// rather than skipped: one whose own name reads as source to a toolchain, and
// one leading to a directory outside the tree or to something the walk cannot
// look at. Each would otherwise be a silent skip while the toolchain reads
// straight through it (spec §11: never skip silently; #54, #143 rows 2-3). The
// root itself is resolved first, so a symlinked root scans the tree it names
// instead of enumerating nothing (#143 row 1), and a root that is not a
// directory at all is refused. Any I/O error also fails the walk.
//
// Walk applies no operator ignores; every pre-scan.ignore caller and test goes
// through here unchanged.
func Walk(root string) (*FileSet, error) { return WalkIgnoring(root, nil) }

// WalkIgnoring is Walk plus scan.ignore: any entry whose repo-relative slash
// path matches an ignore glob is pruned and recorded on FileSet.Ignored. Globs
// are pre-validated by config (exit 2 at load), so Match errors are impossible
// here — same idiom as config.matchAny. Ordering is deliberate: skipDirs, then
// ignore, then the symlink refusals — an ignored path is the operator's declared
// not-ours, so neither #54's refusal nor the directory-symlink refusal fires
// inside it, and ignore_test.go and symlink_test.go pin each direction of that
// trade. A pruned directory is never descended, so its
// contents are neither scanned nor counted.
func WalkIgnoring(root string, ignore []string) (*FileSet, error) {
	return WalkWith(root, Opts{Ignore: ignore})
}

// Opts are the prunes applied to a walk. The zero value walks everything the
// built-in skips leave, so every pre-existing caller is unaffected.
type Opts struct {
	// Ignore are scan.ignore globs — the operator's declaration that a tree is
	// not theirs to gate.
	Ignore []string
	// GitIgnored is the set of paths git itself refuses to track, nil when
	// scan.gitignore is undeclared OR when it was declared but git could not
	// answer. Those two cases must stay distinguishable to the CALLER, which
	// is why this is not a bool: nil here means "prune nothing", and it is the
	// caller's job to say which of the two it is. Pruning nothing is the
	// fail-CLOSED direction — the resulting scan is a superset of the declared
	// one, so no rule can pass that would otherwise have failed.
	GitIgnored *GitIgnored
}

// WalkWith is WalkIgnoring plus the gitignore prune (spec §4). Ordering is
// deliberate and pinned by tests: the root is resolved (resolveRoot, #143 row
// 1) before anything is enumerated, and then per entry the built-in skip, then
// scan.ignore, then gitignore, then the symlink refusals — #54's source-named
// one and #143 row 2's subtree-hiding one, in that order. All three prune
// channels precede the refusals for the same reason — a pruned path is out of
// scope, and the refusal exists to make a bypass of IN-SCOPE source loud, not
// to police trees nobody gates. builtinSkipDir is asked twice for that reason:
// once for a real directory and again inside the subtree-hiding refusal, which
// is the only branch a .git or .formwork spelled as a SYMLINK reaches. Both
// consults pass the ROOT-RELATIVE path rather than the entry's bare name,
// because the skip is depth-sensitive for .formwork (#268) — see
// builtinSkipDir in skipdirs.go.
//
// WHY GITIGNORE PRUNING IS NOT THE FAIL-OPEN CHANGE #80 REJECTS. #80 is right
// that skipping UNTRACKED files would be a rule bypass: an untracked .go is
// still compiled, and it is the file most likely to be work in progress. This
// prunes the IGNORED set instead, which git will not accept into a commit
// without an edit to .gitignore or an explicit `add -f` — and the caller's
// seam confirms every path with git itself, which refuses to call a tracked
// path ignored. So an untracked-but-not-ignored file stays fully in scope, and
// a force-added file under an ignored directory stays fully in scope.
func WalkWith(root string, opts Opts) (*FileSet, error) {
	ignore := opts.Ignore
	walkRoot, rootErr := resolveRoot(root)
	if rootErr != nil {
		return nil, rootErr
	}
	// Root is the root as GIVEN, not as resolved: it is what rule finalizers
	// join their own paths against (rules.FinalizeContext{Root: fset.Root}), and
	// an alias resolves for them exactly as it does here — so carrying the
	// resolved spelling would change those paths for no gain.
	fset := &FileSet{Root: root}
	err := filepath.WalkDir(walkRoot, func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == walkRoot {
			return nil
		}
		rel, relErr := filepath.Rel(walkRoot, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if builtinSkipDir(rel) {
				return filepath.SkipDir
			}
			for _, g := range ignore {
				if ok, _ := doublestar.Match(g, rel); ok {
					fset.Ignored = append(fset.Ignored, Ignored{Path: rel, Dir: true, Glob: g})
					return filepath.SkipDir
				}
			}
			if rule, ok := opts.GitIgnored.lookup(rel, true); ok {
				fset.Ignored = append(fset.Ignored, Ignored{Path: rel, Dir: true, By: SourceGitignore, Rule: rule})
				return filepath.SkipDir
			}
			return nil
		}
		for _, g := range ignore {
			if ok, _ := doublestar.Match(g, rel); ok {
				fset.Ignored = append(fset.Ignored, Ignored{Path: rel, Dir: false, Glob: g})
				return nil
			}
		}
		if rule, ok := opts.GitIgnored.lookup(rel, false); ok {
			fset.Ignored = append(fset.Ignored, Ignored{Path: rel, Dir: false, By: SourceGitignore, Rule: rule})
			return nil
		}
		if !d.Type().IsRegular() {
			// Symlink (or other non-regular): refuse the shapes whose silent
			// skip is a rule bypass. Other non-regular entries stay skipped
			// (not followed) — no traversal / cycle semantics to own.
			if d.Type()&iofs.ModeSymlink != 0 {
				// A source-named link: helper.go → helper.txt hides from every
				// rule while go build still compiles it (#54). The name test now
				// also covers extensionless build files (#143 row 3).
				if isSourceSymlinkName(d.Name()) {
					return fmt.Errorf("scan: committed source symlink %s (formwork does not follow symlinks; remove it or replace it with a regular file)", rel)
				}
				// A link to a DIRECTORY OUTSIDE THE TREE: d.IsDir() is false for
				// it, so it lands here and the whole subtree behind it leaves
				// the walk with no error and no census record (#143 row 2).
				// Nothing in the walk's output distinguishes that from a tree
				// that was looked at and found clean, which is the defect the
				// refusal answers. The name test above cannot see it: the link's
				// own name carries no extension.
				// ...unless the link IS a built-in skip. builtinSkipDir is
				// consulted in the d.IsDir() branch above, which a
				// symlink-to-directory never reaches, so `.git` (ordinary in a
				// linked worktree or submodule) and a root `.formwork` shared
				// across a monorepo fell through to the refusal and hard-failed
				// an ordinary repo. The rationale above does not reach them:
				// these are the subtrees the walk is defined never to
				// enumerate, so nothing goes unscanned by not looking, and
				// NotScannedBy answers "hidden, by the built-in skip" for paths
				// beneath them.
				//
				// The carve-out is asked at the SAME depth the prune uses
				// (#268), which is why it takes rel and not d.Name(): a
				// `.formwork` symlink NESTED in the tree names a subtree the
				// walk would otherwise have enumerated, so the rationale above
				// stops applying to it exactly where the prune does, and it
				// gets the ordinary refusal.
				if !builtinSkipDir(rel) && hidesASubtree(p, walkRoot) {
					return fmt.Errorf("scan: committed directory symlink %s (formwork does not follow symlinks, so nothing under it is scanned; remove it or replace it with a real directory)", rel)
				}
				// Reached: a symlink that is neither source-named nor hiding a
				// subtree — the declared config/doc alias case, correctly
				// skipped. Record it (#143). It left no trace at all before, so
				// a rule scoping `**/*.yaml` over a `config.yaml` symlink saw
				// nothing and nothing said why; the empty-scope signal points at
				// the rule when the cause is the walk.
				fset.Ignored = append(fset.Ignored, Ignored{Path: rel, Dir: false, By: SourceSymlink})
			}
			return nil
		}
		fset.Files = append(fset.Files, &File{relPath: rel, absPath: p})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(fset.Files, func(i, j int) bool { return fset.Files[i].relPath < fset.Files[j].relPath })
	sort.Slice(fset.Ignored, func(i, j int) bool { return fset.Ignored[i].Path < fset.Ignored[j].Path })
	return fset, nil
}
