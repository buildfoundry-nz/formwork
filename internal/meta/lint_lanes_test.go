// lint_test_lanes.go — the lane-reachability, enumeration and suppression
// half of the meta.Lint suite. Split from lint_test.go, which had grown past
// the 750-line ceiling its consumers enforce on vendored source.

package meta_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
)

func TestLintFlagsDeadLane(t *testing.T) {
	// ci/all covers every rule (reachability passes), but `dead` selects no
	// rule → lane-nonempty fails.
	failed, out := lint(t, withRoot(lanedFixtures(),
		"version: 1\nlanes:\n  ci:\n    all: true\n    ci: true\n  dead:\n    tags: [nonexistent]\n"))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{"[lane-nonempty] FAIL", "dead: selects no rules"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintSkipsLaneReachabilityWhenNoLanes(t *testing.T) {
	// No lanes configured (the self-hosted formwork repo) → the check is
	// skipped entirely: no [lane-reachability] line, denominator stays 3.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "lane-reachability") {
		t.Fatalf("lane-reachability must be skipped when no lanes are configured:\n%s", out)
	}
	if !strings.Contains(out, "formwork lint: 5/5 checks passed") {
		t.Fatalf("denominator should stay 4 with no lanes (lane-reachability is the conditional one):\n%s", out)
	}
}

// skipUnlessChmodEnforced skips permission-based tests where chmod(0o000)
// doesn't actually restrict reads: Windows doesn't honor POSIX permission
// bits, and root bypasses them entirely (same pattern as
// internal/fixturetest/run_error_test.go).
func skipUnlessChmodEnforced(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission test not applicable on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits don't restrict root")
	}
}

func TestLintEnumeratesSuppressedFindings(t *testing.T) {
	// G1a: a suppressed finding (whether marker- or allowlist-exempted) is
	// listed in the escape-hatch enumeration after that rule's other lines
	// — an exemption quietly doing its job must be as visible as one that
	// is merely configured.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt":    "banana\n",
		"marker.txt": "banana // formwork:allow no-banana known issue\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"  no-banana: suppressed hit.txt:1 (allowlist:allowlists/legacy.txt:1)",
		"  no-banana: suppressed marker.txt:1 (marker)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintEnumerationUnchangedOnCleanRepo(t *testing.T) {
	// G1a: a repo with no in-force suppressions must render exactly as
	// before — no stray "suppressed" lines.
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if strings.Contains(out, "suppressed") {
		t.Fatalf("clean repo should have no suppressed-finding lines:\n%s", out)
	}
	if !strings.Contains(out, "escape hatches: none") {
		t.Fatalf("missing unchanged escape-hatches-none line:\n%s", out)
	}
}

func TestLintSurvivesEngineErrorAndStillPrintsEscapeHatches(t *testing.T) {
	// D1: an engine error (e.g. an unreadable in-scope file) must not
	// swallow the escape-hatch enumeration — it's purely static and
	// "nothing is silently excluded" must hold even when the repo is
	// degraded. Lint still returns a non-nil error (exit 2 preserved).
	skipUnlessChmodEnforced(t)

	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt": "banana\n",
	})
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("banana\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(secret, 0o644); err != nil {
			t.Fatal(err)
		}
	})

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := meta.Lint(cfg, root, &sb, false, false); err == nil {
		t.Fatal("expected an error from the engine evaluation, got nil")
	}
	out := sb.String()
	for _, want := range []string{
		"escape hatches:",
		"  no-banana: marker enabled",
		"  no-banana: allowlist allowlists/legacy.txt (1 entries)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

const prefilterRule = "rules:\n" +
	"  - id: no-cat-default\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {pattern: 'Category = \"[a-z]+\"', prefilter: material_cat, multiline: true}\n"

// Fixtures so fixture-coverage stays clean. Their CONTENT is load-bearing since
// #133: the scan still skips .formwork/ for the real-tree differential, but the
// fixture differential walks these trees directly, so fire-1 containing the
// prefilter literal is what keeps these rules' verdicts down to the single
// real-tree finding each test asserts.
var prefilterFixtures = map[string]string{
	".formwork/fixtures/no-cat-default/fire-1/f.go": "material_cat\nvar Category = \"x\"\n",
	".formwork/fixtures/no-cat-default/pass-1/f.go": "package p\n",
}

func withFixtures(files map[string]string) map[string]string {
	for k, v := range prefilterFixtures {
		files[k] = v
	}
	return files
}

func TestLintFlagsLoadBearingPrefilter(t *testing.T) {
	failed, out := lint(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  prefilterRule,
		// in-scope, matches the pattern, but LACKS the prefilter literal
		"consts.go": "package p\nvar Category = \"floor\"\n",
	}))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[prefilter-load-bearing] FAIL — 1 problem(s)",
		`no-cat-default: prefilter "material_cat" is load-bearing — removing it makes the rule match consts.go; move the scope to require_present: if intended.`,
		"formwork lint: 5/6 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintPassesRedundantPrefilter(t *testing.T) {
	failed, out := lint(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  prefilterRule,
		// matches the pattern AND contains the prefilter literal → no difference
		"consts.go": "package p\n// material_cat\nvar Category = \"floor\"\n",
	}))
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[prefilter-load-bearing] OK",
		"formwork lint: 6/6 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintPassesRedundantPrefilterWithMainEngineRun(t *testing.T) {
	// The rule opts into markers, so it is in rulesFeedingLint and the main engine
	// run happens; the differential reuses those findings for its base pass.
	// A redundant prefilter (literal present in the matching file) must still
	// report OK — guards the reuse path against a false positive.
	rule := "rules:\n" +
		"  - id: cat-ok\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n" +
		"    except: {marker: true}\n" +
		"    params: {pattern: 'Category = \"[a-z]+\"', prefilter: material_cat, multiline: true}\n"
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":               "version: 1\n",
		".formwork/rules/r.yaml":                rule,
		".formwork/fixtures/cat-ok/fire-1/f.go": "material_cat\nvar Category = \"x\"\n",
		".formwork/fixtures/cat-ok/pass-1/f.go": "package p\n",
		// matches the pattern AND contains the prefilter literal → no difference
		"consts.go": "package p\n// material_cat\nvar Category = \"floor\"\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[prefilter-load-bearing] OK") {
		t.Fatalf("redundant prefilter (main-run reuse path) must pass:\n%s", out)
	}
}

func TestLintPrefilterUsesPreprocessedView(t *testing.T) {
	// The prefilter literal lives only in a comment that decomment-go strips, so
	// on the preprocessed view the file no longer contains it and the rule
	// becomes load-bearing. Proves the differential evaluates the preprocessed
	// variant, not raw bytes.
	rule := "rules:\n" +
		"  - id: no-cat-default\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    preprocess: decomment-go\n" +
		"    params: {pattern: 'Category = \"[a-z]+\"', prefilter: material_cat, multiline: true}\n"
	failed, out := lint(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  rule,
		"consts.go":               "package p\n// material_cat\nvar Category = \"floor\"\n",
	}))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[prefilter-load-bearing] FAIL — 1 problem(s)") ||
		!strings.Contains(out, "removing it makes the rule match consts.go") {
		t.Fatalf("expected preprocessed-view FAIL naming consts.go:\n%s", out)
	}
}

func TestLintSkipsPrefilterCheckWhenNoPrefilterRules(t *testing.T) {
	// lintRule (no prefilter) → the check must not run or count.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "prefilter-load-bearing") {
		t.Fatalf("check must be silent with no prefilter rules:\n%s", out)
	}
	if !strings.Contains(out, "formwork lint: 5/5 checks passed") {
		t.Fatalf("denominator must exclude the skipped check:\n%s", out)
	}
}

func TestLintFlagsLoadBearingPrefilterEvenWhenSuppressed(t *testing.T) {
	// A load-bearing prefilter whose exposed match is marker-suppressed still
	// fails the check (pre-suppression comparison). The marker carries a reason,
	// so exemption-hygiene stays clean and only prefilter-load-bearing fails.
	rule := "rules:\n" +
		"  - id: no-cat-default\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    except: {marker: true}\n" +
		"    params: {pattern: 'Category = \"[a-z]+\"', prefilter: material_cat, multiline: true}\n"
	failed, out := lint(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  rule,
		"bad.go":                  "package p\nvar Category = \"floor\" // formwork:allow no-cat-default legit reason\n",
	}))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[prefilter-load-bearing] FAIL — 1 problem(s)",
		"removing it makes the rule match bad.go",
		"[exemption-hygiene] OK",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintPrefilterCheckFailsClosedOnEngineError(t *testing.T) {
	// An unreadable in-scope file makes the differential's engine run error;
	// Lint must return non-nil (exit 2) and still print the escape-hatch
	// enumeration — never a false "prefilter-load-bearing OK".
	skipUnlessChmodEnforced(t)

	root := writeRepo(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  prefilterRule,
	}))
	secret := filepath.Join(root, "secret.go")
	if err := os.WriteFile(secret, []byte("package p\nvar Category = \"floor\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := meta.Lint(cfg, root, &sb, false, false); err == nil {
		t.Fatalf("expected an engine error (exit 2), got nil\n%s", sb.String())
	}
	out := sb.String()
	if !strings.Contains(out, "escape hatches") {
		t.Fatalf("escape-hatch enumeration must still print on error:\n%s", out)
	}
	if strings.Contains(out, "[prefilter-load-bearing] OK") {
		t.Fatalf("an engine error must never read as a passing check:\n%s", out)
	}
}

func TestLintEmitsPrefilterVerdictBeforeMainEngineError(t *testing.T) {
	// A load-bearing prefilter rule (*.go, readable) plus a marker rule (*.txt)
	// whose in-scope file is unreadable: the main engine run errors, but the
	// prefilter-load-bearing FAIL must still be emitted before lint surfaces the
	// exit-2 error — a load-bearing-prefilter diagnostic must not be hidden
	// behind an unrelated engine error (visibility must not regress on a
	// degraded repo).
	skipUnlessChmodEnforced(t)

	root := writeRepo(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": prefilterRule +
			"  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n" +
			"    except: {marker: true}\n    params: {pattern: banana}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		// in-scope for the prefilter rule (*.go), readable, load-bearing
		"consts.go": "package p\nvar Category = \"floor\"\n",
	}))
	// The marker rule (*.txt) forces the main engine run; this unreadable .txt
	// makes that run error — but it is outside the prefilter rule's *.go scope,
	// so the prefilter check's own base pass succeeds and can still emit.
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("banana\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := meta.Lint(cfg, root, &sb, false, false); err == nil {
		t.Fatalf("expected exit 2 from the unreadable file, got nil:\n%s", sb.String())
	}
	out := sb.String()
	if !strings.Contains(out, "[prefilter-load-bearing] FAIL") ||
		!strings.Contains(out, "removing it makes the rule match consts.go") {
		t.Fatalf("prefilter FAIL must be emitted before the main-run error:\n%s", out)
	}
}

// TestLintSkipsEngineRunWhenNoAllowlistOrMarker used to live here. It proved D2
// — that lint does not run the engine when nothing consumes its findings — by
// showing that an unreadable in-scope file did NOT stop the run, and asserted
// "5/5 checks passed" over a repo lint could not read.
//
// #30 is that assertion read as a bug report: the same repo with a `prefilter:`
// added, which the spec calls a pure optimization that must never change a
// verdict, exited 2 on the same file. lint now refuses either way
// (unreadable_test.go), so this witness is gone — a test cannot both demand the
// pass and demand the refusal.
//
// D2 keeps the witness that had the cost attached:
// TestLintDoesNotExecuteCommandRules in lint_engine_scope_test.go, which catches
// a narrowing regression by the sentinel a heavy rule would leave behind. What
// is no longer pinned is the weaker half — that a run with no allowlist, marker
// or prefilter dispatches no engine at all — because after #30 lint reads every
// governed file regardless, so nothing observable is left to distinguish the two.
// A future change that quietly restores the engine run for such a repo would
// cost matching time and would go unnoticed here.

func TestLintFlagsLoadBearingPrefilterAllOfMode(t *testing.T) {
	// all_of gate path: both patterns co-occur but the prefilter literal is
	// absent → gated in base, fires in stripped. Covers a non-multiline mode.
	rule := "rules:\n" +
		"  - id: co-occur\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    params: {all_of: ['alpha', 'beta'], prefilter: material_cat}\n"
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                 "version: 1\n",
		".formwork/rules/r.yaml":                  rule,
		".formwork/fixtures/co-occur/fire-1/f.go": "material_cat alpha beta\n",
		".formwork/fixtures/co-occur/pass-1/f.go": "package p\n",
		"both.go": "package p\n// alpha\n// beta\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[prefilter-load-bearing] FAIL — 1 problem(s)") ||
		!strings.Contains(out, `co-occur: prefilter "material_cat" is load-bearing — removing it makes the rule match both.go`) {
		t.Fatalf("expected all_of-mode FAIL naming both.go:\n%s", out)
	}
}

func TestLintReportsEachLoadBearingPathAndRule(t *testing.T) {
	// One rule load-bearing on two files + a second rule whose prefilter is
	// redundant → two problem lines for the first rule, none for the second.
	rules := "rules:\n" +
		"  - id: cat-a\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n" +
		"    params: {pattern: 'Category = \"[a-z]+\"', prefilter: material_cat, multiline: true}\n" +
		"  - id: cat-b\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n" +
		"    params: {pattern: 'Widget = \"[a-z]+\"', prefilter: widget_tok, multiline: true}\n"
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":              "version: 1\n",
		".formwork/rules/r.yaml":               rules,
		".formwork/fixtures/cat-a/fire-1/f.go": "material_cat\nvar Category = \"x\"\n",
		".formwork/fixtures/cat-a/pass-1/f.go": "package p\n",
		".formwork/fixtures/cat-b/fire-1/f.go": "widget_tok\nvar Widget = \"x\"\n",
		".formwork/fixtures/cat-b/pass-1/f.go": "package p\n",
		"one.go":                               "package p\nvar Category = \"floor\"\n",
		"two.go":                               "package p\nvar Category = \"roof\"\n",
		"w.go":                                 "package p\n// widget_tok\nvar Widget = \"blue\"\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[prefilter-load-bearing] FAIL — 2 problem(s)",
		`cat-a: prefilter "material_cat" is load-bearing — removing it makes the rule match one.go`,
		`cat-a: prefilter "material_cat" is load-bearing — removing it makes the rule match two.go`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cat-b:") {
		t.Fatalf("redundant prefilter cat-b must not be flagged:\n%s", out)
	}
}
