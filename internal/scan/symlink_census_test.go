package scan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// #143's last residual member. A symlink whose own name carries no source
// extension and which points at a FILE outside the tree is skipped — correctly,
// since formwork does not follow links — but it left the walk with no record at
// all. A rule scoping `**/*.yaml` over a `config.yaml -> ../shared/config.yaml`
// saw nothing, and nothing said why.
//
// Measured before this change: `scan: 0 file(s) scanned`, `scope matched no
// files`, exit 0, and no census line naming the link. There IS a signal — the
// empty scope — but it points at the rule rather than at the cause, so the
// operator is told their glob is wrong when the truth is that the walk declined
// to look.
//
// The refusal is right and stays: this is the declared config/doc alias case.
// What changes is that declining to look is now RECORDED, which is the same
// trade every other skip channel in this package already makes.
func TestNonSourceSymlinkToAFileIsCensused(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "config.yaml"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "config.yaml"), filepath.Join(root, "config.yaml")); err != nil {
		t.Skipf("cannot symlink here: %v", err)
	}

	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("a non-source symlink must be skipped, not refused: %v", err)
	}
	for _, f := range fset.Files {
		if f.Path() == "config.yaml" {
			t.Fatal("formwork does not follow symlinks; it must not be scanned")
		}
	}
	var found bool
	for _, ig := range fset.Ignored {
		if ig.Path == "config.yaml" {
			found = true
			if ig.By != scan.SourceSymlink {
				t.Errorf("censused under the wrong channel: %v", ig.By)
			}
			if ig.Dir {
				t.Error("a link to a FILE must not be recorded as a directory prune")
			}
		}
	}
	if !found {
		t.Fatalf("the skip must be recorded so the census can name it; Ignored = %+v",
			fset.Ignored)
	}
}

// The narrowing: an ordinary regular file is not a skip and must not appear.
// Without this the channel could report every file and still pass the test above.
func TestOrdinaryFileIsNotCensusedAsASkip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, ig := range fset.Ignored {
		if ig.Path == "a.go" {
			t.Fatalf("a scanned regular file must not be censused as skipped: %+v", ig)
		}
	}
}
