package command_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// initRepo makes a repository at a fresh directory under t.TempDir() and
// returns its work tree. No commit is made: the question this package asks
// ("which repository does git resolve here") is answered by `rev-parse` on an
// empty repository, so a commit would only add an author identity to configure.
func initRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", dir)
	// The repository is built with the pointer family removed from the ambient
	// environment: a test that has already exported GIT_DIR must not have its
	// fixture repositories created inside whatever that names. Removed rather
	// than emptied — git reads an empty GIT_DIR as a path and refuses it.
	cmd.Env = withoutPointers(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, out)
	}
	return dir
}

// withoutPointers drops the repository-pointer variables from an environment.
// It is test scaffolding for building fixture repositories, NOT a second copy
// of the engine's policy: nothing under test consults it, and the production
// path deliberately removes nothing at all.
func withoutPointers(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func finalizeAt(t *testing.T, c rules.Checker, root string) ([]rules.Match, error) {
	t.Helper()
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("command should implement ErrFinalizer")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: root})
}

// TestCommandRefusesWhenAmbientGitDirNamesAnotherRepository is #177's
// acceptance criterion: a `command:` rule shelling out to git under an ambient
// GIT_DIR must not answer for a repository the engine was never asked about.
// The tool prints the git directory it resolved; before the fix the rule ran,
// reported repository B, and the run exited 0 or 1 on B's contents while every
// declarative rule in the same run judged A.
func TestCommandRefusesWhenAmbientGitDirNamesAnotherRepository(t *testing.T) {
	engineRoot := initRepo(t, "engine")
	other := initRepo(t, "other")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))

	c := build(t, "cmd: [git, rev-parse, --absolute-git-dir]")
	m, err := finalizeAt(t, c, engineRoot)
	if err == nil {
		t.Fatalf("expected an engine error, got matches=%v and no error — the rule ran against %s", m, other)
	}
	if !errors.Is(err, vcs.ErrGitEnv) {
		t.Fatalf("refusal should carry vcs.ErrGitEnv so a caller can tell policy from a git failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("refusal should name the disagreement it refused on, got %v", err)
	}
}

// TestCommandRefusesWhenAmbientGitWorkTreeNamesAnotherTree is the second
// spelling. GIT_WORK_TREE moves only the top level, which is enough to change
// every answer a tool reads from the working tree, and a guard written against
// GIT_DIR alone would pass here.
func TestCommandRefusesWhenAmbientGitWorkTreeNamesAnotherTree(t *testing.T) {
	engineRoot := initRepo(t, "engine")
	other := initRepo(t, "other")
	t.Setenv("GIT_WORK_TREE", other)

	c := build(t, "cmd: [git, rev-parse, --show-toplevel]")
	if _, err := finalizeAt(t, c, engineRoot); !errors.Is(err, vcs.ErrGitEnv) {
		t.Fatalf("expected a policy refusal for an ambient GIT_WORK_TREE, got %v", err)
	}
}

// TestCommandRunsWhenAmbientGitDirNamesTheSameRepositoryAndStillSeesIt pins
// both halves of the chosen answer at once.
//
// The refusal is on EFFECT, not on presence: `git submodule foreach` exports
// GIT_DIR and runs formwork in the submodule's work tree, which names the
// repository root already names, so nothing is in disagreement and the tool
// must still run.
//
// And the fix is a refusal, not a scrub: the tool exits non-zero unless GIT_DIR
// is still in its environment, so a version of #177 that answered by removing
// the pointer family from cmd.Env fails here — which is the behaviour change
// this rule type, being the disclosed escape hatch, must not make silently.
func TestCommandRunsWhenAmbientGitDirNamesTheSameRepositoryAndStillSeesIt(t *testing.T) {
	engineRoot := initRepo(t, "engine")
	t.Setenv("GIT_DIR", filepath.Join(engineRoot, ".git"))

	c := build(t, `cmd: [sh, -c, 'test -n "$GIT_DIR" || exit 7']`)
	m, err := finalizeAt(t, c, engineRoot)
	if err != nil {
		t.Fatalf("an ambient GIT_DIR naming root's own repository is not a disagreement: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("the tool did not see the inherited GIT_DIR: %v", m)
	}
}

// TestCommandSkippedRuleIsNotRefused pins the guard's altitude: it sits after
// the when: early return, so a rule whose tool never runs is not failed by an
// environment it was never going to consult. Moving the guard to the top of
// FinalizeErr turns every untriggered gate in such a run into an exit 2.
func TestCommandSkippedRuleIsNotRefused(t *testing.T) {
	engineRoot := initRepo(t, "engine")
	other := initRepo(t, "other")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))

	c := build(t, "cmd: [git, rev-parse, --absolute-git-dir]\nwhen: {paths_changed: ['**/*.sql']}")
	m, err := finalizeAt(t, c, engineRoot)
	if err != nil {
		t.Fatalf("an untriggered rule has no tool to refuse: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected no findings, got %v", m)
	}
	sr, ok := c.(rules.SkipReporter)
	if !ok {
		t.Fatal("command should implement SkipReporter")
	}
	if _, skipped := sr.SkipReason(); !skipped {
		t.Fatal("the rule should still report the when: skip")
	}
}

// TestCommandRunsWithNoRepositoryPointerSet is the ordinary run: nothing is
// set, so nothing is asked and nothing is refused. It also pins that the guard
// does not require root to BE a repository — a command rule over a corpus that
// was never initialised must keep running, which is every `formwork test`
// fixture tree.
func TestCommandRunsWithNoRepositoryPointerSet(t *testing.T) {
	t.Setenv("GIT_DIR", "")
	os.Unsetenv("GIT_DIR")

	c := build(t, "cmd: [sh, -c, 'exit 0']")
	if m, err := finalizeAt(t, c, t.TempDir()); err != nil || len(m) != 0 {
		t.Fatalf("a plain command rule over a non-repository must pass, got matches=%v err=%v", m, err)
	}
}

// TestCommandStillInheritsTheEnvironment guards the escape hatch's own
// contract from this fix. `command` rules exist to run the operator's tool,
// which needs PATH, HOME and whatever else it was configured with — so the
// answer to #177 is a refusal, not a scrub, and nothing is removed from what
// the tool sees. A future "fix" that scrubs the environment instead fails here.
func TestCommandStillInheritsTheEnvironment(t *testing.T) {
	t.Setenv("FORMWORK_TEST_TOOL_VAR", "visible")
	// A git variable OUTSIDE the pointer family: git exports GIT_AUTHOR_NAME
	// into pre-commit, and a tool the operator wrote may read it.
	t.Setenv("GIT_AUTHOR_NAME", "Ada")

	c := build(t, `cmd: [sh, -c, 'echo "$FORMWORK_TEST_TOOL_VAR/$GIT_AUTHOR_NAME"']`+"\nexpect: {output_forbid: 'visible/Ada'}")
	m, err := finalizeAt(t, c, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("the tool did not see the ambient environment: %v", m)
	}
}

// TestCommandRefusesAMistypedHatch: FORMWORK_GIT_ENV is the one accepted way to
// turn the repository-pointer policy off, and a value formwork does not
// understand is refused rather than treated as "off" — otherwise an operator
// who mistyped it believes the hatch is on while the run is being refused (or,
// before this change, while the command rule silently answered for another
// repository).
func TestCommandRefusesAMistypedHatch(t *testing.T) {
	t.Setenv(vcs.GitEnvVar, "inhert")

	c := build(t, "cmd: [sh, -c, 'exit 0']")
	if _, err := finalizeAt(t, c, t.TempDir()); !errors.Is(err, vcs.ErrGitEnv) {
		t.Fatalf("expected a refusal for a value %s does not accept, got %v", vcs.GitEnvVar, err)
	}
}

// TestCommandHonoursTheHatchForADetachedWorkTree is the layout FORMWORK_GIT_ENV
// exists for: a bare repository plus a work tree named by the environment. The
// engine honours it, so a command rule must too — refusing here would leave
// that layout with no working spelling at all.
func TestCommandHonoursTheHatchForADetachedWorkTree(t *testing.T) {
	base := t.TempDir()
	gitDir := filepath.Join(base, "bare.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", gitDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", gitDir)
	t.Setenv("GIT_WORK_TREE", work)
	t.Setenv(vcs.GitEnvVar, "inherit")

	c := build(t, "cmd: [sh, -c, 'exit 0']")
	if m, err := finalizeAt(t, c, work); err != nil || len(m) != 0 {
		t.Fatalf("the hatch's own layout must still run: matches=%v err=%v", m, err)
	}
}
