package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/cli"
)

// TESTS IN THIS PACKAGE MUST NOT CALL t.Parallel().
//
// runCLI invokes cli.Run IN-PROCESS, and most tests here pass a root that only
// resolves from the package directory (`-C testdata/toyrepo`, and similar in
// introspect_test.go and rulesfor_test.go). A few tests — the file-set ones
// that must exercise the DEFAULT `-C .` the way a git hook does — call
// t.Chdir, which moves the working directory for the whole process.
//
// Serial execution is what keeps those compatible, and it is currently
// accidental rather than enforced: nothing in the package calls t.Parallel, so
// nothing collides. Parallelising any test here breaks it in one of two ways,
// neither of which names the cause — a chdir test panics outright ("testing:
// t.Chdir called during parallel test"), and any other test parallelised
// alongside one may resolve `testdata/toyrepo` inside a temporary git repo and
// flake to exit 2. If this package ever needs parallelism, the fix is to make
// the cwd-dependent roots absolute first, not to drop the chdir tests.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCheckToyRepoFindsViolationAndExits1(t *testing.T) {
	code, out, _ := runCLI(t, "check", "-C", filepath.Join("testdata", "toyrepo"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"[no-todo-markers] FAIL — 1 finding(s)",
		"src/notes.txt:2: forbidden pattern matched: TODO",
		"Cure: Resolve the item or move it to the issue tracker.",
		"[readme-mentions-formwork] OK",
		"formwork: 1/2 rules passed, 1 finding(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckCleanRepoExitsZero(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, "a.txt"), "apples only\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "[no-banana] OK") {
		t.Fatalf("missing OK line:\n%s", out)
	}
}

func TestCheckSummaryAppendsSuppressedCount(t *testing.T) {
	// G1b: a suppressed finding never affects the exit code or the per-rule
	// verdict, but the summary line must still show it happened.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n"+
			"    except: {allowlist: allowlists/legacy.txt}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "allowlists", "legacy.txt"), "hit.txt\n")
	mustWrite(t, filepath.Join(root, "hit.txt"), "banana\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"[no-banana] OK",
		"formwork: 1/1 rules passed, 0 finding(s), 1 suppressed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckMissingConfigExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "check", "-C", t.TempDir())
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "formwork.yaml") {
		t.Fatalf("stderr should name the missing config: %s", errOut)
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "frobnicate")
	if code != 2 || !strings.Contains(errOut, "frobnicate") {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
}

func TestVersionExitsZero(t *testing.T) {
	code, out, _ := runCLI(t, "version")
	if code != 0 || !strings.HasPrefix(out, "formwork ") {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
}

func TestTestToyRepoFixturesPass(t *testing.T) {
	code, out, _ := runCLI(t, "test", "-C", filepath.Join("testdata", "toyrepo"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"[no-todo-markers] OK — 2 fixture(s)",
		"[readme-mentions-formwork] OK — 2 fixture(s)",
		"formwork test: 2/2 rules passed, 4 fixture(s) run, 0 rule(s) skipped",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestTestFailingFixtureExitsOne(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-banana", "fire-1", "f.txt"), "banana\n")
	code, out, _ := runCLI(t, "test", "-C", root)
	if code != 1 || !strings.Contains(out, "fire fixture declares no expectations") {
		t.Fatalf("exit = %d\noutput:\n%s", code, out)
	}
}

// TestTestWantNamingAbsentFileExitsOne guards the detection mechanism that #39
// relied on: a fire-N.want manifest naming a path absent from the fixture tree
// must fail (exit 1) with "missing expected finding", not pass vacuously. This
// is the CLI-level lock; internal/fixturetest unit-tests the same mechanism.
func TestTestWantNamingAbsentFileExitsOne(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-bak\n    type: file-naming\n    scope: {include: ['**']}\n    params: {forbid_ext: ['.bak']}\n")
	// The manifest names two offending paths; only one exists in fire-1/.
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-bak", "fire-1.want"), "present.bak\nabsent.bak\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-bak", "fire-1", "present.bak"), "x\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-bak", "pass-1", "ok.txt"), "x\n")
	code, out, _ := runCLI(t, "test", "-C", root)
	if code != 1 || !strings.Contains(out, "missing expected finding absent.bak") {
		t.Fatalf("exit = %d, want 1 with missing-expected-finding\noutput:\n%s", code, out)
	}
}

func TestTestMissingConfigExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "test", "-C", t.TempDir())
	if code != 2 || !strings.Contains(errOut, "formwork.yaml") {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
}

func TestLintToyRepoClean(t *testing.T) {
	code, out, _ := runCLI(t, "lint", "-C", filepath.Join("testdata", "toyrepo"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"[fixture-coverage] OK",
		"[empty-scope] OK",
		"[exemption-hygiene] OK",
		"formwork lint: 5/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestLintFlagsProblemsExitsOne(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['nomatch/**']}\n    params: {pattern: banana}\n")
	code, out, _ := runCLI(t, "lint", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"[fixture-coverage] FAIL",
		"[empty-scope] FAIL",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckMarkerSuppressionExitsZero(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n"+
			"    except: {marker: true}\n")
	mustWrite(t, filepath.Join(root, "f.txt"), "banana // formwork:allow no-banana legacy data\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	for _, want := range []string{"[no-banana] OK", "0 finding(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckReasonlessMarkerStillFails(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n"+
			"    except: {marker: true}\n")
	mustWrite(t, filepath.Join(root, "f.txt"), "banana // formwork:allow no-banana\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 || !strings.Contains(out, "[no-banana] FAIL") {
		t.Fatalf("exit = %d\noutput:\n%s", code, out)
	}
}

func TestCheckAllowlistSuppressionExitsZero(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**']}\n    params: {pattern: banana}\n"+
			"    except: {allowlist: allowlists/legacy.txt}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "allowlists", "legacy.txt"), "f.txt\n")
	mustWrite(t, filepath.Join(root, "f.txt"), "banana\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 || !strings.Contains(out, "[no-banana] OK") {
		t.Fatalf("exit = %d\noutput:\n%s", code, out)
	}
}

// lanedRepo writes a repo with two tagged rules and two lanes: pre-commit
// (tags: [go]) and ci (all, ci: true). a.go is clean; notes.txt trips
// no-todo-txt — so the go-only lane passes while the all lane fails.
func lanedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    tags: [go]\n  ci:\n    all: true\n    ci: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: no-todo-go\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: TODO}\n    tags: [go]\n"+
			"  - id: no-todo-txt\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: TODO}\n    tags: [txt]\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package main\n")
	mustWrite(t, filepath.Join(root, "notes.txt"), "TODO: later\n")
	return root
}

func TestCheckLaneRunsOnlySelectedRules(t *testing.T) {
	root := lanedRepo(t)
	code, out, _ := runCLI(t, "check", "-C", root, "--lane", "pre-commit")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "[no-todo-go] OK") {
		t.Errorf("output missing go-rule verdict:\n%s", out)
	}
	if strings.Contains(out, "no-todo-txt") {
		t.Errorf("txt rule should not run in the go lane:\n%s", out)
	}
}

func TestCheckLaneAllRunsEverything(t *testing.T) {
	root := lanedRepo(t)
	code, out, _ := runCLI(t, "check", "-C", root, "--lane", "ci")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{"[no-todo-go] OK", "[no-todo-txt] FAIL"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckNoLaneRunsAllRules(t *testing.T) {
	root := lanedRepo(t)
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\noutput:\n%s", code, out)
	}
	for _, want := range []string{"[no-todo-go] OK", "[no-todo-txt] FAIL"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestCheckUnknownLaneExits2(t *testing.T) {
	root := lanedRepo(t)
	code, _, errOut := runCLI(t, "check", "-C", root, "--lane", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown lane") || !strings.Contains(errOut, "bogus") {
		t.Fatalf("stderr should name the unknown lane: %s", errOut)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	// Disarm auto-maintenance: `git commit` otherwise forks a DETACHED
	// `git maintenance run --auto` child that outlives the test and creates
	// .git/objects/maintenance.lock while t.TempDir's RemoveAll is running
	// (the run-32901235204 flake). Verified on git 2.50.1: with this set, the
	// commit spawns nothing, and gc.auto 0 alone does NOT suppress the spawn.
	// The knob is honoured by the CI runners' git too — v2.55.0's
	// prepare_auto_maintenance (run-command.c) returns early on
	// maintenance.auto=false. (It did not exist yet in v2.29.0's, but that
	// version ran maintenance synchronously — no detach, no race.)
	gitRun(t, dir, "config", "maintenance.auto", "false")
}

func TestCheckStagedRestrictsToStagedFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	// Fixture token is WIDGET, not an annotation marker — this test file lives
	// under internal/, where this repo's own marker-comment gate scans it, and
	// it is vendored into the validating port, whose equivalent gate scans
	// test files too. That is why the prose here cannot name a marker either.
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "staged.go"), "package a\nvar x = WIDGET\n")
	mustWrite(t, filepath.Join(root, "unstaged.go"), "package b\nvar y = WIDGET\n")
	gitInit(t, root)
	gitRun(t, root, "add", ".formwork", "staged.go") // leave unstaged.go out of the index

	code, out, _ := runCLI(t, "check", "-C", root, "--staged")
	if code != 1 {
		t.Fatalf("want exit 1 (staged.go violates), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "staged.go") || strings.Contains(out, "unstaged.go") {
		t.Fatalf("--staged should scan only staged.go, not unstaged.go:\n%s", out)
	}

	// Without --staged, the whole tree is scanned and both violate.
	code2, out2, _ := runCLI(t, "check", "-C", root)
	if code2 != 1 || !strings.Contains(out2, "unstaged.go") {
		t.Fatalf("full-tree check should also see unstaged.go: code=%d\n%s", code2, out2)
	}
}

func TestCheckStagedFailsClosedOutsideRepo(t *testing.T) {
	root := t.TempDir() // config but no git repo
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	code, _, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("--staged outside a git repo must fail closed (exit 2), got %d", code)
	}
	if !strings.Contains(errOut, "git") {
		t.Fatalf("stderr should name the git failure: %q", errOut)
	}
}

// This is the pin for cli.go's "flag validation before config content" ordering,
// and the EMPTY .formwork/rules is load-bearing: with a rule present, both
// orderings produce exit 2 + "mutually exclusive" and the test passes either
// way. Three neighbouring tests in this file were given a rule for exactly the
// reason that would silently defeat this one — do not normalise it the same way.
// The negative assertion is what keeps the pin honest if someone does.
func TestCheckStagedAndRangeMutuallyExclusive(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	code, _, errOut := runCLI(t, "check", "-C", root, "--staged", "--range", "A..B")
	if code != 2 || !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("want exit 2 + mutual-exclusion message, got %d %q", code, errOut)
	}
	if strings.Contains(errOut, "no rules are configured") {
		t.Fatalf("the rule-set guard preempted flag validation — a caller who typed two conflicting flags was told about their config: %q", errOut)
	}
}

func TestScopeCommandClassifiesStaged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	gitInit(t, root)
	gitRun(t, root, "add", "a.go")

	code, out, _ := runCLI(t, "scope", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("scope output:\n%s", out)
	}
}

func TestScopeCommandFailClosedOutsideRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  languages:\n    go: ['**/*.go']\n")
	code, out, _ := runCLI(t, "scope", "-C", root) // no git repo → fail-closed
	if code != 0 {
		t.Fatalf("scope must not fail hard, exit %d", code)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("fail-closed should be runtime + flags true:\n%s", out)
	}
}

func fmtRepo(t *testing.T) string {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, "f.txt"), "banana\n")
	return root
}

func TestCheckFormatJSON(t *testing.T) {
	code, out, _ := runCLI(t, "check", "-C", fmtRepo(t), "--format", "json")
	if code != 1 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{`"rule": "no-banana"`, `"rules_total": 1`, `"path": "f.txt"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json missing %q:\n%s", want, out)
		}
	}
}

func TestCheckFormatGitHub(t *testing.T) {
	code, out, _ := runCLI(t, "check", "-C", fmtRepo(t), "--format", "github")
	if code != 1 || !strings.Contains(out, "::error file=f.txt,line=1::") {
		t.Fatalf("github exit %d:\n%s", code, out)
	}
}

func TestCheckUnknownFormatExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "check", "-C", fmtRepo(t), "--format", "xml")
	if code != 2 || !strings.Contains(errOut, "format") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

func TestCheckEngineFailsClosedOnDevBuild(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=0.2.0\"\n")
	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 2 || !strings.Contains(errOut, "engine") {
		t.Fatalf("exit = %d, stderr = %q; want exit 2 mentioning engine", code, errOut)
	}
}

func TestCheckEngineDevOptOutProceeds(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "1")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=0.2.0\"\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", code, errOut)
	}
	if !strings.Contains(errOut, "FORMWORK_ALLOW_DEV") {
		t.Fatalf("expected a dev-opt-out warning on stderr, got %q", errOut)
	}
}

// TestCheckEngineGateFiresBeforeRuleFilesAreParsed is finding 1's exact
// repro: an old binary meeting a config declaring an unsatisfiable engine
// constraint AND a rule file with a `type:` this binary does not know. The
// gate must fire on the envelope BEFORE config.Load ever reads the rule
// file, so the user is told their binary is unsupported — not that their
// rule file is broken.
func TestCheckEngineGateFiresBeforeRuleFilesAreParsed(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=99.0.0\"\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: a-rule\n    type: brand-new-type\n    scope: {include: ['**']}\n")
	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "engine") {
		t.Fatalf("stderr should mention engine, got:\n%s", errOut)
	}
	if strings.Contains(errOut, "brand-new-type") || strings.Contains(errOut, "unknown type") {
		t.Fatalf("stderr should NOT report the unknown rule type (the gate must fire before rule files are parsed):\n%s", errOut)
	}
}

// TestScopeGatesOnEngine is finding 6: scope must not bypass the engine gate.
func TestScopeGatesOnEngine(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=99.0.0\"\n")
	code, _, errOut := runCLI(t, "scope", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "engine") {
		t.Fatalf("stderr should mention engine, got:\n%s", errOut)
	}
}

// TestLintGatesOnEngine pins the #17 decision: lint is gated uniformly like
// every other config-loading command. The gate runs before rule files are
// parsed, and lint's diagnostics operate on loaded rules — so a binary that
// fails the constraint exits 2 with the engine message and does NOT reach lint's
// own checks. (Carving lint out was considered and rejected: for the newer
// config that motivates it, LoadRules would fail on unknown schema anyway, so a
// carve-out only trades the clean version message for a parse error.)
func TestLintGatesOnEngine(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=99.0.0\"\n")
	code, out, errOut := runCLI(t, "lint", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "engine") {
		t.Fatalf("stderr should mention engine, got:\n%s", errOut)
	}
	if strings.Contains(out, "formwork lint:") {
		t.Fatalf("gated lint must not emit its diagnostics summary; stdout:\n%s", out)
	}
}

// TestTestGatesOnEngine is the sibling of TestLintGatesOnEngine for `formwork
// test`: it is gated uniformly and its fixture run never begins on a binary that
// fails the constraint.
func TestTestGatesOnEngine(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=99.0.0\"\n")
	code, out, errOut := runCLI(t, "test", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "engine") {
		t.Fatalf("stderr should mention engine, got:\n%s", errOut)
	}
	if strings.Contains(out, "formwork test:") {
		t.Fatalf("gated test must not emit its fixture summary; stdout:\n%s", out)
	}
}

func TestCheckEngineDevOptOutRejectsFalsyValues(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("FORMWORK_ALLOW_DEV", v)
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=0.2.0\"\n")
			code, _, errOut := runCLI(t, "check", "-C", root)
			if code != 2 {
				t.Fatalf("FORMWORK_ALLOW_DEV=%q: exit = %d, want 2 (opt-out must not engage); stderr = %q", v, code, errOut)
			}
		})
	}
}

// TestCheckNoEngineFieldIgnoresAllowDev is finding 5's regression guard: a
// config with no `engine:` field leaves the whole backstop — including its
// opt-out — completely inert. Before the fix, enforceEngine printed
// "ignoring unparseable FORMWORK_ALLOW_DEV=..." to stderr on every run with
// no engine: field and a non-boolean value in that env var, which broke this
// repo's own self-host `make check` for any developer who happened to have
// FORMWORK_ALLOW_DEV set to something like "yes" in their shell.
func TestCheckNoEngineFieldIgnoresAllowDev(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "yes")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr should be empty when no engine: field is configured, got %q", errOut)
	}
}

// TestCheckEngineDevPathWarnsOnUnparseableAllowDev is the positive companion to
// #16's suppression: on a dev build (the test binary) WITH an engine:
// constraint, an unparseable FORMWORK_ALLOW_DEV is still relevant — the opt-out
// could apply here — so the notice must print. The trusted-release suppression
// itself can't be exercised through the CLI (the test binary is always a dev
// build) and is covered by the pure TestDevOptOutWarning.
func TestCheckEngineDevPathWarnsOnUnparseableAllowDev(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "yes")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nengine: \">=0.2.0\"\n")
	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("dev build must fail closed against an engine constraint (exit 2), got %d; stderr = %q", code, errOut)
	}
	if !strings.Contains(errOut, "ignoring unparseable FORMWORK_ALLOW_DEV") {
		t.Fatalf("dev path should warn about the unparseable value; stderr = %q", errOut)
	}
}

// escapeRepo has one declarative rule that PASSES and one heavy `command`
// escape that FAILS, so the two are told apart by the exit code alone.
func escapeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: no-todo-go\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: TODO}\n"+
			"  - id: heavy-escape\n    type: command\n    scope: {include: ['**/*.go']}\n    params: {cmd: ['false']}\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package main\n")
	return root
}

// The heavy escapes (command / git-diff) shell out to a script that re-scans
// the whole tree, so --staged cannot make them cheap. --skip-escapes drops
// them so a local hook stays fast; the whole-tree CI run is their backstop.
func TestCheckSkipEscapesDropsHeavyRules(t *testing.T) {
	root := escapeRepo(t)

	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("without --skip-escapes: exit = %d, want 1 (the escape fails)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "heavy-escape") {
		t.Errorf("without --skip-escapes the escape should run:\n%s", out)
	}

	code, out, _ = runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 0 {
		t.Fatalf("with --skip-escapes: exit = %d, want 0 (the escape is dropped)\noutput:\n%s", code, out)
	}
	// The id does now appear — in the not-run disclosure (#159), which is what
	// that disclosure is for. What must be absent is a VERDICT line: a rule
	// dropped before the engine reached none, and `[id] OK` would claim it did.
	if strings.Contains(out, "[heavy-escape]") {
		t.Errorf("--skip-escapes must drop the heavy rule, leaving it no verdict:\n%s", out)
	}
	if !strings.Contains(out, "heavy-escape: did not run") {
		t.Errorf("the dropped rule must be named as not run, not silently absent:\n%s", out)
	}
	if !strings.Contains(out, "[no-todo-go] OK") {
		t.Errorf("--skip-escapes must keep the declarative rules:\n%s", out)
	}
}

func TestCheckScanIgnoreHidesDeclaredPathsFromEveryRule(t *testing.T) {
	rule := "rules:\n  - id: no-todo\n    type: forbidden-pattern\n    severity: error\n    scope: {include: ['**']}\n    params: {pattern: 'TODO'}\n"
	violation := "TODO: from a foreign branch\n"

	// Control: without scan.ignore the worktree violation fails the run. This
	// half is load-bearing — without it, the second half passes vacuously if
	// the rule never fires at all.
	dirty := t.TempDir()
	mustWrite(t, filepath.Join(dirty, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dirty, ".formwork", "rules", "r.yaml"), rule)
	mustWrite(t, filepath.Join(dirty, ".claude", "worktrees", "wt", "dup.txt"), violation)
	mustWrite(t, filepath.Join(dirty, "src", "ok.txt"), "clean\n")
	if code, out, _ := runCLI(t, "check", "-C", dirty); code != 1 {
		t.Fatalf("control: exit = %d, want 1 (violation must be visible without ignore)\noutput:\n%s", code, out)
	}

	// With scan.ignore the worktree copy is invisible, while a real violation
	// outside the ignored tree is still reported.
	clean := t.TempDir()
	mustWrite(t, filepath.Join(clean, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  ignore:\n    - glob: '.claude/worktrees/**'\n      reason: agent harness worktrees are foreign branches\n")
	mustWrite(t, filepath.Join(clean, ".formwork", "rules", "r.yaml"), rule)
	mustWrite(t, filepath.Join(clean, ".claude", "worktrees", "wt", "dup.txt"), violation)
	mustWrite(t, filepath.Join(clean, "src", "bad.txt"), "TODO: real one\n")
	code, out, _ := runCLI(t, "check", "-C", clean)
	// The absent half is asserted on the ignored FILE, not on the substring
	// "worktrees": check now names each declared prune channel in its scan
	// summary (#151), so the glob itself legitimately appears in the output.
	// That is the disclosure, not a leak — what must stay absent is a finding
	// from inside the pruned tree.
	if code != 1 || !strings.Contains(out, "src/bad.txt") || strings.Contains(out, "dup.txt") {
		t.Fatalf("exit = %d — want the real violation reported and the ignored one absent\noutput:\n%s", code, out)
	}
}
