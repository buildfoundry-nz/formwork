// unreadable_test.go — lint's verdict on an unreadable in-scope file (#30).
// Separate from lint_test.go, which the 750-line vendor cap bounds; same
// package.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
)

// unreadableRepo writes files, then chmods rel to 0o000 for the test's
// duration, and returns the root.
func unreadableRepo(t *testing.T, files map[string]string, rel string) string {
	t.Helper()
	skipUnlessChmodEnforced(t)
	root := writeRepo(t, files)
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	return root
}

func lintRootErr(t *testing.T, root string) (int, string, error) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := meta.Lint(cfg, root, &sb, false, false)
	return failed, sb.String(), err
}

const unreadableBaseFiles = ".formwork/formwork.yaml"

// #30: the same repo, differing only by a `prefilter:` the spec calls a PURE
// OPTIMIZATION, gave two different lint verdicts on the same unreadable
// in-scope file — 3/3 passed without it, exit 2 with it. Whether lint failed
// closed depended on whether some check happened to need the engine.
//
// Both rows must now be exit 2: an in-scope file lint cannot read is a rule
// that is not enforced, and that is never a pass. The two rows are one test on
// purpose — the defect was the DISAGREEMENT, so a test that pins only one side
// of it cannot fail for the reason the issue reports.
func TestLintFailsClosedOnUnreadableInScopeFileWhicheverChecksRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules string
	}{
		{
			// No allowlist, no marker, no prefilter: nothing consumes engine
			// findings, so lint never ran the engine and never read a file.
			name: "no check needs the engine",
			rules: "rules:\n" +
				"  - id: no-banana\n    type: forbidden-pattern\n" +
				"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n",
		},
		{
			// Identical but for the prefilter, which turns prefilter-load-bearing
			// on, which runs the engine, which hits the read error.
			name: "a prefilter pulls the engine in",
			rules: "rules:\n" +
				"  - id: no-banana\n    type: forbidden-pattern\n" +
				"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana, prefilter: banana}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := unreadableRepo(t, map[string]string{
				unreadableBaseFiles:                         "version: 1\n",
				".formwork/rules/r.yaml":                    tc.rules,
				".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
				".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
				"secret.txt": "banana\n",
			}, "secret.txt")

			_, out, err := lintRootErr(t, root)
			if err == nil {
				t.Fatalf("an unreadable in-scope file must be exit 2, got a pass:\n%s", out)
			}
			if !strings.Contains(err.Error(), "secret.txt") {
				t.Fatalf("the refusal must name the file it could not read: %v", err)
			}
			// D1: visibility must not regress on a degraded repo.
			if !strings.Contains(out, "escape hatches") {
				t.Fatalf("the escape-hatch enumeration must still print:\n%s", out)
			}
		})
	}
}

// The refusal is about files some rule GOVERNS, which is the claim it makes.
// An unreadable file no rule's scope selects changes no verdict lint reports,
// and failing on it would make lint refuse repos over files it never judges.
func TestLintIgnoresUnreadableFileNoRuleGoverns(t *testing.T) {
	root := unreadableRepo(t, map[string]string{
		unreadableBaseFiles:                         "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt":  "in scope\n",
		"secret.bin": "not governed by any rule\n",
	}, "secret.bin")

	failed, out, err := lintRootErr(t, root)
	if err != nil {
		t.Fatalf("an ungoverned unreadable file must not stop lint: %v\n%s", err, out)
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
}

// The refusal is a precondition, not a check: it must land before any verdict
// that draws on file CONTENT, and after the path-only checks, whose answers are
// complete without reading anything. Pinning the order stops a later edit from
// moving it behind exemption-hygiene, where a read error would be reported as
// that check's problem rather than as a refusal to judge.
func TestLintPathOnlyChecksStillReportBeforeTheUnreadableRefusal(t *testing.T) {
	root := unreadableRepo(t, map[string]string{
		unreadableBaseFiles: "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n",
		"secret.txt": "banana\n",
	}, "secret.txt")

	_, out, err := lintRootErr(t, root)
	if err == nil {
		t.Fatalf("expected exit 2:\n%s", out)
	}
	// fixture-coverage has real problems to report here (no fixtures at all)
	// and needs no file content to find them.
	if !strings.Contains(out, "[fixture-coverage] FAIL") {
		t.Fatalf("a path-only check must still report:\n%s", out)
	}
	if strings.Contains(out, "[exemption-hygiene]") {
		t.Fatalf("a content-reading check must not report a verdict over a tree lint could not read:\n%s", out)
	}
}
