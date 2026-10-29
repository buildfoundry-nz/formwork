package meta_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
)

// lintRoot is lint() against an already-written repo, so a test can inspect the
// tree afterwards for side effects a rule left behind.
func lintRoot(t *testing.T, root string) (int, string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	failed, err := meta.Lint(cfg, root, &sb, devOptOutActive, false)
	if err != nil {
		t.Fatal(err)
	}
	return failed, sb.String()
}

// lint runs the engine only to answer its OWN question — the prefilter rules
// whose findings prefilterLoadBearing reuses as its base pass.
//
// It must therefore NOT dispatch the heavy `command`/`git-diff` escapes. Those
// belong to `formwork check`, which is the enforcement run. Executing them here
// is pure waste: nothing lint reports reads their findings.
//
// The regression this pins is not hypothetical. The predicate was a whole-config
// ANY over the opted-in rules (then marker and allowlist rules), but the run it
// gated was
// `engine.Run(cfg.Rules, ...)` — every rule. So ONE opted-in rule anywhere made
// lint execute EVERY command rule in the config. In the validating port that
// meant two whole-tree resolved-Dart-AST scans (~1,570 files, a Flutter
// toolchain and a workspace `pub get`) running a second time on every PR,
// docs-only included, for findings nothing read (buildfoundry-nz/formwork#64).
//
// Observability: the command writes a sentinel into the repo root, which is
// `cmd.Dir` for a command rule (internal/rules/command: cmd.Dir = ctx.Root).
// Asserting on lint's OUTPUT could not catch this — the enumeration lists
// command rules as escape hatches whether or not they were executed, which is
// exactly why the waste went unnoticed. The side effect is the only witness.
func TestLintDoesNotExecuteCommandRules(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		// The prefiltered rule: its presence alone puts a rule in the engine subset.
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana, prefilter: banana}\n" +
			// The heavy escape that must stay unexecuted.
			"  - id: heavy-escape\n    type: command\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [sh, -c, 'printf x > lint-executed.sentinel']}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt": "banana\n",
	})

	lintRoot(t, root)

	if _, err := os.Stat(filepath.Join(root, "lint-executed.sentinel")); err == nil {
		t.Fatalf("formwork lint executed a command rule: the heavy escapes belong to `check`, not `lint` — nothing lint reports reads their findings")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sentinel: %v", err)
	}
}
