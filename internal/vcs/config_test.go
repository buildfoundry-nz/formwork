package vcs_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// The repository-scoped config reader and writer are tested here rather than in
// vcs_test.go, which is at the 750-line cap this repo enforces on itself. The
// shared helpers (initRepo, run, write) live there and are package-level, so
// nothing about the split changes what these tests can reach.

// isolateWiderScopes points git's global and system config at files this test
// controls, and returns the global file's path. Without it a machine-global
// core.hooksPath would decide the RepoConfig assertions below — in the
// direction that hides the defect, since "the repository declares nothing"
// would then be read off someone's ~/.gitconfig.
func isolateWiderScopes(t *testing.T, globalContent string) string {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	return global
}

// TestRepoConfigIgnoresWiderScopes is the R2 trap, and the reason RepoConfig
// exists next to GetConfig: `git config --get` answers with the EFFECTIVE
// value across system/global/local, so a machine-global core.hooksPath reads
// as something this repository declared. A caller that must decide whether a
// wiring is this project's own gets "yes" from GetConfig for a global
// `.formwork/hooks` in a repository that never mentioned it.
//
// The GetConfig assertion is not decoration: it proves the global fixture is
// LIVE. Drop it and a fixture git never read would make the RepoConfig
// assertion pass for the wrong reason, which is the shape of a test that
// certifies nothing.
func TestRepoConfigIgnoresWiderScopes(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "[core]\n\thooksPath = .husky\n")

	if got, err := vcs.GetConfig(repo, "core.hooksPath"); err != nil || got != ".husky" {
		t.Fatalf("fixture not live: GetConfig = %q, %v; want %q with the global config in effect", got, err, ".husky")
	}
	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("RepoConfig: %v", err)
	}
	if set {
		t.Fatalf("RepoConfig = %q, set=true; a value only the GLOBAL config declares must not read as this repo's", val)
	}
}

// TestRepoConfigUnsetIsNotAnError and TestRepoConfigBrokenGitIsAnError are one
// decision in two directions: unset is a normal answer, a git that could not
// answer is not. Collapsing them either way is a fail-open — "this repository
// declared nothing" is a positive finding a caller acts on, and a broken git
// must not be able to produce it.
func TestRepoConfigUnsetIsNotAnError(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "")

	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("an unset key is a normal answer, not a fault: %v", err)
	}
	if set || val != "" {
		t.Fatalf("RepoConfig = %q, set=%v; want unset", val, set)
	}
}

func TestRepoConfigBrokenGitIsAnError(t *testing.T) {
	isolateWiderScopes(t, "")

	t.Run("unreadable config", func(t *testing.T) {
		repo := initRepo(t)
		// Measured on git 2.50.1: an unparseable config file is `fatal: bad
		// config line N`, exit 128 — distinct from the exit 1 an unset key
		// gets.
		f, err := os.OpenFile(filepath.Join(repo, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("[core\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if val, set, err := vcs.RepoConfig(repo, "core.hooksPath"); err == nil {
			t.Fatalf("RepoConfig = %q, set=%v, err=nil; a config git cannot parse must not read as unset", val, set)
		}
	})

	t.Run("not a repository", func(t *testing.T) {
		if val, set, err := vcs.RepoConfig(t.TempDir(), "core.hooksPath"); err == nil {
			t.Fatalf("RepoConfig = %q, set=%v, err=nil; outside a repository there is no local scope to read", val, set)
		}
	})
}

// TestRepoConfigEmptyValueIsSetNotUnset is why the answer is three-valued.
// Measured on git 2.50.1, `hooksPath =` in the config file is exit 0 with
// empty output — a declared value. A (string, error) signature has to spend
// the empty string on "unset", so this repository's own declaration would
// arrive indistinguishable from silence.
func TestRepoConfigEmptyValueIsSetNotUnset(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "")
	run(t, repo, "config", "--local", "core.hooksPath", "")

	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("RepoConfig: %v", err)
	}
	if !set {
		t.Fatal("an empty value is a declaration, not an unset key")
	}
	if val != "" {
		t.Fatalf("RepoConfig = %q, want the empty value", val)
	}
}

func TestRepoConfigReadsLocalValue(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "[core]\n\thooksPath = .husky\n")
	run(t, repo, "config", "--local", "core.hooksPath", ".formwork/hooks")

	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil || !set {
		t.Fatalf("RepoConfig = %q, set=%v, err=%v; want the repo's own value", val, set, err)
	}
	if val != ".formwork/hooks" {
		t.Fatalf("RepoConfig = %q, want %q", val, ".formwork/hooks")
	}
}

// TestRepoConfigReadsWorktreeScope covers the half of "this repository's own
// config" that is not .git/config. With extensions.worktreeConfig enabled the
// value lives in .git/config.worktree, `--local --get` reports it UNSET, and
// git nevertheless runs hooks from it — asserted here against `rev-parse
// --git-path hooks` so the test states git's behaviour rather than this
// author's belief about it.
func TestRepoConfigReadsWorktreeScope(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "")
	// Spelled "yes", not "true": git's boolean parser accepts both, and a gate
	// comparing the raw string against "true" would read this repository as
	// having the extension off and never look at worktree scope (#90's lesson,
	// pinned by TestGetConfigBoolNormalizesSpellings for the effective-scope
	// reader).
	run(t, repo, "config", "--local", "extensions.worktreeConfig", "yes")
	run(t, repo, "config", "--local", "core.hooksPath", ".local-hooks")
	run(t, repo, "config", "--worktree", "core.hooksPath", ".worktree-hooks")

	out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != ".worktree-hooks" {
		t.Fatalf("git runs hooks from %q; fixture does not exercise worktree scope", got)
	}
	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil || !set {
		t.Fatalf("RepoConfig = %q, set=%v, err=%v; want the worktree-scope value", val, set, err)
	}
	if val != ".worktree-hooks" {
		t.Fatalf("RepoConfig = %q, want %q — worktree scope wins over local, as git resolves it", val, ".worktree-hooks")
	}
}

// TestRepoConfigWithLinkedWorktreeAndNoExtension pins the reason the worktree
// read is gated rather than unconditional. Measured on git 2.50.1: with
// extensions.worktreeConfig absent and more than one working tree registered,
// `git config --worktree --get` is `fatal: --worktree cannot be used with
// multiple working trees`, exit 128. An unconditional second call would turn
// every ordinary repository that has ever run `git worktree add` into one whose
// own config cannot be read at all.
func TestRepoConfigWithLinkedWorktreeAndNoExtension(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "")
	run(t, repo, "config", "--local", "core.hooksPath", ".formwork/hooks")
	run(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	run(t, repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "linked"), "-b", "linked")

	val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("a repository with a linked worktree must still be readable: %v", err)
	}
	if !set || val != ".formwork/hooks" {
		t.Fatalf("RepoConfig = %q, set=%v, want %q", val, set, ".formwork/hooks")
	}
}

// TestSetConfigWritesTheRepositorysOwnConfig pins the explicit --local. It is
// not only a spelling: measured on git 2.50.1, with GIT_CONFIG set in the
// environment a bare `git config <key> <val>` writes to THAT file and exits 0,
// leaving .git/config untouched — install would report a wiring it never made.
// `--local` refuses the ambiguity instead (`error: only one config file at a
// time`, exit 129), which is the loud direction.
func TestSetConfigWritesTheRepositorysOwnConfig(t *testing.T) {
	repo := initRepo(t)
	global := isolateWiderScopes(t, "[core]\n\thooksPath = .husky\n")
	before, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("writes the repo's own config", func(t *testing.T) {
		if err := vcs.SetConfig(repo, "core.hooksPath", ".formwork/hooks"); err != nil {
			t.Fatal(err)
		}
		val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
		if err != nil || !set || val != ".formwork/hooks" {
			t.Fatalf("after SetConfig, RepoConfig = %q, set=%v, err=%v", val, set, err)
		}
	})

	t.Run("refuses an env-redirected config file", func(t *testing.T) {
		redirect := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.WriteFile(redirect, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG", redirect)

		err := vcs.SetConfig(repo, "core.hooksPath", ".redirected")
		body, readErr := os.ReadFile(redirect)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(body) != 0 {
			t.Fatalf("SetConfig wrote to the env-redirected file, not the repository:\n%s", body)
		}
		if err == nil {
			t.Fatal("SetConfig returned nil while writing nothing to the repository; a wiring that did not happen must not read as success")
		}
	})

	// The global config is snapshotted rather than merely "not asserted about":
	// an absence assertion cannot tell a file that was left alone from one that
	// was never looked at. Every write above ran with GIT_CONFIG_GLOBAL pointing
	// at this file.
	after, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("SetConfig changed the GLOBAL config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// RepoConfigWithIncludes is the second half of #173's two-answer test, and each
// case below states BOTH answers: `--local --get` (RepoConfig) unset, and this
// function set. Either alone is unremarkable — the pair is the finding.
//
// Each fixture asserts `rev-parse --git-path hooks` first. An include that
// silently failed to take effect leaves a repository where both answers are
// "unset", which is the correct answer for it, and the test would then certify
// nothing.
func TestRepoConfigWithIncludesSeesWhatRepoConfigCannot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		wire  func(t *testing.T, repo string)
		value string
	}{
		{"an include in .git/config", func(t *testing.T, repo string) {
			write(t, repo, ".git/team.cfg", "[core]\n\thooksPath = team-hooks\n")
			appendTo(t, filepath.Join(repo, ".git", "config"), "[include]\n\tpath = team.cfg\n")
		}, "team-hooks"},
		// The row that decides the scopes. `--local --includes` does NOT see
		// this one — measured on git 2.50.1, it exits 1 while git runs hooks
		// from the included value — so a reader built on the local scope alone
		// reports this repository's own declaration as somebody else's.
		{"an include in .git/config.worktree", func(t *testing.T, repo string) {
			run(t, repo, "config", "--local", "extensions.worktreeConfig", "true")
			write(t, repo, ".git/wt.cfg", "[core]\n\thooksPath = wt-hooks\n")
			appendTo(t, filepath.Join(repo, ".git", "config.worktree"), "[include]\n\tpath = wt.cfg\n")
		}, "wt-hooks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			isolateWiderScopes(t, "")
			tc.wire(t, repo)

			if got := gitHooksPath(t, repo); got != tc.value {
				t.Fatalf("git runs hooks from %q, want %q — the fixture's include is not in effect", got, tc.value)
			}
			val, set, err := vcs.RepoConfig(repo, "core.hooksPath")
			if err != nil {
				t.Fatalf("RepoConfig: %v", err)
			}
			if set {
				t.Fatalf("RepoConfig = %q, set=true; this fixture's whole premise is that the scoped read misses an included declaration", val)
			}
			viaInclude, err := vcs.RepoConfigWithIncludes(repo, "core.hooksPath")
			if err != nil {
				t.Fatalf("RepoConfigWithIncludes: %v", err)
			}
			if !viaInclude {
				t.Fatal("RepoConfigWithIncludes reports nothing while git runs hooks from the included value")
			}
		})
	}
}

// The other direction, twice, because a reader that answers "set" everywhere
// would pass the test above: a repository with no include at all, and one whose
// include declares a different key. The second is the ordinary case — teams
// share identity and aliases through includes — and it is the reason this asks
// about a KEY rather than about the presence of an include.
func TestRepoConfigWithIncludesReportsUnsetWhenNoIncludeDeclaresTheKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire func(t *testing.T, repo string)
	}{
		{"no include at all", func(t *testing.T, repo string) {}},
		{"an include declaring another key", func(t *testing.T, repo string) {
			write(t, repo, ".git/team.cfg", "[teamprobe]\n\tvalue = live\n")
			appendTo(t, filepath.Join(repo, ".git", "config"), "[include]\n\tpath = team.cfg\n")
			out, err := exec.Command("git", "-C", repo, "config", "--get", "teamprobe.value").Output()
			if err != nil || strings.TrimSpace(string(out)) != "live" {
				t.Fatalf("teamprobe.value = %q, %v; the fixture's include is not in effect", out, err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			isolateWiderScopes(t, "[core]\n\thooksPath = .husky\n")
			tc.wire(t, repo)

			viaInclude, err := vcs.RepoConfigWithIncludes(repo, "core.hooksPath")
			if err != nil {
				t.Fatalf("RepoConfigWithIncludes: %v", err)
			}
			if viaInclude {
				// The global fixture is live and declares core.hooksPath, so a
				// reader that reached wider scopes reports "set" here.
				t.Fatal("RepoConfigWithIncludes reports a declaration this repository's config does not make")
			}
		})
	}
}

// The gate on the worktree scope matches git's own behaviour rather than merely
// reusing RepoConfig's helper: measured on git 2.50.1, an extensions.worktreeConfig
// arriving THROUGH an include is not honoured — .git/config.worktree is not read
// at all. If a future git starts honouring it, this test fails, which is the
// direction that matters: the declaration would then govern git's hooks while
// both readers here report unset.
func TestWorktreeScopeIsNotOpenedByAnIncludedExtension(t *testing.T) {
	repo := initRepo(t)
	isolateWiderScopes(t, "")
	write(t, repo, ".git/ext.cfg", "[extensions]\n\tworktreeConfig = true\n")
	appendTo(t, filepath.Join(repo, ".git", "config"), "[include]\n\tpath = ext.cfg\n")
	write(t, repo, ".git/config.worktree", "[core]\n\thooksPath = wt-hooks\n")

	if got := gitHooksPath(t, repo); got != ".git/hooks" {
		t.Fatalf("git runs hooks from %q; it now honours an included extensions.worktreeConfig, so RepoConfigWithIncludes' gate reads a scope git opens and it does not", got)
	}
	viaInclude, err := vcs.RepoConfigWithIncludes(repo, "core.hooksPath")
	if err != nil {
		t.Fatalf("RepoConfigWithIncludes: %v", err)
	}
	if viaInclude {
		t.Fatal("RepoConfigWithIncludes read .git/config.worktree, which git itself did not")
	}
}

// gitHooksPath is what git says it will do, as git spells it — the answer these
// fixtures are built against.
func gitHooksPath(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-path hooks: %v", err)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// appendTo adds config lines to a file, creating it if git has not. WHERE an
// include sits in a config file matters (last-one-wins), and appending is how
// these fixtures put one after the section git init wrote.
func appendTo(t *testing.T, path, body string) {
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
