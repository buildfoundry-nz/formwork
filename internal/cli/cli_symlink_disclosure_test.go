// cli_symlink_disclosure_test.go — #309: the walk's symlink skip has to reach
// `check`, not only `lint`.
//
// #143 gave the walk three options for a path it cannot scan — scan it, refuse
// it loudly, or skip it and census it — and chose the third for a link whose
// own name reads as nothing a toolchain compiles. That choice is only honest
// while the census is reachable from the command that enforces. It was not:
// enumerateEscapeHatches is the sole reader of Ignored{By: SourceSymlink} and
// `formwork lint` is its sole caller, so a rule scoping an extension absent
// from scan.go's sourceExts — .cs, .tf, .yaml, and every other config/IaC
// format — over an in-scope symlink passed `check` at exit 0 with every
// vacuity indicator empty: `rules_matching_no_files: []`, `rules_not_run: []`,
// `prune_channels: []`. The installed pre-commit shim runs `check`, and the
// documented CI recipe runs `check --lane ci --format github`; neither runs
// lint.
//
// THE EXIT CODE IS DELIBERATELY UNCHANGED, and the control below pins that.
// The walk never follows a symlink in any mode, that contract is written into
// internal/scan and internal/cli/fileset.go and pinned by
// TestCheckStagedSourceSymlinkInsideAnIgnoredTreeIsNotRefused, and #309 asks
// for the record to reach check's renderers — disclosure — not for a new
// refusal. A run that discloses what it did not look at is the fix; a run that
// starts failing over a config alias is a different change with different
// consequences.
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unfollowedLinkRepo is #309's filed corpus: one rule scoping three extensions
// the walk does not treat as source, one clean regular file so the run is not
// rescued by `rules_matching_no_files` naming the rule, and one in-scope
// symlink whose target — outside the tree, so nothing else enumerates it —
// holds the pattern.
func unfollowedLinkRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-secret\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**/*.cs', '**/*.tf', '**/*.yaml']}\n    params: {pattern: SECRET_KEY}\n")
	mustWrite(t, filepath.Join(root, "src", "Ok.cs"), "var ok = 1;\n")

	outside := t.TempDir()
	target := filepath.Join(outside, "Program.cs")
	mustWrite(t, target, "var x = SECRET_KEY;\n")
	if err := os.Symlink(target, filepath.Join(root, "src", "Program.cs")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	return root
}

// TestCheckDisclosesTheSymlinkItDeclinedToFollow — all three renderers, one
// test, because the defect was not that some renderer dropped the line: it was
// that the record never reached any of them. A per-format test would pass on a
// fix wired into one.
func TestCheckDisclosesTheSymlinkItDeclinedToFollow(t *testing.T) {
	root := unfollowedLinkRepo(t)
	const path = "src/Program.cs"

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — this change is disclosure, not a new refusal\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "symlink not followed: "+path) {
		t.Errorf("the human renderer must name the path the walk declined to follow:\n%s", out)
	}
	// The consequence, not just the fact: an operator who reads "a link was
	// skipped" and not "nothing under it was scanned" has no reason to look.
	if !strings.Contains(out, "nothing under it scanned") {
		t.Errorf("the human renderer must say what went unscanned:\n%s", out)
	}

	code, gh, errOut := runCLI(t, "check", "-C", root, "-format", "github")
	if code != 0 {
		t.Fatalf("github exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, gh, errOut)
	}
	// Prefix included: an annotation is what a reviewer sees on the PR, and a
	// line emitted outside the workflow-command form is invisible there.
	if !strings.Contains(gh, "::notice::formwork: scan: symlink not followed: "+path) {
		t.Errorf("the github renderer must annotate the skip:\n%s", gh)
	}

	code, js, errOut := runCLI(t, "check", "-C", root, "-format", "json")
	if code != 0 {
		t.Fatalf("json exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, js, errOut)
	}
	var rep struct {
		Scan struct {
			Unfollowed []string `json:"unfollowed_symlinks"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js), &rep); err != nil {
		t.Fatalf("%v\n%s", err, js)
	}
	// A structured field rather than prose, for the same reason rules_not_run
	// carries the rule id beside the rendered reason: a machine deciding
	// whether this run's coverage is complete must not have to parse a
	// sentence.
	if len(rep.Scan.Unfollowed) != 1 || rep.Scan.Unfollowed[0] != path {
		t.Errorf("the json renderer must carry the skipped path, got %v\n%s", rep.Scan.Unfollowed, js)
	}
}

// TestCheckDisclosesNoSymlinkWhenItFollowedEverything is the control. Without
// it the assertions above are satisfied by a renderer that prints the line
// unconditionally, which would put a false coverage warning on every clean
// repository and train every reader to skip the block — the failure mode the
// escape-hatch census exists to avoid, reintroduced by its own fix.
func TestCheckDisclosesNoSymlinkWhenItFollowedEverything(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-secret\n    type: forbidden-pattern\n"+
			"    scope: {include: ['**/*.cs']}\n    params: {pattern: SECRET_KEY}\n")
	// The same in-scope path as a REGULAR file: nothing was declined.
	mustWrite(t, filepath.Join(root, "src", "Program.cs"), "var ok = 1;\n")

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "symlink not followed") {
		t.Errorf("a run that declined nothing must disclose nothing:\n%s", out)
	}

	_, js, _ := runCLI(t, "check", "-C", root, "-format", "json")
	var rep struct {
		Scan struct {
			Unfollowed []string `json:"unfollowed_symlinks"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js), &rep); err != nil {
		t.Fatalf("%v\n%s", err, js)
	}
	// Present and empty, never absent: a consumer distinguishing "none" from
	// "this build does not report it" should not have to, which is why toJSON
	// builds every slice non-nil.
	if rep.Scan.Unfollowed == nil {
		t.Errorf("unfollowed_symlinks must encode as [] rather than null:\n%s", js)
	}
	if len(rep.Scan.Unfollowed) != 0 {
		t.Errorf("got %v, want none\n%s", rep.Scan.Unfollowed, js)
	}
}
