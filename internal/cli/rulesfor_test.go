package cli_test

// rules-for's end-to-end tests, split from introspect_test.go when the
// 750-line vendor cap fired (self-hosted file-size-vendor-cap). Shares
// runCLI (cli_test.go) and writeFile/explainRepo (introspect_test.go).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesForListsGoverningRules(t *testing.T) {
	code, out, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "src/notes.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, needed := range []string{"no-todo-markers", "error", "Resolve the item"} {
		if !strings.Contains(out, needed) {
			t.Fatalf("missing %q:\n%s", needed, out)
		}
	}
	if strings.Contains(out, "readme-mentions-formwork") {
		t.Fatalf("README-scoped rule must not govern a txt file:\n%s", out)
	}
}

func TestRulesForRespectsExceptPaths(t *testing.T) {
	// src/clean.txt is carved out via except.paths — the display must agree
	// with the verdict Applies() would give, or guidance lies.
	code, out, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "src/clean.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if strings.Contains(out, "no-todo-markers") {
		t.Fatalf("except.paths carve-out ignored:\n%s", out)
	}
	if !strings.Contains(out, "none") {
		t.Fatalf("zero governing rules must be explicit, not silent:\n%s", out)
	}
}

func TestRulesForNonexistentPathIsNotAnError(t *testing.T) {
	// Scope is a glob question, not a filesystem question: guidance is asked
	// about files the agent is ABOUT to create.
	code, out, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "src/ghost.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-todo-markers") {
		t.Fatalf("glob-governed future file must list its rules:\n%s", out)
	}
}

func TestRulesForZeroRulesIsLegitimate(t *testing.T) {
	code, out, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "image.png")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "none") {
		t.Fatalf("zero rules must say so explicitly:\n%s", out)
	}
}

func TestRulesForAbsolutePathUnderRootRelativized(t *testing.T) {
	absRoot, err := filepath.Abs(filepath.Join("testdata", "toyrepo"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI(t, "rules-for", "-C", absRoot, filepath.Join(absRoot, "src", "notes.txt"))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-todo-markers") {
		t.Fatalf("absolute path under root must relativize and govern:\n%s", out)
	}
}

func TestRulesForPathOutsideRootExits2(t *testing.T) {
	// A wrong-frame path must be LOUD: returning empty would read as "no
	// rules govern this" — a guidance fail-open.
	code, _, errOut := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "../outside.txt")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(errOut, "outside") {
		t.Fatalf("frame error must say the path is outside the root: %s", errOut)
	}
}

func TestRulesForNoPathsExits2(t *testing.T) {
	code, _, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"))
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRulesForJSONGroupsByPath(t *testing.T) {
	code, out, _ := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), "-format", "json", "README.md", "src/notes.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got []struct {
		Path  string `json:"path"`
		Rules []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Cure     string `json:"cure"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].Path != "README.md" || got[1].Path != "src/notes.txt" {
		t.Fatalf("paths not in argv order: %+v", got)
	}
	if len(got[0].Rules) != 1 || got[0].Rules[0].ID != "readme-mentions-formwork" {
		t.Fatalf("README rules: %+v", got[0].Rules)
	}
	if len(got[1].Rules) != 1 || got[1].Rules[0].ID != "no-todo-markers" || got[1].Rules[0].Cure == "" {
		t.Fatalf("notes rules: %+v", got[1].Rules)
	}
}

func TestRulesForDirectoryQueryExits2(t *testing.T) {
	// A directory query answered "(none)" at exit 0 is the trusted-empty
	// fail-open this command exists to avoid: every file inside may be
	// governed. Both explicit (trailing slash) and implicit (existing dir)
	// shapes must refuse loudly (fail-open review finding 1).
	for _, arg := range []string{"src/", "src"} {
		code, out, errOut := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), arg)
		if code != 2 {
			t.Fatalf("%q: exit %d, want 2 (out: %s)", arg, code, out)
		}
		if !strings.Contains(errOut, "director") {
			t.Fatalf("%q: error must say it names a directory: %s", arg, errOut)
		}
	}
}

func TestRulesForScanIgnoredPathIsLoud(t *testing.T) {
	// scan.ignore is the widest exemption channel in the engine; rules-for
	// listing rules for a path the walk prunes would assert enforcement that
	// check never performs (fail-open review finding 2). The answer must name
	// the glob and reason, never a bare governing-rules list.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "vendor/**"
      reason: "vendored, not ours"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, needed := range []string{"vendor/**", "vendored, not ours", "not scanned"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(needed)) {
			t.Fatalf("ignored path answer missing %q:\n%s", needed, out)
		}
	}
	if strings.Contains(out, "no-todo") {
		t.Fatalf("must not list rules as governing an unscanned path:\n%s", out)
	}
}

func TestRulesForBuiltinSkipIsLoud(t *testing.T) {
	root := explainRepo(t)
	code, out, _ := runCLI(t, "rules-for", "-C", root, ".formwork/rules/main.yaml", ".git/hooks/x.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("built-in-skip paths must be loud:\n%s", out)
	}
	if strings.Contains(out, "guard-pool") {
		t.Fatalf("must not list rules under a built-in skip:\n%s", out)
	}
}

func TestRulesForScanIgnoredJSONIsStructural(t *testing.T) {
	// A JSON consumer must see not-scanned structurally, not by parsing prose.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "vendor/**"
      reason: "vendored, not ours"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "-format", "json", "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got []struct {
		Path       string `json:"path"`
		Rules      []any  `json:"rules"`
		NotScanned *struct {
			By     string `json:"by"`
			Glob   string `json:"glob"`
			Reason string `json:"reason"`
		} `json:"not_scanned"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].NotScanned == nil || got[0].NotScanned.Glob != "vendor/**" || got[0].NotScanned.Reason != "vendored, not ours" || len(got[0].Rules) != 0 {
		t.Fatalf("not_scanned must be structural with empty rules: %+v", got[0])
	}
}

// --- /code-review findings (PR #119) ---

func TestRulesForNotScannedNamesExternalToolReach(t *testing.T) {
	// Finding 2: scan.ignore prunes the WALK, but command/git-diff rules exec
	// external tools that re-scan on their own — "nothing enforces here" is
	// false while such rules exist. The annotation must name them.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "vendor/**"
      reason: "vendored, not ours"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: whole-tree-sweep
    type: command
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      cmd: ["true"]
`)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "whole-tree-sweep") || !strings.Contains(strings.ToLower(out), "external") {
		t.Fatalf("NOT SCANNED must disclose external-tool rules that may still reach the path:\n%s", out)
	}
}

func TestRulesForCanonicalizesCaseOnInsensitiveFS(t *testing.T) {
	// Finding 3: on a case-insensitive filesystem, a case-divergent query
	// opens the real file but judges the wrong frame — governed files answer
	// "(none)" and ignored files answer with rules. Canonicalize to on-disk
	// spelling so the frame matches the walk's. Skipped where the fs is
	// case-sensitive: there, SRC/notes.txt genuinely IS a different path.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["src/**/*.txt"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "src/notes.txt", "hi\n")
	if _, err := os.Stat(filepath.Join(root, "SRC", "notes.txt")); err != nil {
		t.Skip("filesystem is case-sensitive; canonicalization deliberately does not apply")
	}
	code, out, _ := runCLI(t, "rules-for", "-C", root, "SRC/Notes.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-todo") {
		t.Fatalf("case-divergent query for an existing governed file must canonicalize and list rules:\n%s", out)
	}
	if !strings.Contains(out, "src/notes.txt") {
		t.Fatalf("answer must be framed at the on-disk spelling:\n%s", out)
	}
}

func TestRulesForAllowlistedPathShowsSuppression(t *testing.T) {
	// Finding 4: an allowlisted path IS governed but every finding there is
	// suppressed — display must carry the suppression like the engine does
	// (finding.SuppressedBy), not report bare governance.
	root := explainRepo(t)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "internal/old/db.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "guard-pool") {
		t.Fatalf("allowlisted path is still governed; rule must be listed:\n%s", out)
	}
	if !strings.Contains(out, "suppressed") || !strings.Contains(out, "allowlists/pool.txt") {
		t.Fatalf("suppression must be disclosed with the allowlist file:\n%s", out)
	}
	// JSON: structural field, matching the engine's SuppressedBy shape.
	code, jout, _ := runCLI(t, "rules-for", "-C", root, "-format", "json", "internal/old/db.go")
	if code != 0 || !strings.Contains(jout, `"suppressed_by"`) {
		t.Fatalf("JSON must carry suppressed_by (exit %d):\n%s", code, jout)
	}
}

func TestRulesForSymlinkPathIsLoud(t *testing.T) {
	// Finding 5: the walk's third skip channel — a committed non-source
	// symlink is silently skipped, so listing rules for its path asserts
	// enforcement that never happens.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: readme-required
    type: required-pattern
    severity: error
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'formwork'
`)
	writeFile(t, root, "README.md", "formwork\n")
	if err := os.Symlink("README.md", filepath.Join(root, "alias.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	code, out, _ := runCLI(t, "rules-for", "-C", root, "alias.md")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("symlink path must be loud, not listed as governed:\n%s", out)
	}
	if strings.Contains(out, "readme-required") {
		t.Fatalf("must not list rules for a path the walk skips:\n%s", out)
	}
}

func TestRulesForAttributionMatchesWalkForNestedBuiltin(t *testing.T) {
	// Finding 6: the walk prunes at the SHALLOWEST trigger. A .git nested
	// under a scan.ignore'd tree is never reached by the walk — the operator
	// glob did the hiding and must get the attribution, exactly as lint
	// attributes it.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "third_party/**"
      reason: "vendored validating target"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: r1
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'x'
`)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "third_party/dep/.git/config")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "third_party/**") || !strings.Contains(out, "vendored validating target") {
		t.Fatalf("shallowest trigger is the operator glob; attribution must name it:\n%s", out)
	}
	if strings.Contains(out, "built-in") {
		t.Fatalf("builtin-skip attribution is wrong here — the walk pruned at third_party/:\n%s", out)
	}
	// And the plain shape still attributes builtin.
	code, out2, _ := runCLI(t, "rules-for", "-C", root, ".git/config")
	if code != 0 || !strings.Contains(out2, "built-in") {
		t.Fatalf("root-level .git must still attribute builtin (exit %d):\n%s", code, out2)
	}
}

func TestRulesForRootQueryNamesDirectory(t *testing.T) {
	// Finding 10: '.' IS the root — the error must give the accurate
	// diagnosis (a directory), not claim it is outside the repository.
	code, _, errOut := runCLI(t, "rules-for", "-C", filepath.Join("testdata", "toyrepo"), ".")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "director") || strings.Contains(errOut, "outside") {
		t.Fatalf("root query must be diagnosed as a directory, not outside-root: %s", errOut)
	}
}

// --- third review pass findings ---

func TestRulesForSymlinkedAncestorIsLoud(t *testing.T) {
	// Finding 1a: the walk stops at the first non-regular ANCESTOR — it
	// skips the symlinked dir entry and never descends — so a path beneath
	// one must answer NOT SCANNED, never a governing-rules list.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: md-guard
    type: required-pattern
    severity: error
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'formwork'
`)
	writeFile(t, root, "real/doc.md", "formwork\n")
	if err := os.Symlink("real", filepath.Join(root, "ext")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	code, out, _ := runCLI(t, "rules-for", "-C", root, "ext/bad.md")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("path under a symlinked ancestor must be NOT SCANNED:\n%s", out)
	}
	if strings.Contains(out, "md-guard") {
		t.Fatalf("must not assert governance the walk never performs:\n%s", out)
	}
}

func TestRulesForRegularFileAncestorIsLoudError(t *testing.T) {
	// Finding 1b: a path beneath a regular FILE can never exist as a
	// scanned file — a governing-rules answer for it is fiction; refuse.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: md-guard
    type: required-pattern
    severity: error
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'formwork'
`)
	writeFile(t, root, "README.md", "formwork\n")
	code, _, errOut := runCLI(t, "rules-for", "-C", root, "README.md/x.md")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(errOut, "README.md") {
		t.Fatalf("refusal must name the file ancestor: %s", errOut)
	}
}

func TestRulesForRegularFileNamedGitIsGoverned(t *testing.T) {
	// Finding 2 (e2e half): skipDirs prunes DIRECTORIES only — the walk
	// scans a regular file named .git (a linked-worktree gitdir pointer is
	// exactly this shape) and check enforces on it, so rules-for must list
	// its governing rules, not claim builtin-skip.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["sub/**"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "sub/.git", "gitdir: ../real\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "sub/.git")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-todo") || strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("a regular FILE named .git is scanned and governed:\n%s", out)
	}
}

func TestRulesForUnstatableAncestorIsLoud(t *testing.T) {
	// Finding 3: a stat failure other than ENOENT means the path cannot be
	// classified — a confident governed-rules answer there is a fail-open.
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bits do not bind")
	}
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "locked/x.txt", "hi\n")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	code, _, errOut := runCLI(t, "rules-for", "-C", root, "locked/x.txt")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(errOut, "locked/x.txt") {
		t.Fatalf("classification failure must name the path: %s", errOut)
	}
}

func TestRulesForCaseDivergentAbsolutePath(t *testing.T) {
	// Finding 4: an absolute path in a case-divergent spelling opens the
	// real file on a case-insensitive fs — it is NOT outside the root, and
	// must canonicalize and answer like its relative twin does.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["src/**"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "src/notes.txt", "hi\n")
	if _, err := os.Stat(filepath.Join(root, "SRC", "notes.txt")); err != nil {
		t.Skip("filesystem is case-sensitive; canonicalization deliberately does not apply")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	divergent := strings.ToUpper(absRoot) + "/src/notes.txt"
	code, out, errOut := runCLI(t, "rules-for", "-C", absRoot, divergent)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "no-todo") {
		t.Fatalf("case-divergent absolute path must canonicalize and govern:\n%s", out)
	}
}

func TestRulesForSymlinkToDirectoryIsNotScanned(t *testing.T) {
	// Finding 5: the walk treats a symlink-to-directory as a non-regular
	// skip, not as a directory — the diagnosis must match the walk.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "real/x.txt", "hi\n")
	if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	code, out, _ := runCLI(t, "rules-for", "-C", root, "alias")
	if code != 0 {
		t.Fatalf("exit %d, want 0 with NOT SCANNED:\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("symlink-to-dir is the walk's non-regular skip:\n%s", out)
	}
}
