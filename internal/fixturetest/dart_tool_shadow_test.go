// dart_tool_shadow_test.go — a tool's ignored output cannot shadow a command
// fixture's next run (formwork #44).
//
// Dart's implicit `pub get` inside a fixture writes an ignored
// `.dart_tool/package_config.json` there. That config does not declare the
// workspace, so once it exists it shadows the workspace's resolver on every
// later run, and the fixture fails for a reason nobody committed.
package fixturetest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

const shadowConfig = `{"configVersion":2,"packages":[]}` + "\n"

func runFixtures(t *testing.T, root string) (int, string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	return failed, sb.String()
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Env = gitCleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// An isolated arm is copied without the files git ignores, so the planted
// config never reaches the detector, and the committed tree is left as it was.
func TestIsolatedFixtureLeavesAnIgnoredDartToolBehind(t *testing.T) {
	for name, tc := range map[string]struct {
		gitignore  string
		wantFailed int
	}{
		"ignored by the repository": {gitignore: ".dart_tool/\n", wantFailed: 0},
		// The control: the same file, not ignored, is the author's and is copied.
		"not ignored": {gitignore: "", wantFailed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{
				".formwork/formwork.yaml": "version: 1\n",
				".formwork/rules/r.yaml": `rules:
  - id: no-shadow-config
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'test -e {{root}}/a.txt && test ! -e {{root}}/.dart_tool/package_config.json']
    cure: a fixture-local package config must not reach the detector.
`,
				".formwork/fixtures/no-shadow-config/pass-1/a.txt":                          "a\n",
				".formwork/fixtures/no-shadow-config/pass-1/.dart_tool/package_config.json": shadowConfig,
			}
			if tc.gitignore != "" {
				files[".gitignore"] = tc.gitignore
			}
			root := writeRepo(t, files)
			gitInitCommitted(t, root)

			failed, out := runFixtures(t, root)
			if failed != tc.wantFailed {
				t.Fatalf("failed = %d, want %d:\n%s", failed, tc.wantFailed, out)
			}
			planted := filepath.Join(root, ".formwork/fixtures/no-shadow-config/pass-1/.dart_tool/package_config.json")
			if _, err := os.Stat(planted); err != nil {
				t.Fatalf("isolation must not write the committed tree, but the planted config is gone: %v", err)
			}
		})
	}
}

// An arm judged in place loses the ignored files under `.dart_tool` before its
// command runs. A tracked file there is the author's and stays, and so does a
// file the author has written but not yet committed.
func TestInPlaceCommandFixtureDropsAnIgnoredDartTool(t *testing.T) {
	arm := ".formwork/fixtures/in-place-no-shadow/pass-1"
	root := writeRepo(t, map[string]string{
		".gitignore":              ".dart_tool/\n",
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: in-place-no-shadow
    type: command
    scope:
      include: ["**/*.txt"]
    params:
      cmd: [sh, -c, 'test ! -e ` + arm + `/.dart_tool/package_config.json']
    cure: a fixture-local package config must not survive into the run.
`,
		arm + "/a.txt":                          "a\n",
		arm + "/new.txt":                        "not committed yet\n",
		arm + "/.dart_tool/package_config.json": shadowConfig,
		arm + "/.dart_tool/kept.txt":            "tracked on purpose\n",
	})
	gitInit(t, root)
	gitRun(t, root, "add", ".gitignore", ".formwork/formwork.yaml", ".formwork/rules/r.yaml", arm+"/a.txt")
	gitRun(t, root, "add", "-f", arm+"/.dart_tool/kept.txt")
	gitRun(t, root, "commit", "-q", "-m", "seed")

	failed, out := runFixtures(t, root)
	if failed != 0 {
		t.Fatalf("the planted config reached the run:\n%s", out)
	}
	for rel, want := range map[string]bool{
		".dart_tool/package_config.json": false,
		".dart_tool/kept.txt":            true,
		"new.txt":                        true,
		"a.txt":                          true,
	} {
		_, err := os.Stat(filepath.Join(root, arm, rel))
		if got := err == nil; got != want {
			t.Errorf("%s exists = %v, want %v", rel, got, want)
		}
	}
}
