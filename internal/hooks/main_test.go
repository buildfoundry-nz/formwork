package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs/gitseal"
)

// Hermeticity for every throwaway repository this package builds (#170).
//
// The seal is internal/vcs/gitseal, which holds the whole argument: why it sits
// on the process rather than on `repo()`, why the GIT_* family is swept by
// prefix instead of by list, why global config is sealed through HOME rather
// than GIT_CONFIG_GLOBAL, and why system config is refused loudly rather than
// worked around. It was written here and lived here as unexported lines of
// `package hooks_test`, which is what left internal/vcs unsealed and #295 open;
// nothing about it is specific to this package, so it now lives where every
// package can call it.
func TestMain(m *testing.M) { os.Exit(gitseal.Run(m)) }

// The seal, proved from outside the sealed process — which is the only place it
// can be proved.
//
// A TEST INSIDE THIS BINARY CANNOT DEMONSTRATE IT. TestMain has already run by
// the time any test body does, so an assertion here is taken after the hostile
// variable was removed and passes identically on a machine that never set one.
// That is the vacuous shape this repo calls a tautological test: it would go
// green for the same reason the code does, and stay green with the seal deleted
// on every CI machine, which is exactly where #170 was invisible.
//
// So the hostile environment is constructed here and the seal is exercised in a
// CHILD run of this same test binary, which runs TestMain for itself. Both
// spellings the issue names are covered, and they are not the same test:
// GIT_CONFIG_GLOBAL is the variable the reproduction uses, while HOME is the
// mechanism the seal itself relies on, so a seal that merely neutralised one
// variable passes the first row and fails the second.
//
// The child is pinned to ONE test rather than the whole package: what is being
// proved is that the ambient configuration does not reach a fixture, and
// TestInstallAndVerify is a fixture that goes red the moment it does (measured:
// under GIT_CONFIG_GLOBAL naming a file with core.hooksPath = .husky, it is one
// of 61 failures on the pre-fix tree). Running the package would prove the same
// thing at 60× the cost and would recurse through this test.
func TestTheSuiteIsSealedFromAmbientGitConfiguration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	self := os.Args[0]
	if fi, err := os.Stat(self); err != nil || fi.IsDir() {
		t.Skipf("this test binary is not runnable as %q: %v", self, err)
	}

	hostileHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostileHome, ".gitconfig"), []byte("[core]\n\thooksPath = .husky\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostileFile := filepath.Join(t.TempDir(), "devglobal.cfg")
	if err := os.WriteFile(hostileFile, []byte("[core]\n\thooksPath = .husky\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		env  []string
	}{
		{"GIT_CONFIG_GLOBAL naming a file that sets core.hooksPath", []string{"GIT_CONFIG_GLOBAL=" + hostileFile}},
		{"a HOME whose .gitconfig sets core.hooksPath", []string{"HOME=" + hostileHome, "XDG_CONFIG_HOME=" + filepath.Join(hostileHome, ".config")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(self, "-test.run=^TestInstallAndVerify$")
			cmd.Env = append(os.Environ(), tc.env...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("a child run of this test binary under %s failed, so the ambient git configuration reaches this package's fixtures: %v\n%s", tc.name, err, out)
			}
		})
	}
}
