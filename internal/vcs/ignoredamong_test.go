package vcs_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// keysOf is every query plus every directory above it — the keys a path-scoped
// caller looks up.
func keysOf(queries ...string) map[string]bool {
	keys := map[string]bool{}
	for _, q := range queries {
		for p := q; p != "." && p != ""; p = path.Dir(p) {
			keys[p] = true
		}
	}
	return keys
}

// wholeListingFor is the oracle: IgnoredUnder's answer, restricted to the keys
// the queries can look up. IgnoredAmong must return exactly this.
func wholeListingFor(t *testing.T, dir string, queries ...string) []vcs.IgnoredPath {
	t.Helper()
	all, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	return restrictTo(all, queries...)
}

func restrictTo(all []vcs.IgnoredPath, queries ...string) []vcs.IgnoredPath {
	keys := keysOf(queries...)
	var kept []vcs.IgnoredPath
	for _, r := range all {
		if keys[r.Path] {
			kept = append(kept, r)
		}
	}
	return kept
}

// ignoreFixture is one repository holding every shape the ls-files walk treats
// differently: nested .gitignore files, negations (including one that re-opens
// a directory under a `dir/*` pattern), the --directory collapse of an ignored
// directory and the listing of untracked-but-not-ignored ancestors, tracked
// files under an ignored directory, symlinks, nested repositories, a gitlink,
// an empty ignored directory and a quote-worthy name.
func ignoreFixture(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, ".gitignore", strings.Join([]string{
		"build/", "*.log", "!important.log", "gen/*", "!gen/keep/", "/top-only",
		"emptyign/", "nestign/", "linkbuild/", "ignored/", "vendor/", "gen2/*", "sparse/",
		"casedir/", "config", "",
	}, "\n"))
	for _, f := range []string{
		"build/a.txt", "build/sub/deep/x.go",
		"vendor/kept.go", "vendor/a.txt", "vendor/sub/z.txt",
		"tracked.log", "x.log", "important.log", "logs/y.log", "logs/important.log",
		"gen/g.txt", "gen/keep/k.txt", "onlyign/z.log", "a/b/ignored/deep.txt",
		"top-only", "sub/top-only", "sub/x.tmp", "sub/keep.tmp", "sub/deeper/y.tmp",
		"src/main.go", "src/new.go", "nestign/z", "nest/y.log", "sublink/x.log", "café.log",
		"gen2/a.txt", "sparse/t.go", "sparse/u.txt", "casedir/t.go", "casedir/u.txt",
	} {
		write(t, dir, f, "x\n")
	}
	write(t, dir, "sub/.gitignore", "*.tmp\n!keep.tmp\n")
	if err := os.MkdirAll(filepath.Join(dir, "emptyign"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, filepath.Join(dir, "nest"), "init", "-q")
	run(t, filepath.Join(dir, "nestign"), "init", "-q")
	for link, target := range map[string]string{"linkbuild": "src", "link.log": "src/main.go"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	run(t, dir, "add", ".gitignore", "sub/.gitignore", "src/main.go")
	run(t, dir, "add", "-f", "vendor/kept.go", "tracked.log", "sparse/t.go", "casedir/t.go")
	run(t, dir, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,sublink")
	// A skip-worktree entry is still an index entry: the walk enters sparse/
	// and never lists t.go, though the file is on disk and a pattern matches it.
	run(t, dir, "update-index", "--skip-worktree", "sparse/t.go")
	// The index spells casedir/ in lower case and the disk now does not. Where
	// core.ignorecase is set the walk's index lookups fold case, so CaseDir is a
	// directory with a tracked file — entered, not collapsed, and its t.go never
	// listed — while check-ignore's own tracked test is case-sensitive and
	// confirms both CaseDir and CaseDir/t.go. Only the folded index facts keep
	// the answer the walk's. (On a case-sensitive filesystem without
	// core.ignorecase this is simply an untracked directory no pattern matches.)
	if err := os.Rename(filepath.Join(dir, "casedir"), filepath.Join(dir, "CaseDir")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// One row per shape: the answer must equal the whole listing restricted to the
// query's keys, and `want` pins what that is, so a row cannot pass by both
// sides returning nothing.
func TestIgnoredAmongMatchesTheWholeListing(t *testing.T) {
	dir := ignoreFixture(t)
	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"ignored directory collapses", "build/sub/deep/x.go", []string{"build"}},
		{"ghost under a collapsed directory", "build/new/ghost.go", []string{"build"}},
		{"tracked file under an ignored directory", "vendor/kept.go", nil},
		{"untracked sibling of a tracked file under an ignored directory", "vendor/a.txt", []string{"vendor/a.txt"}},
		{"ignored subdirectory of a directory with tracked files", "vendor/sub/z.txt", []string{"vendor/sub"}},
		{"tracked file matching a pattern", "tracked.log", nil},
		{"untracked file matching a pattern", "x.log", []string{"x.log"}},
		{"negated file", "important.log", nil},
		{"pattern below the root", "logs/y.log", []string{"logs/y.log"}},
		{"negated file below the root", "logs/important.log", nil},
		{"dir/* pattern", "gen/g.txt", []string{"gen/g.txt"}},
		{"directory re-included under dir/*", "gen/keep/k.txt", nil},
		{"untracked directory holding only ignored files", "onlyign/z.log", []string{"onlyign/z.log"}},
		{"untracked, unignored ancestors of an ignored directory", "a/b/ignored/deep.txt", []string{"a/b/ignored"}},
		{"anchored pattern at the root", "top-only", []string{"top-only"}},
		{"anchored pattern does not reach a subdirectory", "sub/top-only", nil},
		{"nested .gitignore", "sub/x.tmp", []string{"sub/x.tmp"}},
		{"nested .gitignore negation", "sub/keep.tmp", nil},
		{"nested .gitignore below its directory", "sub/deeper/y.tmp", []string{"sub/deeper/y.tmp"}},
		{"untracked file no pattern matches", "src/new.go", nil},
		{"empty ignored directory", "emptyign/f.go", []string{"emptyign"}},
		{"ignored nested repository", "nestign/z", []string{"nestign"}},
		{"unignored nested repository", "nest/y.log", nil},
		{"symlink to a directory is not one", "linkbuild/main.go", nil},
		{"ignored symlink", "link.log", []string{"link.log"}},
		{"gitlink", "sublink/x.log", nil},
		{"quote-worthy name", "café.log", []string{"café.log"}},
		{".git is never listed", ".git/config", nil},
		{"dir/* pattern over a directory holding only ignored files", "gen2/a.txt", []string{"gen2", "gen2/a.txt"}},
		{"skip-worktree file under an ignored directory", "sparse/t.go", nil},
		{"untracked sibling of a skip-worktree file", "sparse/u.txt", []string{"sparse/u.txt"}},
		{"directory whose index spelling differs in case", "CaseDir/t.go", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vcs.IgnoredAmong(dir, []string{tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if want := wholeListingFor(t, dir, tc.query); !reflect.DeepEqual(got, want) {
				t.Fatalf("IgnoredAmong(%q) = %+v, whole listing restricted to its keys = %+v", tc.query, got, want)
			}
			if !reflect.DeepEqual(paths(got), append([]string{}, tc.want...)) {
				t.Fatalf("IgnoredAmong(%q) paths = %v, want %v", tc.query, paths(got), tc.want)
			}
		})
	}
}

// Every path in the fixture, and a ghost file in every directory, asked one at
// a time and all at once: the rows above name the shapes, this asks every key
// the fixture has, so a shape the table does not name still has to agree.
func TestIgnoredAmongAgreesOnEveryPathOfTheFixture(t *testing.T) {
	dir := ignoreFixture(t)
	var queries []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			queries = append(queries, "zz-ghost.go")
			return nil
		}
		if d.Name() == ".git" && d.IsDir() {
			queries = append(queries, rel+"/config")
			return filepath.SkipDir
		}
		queries = append(queries, rel)
		if d.IsDir() {
			queries = append(queries, rel+"/zz-ghost.go")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(queries)
	all := wholeListingFor(t, dir, queries...)
	for _, q := range queries {
		got, err := vcs.IgnoredAmong(dir, []string{q})
		if err != nil {
			t.Fatalf("IgnoredAmong(%q): %v", q, err)
		}
		if want := restrictTo(all, q); !reflect.DeepEqual(got, want) {
			t.Errorf("IgnoredAmong(%q) = %+v, want %+v", q, got, want)
		}
	}
	got, err := vcs.IgnoredAmong(dir, queries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, all) {
		t.Errorf("IgnoredAmong(all %d paths) = %v, want %v", len(queries), paths(got), paths(all))
	}
}

// THE POINT OF #126, PINNED: an ordinary query never lists the working tree,
// and the shapes the model hands back to the whole listing do. git is wrapped
// on PATH so the test reads the argv of every call this package makes.
func TestIgnoredAmongListsTheTreeOnlyWhereTheModelStops(t *testing.T) {
	dir := ignoreFixture(t)
	log := gitCallLog(t)
	for _, tc := range []struct {
		query     string
		wantWhole bool
	}{
		{"build/sub/deep/x.go", false},
		{"vendor/sub/z.txt", false},
		{"sub/deeper/y.tmp", false},
		{"a/b/ignored/deep.txt", false},
		{"src/new.go", false},
		{"nest/y.log", true}, // nested repository: git decides what is one
		{"gen/g.txt", true},  // gen/ is confirmed only in its slash spelling
		{"sparse/u.txt", false},
	} {
		log.reset(t)
		if _, err := vcs.IgnoredAmong(dir, []string{tc.query}); err != nil {
			t.Fatal(err)
		}
		if whole := strings.Contains(log.read(t), "--others"); whole != tc.wantWhole {
			t.Errorf("IgnoredAmong(%q) listed the whole tree = %v, want %v; git calls:\n%s", tc.query, whole, tc.wantWhole, log.read(t))
		}
	}
}

// Above the top level, the walk's answer depends on directories the model was
// not asked about, so a subdirectory root takes the whole listing — and still
// answers in root-relative paths, as IgnoredUnder does.
func TestIgnoredAmongFromASubdirectoryRoot(t *testing.T) {
	dir := ignoreFixture(t)
	sub := filepath.Join(dir, "sub")
	got, err := vcs.IgnoredAmong(sub, []string{"x.tmp", "keep.tmp", "deeper/y.tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"deeper/y.tmp", "x.tmp"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("paths = %v, want %v", paths(got), want)
	}
	if want := wholeListingFor(t, sub, "x.tmp", "keep.tmp", "deeper/y.tmp"); !reflect.DeepEqual(got, want) {
		t.Fatalf("IgnoredAmong = %+v, want %+v", got, want)
	}
	// Inside a collapsed directory the whole listing is git's own internal
	// error ("directory entry not superset of prefix"), and the narrowed answer
	// must not be more confident than that: a root there is unanswerable both
	// ways, not answered by a model that cannot see the ignored parent.
	inside := filepath.Join(dir, "build", "sub")
	_, wholeErr := vcs.IgnoredUnder(inside)
	_, err = vcs.IgnoredAmong(inside, []string{"deep/x.go"})
	if (wholeErr == nil) != (err == nil) {
		t.Fatalf("root inside an ignored directory: IgnoredAmong err = %v, IgnoredUnder err = %v", err, wholeErr)
	}
}

// #175 holds for the narrowed query: under a moved index the answer is the
// paths both indexes call ignored, so a committed file is never pruned.
func TestIgnoredAmongNeverPrunesUnderAMovedIndex(t *testing.T) {
	dir := movedIndexRepo(t)
	queries := []string{"build/keep.go", "build/a.txt"}
	got, err := vcs.IgnoredAmong(dir, queries)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build/a.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("IgnoredAmong under a moved index = %v, want %v — a committed file inside build/ is pruned", paths(got), want)
	}
	if want := wholeListingFor(t, dir, queries...); !reflect.DeepEqual(got, want) {
		t.Fatalf("IgnoredAmong = %+v, want %+v", got, want)
	}
}

// Fail-closed, as IgnoredUnder: no repository is an error, never "nothing is
// ignored".
func TestIgnoredAmongNotARepoErrors(t *testing.T) {
	if _, err := vcs.IgnoredAmong(t.TempDir(), []string{"x.go"}); err == nil {
		t.Fatal("IgnoredAmong in a non-repo returned nil error")
	}
}

// BenchmarkIgnoredOnePath times the question a single-file check asks, both
// ways, over a real checkout: FORMWORK_BENCH_REPO names its root and
// FORMWORK_BENCH_PATH the root-relative file. It is skipped without them.
//
//	FORMWORK_BENCH_REPO=~/src/repo FORMWORK_BENCH_PATH=cmd/main.go \
//	  go test ./internal/vcs -run '^$' -bench IgnoredOnePath -benchtime 5x
func BenchmarkIgnoredOnePath(b *testing.B) {
	repo, file := os.Getenv("FORMWORK_BENCH_REPO"), os.Getenv("FORMWORK_BENCH_PATH")
	if repo == "" || file == "" {
		b.Skip("set FORMWORK_BENCH_REPO and FORMWORK_BENCH_PATH")
	}
	b.Run("whole-listing", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := vcs.IgnoredUnder(repo); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("path-scoped", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := vcs.IgnoredAmong(repo, []string{file}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// callLog is a git wrapper placed first on PATH that appends each call's argv
// to a file before running the real git.
type callLog struct{ file string }

func gitCallLog(t *testing.T) callLog {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	bin := t.TempDir()
	log := callLog{file: filepath.Join(t.TempDir(), "calls")}
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log.file + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func (l callLog) reset(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(l.file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (l callLog) read(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(l.file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
