package vcs_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs/gitseal"
)

// Hermeticity for every throwaway repository this package builds (#295).
//
// THE SEAL IS ON THE PROCESS, NOT ON initRepo, and that is the same altitude
// decision internal/hooks recorded for #170. `initRepo` in vcs_test.go is the
// obvious place and it is the wrong one: hooks_test.go reaches linked worktrees
// through `git worktree add`, hatch_test.go builds a repository at a private
// $HOME, ignored_test.go and configenv_test.go run git at paths initRepo never
// saw, and the next fixture written here will start by calling
// `exec.Command("git", ...)` because that is what the file above it does. A
// seal applied per repository is a seal each of those bypasses. git reads its
// configuration from the ENVIRONMENT of the process that runs it, so the
// environment is where the invariant lives, and TestMain is the only seam in a
// test binary that owns it before the first test runs.
//
// The seal itself is internal/vcs/gitseal, shared rather than copied: #170
// wrote it as unexported lines inside `package hooks_test`, so this package —
// which asserts git's answers more directly than any other in the module —
// could not call it and did without (#295).
func TestMain(m *testing.M) { os.Exit(gitseal.Run(m)) }

// The seal, proved from outside the sealed process — which is the only place it
// can be proved.
//
// A TEST INSIDE THIS BINARY CANNOT DEMONSTRATE IT. TestMain has already run by
// the time any test body does, so an assertion here is taken after the hostile
// variable was removed and passes identically on a machine that never set one.
// That is the vacuous shape this repo refuses: it would go green for the same
// reason the code does, and stay green with the seal deleted on every CI
// machine — which is exactly where #170's residue hid until #295, because
// GitHub and Blacksmith runners set no global core.hooksPath.
//
// So the hostile environment is constructed here and the seal is exercised in a
// CHILD run of this same test binary, which runs TestMain for itself. Both
// spellings the issue names are covered, and they are not the same test:
// GIT_CONFIG_GLOBAL is the variable #170's reproduction uses, while HOME is the
// mechanism the seal itself relies on and the one a machine-wide hook runner
// actually installs into, so a seal that merely neutralised one variable passes
// the first row and fails the second.
//
// The child is pinned to ONE test rather than the whole package: what is being
// proved is that ambient configuration does not reach a fixture, and
// TestHooksPathUnsetIsGitsDefaultHooksDir is a fixture that goes red the moment
// it does — it asks git for the hooks directory of a repository that sets none,
// so a global core.hooksPath answers in place of the repository and the assert
// fails with the developer's path in it. It is one of the three internal/vcs
// failures #295 names. Running the whole package would prove the same thing at
// many times the cost and would recurse through this test.
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
			cmd := exec.Command(self, "-test.run=^TestHooksPathUnsetIsGitsDefaultHooksDir$")
			cmd.Env = append(os.Environ(), tc.env...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("a child run of this test binary under %s failed, so the ambient git configuration reaches this package's fixtures: %v\n%s", tc.name, err, out)
			}
		})
	}
}

// The seal's proof above is only as discriminating as the fixture it is pinned
// to, and that fixture discriminates only while initRepo leaves core.hooksPath
// unset. This holds that (#295).
//
// It is not hypothetical housekeeping. The cheap way to make #295's three
// failures go away is one more line in initRepo's config loop —
// `{"config", "core.hooksPath", ".git/hooks"}` — beside the core.quotepath pin
// that is already there for the same class of ambient leak. It works: the three
// tests go green under a global core.hooksPath. It also silently converts
// TestHooksPathUnsetIsGitsDefaultHooksDir from "what does git answer when
// nothing sets a hooks path" into "what does git answer when this repository
// sets one", which is a question with no ambient config in it and therefore an
// answer that cannot disagree with any environment. The seal proof pinned to
// that fixture would then pass under both hostile spellings with the seal
// deleted, and #295 would close a second time with the residue intact.
//
// So the pin is refused at the fixture and the leak is closed at the process,
// and this test is what keeps the two decisions from being quietly swapped.
//
// LOCAL SCOPE ONLY, deliberately: an effective `git config --get` would be
// asking the seal's question again, and would report a difference the seal
// already owns. What cannot be delegated is whether the repository writes the
// setting into its own config, which is exactly what --local reads.
func TestInitRepoLeavesCoreHooksPathUnsetSoTheFixturesStayDiscriminating(t *testing.T) {
	dir := initRepo(t)

	out, err := exec.Command("git", "-C", dir, "config", "--local", "--get", "core.hooksPath").Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		t.Fatalf("initRepo wrote core.hooksPath = %q into the fixture repository's own config, so every hooks-path assertion in this package now measures that value instead of git's answer for a repository that declares none", strings.TrimRight(string(out), "\r\n"))
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		// Exit 1 is git's "not set", which is the state this test is for.
	default:
		// Anything else means the question was never asked. A git that cannot
		// answer must not read as an answer of "unset".
		t.Fatalf("cannot ask git for the fixture repository's local core.hooksPath: %v", err)
	}
}
