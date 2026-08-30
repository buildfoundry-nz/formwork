package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// scope.min_files (#23) is the arming end of #160's disclosure. #160 made a rule
// whose scope matched nothing DISCLOSED but still a pass — deliberately, since a
// rule scoped to a path the repo has not created yet is not a defect. A finished
// port needs the other end: a way to say the corpus is real, and a run where it
// vanished is a failure. The floor is opt-in per rule, default 0, copying
// set-relation's min_count.
//
// Every row below is the same two-file tree seen through a different floor, so a
// row that reads green is always the config's doing and never the fixture's.

// minFilesRepo writes a repo with two .go files and one rule over `include`
// carrying the given scope body.
func minFilesRepo(t *testing.T, scope string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: go-corpus\n    type: forbidden-pattern\n    scope: "+scope+"\n"+
			"    cure: 'Keep the Go corpus intact.'\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	mustWrite(t, filepath.Join(root, "b.go"), "package b\n")
	return root
}

// The control, and the load-bearing half of this whole feature: the identical
// tree and an identical rule WITHOUT a floor keeps today's verdict — a rule that
// matches nothing is named in the scan summary and the run still exits 0.
func TestCheckWithoutAFloorIsUnchanged(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.dart']}")
	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — an undeclared floor must not change any verdict\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[go-corpus] OK") {
		t.Errorf("want the rule still OK:\n%s", out)
	}
	if !strings.Contains(out, "go-corpus: scope matched no files") {
		t.Errorf("#160's disclosure must survive:\n%s", out)
	}
	if strings.Contains(out, "min_files") {
		t.Errorf("a rule with no floor must not mention one:\n%s", out)
	}
}

// The feature: the floor is armed, the corpus is short, and the run FAILS.
func TestCheckArmedFloorShortfallExits1(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 5}")
	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — an armed floor the tree does not meet is a violation\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[go-corpus] FAIL") {
		t.Fatalf("want the rule reported FAIL:\n%s", out)
	}
	// The shortfall is the whole cure surface: both numbers, and the key to edit.
	for _, want := range []string{"scope matched 2 file(s)", "floor of 5", "scope.min_files"} {
		if !strings.Contains(out, want) {
			t.Errorf("output must contain %q:\n%s", want, out)
		}
	}
}

// A floor the tree MEETS is silent — the arming does not itself cost a verdict.
// Without this, a floor implemented as "always fail when declared" would pass
// the test above.
func TestCheckSatisfiedFloorIsSilent(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 2}")
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — 2 files meet a floor of 2\n%s", code, out)
	}
	if strings.Contains(out, "min_files") {
		t.Errorf("a satisfied floor must say nothing:\n%s", out)
	}
}

// The floor is a statement about the REPOSITORY, not about a changeset, so a
// --staged run must reach the same verdict as the whole-tree run one line above.
// The alternative — evaluating the floor over the staged set — would false-fail
// every armed rule on every commit, and skipping it under --staged would let the
// pre-commit hook pass what CI fails. #151's lesson is that two commands (here,
// two modes) giving opposite verdicts about identical state is the defect.
func TestCheckArmedFloorFiresUnderStaged(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 5}")
	gitInit(t, root)
	gitRun(t, root, "add", ".formwork", "a.go", "b.go")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 under --staged\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "floor of 5") {
		t.Errorf("the shortfall must be reported under --staged too:\n%s", out)
	}
}

// The repro the mode decision above got WRONG on its first cut (fix round 1).
// Untracking a corpus — `git rm --cached`, a .gitignore entry, files still on
// disk — is the commonest way a corpus vanishes, and it is exactly what the
// floor is armed against. Counting the raw walk let that commit pass the
// pre-commit shim at exit 0 while a fresh clone of the same commit failed, which
// is the local-vs-CI divergence the comment one screen up argues AGAINST.
//
// So in a file-set mode the floor is measured against the TRACKED tree — the
// same set the whole-tree invariants beside it already use, for the same reason.
func TestCheckArmedFloorFiresUnderStagedWhenTheCorpusIsUntracked(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 2}")
	mustWrite(t, filepath.Join(root, "README.md"), "docs\n")
	gitInit(t, root)
	// a.go and b.go stay OUT of the index: on disk, meeting the floor by file
	// count, and in no commit this run could produce.
	gitRun(t, root, "add", ".formwork", "README.md")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — an untracked corpus cannot satisfy a floor\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "scope matched 0 file(s)") || !strings.Contains(out, "floor of 2") {
		t.Errorf("the shortfall must count the tracked tree, not the walk:\n%s", out)
	}
}

// The other half of that decision, and the one that keeps it a trade rather than
// a blanket rule: in WHOLE-TREE mode the floor counts every file the walk
// produced, tracked or not. The set is the walk's, not git's. That is the same
// set the engine scanned, and restricting it would make `check` need git in a
// tree that has none.
func TestCheckWholeTreeFloorCountsUntrackedFiles(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 2}")
	gitInit(t, root)
	gitRun(t, root, "add", ".formwork") // a.go and b.go untracked, on disk

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the whole-tree floor counts the walk\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "min_files") {
		t.Errorf("no shortfall was owed:\n%s", out)
	}
}

// --skip-escapes drops a heavy rule because its CHECKER re-scans the tree. Its
// floor is glob matching over a file set already in hand, so the cost argument
// does not reach it — but a floor finding for a dropped rule would be worse than
// the gap: report.Human renders findings by iterating rls, which no longer holds
// that rule, so the run would exit 1 having printed nothing. The honest move is
// to say the floor went unevaluated, on the drop line that already exists.
func TestCheckSkipEscapesDisclosesTheUnevaluatedFloor(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: heavy-corpus\n    type: command\n    scope: {include: ['**/*.go'], min_files: 9}\n"+
			"    params:\n      cmd: [\"true\"]\n      expect: {exit: 0}\n"+
			"  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the drop is legitimate\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "heavy-corpus: did not run") {
		t.Fatalf("the drop line must still be there:\n%s", out)
	}
	for _, want := range []string{"scope.min_files", "9"} {
		if !strings.Contains(out, want) {
			t.Errorf("the drop line must disclose the unevaluated floor (%q):\n%s", want, out)
		}
	}
}

// --lane is selection working as asked: a rule the lane does not select did not
// run, so its floor is not evaluated either. Pinned as a decision rather than an
// accident, and deliberately WITHOUT a disclosure line — cli.go already argues
// that a lane not choosing a rule is not a rule being dropped out from under the
// run, and the selected lane is what the operator asked to be told about.
func TestCheckLaneFilterLeavesUnselectedFloorsUnevaluated(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  go-only:\n    tags: [go]\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: dart-corpus\n    type: forbidden-pattern\n    scope: {include: ['**/*.dart'], min_files: 9}\n"+
			"    tags: [dart]\n    params: {pattern: WIDGET}\n"+
			"  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    tags: [go]\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "go-only")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — an unselected rule's floor is not this run's business\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "dart-corpus") {
		t.Errorf("a rule the lane did not select must not appear at all:\n%s", out)
	}
}

// Fixture trees are tiny by construction — a corpus floor of 5 could never be met
// inside one — so `formwork test` must not evaluate floors. It does not, because
// the fixture runner goes straight to the engine and the floor lives at the
// check seam; this pins that, since the alternative breaks every fixture of every
// armed rule at once.
func TestTestIgnoresTheScopeFloor(t *testing.T) {
	root := minFilesRepo(t, "{include: ['**/*.go'], min_files: 5}")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "go-corpus", "fire-1", "x.go"), "const x = \"WIDGET\" // want: go-corpus\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "go-corpus", "pass-1", "x.go"), "const x = \"ok\"\n")

	code, out, errOut := runCLI(t, "test", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — an armed floor must not fail its own fixtures\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

// A floor is a rule field that changes verdicts, so it has to be visible from
// the command that renders what governs (#105/#108). The unset case is checked
// against the same renderer, so the assertion cannot be satisfied by a `min_files`
// line that is never emitted at all.
func TestExplainRendersTheScopeFloor(t *testing.T) {
	armed := minFilesRepo(t, "{include: ['**/*.go'], min_files: 5}")
	code, out, errOut := runCLI(t, "explain", "-C", armed, "go-corpus")
	if code != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "min_files: 5") {
		t.Errorf("explain must render the floor:\n%s", out)
	}

	var armedJSON struct {
		MinFiles int `json:"min_files"`
	}
	code, out, _ = runCLI(t, "explain", "-C", armed, "-format", "json", "go-corpus")
	if code != 0 {
		t.Fatalf("json explain exit = %d\n%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &armedJSON); err != nil {
		t.Fatal(err)
	}
	if armedJSON.MinFiles != 5 {
		t.Errorf("json min_files = %d, want 5\n%s", armedJSON.MinFiles, out)
	}

	unarmed := minFilesRepo(t, "{include: ['**/*.go']}")
	_, out, _ = runCLI(t, "explain", "-C", unarmed, "go-corpus")
	if strings.Contains(out, "min_files") {
		t.Errorf("an unset floor must not be rendered:\n%s", out)
	}
}
