package fixturetest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
)

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const testConfig = `rules:
  - id: fruit-free
    type: forbidden-pattern
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'banana'
  - id: has-anchor
    type: required-pattern
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'anchor'
      mode: exists
`

func run(t *testing.T, files map[string]string) (int, string) {
	t.Helper()
	base := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  testConfig,
	}
	for k, v := range files {
		base[k] = v
	}
	root := writeRepo(t, base)
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

func TestRunPassesMatchingFixtures(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "ok\na banana here want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "all clean\n",
		".formwork/fixtures/has-anchor/fire-1/a.md":  "nothing here\n",
		".formwork/fixtures/has-anchor/fire-1.want":  "-\n",
		".formwork/fixtures/has-anchor/pass-1/a.md":  "the anchor\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[has-anchor] OK — 2 fixture(s)",
		"[fruit-free] OK — 2 fixture(s)",
		"formwork test: 2/2 rules passed, 4 fixture(s) run, 0 rule(s) skipped",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunFailsOnMissingAndUnexpectedFindings(t *testing.T) {
	failed, out := run(t, map[string]string{
		// Marker on line 1, but violation is on line 2: one missing + one unexpected.
		".formwork/fixtures/fruit-free/fire-1/f.txt": "clean want: fruit-free\nbanana\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[fruit-free] FAIL — 2 problem(s)",
		"fire-1: missing expected finding f.txt:1",
		"fire-1: unexpected finding f.txt:2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunFailsPassFixtureWithFindingOrMarker(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "a banana snuck in\n",
	})
	if failed != 1 || !strings.Contains(out, "pass-1: unexpected finding f.txt:1") {
		t.Fatalf("failed=%d\n%s", failed, out)
	}

	failed, out = run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean want: fruit-free\n",
	})
	if failed != 1 || !strings.Contains(out, "pass-1: pass fixture declares want expectations") {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
}

func TestRunFailsPassFixtureWithCommentOnlyWantManifest(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
		".formwork/fixtures/fruit-free/pass-1.want":  "# not a real expectation\n\n",
	})
	if failed != 1 || !strings.Contains(out, "pass-1: pass fixture has a .want manifest") {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
}

func TestRunFailsFireFixtureWithNoExpectations(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
	})
	if failed != 1 || !strings.Contains(out, "fire-1: fire fixture declares no expectations") {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
}

func TestRunSkipsRulesWithoutFixtures(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[has-anchor] SKIP — no fixtures (formwork lint reports coverage)",
		"formwork test: 1/1 rules passed, 2 fixture(s) run, 1 rule(s) skipped",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunFireFixtureIgnoresRepoAllowlist(t *testing.T) {
	// The rule's allowlist suppresses sub/f.txt at the repo level. The fixture
	// tree happens to contain a colliding path (fixture-root-relative, not
	// repo-relative) — the allowlist must not bleed into fixture evaluation,
	// or the fixture's declared violation gets silently suppressed.
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":         "version: 1\n",
		".formwork/allowlists/legacy.txt": "sub/f.txt\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n" +
			"    except: {allowlist: allowlists/legacy.txt}\n",
		".formwork/fixtures/no-banana/fire-1/sub/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt":     "clean\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[no-banana] OK — 2 fixture(s)") {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
}

func TestRunPassFixtureAtAllowlistedPathStillFails(t *testing.T) {
	// Symmetric direction: a pass fixture with a violation at a path that
	// happens to collide with a repo allowlist entry must still fail — the
	// allowlist must not suppress it inside the fixture tree.
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":         "version: 1\n",
		".formwork/allowlists/legacy.txt": "sub/f.txt\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n" +
			"    except: {allowlist: allowlists/legacy.txt}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt":     "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/sub/f.txt": "a banana snuck in\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 || !strings.Contains(sb.String(), "pass-1: unexpected finding sub/f.txt:1") {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
}

func TestRunPassFixtureWithMarkerSuppressedViolation(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n" +
			"    except: {marker: true}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "banana // formwork:allow no-banana proves suppression\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[no-banana] OK — 2 fixture(s)") {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
}

func TestFixtureRunsAreScanIgnoreFree(t *testing.T) {
	// scan.ignore globs are repo-root-relative; fixture trees have their own
	// path namespace (same reasoning as the allowlist nil in runFixture). A
	// repo-level ignore must never blind a fixture: this fire fixture keeps
	// its violation under vendor/, which the repo config ignores.
	// Discriminating pin — verified red by temporarily threading the repo
	// globs into runFixture's Walk (see the comment beside the allowlist nil).
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                             "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: vendored source\n",
		".formwork/rules/r.yaml":                              testConfig,
		".formwork/fixtures/fruit-free/fire-1/vendor/bad.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt":          "all clean\n",
		".formwork/fixtures/has-anchor/fire-1/a.md":           "nothing here\n",
		".formwork/fixtures/has-anchor/fire-1.want":           "-\n",
		".formwork/fixtures/has-anchor/pass-1/a.md":           "the anchor\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[fruit-free] OK — 2 fixture(s)") {
		t.Fatalf("fixture under vendor/ was blinded by repo-level scan.ignore: failed=%d\n%s", failed, sb.String())
	}
}

func TestFixtureRunsAreGitignoreFree(t *testing.T) {
	// The scan.gitignore twin of the test above (#100). It matters MORE than
	// the scan.ignore case, because the paths a consumer's .gitignore covers
	// are exactly the ones fixture trees like to use: build/, .dart_tool/,
	// node_modules/. A fire fixture whose violation sits under build/ must
	// still fire — otherwise the rule reports OK and the fixture that was
	// supposed to prove it can fail silently stops proving anything.
	//
	// runFixture calls scan.Walk, which takes no prune set at all, so this
	// holds by construction today. It is pinned because the construction is
	// one word wide: switching that call to WalkWith with the repo's resolved
	// set would blind these trees with nothing else going red.
	//
	// Discriminating pin — verified red by threading a GitIgnored set covering
	// "build" into runFixture's Walk.
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n",
		".formwork/rules/r.yaml":  testConfig,
		".gitignore":              "build/\n",
		".formwork/fixtures/fruit-free/fire-1/build/bad.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt":         "all clean\n",
		".formwork/fixtures/has-anchor/fire-1/a.md":          "nothing here\n",
		".formwork/fixtures/has-anchor/fire-1.want":          "-\n",
		".formwork/fixtures/has-anchor/pass-1/a.md":          "the anchor\n",
	})
	gitInit(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[fruit-free] OK — 2 fixture(s)") {
		t.Fatalf("fixture under build/ was blinded by repo-level scan.gitignore: failed=%d\n%s", failed, sb.String())
	}
}

// gitInit makes root a git repository so the .gitignore above is a real one
// rather than an inert file. Skips when git is unavailable — a skip here is
// honest (the pin genuinely cannot be evaluated), unlike a silent pass.
func gitInit(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}
