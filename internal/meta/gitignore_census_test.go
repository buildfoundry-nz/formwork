// gitignore_census_test.go — scan.gitignore's escape-hatch census line and its
// fail-closed degradation (#100). Same package as lint_test.go; shares
// writeRepo/lint/lintRule and scanignore_tracked_test.go's git helpers.
package meta_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/meta"
)

const gitignoreCfg = "version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n"

func TestLintEnumeratesGitignorePruneWithCounts(t *testing.T) {
	_, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml":                   gitignoreCfg,
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		".gitignore":    "build/\nscratch.txt\n",
		"build/out.txt": "x\n",
		"scratch.txt":   "x\n",
		"notes.txt":     "in scope\n",
	}, ".formwork", ".gitignore", "notes.txt")
	// build/ prunes as a dir without descending; scratch.txt as a file. The
	// shape mirrors scan.ignore's line so a reader learns one format, and the
	// reason is echoed so the census answers "why" without opening the config.
	want := "scan.gitignore: 1 dirs pruned (subtrees not scanned), 1 files ignored (git already refuses these)"
	if !strings.Contains(out, want) {
		t.Fatalf("lint output missing %q\n%s", want, out)
	}
}

// A declared channel that currently hides nothing must still appear, for the
// same reason a dead scan.ignore glob does: "0" is an answer, and an
// unenumerated channel is one nobody audits.
func TestLintEnumeratesGitignoreWithNothingIgnored(t *testing.T) {
	_, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml":                   gitignoreCfg,
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	}, ".formwork", "notes.txt")
	want := "scan.gitignore: 0 matches (git already refuses these)"
	if !strings.Contains(out, want) {
		t.Fatalf("lint output missing %q\n%s", want, out)
	}
}

// THE fail-closed test. When git cannot answer, the census must say so in
// words distinct from "nothing was ignored" — reporting a clean zero for a
// question that was never answered is this repo's signature defect.
//
// THE SUPERSET IS ONLY SAFE FOR CHECKS THAT FIRE ON PRESENCE, which this comment
// used to state without the qualifier ("no rule can pass that would otherwise
// fail"). Nothing being pruned means the scan grows, which can only add findings
// for a rule that fires on a match — and can SATISFY a check that fires on
// absence. `lint`'s own empty-scope is one, so this state is now an error there
// rather than a judged verdict (lint.go).
//
// THE CENSUS STILL PRINTS, WHICH IS WHAT THIS TEST IS FOR. The refusal is
// deliberately placed after enumerateEscapeHatches, so a degraded run still
// discloses what it could not determine before it stops — the D1 contract every
// other lint error path honours. So this now asserts on the output of a run that
// also returns an error, and calls Lint directly rather than through the `lint`
// helper, which t.Fatals on one.
func TestLintGitignoreCensusDistinguishesUnknownFromNone(t *testing.T) {
	// writeRepo makes a plain directory — deliberately NOT a git repo here, so
	// the seam genuinely cannot answer.
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                   gitignoreCfg,
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	var sb strings.Builder
	if _, err := meta.Lint(mustLoad(t, root), root, &sb, false, false); err == nil {
		t.Fatalf("an unresolved scan.gitignore must not be judged:\n%s", sb.String())
	}
	out := sb.String()
	if !strings.Contains(out, "scan.gitignore: could not determine") {
		t.Fatalf("census must say the question went unanswered:\n%s", out)
	}
	if strings.Contains(out, "scan.gitignore: 0 matches") {
		t.Fatalf("census reported a clean zero for an unanswered question:\n%s", out)
	}
	if !strings.Contains(out, "nothing pruned") {
		t.Fatalf("census must state the fail-closed consequence:\n%s", out)
	}
}

// The scan.gitignore twin of TestLintAllowlistEntryUnderScanIgnoreSaysSoNot-
// DoesNotExist. An allowlist entry naming a path the walk pruned is inert
// either way, but the two channels are cured in different files — narrow a
// glob in .formwork/ vs. edit .gitignore — so a reader told the wrong one
// learns nothing. "does not exist" would also be a lie: the file is on disk.
func TestLintAllowlistEntryUnderGitignoreSaysSoNotDoesNotExist(t *testing.T) {
	failed, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml":                   gitignoreCfg,
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "build/gen.txt\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		".gitignore":    "build/\n",
		"build/gen.txt": "banana\n", // on disk, pruned by git's own refusal
		"hit.txt":       "banana\n", // keeps the allowlist otherwise live
	}, ".formwork", ".gitignore", "hit.txt")
	if failed == 0 {
		t.Fatalf("want hygiene failure: allowlisted path can never fire\n%s", out)
	}
	if !strings.Contains(out, "build/gen.txt hidden by scan.gitignore (.gitignore:1:build/)") {
		t.Fatalf("want the gitignore channel named with its responsible rule, got:\n%s", out)
	}
	if strings.Contains(out, "build/gen.txt does not exist") {
		t.Fatalf("misleading does-not-exist diagnosis for an on-disk file:\n%s", out)
	}
	if strings.Contains(out, "build/gen.txt hidden by scan.ignore") {
		t.Fatalf("gitignore prune reported as the scan.ignore channel:\n%s", out)
	}
}

// The clean-repo contract: a repo that never declares the key gets no scan
// lines at all, so "escape hatches: none" still holds.
func TestLintCensusUnaffectedWhenGitignoreUnset(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if strings.Contains(out, "scan.gitignore") {
		t.Fatalf("undeclared repo grew a scan.gitignore census line:\n%s", out)
	}
}
