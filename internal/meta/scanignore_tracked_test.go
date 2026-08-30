// scanignore_tracked_test.go — the scan-ignore-tracked check (#90) and the
// git-index test plumbing it needs. Split from lint_test.go, which the
// 750-line vendor cap bounds; same package, shares writeRepo/lint/lintRule.
package meta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// gitTrack initialises a git repository at root and stages the named paths
// (add -f, so .gitignore cannot interfere). Index-only: TrackedUnder reads
// git ls-files, and tracked-in-index is the bypass's earliest form.
func gitTrack(t *testing.T, root string, paths ...string) {
	t.Helper()
	gitRun(t, root, "init", "-q")
	if len(paths) > 0 {
		gitRun(t, root, append([]string{"add", "-f", "--"}, paths...)...)
	}
}

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mustLoad loads root's config or fails the test. (The lint() helper inlines
// config.Load and fatals on Lint errors too, which the #90 git-failure tests
// cannot use — they need the error back.)
func mustLoad(t *testing.T, root string) *config.Config {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// lintTracked is lint() plus a git index: writes files, tracks the named
// paths, runs Lint. For scan.ignore-carrying configs, which as of #90
// require a verifiable tracked set. The shared lint() helper deliberately
// stays git-free — its non-git shape is load-bearing for every other caller.
func lintTracked(t *testing.T, files map[string]string, track ...string) (int, string) {
	t.Helper()
	root := writeRepo(t, files)
	gitTrack(t, root, track...)
	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, devOptOutActive, false)
	if err != nil {
		t.Fatalf("lint: %v\n%s", err, sb.String())
	}
	return failed, sb.String()
}

// scanIgnoreFiles is the shared base corpus for the scan-ignore-tracked (#90)
// tests: a scratch/** ignore, one rule with fixtures, one in-scope file.
func scanIgnoreFiles(extra map[string]string) map[string]string {
	files := map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  ignore:\n    - glob: 'scratch/**'\n      reason: agent scratch trees\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

// TestLintFailsOnTrackedFileHiddenByScanIgnore pins #90's core: a git-tracked
// file under a pruned glob is a committed bypass — every rule reports clean
// while the file rides along in the repository — and lint must fail it BY
// PATH, naming the glob (never a bare count, #57's lesson).
func TestLintFailsOnTrackedFileHiddenByScanIgnore(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/tools/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt", "scratch/tools/evil.ts")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 {
		t.Fatalf("a tracked file under scan.ignore must fail lint:\n%s", sb.String())
	}
	out := sb.String()
	for _, want := range []string{"[scan-ignore-tracked] FAIL", "scratch/tools/evil.ts", "scratch/**"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestLintScanIgnoreTrackedPassesOnUntrackedFile pins the tracked axis: the
// same file NOT in the index is the feature working (a pruned scratch tree),
// not a bypass — #80 owns the untracked direction.
func TestLintScanIgnoreTrackedPassesOnUntrackedFile(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/tools/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt") // evil.ts on disk, untracked
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[scan-ignore-tracked] OK") {
		t.Fatalf("untracked file under scan.ignore must pass the check, failed=%d:\n%s", failed, sb.String())
	}
}

// TestLintScanIgnoreTrackedCatchesDeepDescendantOfPrunedDir pins the
// pruned-ancestor branch: the glob prunes at the directory, the walk never
// descends, so the tracked file has no record of its own — the Dir-record
// prefix match is what catches it.
func TestLintScanIgnoreTrackedCatchesDeepDescendantOfPrunedDir(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/tools/a/b/deep.ts": "export const deep = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt", "scratch/tools/a/b/deep.ts")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 || !strings.Contains(sb.String(), "scratch/tools/a/b/deep.ts") {
		t.Fatalf("deep descendant of a pruned dir must be caught via the ancestor record:\n%s", sb.String())
	}
}

// TestLintScanIgnoreTrackedCatchesFileLevelIgnore pins the file-level-ignore
// verdict (#90 review): a glob that matches files directly without matching
// any directory produces Dir:false records. Two nets cover this input — the
// exact-path arm of ignoredByFold AND the record-free fallback (a
// file-level-ignored file is never in fset.Files) — so deleting either one
// alone keeps the verdict; mutation-verified that deleting BOTH fails here.
// The arm is still independently load-bearing where the fallback cannot
// reach: exemptionHygiene's allowlist "hidden by scan.ignore" diagnosis
// (which has no fallback), and the case-folded compare against a
// literal-cased glob doublestar would miss.
func TestLintScanIgnoreTrackedCatchesFileLevelIgnore(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  ignore:\n    - glob: '**/*.gen.ts'\n      reason: generated output\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt":    "in scope\n",
		"src/x.gen.ts": "export const gen = 1\n",
	})
	gitTrack(t, root, ".formwork", "notes.txt", "src/x.gen.ts")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if failed == 0 || !strings.Contains(out, "src/x.gen.ts") || !strings.Contains(out, "**/*.gen.ts") {
		t.Fatalf("a tracked file hidden by a file-level ignore record must be caught:\n%s", out)
	}
}

// TestLintScanIgnoreTrackedCatchesIndexOnlyBypass pins the record-free
// fallback (#90 review): git add -f + rm -rf leaves the file tracked with NO
// on-disk ancestor, so the walk yields no record — a state that persists
// indefinitely locally and for the life of a sparse CI checkout. The check
// must still fail it by matching the index path against the globs directly.
func TestLintScanIgnoreTrackedCatchesIndexOnlyBypass(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/tools/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt", "scratch/tools/evil.ts")
	if err := os.RemoveAll(filepath.Join(root, "scratch")); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 || !strings.Contains(sb.String(), "scratch/tools/evil.ts") {
		t.Fatalf("an index-only bypass (tree deleted / sparse checkout) must still fail:\n%s", sb.String())
	}
}

// TestLintScanIgnoreTrackedAbsentWithoutIgnores pins the gate. NOTE: this
// test is GREEN AT BIRTH (current Lint never emits the check name either) —
// it pins current behavior; its falsification is the gate mutation in the
// #90 plan's Task 5 step 5, not the RED phase. Load-bearing half: no
// ignores → no git requirement, so every non-git lint consumer is untouched
// (this repo has no .git-independent lint path otherwise).
func TestLintScanIgnoreTrackedAbsentWithoutIgnores(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	// Deliberately no gitTrack: no ignores -> no git requirement.
	var sb strings.Builder
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, false, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "scan-ignore-tracked") {
		t.Fatalf("check must not run (or even appear) without scan.ignore:\n%s", sb.String())
	}
}

// TestLintScanIgnoreTrackedGitFailureIsError pins fail-closed: scan.ignore
// configured but root is not a git repository — Lint must return an error
// (exit 2 at the CLI), never a vacuous OK, and must still print the
// escape-hatch enumeration first (the D1 degraded-repo contract every other
// lint error path honors).
func TestLintScanIgnoreTrackedGitFailureIsError(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(nil)) // NO gitTrack
	var sb strings.Builder
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, false, false); err == nil {
		t.Fatal("git failure while scan.ignore is configured must be an error, not a silent skip")
	}
	if !strings.Contains(sb.String(), "escape hatches") {
		t.Fatalf("enumeration must still print on the error path (D1):\n%s", sb.String())
	}
}

// TestLintScanIgnoreTrackedNeverReportsBuiltinSkips pins acceptance
// criterion 3 against BOTH match paths: .formwork produces no records
// (skipDirs precedes ignore matching in the walk), and the record-free
// fallback must exclude built-in-skip components too — without that
// exclusion this test fails, because tracked .formwork files are record-less
// and '.formwork/**' textually matches them. .formwork must stay
// committable — it holds the rule corpus.
func TestLintScanIgnoreTrackedNeverReportsBuiltinSkips(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  ignore:\n    - glob: '.formwork/**'\n      reason: would textually match the corpus itself\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	gitTrack(t, root, ".formwork", "notes.txt")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 || !strings.Contains(sb.String(), "[scan-ignore-tracked] OK") {
		t.Fatalf("tracked .formwork files are built-in-skip territory, never scan.ignore-hidden, failed=%d:\n%s", failed, sb.String())
	}
}

// TestLintScanIgnoreTrackedFoldsCaseOnIgnorecaseRepos pins the
// case-insensitive-filesystem bypass (#90 review): on such a filesystem
// (core.ignorecase=true, set by git init itself) the index can hold
// Scratch/evil.ts while the file lives under pruned scratch/ — one
// `git add -f SCRATCH/evil.ts` away. The record compare must fold case
// exactly when core.ignorecase says so. Skipped on case-sensitive
// filesystems, where the divergence cannot arise and folding would be
// wrong (two dirs differing only by case genuinely coexist there).
func TestLintScanIgnoreTrackedFoldsCaseOnIgnorecaseRepos(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt")
	if v, _ := vcs.GetConfig(root, "core.ignorecase"); v != "true" {
		t.Skip("filesystem is case-sensitive; the casing divergence cannot arise")
	}
	// Index the file under divergent casing, as a cross-platform commit or a
	// local add through a case-folding path would record it:
	blob := gitOut(t, root, "hash-object", "-w", filepath.Join(root, "scratch", "evil.ts"))
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "100644,"+blob+",Scratch/evil.ts")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 || !strings.Contains(sb.String(), "Scratch/evil.ts") {
		t.Fatalf("case-divergent index spelling must still be caught on an ignorecase repo:\n%s", sb.String())
	}
}

func gitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestLintScanIgnoreTrackedRunsUnderRuleScoping pins the --rule decision:
// unlike the lane checks (which scoping makes false-fail, so scopeToRule
// drops them), this verdict is scoping-invariant — cfg.Ignore and the tree
// are untouched by --rule — so the check still runs and still fails scoped.
func TestLintScanIgnoreTrackedRunsUnderRuleScoping(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/tools/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt", "scratch/tools/evil.ts")
	cfg := mustLoad(t, root)
	// Mirror cli.scopeToRule: shallow copy, Rules narrowed, Lanes dropped,
	// Ignore intact.
	scoped := *cfg
	scoped.Rules = nil
	for _, r := range cfg.Rules {
		if r.ID == "no-banana" {
			scoped.Rules = append(scoped.Rules, r)
		}
	}
	scoped.Lanes = nil
	var sb strings.Builder
	failed, err := meta.Lint(&scoped, root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if failed == 0 || !strings.Contains(out, "[scan-ignore-tracked] FAIL") || !strings.Contains(out, "scratch/tools/evil.ts") {
		t.Fatalf("the check must run (and fail) under --rule scoping:\n%s", out)
	}
}

// TestLintScanIgnoreTrackedFoldsCaseSpelledYes pins the detection gate the
// fold rides on (#90 fail-open review, proven end-to-end): git's boolean
// parser accepts yes/on/1/True for core.ignorecase, but `git config --get`
// returns the value AS SPELLED — a raw string compare against "true" would
// silently disable folding after nothing but `git config core.ignorecase
// yes`, reopening the case-divergence bypass with no trace in lint output.
func TestLintScanIgnoreTrackedFoldsCaseSpelledYes(t *testing.T) {
	root := writeRepo(t, scanIgnoreFiles(map[string]string{
		"scratch/evil.ts": "export const evil = 1\n",
	}))
	gitTrack(t, root, ".formwork", "notes.txt")
	if v, _ := vcs.GetConfig(root, "core.ignorecase"); v != "true" {
		t.Skip("filesystem is case-sensitive; the casing divergence cannot arise")
	}
	gitRun(t, root, "config", "core.ignorecase", "yes") // same meaning to git, different spelling
	blob := gitOut(t, root, "hash-object", "-w", filepath.Join(root, "scratch", "evil.ts"))
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "100644,"+blob+",Scratch/evil.ts")
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 || !strings.Contains(sb.String(), "Scratch/evil.ts") {
		t.Fatalf("a truthy core.ignorecase spelling must not disable folding:\n%s", sb.String())
	}
}

// TestLintScanIgnoreTrackedFallbackAttributionMatchesWalk pins the fallback's
// attribution order against the walk's (#95 review): the walk prunes at the
// SHALLOWEST matching ancestor, testing every glob at each level in config
// order — so with overlapping globs at different depths ('scratch/tools/**'
// before 'scratch/**' in config order), a pruned-record match attributes
// 'scratch/**' (zero-segment ** matches the dir itself). The record-free
// fallback must attribute identically, or the same tracked path names a
// different glob depending on whether its tree happens to be on disk.
func TestLintScanIgnoreTrackedFallbackAttributionMatchesWalk(t *testing.T) {
	files := scanIgnoreFiles(map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  ignore:\n    - glob: 'scratch/tools/**'\n      reason: deeper glob first in config order\n    - glob: 'scratch/**'\n      reason: shallower glob second\n",
		"scratch/tools/evil.ts":   "export const evil = 1\n",
	})
	root := writeRepo(t, files)
	gitTrack(t, root, ".formwork", "notes.txt", "scratch/tools/evil.ts")

	// Record-based half (tree on disk): the walk prunes at dir "scratch",
	// which only 'scratch/**' matches — this is the reference attribution.
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 || !strings.Contains(sb.String(), "scan.ignore (scratch/**)") {
		t.Fatalf("record-based attribution must be the walk's shallowest-prune glob:\n%s", sb.String())
	}

	// Record-free half (tree deleted): the fallback must agree.
	if err := os.RemoveAll(filepath.Join(root, "scratch")); err != nil {
		t.Fatal(err)
	}
	sb.Reset()
	failed, err = meta.Lint(mustLoad(t, root), root, &sb, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// The census legitimately enumerates BOTH globs ("scan.ignore: <glob> —
	// ..."), so the negative assertion pins the finding format specifically
	// ("scan.ignore (<glob>)"), not the whole output.
	out := sb.String()
	if failed == 0 || !strings.Contains(out, "scan.ignore (scratch/**)") || strings.Contains(out, "scan.ignore (scratch/tools/**)") {
		t.Fatalf("record-free fallback must attribute the same glob the walk would (scratch/**), got:\n%s", out)
	}
}

// EMPTY-SCOPE IS AN ABSENCE CHECK, AND THE UNRESOLVED-GITIGNORE FALLBACK MAKES
// ABSENCES GO AWAY.
//
// ResolveGitIgnore's Unknown state prunes nothing and scans the whole tree. For
// a check that fires on the PRESENCE of something that is a harmless superset.
// empty-scope fails when a rule's scope matches NO files, so the very files the
// fallback declined to prune are what populate the scope it was about to
// condemn. Measured through the CLI before the refusal: healthy run reports
// `[empty-scope] FAIL`, exit 1; with the fallback in force, `[empty-scope] OK`
// and `formwork lint: 5/5 checks passed`, exit 0 — the sentence this check
// exists to prevent.
//
// The refusal is an ERROR, not a problem: a problem is a verdict about the
// corpus, and the point is that the corpus could not be determined. Same posture
// as scan-ignore-tracked's git failure above, and as cli/rulesfor.go on this
// exact state.
//
// The trigger here is "no git repository at all", which is the cheapest way to
// reach GitIgnoreUnknown and is the same state the CLI reaches through an
// ambient GIT_DIR — the guard is on the state, not on how it arose.
func TestEmptyScopeIsNotAnsweredByAnUnprunedGitignoreFallback(t *testing.T) {
	files := map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	}
	// NO git repository, so scan.gitignore cannot be resolved.
	root := writeRepo(t, files)
	var sb strings.Builder
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, false, false); err == nil {
		t.Fatalf("an unresolved scan.gitignore must not be judged by empty-scope:\n%s", sb.String())
	}
	if strings.Contains(sb.String(), "[empty-scope]") {
		t.Errorf("empty-scope emitted a verdict over a corpus that could not be determined:\n%s", sb.String())
	}
	if !strings.Contains(sb.String(), "escape hatches") {
		t.Errorf("the enumeration must still print on the error path (D1):\n%s", sb.String())
	}
}

// The control: with scan.gitignore NOT declared there is nothing to resolve, so
// empty-scope judges as it always has and lint needs no git at all. Without this
// the test above is satisfied by refusing every git-free lint, which would break
// every non-git consumer.
func TestEmptyScopeStillJudgesWithoutScanGitignore(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	var sb strings.Builder
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, false, false); err != nil {
		t.Fatalf("a corpus that declares no scan.gitignore needs no git: %v\n%s", err, sb.String())
	}
	if !strings.Contains(sb.String(), "empty-scope") {
		t.Errorf("empty-scope must still run:\n%s", sb.String())
	}
}
