// symlinkdir_test.go — #143 row 4. os.ReadDir reports a symlink by its own
// Lstat, so a symlink named `fire-1` has IsDir() == false and the discovery
// loop's `if !e.IsDir() { continue }` skipped it exactly the way it skips a
// legitimate `fire-1.want` manifest. The fire proof never executed and
// `formwork test` printed `OK — 1 fixture(s)` at exit 0: the shape this whole
// codebase exists to stop, aimed at the proof tree itself.
//
// The refusal is narrowed to entries NAMED fire-*/pass-*, because that is the
// only way to tell "a fixture we cannot enter" from ".want file" — and .want
// files are why the skip exists at all.
package fixturetest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

func TestSymlinkedFixtureDirIsRefusedNotSkipped(t *testing.T) {
	for _, name := range []string{"fire-1", "pass-1"} {
		t.Run(name, func(t *testing.T) {
			cfg, root := loadRepo(t, map[string]string{
				// A real, passing fixture pair so the run has something to
				// report — without it the rule has no fixtures at all and the
				// loop never reaches the entry under test.
				".formwork/fixtures/fruit-free/fire-9/f.txt": "a banana here want: fruit-free\n",
				".formwork/fixtures/fruit-free/pass-9/f.txt": "nothing here\n",
				// The real tree the symlink will point at, parked outside the
				// fixtures root so it is not itself discovered.
				"elsewhere/f.txt": "a banana here want: fruit-free\n",
			})

			link := filepath.Join(root, ".formwork", "fixtures", "fruit-free", name)
			if err := os.Symlink(filepath.Join(root, "elsewhere"), link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}

			var sb strings.Builder
			_, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
			if err == nil {
				t.Fatalf("a symlinked %s fixture dir must be refused, not silently skipped; output:\n%s", name, sb.String())
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("the refusal must name the entry it could not enter, got: %v", err)
			}
		})
	}
}

// The counterpart, and the reason the refusal is keyed on the NAME: a `.want`
// manifest is a regular file sitting beside the fixture dirs and must keep
// being skipped in silence. Without this, closing row 4 would refuse every
// corpus in the repo.
func TestWantManifestIsStillSkippedSilently(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "a banana here\n",
		".formwork/fixtures/fruit-free/fire-1.want":  "f.txt:1\n",
	})
	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil {
		t.Fatalf("a .want manifest beside the fixture dirs must not be refused: %v\n%s", err, sb.String())
	}
}
