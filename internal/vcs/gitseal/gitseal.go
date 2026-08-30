// Package gitseal seals a test binary's process environment against the
// developer's ambient git configuration.
//
// It exists because #170's seal was written as unexported lines inside
// `package hooks_test`, which put it out of reach of every other package in the
// module — and internal/vcs, internal/cli and internal/repoproof build the same
// throwaway repositories against the same assumption that no machine-wide
// `core.hooksPath` is answering for them (#295). A seal that only one package
// can call is a seal the next package silently does without.
//
// The entry point is Run, wired from a TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(gitseal.Run(m)) }
//
// THE SEAL IS ON THE PROCESS, NOT ON THE REPO BUILDER, and that is the altitude
// decision. Each caller has an obvious per-repository place to put this — a
// `repo()` or `initRepo()` helper — and it is the wrong one: fixtures reach
// linked worktrees through `git worktree add`, build repositories with a bare
// `git init` of their own, and run git at paths the helper never saw, and the
// next fixture written in those files starts by calling `exec.Command("git",
// ...)` because that is what the file above it does. A seal applied per
// repository is a seal each of those bypasses. git reads its configuration from
// the ENVIRONMENT of the process that runs it, so the environment is where the
// invariant lives, and TestMain is the only seam in a test binary that owns it
// before the first test runs.
//
// WHAT IS SEALED, AND WHY IT IS A PREFIX SWEEP RATHER THAN A LIST. Every
// variable named GIT_*, plus FORMWORK_GIT_ENV, is removed. internal/hooks's
// gitenv.go records why a list is the wrong shape — git's documentation does not
// name GIT_CONFIG_PARAMETERS anywhere, so no reading of it produces the variable
// that mattered — and the same argument applies to a test seal: GIT_TEMPLATE_DIR
// changes what `git init` writes into .git/hooks, GIT_CEILING_DIRECTORIES
// changes which repository git discovers, and neither is in the family the
// production guards measure. No caller needs a GIT_* variable inherited from the
// developer, so removing all of them costs nothing and bounds the hostile set by
// a rule rather than by whoever last read githooks(5). A test that wants one
// sets it for itself with t.Setenv, which runs long after this does.
//
// GLOBAL CONFIG IS SEALED THROUGH HOME, NOT GIT_CONFIG_GLOBAL. Pointing that
// variable at an empty file is the usual move and it cannot be used here:
// #146 R7 refuses to certify a repository whenever the ambient environment moves
// git's answer, and #167 D9 extended the same refusal to install, so the fixture
// would trip the guard it exists to neutralise. A private HOME with no
// .gitconfig in it is what a machine-wide hook runner actually installs into,
// and it reaches git's global scope while setting no variable any guard
// measures. XDG_CONFIG_HOME moves with it because git reads
// $XDG_CONFIG_HOME/git/config as well — the same pairing internal/hooks's
// preflight_test.go globalHooksPath and internal/vcs's configenv_test.go
// globalGitConfig make, for the same reason.
//
// SYSTEM CONFIG IS NOT SEALED, AND THAT IS A DECISION RATHER THAN AN OVERSIGHT.
// /etc/gitconfig has exactly two overrides, GIT_CONFIG_SYSTEM and
// GIT_CONFIG_NOSYSTEM, and both are members of the family above: setting either
// would put every test in the calling package under a variable several of those
// tests exist to distinguish the presence of. So the seal cannot reach it, and
// the answer is to say so LOUDLY instead of running the suite and watching it
// fail one test at a time with no shared cause — see systemConfigRefusal below.
package gitseal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// sealedGitEnvPrefix is the family swept from the environment before any test
// runs. Compared as a prefix on the NAME, which is what the environment is on
// unix; a Windows port needs a case fold, as internal/vcs's own filters do.
const sealedGitEnvPrefix = "GIT_"

// Run applies the seal and runs the suite, returning the code TestMain exits
// with.
//
// The suite is run from here rather than from the caller's TestMain so the
// temporary HOME can be removed on a defer — os.Exit runs none, and a caller
// that took a seal and then called m.Run() itself would leak one directory per
// package per run.
//
// A failure to seal returns 2 rather than running: the exit code this repo
// reserves for "the run could not be performed", as against 1 for a run that
// found something. Every fixture below the seal assumes a repository with no
// ambient wiring, so an unsealed run reports defects it invented.
func Run(m *testing.M) int {
	who := diagnosticName()

	home, err := os.MkdirTemp("", "formwork-test-home")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot create a private HOME to seal git's global config: %v\n", who, err)
		return 2
	}
	defer func() { _ = os.RemoveAll(home) }()

	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			// An entry with no "=" is not a variable this can be about — the
			// reading internal/vcs's own environment filters take.
			continue
		}
		if !strings.HasPrefix(name, sealedGitEnvPrefix) && name != vcs.GitEnvVar {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot unset %s: %v\n", who, name, err)
			return 2
		}
	}
	// The Go toolchain's caches are read from HOME too, and internal/hooks's
	// commit_test.go builds the formwork binary — so the redirect below moved
	// GOMODCACHE and GOCACHE to an empty directory and every run of that package
	// re-downloaded the whole module graph (measured: ten `go: downloading` lines
	// and 10s added, and on a machine with no network it is a failure rather than
	// a delay). Pinning them to the values the developer's environment already
	// resolves keeps the seal about git, which is the only thing it is entitled
	// to be about. Asked BEFORE HOME moves, which is what makes the answer the
	// developer's rather than the private home's.
	goEnv := goCachePaths()
	for k, v := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot set %s: %v\n", who, k, err)
			return 2
		}
	}
	for k, v := range goEnv {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot set %s: %v\n", who, k, err)
			return 2
		}
	}
	if msg := systemConfigRefusal(who, home); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		return 2
	}
	return m.Run()
}

// diagnosticName names the test binary in the messages below.
//
// The seal used to live in one package and could say "internal/hooks tests:"
// truthfully; shared, that spelling would be a lie in three of four callers, and
// a package name passed in as an argument is a string a caller can get wrong and
// nothing checks. os.Args[0] is the running binary, whose base is <package>.test
// under `go test` — so the message names the process that printed it, which is
// what the reader needs to find it.
func diagnosticName() string {
	base := filepath.Base(os.Args[0])
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "formwork test git seal"
	}
	return "formwork test git seal (" + base + ")"
}

// goCachePaths asks the toolchain where its caches are, BEFORE HOME moves, so
// they can be pinned there afterwards.
//
// It asks `go env` rather than spelling the defaults, because those defaults are
// what HOME is about to invalidate and a second guess at them would be the same
// bug written twice. A failure to ask returns nothing: the Go environment is
// then left to resolve however it resolves, which is a slow build rather than a
// wrong one, and this is a test seal with no business failing a suite over the
// toolchain's own configuration.
func goCachePaths() map[string]string {
	names := []string{"GOPATH", "GOCACHE", "GOMODCACHE"}
	out, err := exec.Command("go", append([]string{"env"}, names...)...).Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(lines) != len(names) {
		return nil
	}
	pinned := map[string]string{}
	for i, name := range names {
		if v := strings.TrimSpace(lines[i]); v != "" {
			pinned[name] = v
		}
	}
	return pinned
}

// systemConfigRefusal returns the message to die on when core.hooksPath is still
// set after the seal, and the empty string when it is not.
//
// IT ASKS GIT RATHER THAN READING /etc/gitconfig. What matters is the effective
// answer in a fresh repository built the way the calling package's fixtures
// build one, and git resolves that across scopes no caller can enumerate — a
// `$(prefix)` gitconfig beside the binary answers here too, and no path spelled
// in Go finds it. Once HOME, XDG_CONFIG_HOME and the whole GIT_* family are
// sealed, an answer at all means a scope the seal cannot reach.
//
// A GIT THAT IS NOT THERE IS NOT A REFUSAL. Every caller's repository helper
// skips when git is unavailable, and dying here would turn a skip into a failure
// for every machine without git.
func systemConfigRefusal(who, tmp string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	probe := filepath.Join(tmp, "seal-probe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		return fmt.Sprintf("%s: cannot create the seal probe repository: %v", who, err)
	}
	if out, err := exec.Command("git", "init", "-q", probe).CombinedOutput(); err != nil {
		return fmt.Sprintf("%s: cannot init the seal probe repository: %v\n%s", who, err, out)
	}
	out, err := exec.Command("git", "-C", probe, "config", "--get", "core.hooksPath").Output()
	if err != nil {
		return "" // exit 1 is "not set", which is the state the seal is for
	}
	return fmt.Sprintf(`%s: git still reports core.hooksPath = %q in a brand-new repository after this binary sealed HOME, XDG_CONFIG_HOME and every GIT_* variable.
The only scope left is system config (/etc/gitconfig, or a gitconfig beside the git binary), and its only overrides — GIT_CONFIG_SYSTEM and GIT_CONFIG_NOSYSTEM — are members of the family this repository's own guards measure, so the seal deliberately does not use them (#170).
Unset core.hooksPath in system config, or run these tests as a user whose git does not read it. Refusing here rather than running: every fixture below assumes a repository with no hook wiring, and the failures would arrive one per test with no shared cause.`,
		who, strings.TrimRight(string(out), "\r\n"))
}
