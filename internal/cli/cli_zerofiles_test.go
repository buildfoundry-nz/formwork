package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// #151 rows 1-9 and 13: the scan looked at nothing it was scoped to look at,
// and `check` reported "N/N rules passed" at exit 0. PR #154 landed the zero-
// RULES half (rows 10-12); this is the zero-FILES half.
//
// TWO DISTINCT DEFECTS LIVE HERE, and the issue's own framing sees only the
// first. Rows 1-5 have an empty or pruned FileSet; rows 6, 7, 8 and 13 have a
// NON-EMPTY FileSet and a rule that still cannot fire, because scope.exclude,
// except.paths or a mistyped include glob selected nothing out of it. Row 8 —
// the include-glob typo PR #154's scope note calls the commonest real cause —
// has both a non-empty FileSet and an EMPTY prune census, so neither a
// `len(fset.Files) == 0` guard nor anything built on `fset.Ignored` reaches it.
//
// The contract these pin, per the two answers this increment gives:
//
//   - REPORT (rows 1-8, 13): check emits a scan summary in all three formats —
//     how many files it looked at, which rules matched none of them, and what
//     each declared prune channel removed. Exit codes are unchanged, because an
//     empty scope is legitimate: fixture roots are small, and a rule scoped to a
//     path the repo has not created yet is not a defect. `set-relation`'s
//     min_count is the shipped precedent for making such a floor opt-in.
//   - REFUSE (row 9): git NAMED a changed path and a declared prune channel hid
//     it from the scan. There is no benign reading of that — the operator asked
//     for a specific file set and silently got a different one — so it is exit
//     2, the same answer rangeValueUsable already gives one screen up for a
//     supplied flag that silently becomes a different run.

// zeroFilesRepo builds a repo whose src/bad.go holds a known violation, with
// the caller's formwork.yaml body and rule scope. Every row below is this one
// tree seen through a different config, so a row that reads green is always
// the config's doing and never the fixture's.
func zeroFilesRepo(t *testing.T, envelope, scope string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), envelope)
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: "+scope+"\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")
	return root
}

// The control for every row in this file: the same tree, scanned, fails. A
// guard that made these rows loud by making everything loud would still pass
// its own tests; this is what stops that.
func TestZeroFilesControlViolationIsStillFound(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['src/**/*.go']}")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the fixture must violate when it is actually scanned\n%s", code, out)
	}
	if !strings.Contains(out, "[no-widget] FAIL") {
		t.Fatalf("expected the violation:\n%s", out)
	}
}

// Rows 1-2: a root holding only .formwork/ — a sparse or partial checkout, a
// stale worktree, or simply the wrong directory that happens to carry config.
// Zero files scanned must not read like a clean tree.
func TestCheckZeroFilesScannedIsDisclosed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['src/**/*.go']}\n    params: {pattern: WIDGET}\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — an empty tree stays a pass; it must stop being a SILENT one\n%s", code, out)
	}
	if !strings.Contains(out, "scan: 0 file(s) scanned") {
		t.Errorf("check must say it looked at nothing:\n%s", out)
	}
	if !strings.Contains(out, "no-widget: scope matched no files") {
		t.Errorf("the rule that could not fire must be named:\n%s", out)
	}
}

// Row 8, the commonest real cause: an include-glob typo. TWO files are scanned
// and the prune census is EMPTY, so this row is the one that falsifies both
// mechanisms the issue proposes. The rule is vacuous and nothing says so.
func TestCheckIncludeGlobTypoIsNamed(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['srcs/**/*.go']}")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const y = 1\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (report-only)\n%s", code, out)
	}
	if !strings.Contains(out, "scan: 2 file(s) scanned") {
		t.Errorf("the FileSet is not empty here — the summary must say so:\n%s", out)
	}
	if !strings.Contains(out, "no-widget: scope matched no files") {
		t.Errorf("a rule whose include glob selects nothing must be named:\n%s", out)
	}
}

// Row 6: scope.exclude swallows the include set. Non-empty FileSet again.
func TestCheckScopeExcludeSwallowingIncludeIsNamed(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['src/**/*.go'], exclude: ['src/**']}")
	_, out, _ := runCLI(t, "check", "-C", root)
	if !strings.Contains(out, "no-widget: scope matched no files") {
		t.Errorf("scope.exclude swallowing the include set must be named:\n%s", out)
	}
}

// Row 7: except.paths swallows the include set. A different field, the same
// vacuity, and neither reaches the scan package at all — both are decided in
// config.Rule.Applies, which is why the summary is computed from that predicate
// rather than from the prune census.
func TestCheckExceptPathsSwallowingIncludeIsNamed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['src/**/*.go']}\n"+
			"    except: {paths: ['src/**']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")
	_, out, _ := runCLI(t, "check", "-C", root)
	if !strings.Contains(out, "no-widget: scope matched no files") {
		t.Errorf("except.paths swallowing the include set must be named:\n%s", out)
	}
}

// Row 13: one vacuous rule sitting beside a live one that really ran. Both
// print `[id] OK` today and the two OKs mean different things.
func TestCheckVacuousRuleBesideLiveOneIsNamed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: live-rule\n    type: forbidden-pattern\n    scope: {include: ['src/**/*.go']}\n    params: {pattern: BANANA}\n"+
			"  - id: vacuous-rule\n    type: forbidden-pattern\n    scope: {include: ['nowhere/**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const y = 1\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "vacuous-rule: scope matched no files") {
		t.Errorf("the vacuous rule must be named:\n%s", out)
	}
	if strings.Contains(out, "live-rule: scope matched no files") {
		t.Errorf("the rule that DID look at files must not be named — that would make the disclosure worthless:\n%s", out)
	}
}

// Rows 3-4: a scan.ignore glob one level too broad. Row 4 is the sharper of the
// two and the reason the prune census is reported unconditionally: when the
// glob hides only SOME of a rule's scope, the rule still matches files, so
// nothing about empty scopes fires. `formwork lint` catches this whenever the
// hidden file is TRACKED — scan-ignore-tracked (#90) names the file and exits 1
// — so the census line is the only signal for an UNTRACKED hidden file, and the
// only one available without a second invocation.
func TestCheckScanIgnorePruneIsDisclosed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  ignore:\n    - glob: 'src/gen/**'\n      reason: generated\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['src/**/*.go']}\n    params: {pattern: WIDGET}\n")
	// The violation is INSIDE the pruned tree; everything the rule can still
	// see is clean. That is what makes this row 4 rather than row 3.
	mustWrite(t, filepath.Join(root, "src", "gen", "bad.go"), "const x = \"WIDGET\"\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const y = 1\n")
	mustWrite(t, filepath.Join(root, "src", "also-ok.go"), "const z = 2\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (report-only)\n%s", code, out)
	}
	if strings.Contains(out, "scope matched no files") {
		t.Fatalf("this row has a rule that DID match files — the empty-scope arm must not be what reports it:\n%s", out)
	}
	if !strings.Contains(out, "scan.ignore: src/gen/**") {
		t.Errorf("the prune channel must be named at check time, not only under lint:\n%s", out)
	}
}

// Row 5: scan.gitignore pruning stays REPORT-ONLY. Failing on it would
// contradict #80's design — git will not take an ignored path into a commit
// without an `add -f` — so the exit code must not move.
func TestCheckGitignorePruneIsDisclosedAndStaysExit0(t *testing.T) {
	root := zeroFilesRepo(t,
		"version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n",
		"{include: ['src/**/*.go']}")
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, ".gitignore"), "src/bad.go\n")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — gitignore pruning is report-only (#80)\n%s", code, out)
	}
	if !strings.Contains(out, "scan.gitignore:") {
		t.Errorf("the gitignore channel must be named at check time:\n%s", out)
	}
}

// The summary must reach -format json, which adopters' tooling reads.
func TestCheckScanSummaryReachesJSON(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['srcs/**/*.go']}")
	code, out, _ := runCLI(t, "check", "-C", root, "-format", "json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	var rep struct {
		Scan struct {
			FilesScanned         int      `json:"files_scanned"`
			RulesMatchingNoFiles []string `json:"rules_matching_no_files"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if rep.Scan.FilesScanned != 1 {
		t.Errorf("files_scanned = %d, want 1\n%s", rep.Scan.FilesScanned, out)
	}
	if len(rep.Scan.RulesMatchingNoFiles) != 1 || rep.Scan.RulesMatchingNoFiles[0] != "no-widget" {
		t.Errorf("rules_matching_no_files = %v, want [no-widget]\n%s", rep.Scan.RulesMatchingNoFiles, out)
	}
}

// -format github wrote ZERO BYTES for a zero-finding run, so the one surface
// most adopters actually read said nothing at all — the summary has to reach it
// or the fix is invisible where it matters most. Notices can never read as a
// failure and never affect the exit code.
func TestCheckScanSummaryReachesGitHub(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['srcs/**/*.go']}")
	code, out, _ := runCLI(t, "check", "-C", root, "-format", "github")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("github format wrote nothing at all for a run that checked nothing")
	}
	for _, want := range []string{"::notice::", "1 file(s) scanned", "no-widget: scope matched no files"} {
		if !strings.Contains(out, want) {
			t.Errorf("github output missing %q:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Row 9 — the refusal.
// ---------------------------------------------------------------------------

// stagedHiddenRepo stages src/gen/bad.go (a violation) under a scan.ignore glob
// that hides it, plus whatever extra files the caller wants staged alongside.
func stagedHiddenRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := zeroFilesRepo(t,
		"version: 1\nscan:\n  ignore:\n    - glob: 'src/gen/**'\n      reason: generated\n",
		"{include: ['**/*.go']}")
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "src", "gen", "bad.go"), "const x = \"WIDGET\"\n")
	for p, c := range extra {
		mustWrite(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	gitRun(t, root, "add", "-A")
	return root
}

// Row 9, the sharpest in the issue: git named the file, it was staged, and a
// declared prune channel removed it before any rule saw it. The output was
// BYTE-IDENTICAL to `--staged` with nothing staged.
func TestCheckStagedPathHiddenByScanIgnoreExits2(t *testing.T) {
	root := stagedHiddenRepo(t, nil)
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the file set asked for is not the file set checked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "rules passed") {
		t.Errorf("a run that never saw the paths it was given must not report rules passed:\n%s", out)
	}
	for _, want := range []string{"src/gen/bad.go", "scan.ignore", "src/gen/**"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q — the path and the channel that hid it must both be named:\n%s", want, errOut)
		}
	}
}

// The same row with a SECOND staged file that survives the prune, so the
// restricted FileSet is NON-EMPTY. A guard written to
// `len(changed) > 0 && len(changedFset.Files) == 0` — the set-level
// discriminator — does not fire here and leaves the staged violation exactly as
// green as before. The accounting has to be per-path.
func TestCheckStagedHiddenPathAmongVisibleOnesExits2(t *testing.T) {
	root := stagedHiddenRepo(t, map[string]string{"lib/ok.go": "const y = 1\n"})
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — one visible staged file must not excuse a hidden one\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/gen/bad.go") {
		t.Errorf("stderr must name the hidden path:\n%s", errOut)
	}
	if strings.Contains(errOut, "lib/ok.go") {
		t.Errorf("the staged path that WAS scanned must not be named as hidden:\n%s", errOut)
	}
}

// --range reaches the same seam through a different flag; #154's round 3 shipped
// a guard into one of two CALLERS of the same flag and left the sibling behind.
func TestCheckRangeHiddenPathExits2(t *testing.T) {
	root := stagedHiddenRepo(t, nil)
	gitRun(t, root, "commit", "-qm", "one")
	mustWrite(t, filepath.Join(root, "src", "gen", "bad.go"), "const x = \"WIDGET\"\nconst z = 2\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "two")
	code, _, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "--range") {
		t.Errorf("stderr must name the flag that asked for the file set:\n%s", errOut)
	}
}

// ---------------------------------------------------------------------------
// The legitimate empty cases. A fix that turns these red is worse than the bug.
// ---------------------------------------------------------------------------

// --staged with nothing staged: exit 0, and no refusal.
func TestCheckStagedNothingStagedStaysExit0(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — nothing staged is a legitimate empty\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// --staged with ONLY .formwork/ staged: the built-in skip set removed those
// paths, and it is not a declared prune channel. Refusing here would make every
// config-only commit impossible, which is why the attribution filter excludes
// built-in skips rather than treating "absent from the scan" as sufficient.
func TestCheckStagedOnlyFormworkStagedStaysExit0(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r2.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: BANANA}\n")
	gitRun(t, root, "add", "-A")
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a config-only commit stages only built-in-skipped paths\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// A path staged and then removed from the WORKTREE, sitting OUTSIDE every
// declared glob, used to exit 0 here: no configured channel covered it, so this
// guard had nothing it could truthfully name and stayed quiet while the staged
// bytes committed unchecked (#158). It is refused now — by the ARRIVAL guard,
// which observes the filesystem rather than reading the globs.
//
// What this row pins is that the two guards stay UNCONFLATED (task-2 rule 3).
// Their cures are opposites — "narrow the glob" does nothing for a file that is
// not on disk — so the channel guard's wording must not appear for a path no
// channel is responsible for. The refusal itself is asserted in
// cli_absent_test.go; only the separation is asserted here.
func TestCheckStagedThenDeletedFromWorktreeIsNotTheChannelGuard(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "src", "bad.go"))
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "hidden by") || strings.Contains(errOut, "narrow the glob") {
		t.Errorf("no declared channel hid this path — the channel guard's cure must not be printed for it:\n%s", errOut)
	}
}

// Under a file-set mode the summary must report the RESTRICTED count. Reporting
// the whole-tree number would be a false disclosure of exactly the kind this
// change exists to remove: "12 file(s) scanned" over a --staged run that looked
// at one is a more confident lie than the silence it replaced. Found by
// mutation — the whole-tree spelling passed every other test here.
func TestCheckStagedSummaryReportsTheRestrictedSet(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	for _, p := range []string{"a.go", "b.go", "c.go", "d.go"} {
		mustWrite(t, filepath.Join(root, "lib", p), "const q = 1\n")
	}
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "lib", "a.go"), "const q = 2\n")
	gitRun(t, root, "add", "lib/a.go")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "scan: 1 path(s) requested by --staged, 1 file(s) scanned") {
		t.Errorf("--staged must report the restricted count, not the whole tree (5 files):\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Review round 1: what the summary must say under a file-set mode.
// ---------------------------------------------------------------------------

// A run where git NAMED paths and the walk produced none of them rendered
// byte-identically to `--staged` with nothing staged — both `0 file(s)
// scanned`. The counts have to differ, and the ONE construction that still
// reaches that state is a POINTER ENTRY git named: a symlink or a submodule
// gitlink, which the walk produces for nobody and which the refuse half
// deliberately carves out. ScanSummary.PathsRequested says the same.
//
// Everything else that used to reach it is now refused instead. A path git
// named that the scan did not produce is exit 2 before any summary renders,
// and that includes the spelling-mismatch shape — refused, by one of the two
// absence reasons depending on whether the filesystem normalizes names, not a
// silent gap.
//
// The construction changed with #158's fix for exactly that reason. This row
// used to stage a file and delete it from the worktree; that is now exit 2 with
// no report rendered at all, so the assertion had nothing left to read.
func TestCheckStagedDistinguishesRequestedFromScanned(t *testing.T) {
	// A path git names and the walk does not produce, at exit 0.
	hidden := t.TempDir()
	mustWrite(t, filepath.Join(hidden, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(hidden, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, hidden)
	gitRun(t, hidden, "add", "-A")
	gitRun(t, hidden, "commit", "-qm", "init")
	if err := os.Symlink("nowhere", filepath.Join(hidden, "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitRun(t, hidden, "add", "-A")
	hiddenCode, hiddenOut, hiddenErr := runCLI(t, "check", "-C", hidden, "--staged")
	if hiddenCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", hiddenCode, hiddenOut, hiddenErr)
	}

	// Nothing staged at all: the legitimate empty.
	quiet := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, quiet)
	gitRun(t, quiet, "add", "-A")
	gitRun(t, quiet, "commit", "-qm", "init")
	_, quietOut, _ := runCLI(t, "check", "-C", quiet, "--staged")

	if hiddenOut == quietOut {
		t.Fatalf("a run that asked for paths and got none renders identically to one that asked for nothing:\n%s", hiddenOut)
	}
	if !strings.Contains(hiddenOut, "path(s) requested") {
		t.Errorf("the requested count must be reported under a file-set mode:\n%s", hiddenOut)
	}
	if !strings.Contains(quietOut, "0 path(s) requested") {
		t.Errorf("nothing staged must say so in the same vocabulary, not fall silent:\n%s", quietOut)
	}
}

// When every selected rule is a whole-tree invariant, the changeset is handed to
// nothing — the rules evaluate over the TRACKED tree instead (#4's monotonicity
// partition). Reporting the changeset's size beside "1/1 rules passed" is a
// manufactured coverage claim: no rule read those files. The partition is
// disclosed instead of implied.
func TestCheckStagedNamesTheWholeTreeInvariantArm(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: license-required\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n"+
			"    params: {pattern: LICENSE, mode: exists}\n")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "// LICENSE\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "notes.txt"), "unrelated\n")
	gitRun(t, root, "add", "notes.txt")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "whole-tree invariant") {
		t.Errorf("a run whose rules all bypassed the changeset must say so:\n%s", out)
	}
}

// Under --staged, "scope matched no files" is the NORMAL state of nearly every
// rule — a rule that does not cover this commit is not vacuous. Reporting it
// buries the real signal on the path that runs most (the pre-commit shim), and
// the overflow line's referral to `formwork lint` is FALSE there, because lint
// judges vacuity against the whole tree. Vacuity is a whole-tree question and
// is asked only in the whole-tree run.
func TestCheckStagedDoesNotReportChangesetIrrelevanceAsVacuity(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: go-rule\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n"+
			"  - id: sql-rule\n    type: forbidden-pattern\n    scope: {include: ['**/*.sql']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "const a = 1\n")
	mustWrite(t, filepath.Join(root, "db", "m.sql"), "select 1;\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "const a = 2\n")
	gitRun(t, root, "add", "src/a.go")

	code, out, _ := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "scope matched no files") {
		t.Errorf("a healthy rule outside the changeset is not vacuous — check --staged must not say it is:\n%s", out)
	}
	// The control: the same rule IS reported over the whole tree only if it is
	// genuinely vacuous there. Here both rules cover files, so neither is named.
	_, whole, _ := runCLI(t, "check", "-C", root)
	if strings.Contains(whole, "scope matched no files") {
		t.Errorf("neither rule is vacuous over the whole tree:\n%s", whole)
	}
}

// The other half of the boundary above, and the case that decided the fix's
// shape. The SAME staged-then-deleted path, moved INSIDE a declared glob, was
// already refused before #158's fix — but for the wrong reason. Channel
// attribution reads the configured globs, so it could not tell a path the glob
// hid from a path that never arrived and merely happened to match one, and it
// printed "hidden by scan.ignore (src/gen/**) … narrow the glob".
//
// It now reports the ARRIVAL reason instead, and the change is deliberate: the
// glob is not the cause here. Deleting the glob would not make this path
// scannable, so naming it is a false cause carrying an inert cure. os.Lstat
// settles the same question by looking, so the observation is asked first and
// wins. The exit code is 2 on both sides of that choice — precedence moves only
// the words.
func TestCheckStagedThenDeletedInsideAGlobReportsTheArrivalReason(t *testing.T) {
	root := zeroFilesRepo(t,
		"version: 1\nscan:\n  ignore:\n    - glob: 'src/gen/**'\n      reason: generated\n",
		"{include: ['**/*.go']}")
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "src", "gen", "bad.go"), "const x = \"WIDGET\"\n")
	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "src", "gen", "bad.go"))

	code, _, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — glob-derived attribution cannot exempt a path that never arrived", code)
	}
	if !strings.Contains(errOut, "src/gen/bad.go") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
	if !strings.Contains(errOut, "not present in the working tree") {
		t.Errorf("the reason must be the observed one:\n%s", errOut)
	}
	if strings.Contains(errOut, "src/gen/**") {
		t.Errorf("the glob is not why this path is missing — naming it prints an inert cure:\n%s", errOut)
	}
}

// The control for the row above: the same glob, the same path, but the file is
// still ON DISK. Now the glob really is the cause, and the channel guard —
// which the arrival check must not have swallowed — reports it with the cure
// that works.
func TestCheckStagedInsideAGlobStillOnDiskReportsTheChannel(t *testing.T) {
	root := zeroFilesRepo(t,
		"version: 1\nscan:\n  ignore:\n    - glob: 'src/gen/**'\n      reason: generated\n",
		"{include: ['**/*.go']}")
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "src", "gen", "bad.go"), "const x = \"WIDGET\"\n")
	gitRun(t, root, "add", "-A")

	code, _, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "hidden by scan.ignore (src/gen/**)") {
		t.Errorf("a path the glob really does hide must still name the glob:\n%s", errOut)
	}
	if strings.Contains(errOut, "not present in the working tree") {
		t.Errorf("the file is on disk — the arrival reason would be false:\n%s", errOut)
	}
}

// PathsRequested counts paths that COULD have been scanned. A path beneath a
// built-in skip DIRECTORY (.git, .formwork) never could, so counting it
// manufactures a requested-vs-scanned gap on the most ordinary commit there is —
// editing a rule file. A signal that fires on every config-only commit is one
// readers learn to skip, which is the failure this whole block is meant to
// avoid.
//
// DIRECTORY is the operative word, and the count got it wrong until review: the
// walk consults its skip set only for directories, so a regular file NAMED
// .formwork is scanned like any other and must be counted. scannablePaths uses
// scan.UnderBuiltinSkipDir for exactly that reason —
// TestCheckStagedFormworkAncestorIsStillExcused pins this direction and
// TestCheckStagedRegularFileNamedFormworkIsRefused pins the other.
func TestCheckStagedConfigOnlyCommitReportsNoPhantomGap(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r2.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: BANANA}\n")
	gitRun(t, root, "add", "-A")

	code, out, _ := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "0 path(s) requested") {
		t.Errorf("a built-in-skipped path was never scannable — counting it invents a gap:\n%s", out)
	}
	// The control: a real source path staged alongside still counts.
	mustWrite(t, filepath.Join(root, "lib", "x.go"), "const q = 1\n")
	gitRun(t, root, "add", "-A")
	_, out2, _ := runCLI(t, "check", "-C", root, "--staged")
	if !strings.Contains(out2, "1 path(s) requested") {
		t.Errorf("a scannable staged path must still be counted:\n%s", out2)
	}
}
