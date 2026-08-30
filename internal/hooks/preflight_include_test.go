package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/hooks"
)

// D11: a core.hooksPath this repository declares through an `include.path`
// directive (#173). It is a THIRD refusal, not a variant of D7's, and the
// difference is the sentence D7 prints — "a setting outside this repository owns
// the hook wiring here" — which is FALSE here: the declaration is in this
// repository's own .git/config, one indirection away. `git config --local --get`
// defaults to --no-includes, so the ownership read reports unset while git runs
// hooks from the included value, and --override-global (documented as never
// clearing D2) then overwrote the project's own wiring.
//
// These live in their own file rather than in preflight_test.go, which is close
// enough to the repo's own 750-line cap (.formwork/rules/file-size.yaml) that
// this section would need a split immediately.
//
// The helpers come from the rest of the package's tests: repo, gitT, laneCfg,
// treeSnapshot/wantUnchanged, wantErrContains/wantErrLacks, managedDir.

// gitHooksPath is the directory git says it will run hooks from, as git spells
// it. Every fixture below asserts on it before install runs: an include that
// silently failed to take effect leaves a repository install has no reason to
// refuse, and the test would then pass for the wrong reason on a detector that
// never fired. Two of the fixtures below were inert when first written — the
// includeIf condition and the escaping relative path — which is exactly the
// state this assertion exists to catch.
func gitHooksPath(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-path hooks: %v", err)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func wantGitRunsHooksFrom(t *testing.T, dir, want string) {
	t.Helper()
	if got := gitHooksPath(t, dir); got != want {
		t.Fatalf("git runs hooks from %q, want %q — the fixture's include is not in effect, so it exercises nothing", got, want)
	}
}

// appendConfig adds lines to a config file of this repository, creating it if
// git has not. `git config --local include.path <p>` would do the first fixture
// only: it appends, and half the point of these fixtures is WHERE in the file
// the include sits (config is last-one-wins), plus includeIf and
// config.worktree, which that command cannot spell at all.
func appendConfig(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// includeSpellings is the class, not the one spelling issue #173 was filed with.
// Every row is measured on git 2.50.1: `--local --get` reports the key unset,
// `rev-parse --git-path hooks` answers the included value, and git therefore
// runs the project's hooks while formwork's ownership read sees nothing.
//
// The last row is the one that decides the implementation. An include inside
// .git/config.worktree is invisible to `--local --includes` as well, so the fix
// the issue considers — adding --includes to the local read — closes six of
// these seven and reports the seventh as somebody else's wiring.
//
// wire installs the fixture and returns the value git will then answer with.
var includeSpellings = []struct {
	name string
	wire func(t *testing.T, dir string) string
}{
	{"a relative include inside .git", func(t *testing.T, dir string) string {
		writeShimFile(t, filepath.Join(dir, ".git", "team.cfg"), "[core]\n\thooksPath = team-hooks\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = team.cfg\n")
		return "team-hooks"
	}},
	{"an absolute include inside the worktree", func(t *testing.T, dir string) string {
		cfg := filepath.Join(dir, "team.cfg")
		writeShimFile(t, cfg, "[core]\n\thooksPath = abs-hooks\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = "+cfg+"\n")
		return "abs-hooks"
	}},
	{"an absolute include outside the repository", func(t *testing.T, dir string) string {
		cfg := filepath.Join(t.TempDir(), "elsewhere.cfg")
		writeShimFile(t, cfg, "[core]\n\thooksPath = outside-hooks\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = "+cfg+"\n")
		return "outside-hooks"
	}},
	// The relative spelling escapes the repository too, which is why "follow
	// only relative includes" is not a safe middle ground: a relative path is
	// resolved from the directory of the file holding it, and `../../` leaves
	// .git and then the worktree.
	{"a relative include escaping the repository", func(t *testing.T, dir string) string {
		writeShimFile(t, filepath.Join(filepath.Dir(dir), "up.cfg"), "[core]\n\thooksPath = up-hooks\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = ../../up.cfg\n")
		return "up-hooks"
	}},
	{"a nested include", func(t *testing.T, dir string) string {
		writeShimFile(t, filepath.Join(dir, ".git", "inner.cfg"), "[core]\n\thooksPath = nested-hooks\n", 0o644)
		writeShimFile(t, filepath.Join(dir, ".git", "outer.cfg"), "[include]\n\tpath = inner.cfg\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = outer.cfg\n")
		return "nested-hooks"
	}},
	{"an includeIf gitdir condition", func(t *testing.T, dir string) string {
		writeShimFile(t, filepath.Join(dir, ".git", "cond.cfg"), "[core]\n\thooksPath = cond-hooks\n", 0o644)
		// git matches the pattern against the RESOLVED git directory, so the
		// spelling t.TempDir hands back on macOS (/var/… for /private/var/…)
		// matches nothing and the fixture goes inert.
		appendConfig(t, filepath.Join(dir, ".git", "config"), "[includeIf \"gitdir:"+resolved(t, dir)+"/\"]\n\tpath = cond.cfg\n")
		return "cond-hooks"
	}},
	{"an include in .git/config.worktree", func(t *testing.T, dir string) string {
		gitT(t, dir, "config", "--local", "extensions.worktreeConfig", "true")
		writeShimFile(t, filepath.Join(dir, ".git", "wt.cfg"), "[core]\n\thooksPath = wt-hooks\n", 0o644)
		appendConfig(t, filepath.Join(dir, ".git", "config.worktree"), "[include]\n\tpath = wt.cfg\n")
		return "wt-hooks"
	}},
}

func TestInstallRefusesADeclarationMadeThroughAnInclude(t *testing.T) {
	for _, tc := range includeSpellings {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			hooksPath := tc.wire(t, dir)
			wantGitRunsHooksFrom(t, dir, hooksPath)
			before := treeSnapshot(t, dir)

			installed, err := hooks.Install(dir, laneCfg("pre-commit", "pre-push"), false)
			wantErrContains(t, err,
				"include.path",   // what formwork found, named so the operator can go and look
				hooksPath,        // and where git is running hooks from because of it
				"will not guess", // the verdict: neither taken over nor certified
				"formwork check --lane pre-commit --staged", // chain formwork from the runner in charge
				"formwork check --lane pre-push")
			// D7's escape is for a wiring wider than this repository. Formwork
			// cannot say that here — the declaration is one indirection away
			// inside this repository's own config — and a flag offered on a
			// false premise is how #173 overwrote the project's wiring.
			wantErrLacks(t, err, "--override-global",
				"a setting outside this repository owns the hook wiring")
			if len(installed) != 0 {
				t.Errorf("a refusal reported hooks it installed: %v", installed)
			}
			wantUnchanged(t, before, treeSnapshot(t, dir))
			if got := gitHooksPath(t, dir); got != hooksPath {
				t.Errorf("after the refusal git runs hooks from %q, want %q", got, hooksPath)
			}
		})
	}
}

// --override-global answers D7 and nothing else, and this is not D7. The flag's
// own documentation says it never clears D2 — the project's own wiring — and a
// declaration made through an include is at least as likely to be that.
//
// This is also where the INERT WRITE (#173's second half) dies. With the include
// sitting after `[core]` in .git/config — where `git config --local` writes —
// config is last-one-wins, so install's write landed, changed nothing git does,
// and the command reported `installed git hooks` at exit 0. Reaching that state
// needs install to get past the pre-flight, which is what this asserts it cannot.
func TestOverrideGlobalDoesNotUnlockAnIncludedDeclaration(t *testing.T) {
	for _, tc := range includeSpellings {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			hooksPath := tc.wire(t, dir)
			wantGitRunsHooksFrom(t, dir, hooksPath)
			before := treeSnapshot(t, dir)

			installed, err := hooks.Install(dir, laneCfg("pre-commit"), true)
			if err == nil {
				t.Fatal("--override-global unlocked a refusal that is not D7's")
			}
			if len(installed) != 0 {
				t.Errorf("a refusal reported hooks it installed: %v", installed)
			}
			wantUnchanged(t, before, treeSnapshot(t, dir))
			if got := gitHooksPath(t, dir); got != hooksPath {
				t.Errorf("after the refusal git runs hooks from %q, want %q — the include still governs", got, hooksPath)
			}
		})
	}
}

// THE CONTROL, and it is the half that keeps the refusal from being "this
// repository has an include". Includes are ordinary: teams share user identity,
// aliases and URL rewrites through them, and a repository whose include says
// nothing about core.hooksPath is one formwork installs into normally.
//
// The teamprobe assertion proves the include is LIVE. Without it the include
// could be inert — a path git never read — and this test would pass on a
// detector that fires for every include.
func TestInstallWiresARepositoryWhoseIncludeDeclaresSomethingElse(t *testing.T) {
	dir := repo(t)
	writeShimFile(t, filepath.Join(dir, ".git", "team.cfg"), "[teamprobe]\n\tvalue = live\n", 0o644)
	appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = team.cfg\n")
	out, err := exec.Command("git", "-C", dir, "config", "--get", "teamprobe.value").Output()
	if err != nil || strings.TrimSpace(string(out)) != "live" {
		t.Fatalf("teamprobe.value = %q, %v; the fixture's include is not in effect", out, err)
	}
	wantGitRunsHooksFrom(t, dir, ".git/hooks")

	installed, err := hooks.Install(dir, laneCfg("pre-commit"), false)
	if err != nil {
		t.Fatalf("install refused a repository whose include says nothing about core.hooksPath: %v", err)
	}
	if len(installed) != 1 || installed[0] != "pre-commit" {
		t.Fatalf("installed = %v, want [pre-commit]", installed)
	}
	if _, err := os.Stat(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
		t.Fatalf("no shim on disk: %v", err)
	}
}

// The other control: an include that declares core.hooksPath is refused, but a
// repository that declares it in the BODY of its own config is D2's, and D2's
// message is the one that must arrive — it names the declared value, which
// formwork will not print for an included one.
func TestADeclaredValueIsStillD2NotTheIncludeRefusal(t *testing.T) {
	dir := repo(t)
	gitT(t, dir, "config", "--local", "core.hooksPath", ".husky")

	_, err := hooks.Install(dir, laneCfg("pre-commit"), false)
	wantErrContains(t, err, "this repository declares core.hooksPath = .husky")
	wantErrLacks(t, err, "include.path")
}
