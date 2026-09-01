// orphan_test.go — a fixture directory matching no rule id is unreachable by
// the per-rule discovery loop: no run ever opens it, so the proof tree it
// holds is dead weight that reads as green (#58). These tests pin the
// fail-closed counterpart of the loud unrecognized-subdir error inside that
// loop: an orphan at the fixtures root is a FAIL verdict naming every
// orphan, sibling rules still run, and --rule scoping must not manufacture
// false orphans. The check stays; the blackout (aborting the whole run
// before any rule executes) does not.
package fixturetest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

// fullIDs is the caller-side contract of Run's allRuleIDs param: the FULL
// corpus id set, collected before any --rule scoping narrows cfg.
func fullIDs(cfg *config.Config) []string {
	ids := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		ids = append(ids, r.ID)
	}
	return ids
}

func loadRepo(t *testing.T, files map[string]string) (*config.Config, string) {
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
	return cfg, root
}

func TestOrphanFixtureDirIsError(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt":   "a banana here want: fruit-free\n",
		".formwork/fixtures/no-such-rule/fire-1/f.txt": "a banana here\n",
	})
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("orphan must not abort the run (err is exit 2; this is a FAIL verdict): %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "[fruit-free] OK") {
		t.Fatalf("live sibling rule must still run:\n%s", out)
	}
	if !strings.Contains(out, "[no-such-rule] FAIL") {
		t.Fatalf("orphan must be named as a FAIL verdict:\n%s", out)
	}
	if !strings.Contains(out, "no rule id") {
		t.Fatalf("FAIL must say the dir matches no rule id:\n%s", out)
	}
	if failed < 1 {
		t.Fatalf("failed=%d, orphan must count as a failure\n%s", failed, out)
	}
}

func TestOrphanErrorListsAllOrphansSorted(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/zz-orphan/fire-1/f.txt": "x\n",
		".formwork/fixtures/aa-orphan/pass-1/f.txt": "x\n",
	})
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("orphans are FAIL verdicts, not a run abort: %v", err)
	}
	out := sb.String()
	if failed < 2 {
		t.Fatalf("failed=%d, each orphan must count\n%s", failed, out)
	}
	ai, zi := strings.Index(out, "aa-orphan"), strings.Index(out, "zz-orphan")
	if ai < 0 || zi < 0 {
		t.Fatalf("verdicts must name every orphan (not stop at the first):\n%s", out)
	}
	if ai > zi {
		t.Fatalf("orphans must be listed sorted for deterministic output:\n%s", out)
	}
	if !strings.Contains(out, "[aa-orphan] FAIL") || !strings.Contains(out, "[zz-orphan] FAIL") {
		t.Fatalf("each orphan must be a FAIL verdict:\n%s", out)
	}
}

// TestOrphanDoesNotPreventSiblingFixtureExecution is the load-bearing half
// of the blackout fix: an unreachable fixture dir must not stop a different
// rule's fire/pass trees from being evaluated. fruit-free's fire fixture is
// written to FAIL (want-marker on a banana-free line) so a skip would not
// look like a pass — we have to see the fixture problem itself.
func TestOrphanDoesNotPreventSiblingFixtureExecution(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt":   "clean want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt":   "all clean\n",
		".formwork/fixtures/no-such-rule/fire-1/f.txt": "x\n",
	})
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("orphan must not abort sibling fixture execution: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "[fruit-free] FAIL") {
		t.Fatalf("fruit-free's fixtures must still execute (fire-1 should FAIL — no banana on the want line):\n%s", out)
	}
	if !strings.Contains(out, "fire-1:") {
		t.Fatalf("fruit-free's fire-1 must have been evaluated, not skipped:\n%s", out)
	}
	if !strings.Contains(out, "[no-such-rule] FAIL") {
		t.Fatalf("orphan must still be reported:\n%s", out)
	}
	if failed < 2 {
		t.Fatalf("failed=%d, want at least 2 (live-rule fixture failure + orphan)\n%s", failed, out)
	}
}

// TestScopedRunStillReportsTrueOrphans pins the --rule half of the new
// contract: sibling LIVE rules stay non-orphans (#58), but a dir matching
// no rule in the full corpus is still a FAIL finding, and the selected
// rule still runs.
func TestScopedRunStillReportsTrueOrphans(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt":   "a banana here want: fruit-free\n",
		".formwork/fixtures/has-anchor/pass-1/a.md":    "the anchor\n",
		".formwork/fixtures/no-such-rule/fire-1/f.txt": "x\n",
	})
	all := fullIDs(cfg)
	scoped := *cfg
	scoped.Rules = nil
	for _, r := range cfg.Rules {
		if r.ID == "fruit-free" {
			scoped.Rules = append(scoped.Rules, r)
		}
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(&scoped, all, root, 2, &sb)
	if err != nil {
		t.Fatalf("true orphan must not abort a scoped run: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "[fruit-free] OK") {
		t.Fatalf("selected rule must still run:\n%s", out)
	}
	if strings.Contains(out, "[has-anchor]") {
		t.Fatalf("sibling LIVE rule must not execute under --rule:\n%s", out)
	}
	if !strings.Contains(out, "[no-such-rule] FAIL") {
		t.Fatalf("dir matching no rule in the full corpus is still an orphan finding:\n%s", out)
	}
	if failed < 1 {
		t.Fatalf("failed=%d, true orphan must count\n%s", failed, out)
	}
}

// TestRuleScopedRunToleratesSiblingFixtures is the --rule regression guard:
// runTest scopes cfg to one rule BEFORE calling Run, so the orphan check must
// compare against the full corpus id set — otherwise every sibling rule's
// fixture tree reads as an orphan under --rule.
func TestRuleScopedRunToleratesSiblingFixtures(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "a banana here want: fruit-free\n",
		".formwork/fixtures/has-anchor/pass-1/a.md":  "the anchor\n",
	})
	all := fullIDs(cfg)
	scoped := *cfg
	scoped.Rules = nil
	for _, r := range cfg.Rules {
		if r.ID == "fruit-free" {
			scoped.Rules = append(scoped.Rules, r)
		}
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(&scoped, all, root, 2, &sb)
	if err != nil {
		t.Fatalf("scoped run read a sibling rule's fixtures as orphans: %v", err)
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
	if strings.Contains(sb.String(), "has-anchor") {
		t.Fatalf("scoped run must not execute the sibling rule's fixtures:\n%s", sb.String())
	}
}

func TestNonDirEntryAtFixturesRootIgnored(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "a banana here want: fruit-free\n",
		".formwork/fixtures/README.md":               "notes about fixtures\n",
	})
	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil {
		t.Fatalf("a non-dir entry at the fixtures root must be ignored (dirs-only, mirroring the per-rule loop): %v", err)
	}
}

// TestSymlinkedDirAtFixturesRootIsError closes the orphan walk's own blind
// spot (fail-open review of #58): DirEntry.IsDir is lstat-based, so a
// symlink-to-dir is "not a dir" to the walk — with an unknown name it would
// evade the orphan check AND execution, a tree that neither runs nor errors.
// Refusal is the loud move (the #54 convention): symlinks at the fixtures
// root are errors regardless of target or name.
func TestSymlinkedDirAtFixturesRootIsError(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/fruit-free/fire-1/f.txt": "a banana here want: fruit-free\n",
	})
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "fire-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "fire-1", "f.txt"), []byte("a banana here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".formwork", "fixtures", "linked-orphan")); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	_, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err == nil {
		t.Fatal("a symlinked dir at the fixtures root must be refused — unfollowed, it is a tree that neither runs nor errors")
	}
	if !strings.Contains(err.Error(), "linked-orphan") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("refusal must name the entry and say it is a symlink, got: %v", err)
	}
}

// TestSymlinkedRuleDirIsAlsoRefused pins the policy for the KNOWN-name case:
// before this refusal, a symlinked rule dir silently ran via os.ReadDir's
// path-follow while an unknown-name symlink silently vanished — the same
// object loud or invisible depending on its name. Refusing both makes the
// policy a decision instead of an lstat accident.
func TestSymlinkedRuleDirIsAlsoRefused(t *testing.T) {
	cfg, root := loadRepo(t, nil)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "fire-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "fire-1", "f.txt"), []byte("a banana here want: fruit-free\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".formwork", "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".formwork", "fixtures", "fruit-free")); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	_, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err == nil {
		t.Fatal("a symlinked rule dir must be refused, not silently followed")
	}
	if !strings.Contains(err.Error(), "fruit-free") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("refusal must name the entry and say it is a symlink, got: %v", err)
	}
}

func TestMissingFixturesRootIsNotAnOrphanError(t *testing.T) {
	cfg, root := loadRepo(t, nil) // no fixtures dir at all
	if err := os.RemoveAll(filepath.Join(root, ".formwork", "fixtures")); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("a corpus with no fixtures root is legal (rules SKIP): %v", err)
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
}
