package scan_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func TestWalkFailsOnUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; directory permissions not enforced")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "hidden.txt"), "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := scan.Walk(root); err == nil {
		t.Fatal("expected an error for an unreadable directory; walk must never skip silently")
	}
}

func TestFileConcurrentContentAndLines(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "f.txt"), "one\ntwo\n")
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	f := fset.Files[0]
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Content(); err != nil {
				t.Error(err)
			}
			if _, err := f.Lines(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	lines, err := f.Lines()
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines after concurrent reads: %v, %v", lines, err)
	}
}
