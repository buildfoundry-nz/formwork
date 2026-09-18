// isolation_test.go — a fixture is judged in isolation (#28).
//
// The argv tokens stop a rule NAMING a tree outside itself. They do not stop
// git, which discovers a repository by walking UP from the working directory:
// a fixture tree has no .git, so a detector that runs `git rev-parse` inside
// one finds whatever repository encloses the corpus, and a `--default-range`
// arm then judges the real branch's commits. That is the same escape through
// a channel no argv mentions, so it has to be closed where the engine hands
// the tree over: a fixture for a rule that spawns a process is materialised
// as its own repository, and discovery stops there.
package fixturetest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// gitCleanEnv is the ambient environment minus the pointers that would send
// git at a repository other than the one named by -C.
func gitCleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "GIT_DIR="), strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_COMMON_DIR="), strings.HasPrefix(kv, "GIT_INDEX_FILE="):
		default:
			env = append(env, kv)
		}
	}
	return env
}

// gitInitCommitted makes dir a repository with one commit, which is what
// encloses the corpus in the case these tests are about. It is gitInit plus
// the commit, because git discovery finds a repository with no commits just
// as readily and the defect needs a range to read.
func gitInitCommitted(t *testing.T, dir string) {
	t.Helper()
	gitInit(t, dir)
	cmd := exec.Command("git", "-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "seed")
	cmd.Env = gitCleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// The corpus sits inside a repository, and the rule's detector asks git where
// it is. The fixture must answer with itself: with the enclosing repository's
// toplevel it would be judging the real tree, which is the whole defect.
func TestFixtureForAProcessRuleIsItsOwnRepository(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: toplevel-is-the-fixture
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'test "$(git rev-parse --show-toplevel)" = "$(cd {{root}} && pwd -P)"']
    cure: the detector must see the tree under evaluation as its repository.
`,
		".formwork/fixtures/toplevel-is-the-fixture/pass-1/a.txt": "a\n",
	})
	gitInitCommitted(t, root)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("the pass fixture answered with a repository that is not itself — git discovery climbed out of the fixture:\n%s", sb.String())
	}
}

// The isolation must not cost the fixture its own content: what the detector
// reads inside the materialised tree is what the author committed.
func TestIsolatedFixtureKeepsItsFiles(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: fixture-content-survives
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'test "$(cat {{root}}/nested/a.txt)" = "planted"']
    cure: the fixture's own files must reach the detector.
`,
		".formwork/fixtures/fixture-content-survives/pass-1/nested/a.txt": "planted\n",
	})
	gitInitCommitted(t, root)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("the fixture's own file did not reach the detector:\n%s", sb.String())
	}
}

// {{repo}} is how a detector that LIVES in the repository stays reachable
// while judging an isolated fixture — the reason the second token exists.
func TestRepoTokenReachesTheCorpusFromAnIsolatedFixture(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: detector-is-reachable
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'test "$(cat {{repo}}/tools/marker.txt)" = "detector"']
    cure: a detector living in the repository must be reachable from a fixture.
`,
		"tools/marker.txt": "detector\n",
		".formwork/fixtures/detector-is-reachable/pass-1/a.txt": "a\n",
	})
	gitInitCommitted(t, root)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("{{repo}} did not reach the corpus from an isolated fixture:\n%s", sb.String())
	}
}

// A fixture that is materialised must not leave the original tree changed —
// the fixture is the author's committed evidence, not scratch space.
func TestIsolationDoesNotWriteIntoTheFixtureTree(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: leaves-no-trace
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'true']
    cure: unused.
`,
		".formwork/fixtures/leaves-no-trace/pass-1/a.txt": "a\n",
	})
	gitInitCommitted(t, root)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil {
		t.Fatal(err)
	}
	arm := filepath.Join(root, ".formwork", "fixtures", "leaves-no-trace", "pass-1")
	entries, err := os.ReadDir(arm)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "a.txt" {
			t.Fatalf("isolation wrote %q into the committed fixture tree", e.Name())
		}
	}
}
