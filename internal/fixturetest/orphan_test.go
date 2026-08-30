// orphan_test.go — a fixture directory matching no rule id is unreachable by
// the per-rule discovery loop: no run ever opens it, so the proof tree it
// holds is dead weight that reads as green (#58). These tests pin the
// fail-closed counterpart of the loud unrecognized-subdir error two lines
// into that loop: an orphan at the fixtures root is an error naming every
// orphan, and --rule scoping must not manufacture false orphans.
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
	_, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err == nil {
		t.Fatal("a fixture dir matching no rule id must be an error, not a silent pass")
	}
	if !strings.Contains(err.Error(), "no-such-rule") {
		t.Fatalf("error must name the orphan dir, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no rule id") {
		t.Fatalf("error must say the dir matches no rule id, got: %v", err)
	}
}

func TestOrphanErrorListsAllOrphansSorted(t *testing.T) {
	cfg, root := loadRepo(t, map[string]string{
		".formwork/fixtures/zz-orphan/fire-1/f.txt": "x\n",
		".formwork/fixtures/aa-orphan/pass-1/f.txt": "x\n",
	})
	var sb strings.Builder
	_, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err == nil {
		t.Fatal("expected an orphan error")
	}
	msg := err.Error()
	ai, zi := strings.Index(msg, "aa-orphan"), strings.Index(msg, "zz-orphan")
	if ai < 0 || zi < 0 {
		t.Fatalf("error must list every orphan in one message (not stop at the first), got: %v", err)
	}
	if ai > zi {
		t.Fatalf("orphans must be listed sorted for deterministic output, got: %v", err)
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
