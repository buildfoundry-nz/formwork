package scan_test

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func treeOf(t *testing.T) *scan.FileSet {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"go.mod", "db/a.go", "db/b.go", "db/sub/c.go"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package db\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// A file knows the walk that found it, and so does every file standing in for
// it — a preprocessor variant, a masked copy — and a file restricted out of a
// range run's set is still in it. A file made in memory has no walk.
func TestAFileAnswersWithTheWalkThatFoundIt(t *testing.T) {
	fs := treeOf(t)
	f := fs.File("db/a.go")
	if f == nil || f.Tree() != fs {
		t.Fatalf("File(db/a.go) = %v, its Tree is not the walk", f)
	}
	v, err := f.Variant("decomment-go")
	if err != nil || v.Tree() != fs {
		t.Fatalf("a variant's Tree is its source's walk: err %v", err)
	}
	if m := f.WithContent([]byte("package db // masked\n")); m.Tree() != fs || m.Raw() != m {
		t.Fatalf("a masked copy keeps the walk and is its own Raw")
	}
	only := fs.Restrict(map[string]bool{"db/b.go": true})
	if len(only.Files) != 1 || only.Files[0].Tree() != fs {
		t.Fatalf("a restricted run's file answers with the whole walk")
	}
	if scan.NewMemFile("x.go", nil).Tree() != nil {
		t.Fatalf("a file made in memory has no walk")
	}
}

func TestDirListsTheFilesDirectlyInADirectory(t *testing.T) {
	fs := treeOf(t)
	var got []string
	for _, f := range fs.Dir("db") {
		got = append(got, f.Path())
	}
	if len(got) != 2 || got[0] != "db/a.go" || got[1] != "db/b.go" {
		t.Fatalf("Dir(db) = %v, want [db/a.go db/b.go]", got)
	}
	if root := fs.Dir("."); len(root) != 1 || root[0].Path() != "go.mod" {
		t.Fatalf("Dir(.) = %v, want [go.mod]", root)
	}
	if fs.File("db/none.go") != nil || fs.Dir("none") != nil {
		t.Fatalf("a path the walk did not keep answers nil")
	}
}

// One value per key per walk, whoever asks first and however many ask at once.
func TestMemoBuildsOneValuePerKey(t *testing.T) {
	fs := treeOf(t)
	type key struct{}
	var builds atomic.Int32
	var wg sync.WaitGroup
	got := make([]any, 16)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = fs.Memo(key{}, func() any { builds.Add(1); return new(int) })
		}(i)
	}
	wg.Wait()
	for _, g := range got {
		if g != got[0] {
			t.Fatalf("Memo returned two values for one key")
		}
	}
	if fs.Memo(key{}, func() any { t.Fatal("built again"); return nil }) != got[0] {
		t.Fatalf("a later call returns the stored value")
	}
	if builds.Load() < 1 {
		t.Fatalf("never built")
	}
}
