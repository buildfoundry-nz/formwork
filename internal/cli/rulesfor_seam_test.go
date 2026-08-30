package cli

// Internal test for the ghost-batch failure deferral (#125 round-2 finding
// 2). Both git interactions of one rules-for invocation — the snapshot
// resolve and the ghost check-ignore batch — happen milliseconds apart in
// one process, so no external test can flip git state between them; the
// checkIgnored seam is the honest way to make the second call fail while
// the first succeeded.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

func writeSeamFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRulesForGhostBatchFailureDefersToGitFreeVerdicts pins the precedence
// the wrErr deferral already has: a ghost path hidden by a scan.ignore glob
// answers NOT SCANNED without needing git at all, so a check-ignore batch
// failure must not preempt it — while a ghost whose verdict genuinely needs
// the gitignore answer refuses loudly (exit 2), never a confident governed
// answer.
func TestRulesForGhostBatchFailureDefersToGitFreeVerdicts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	writeSeamFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "third_party/**"
      reason: "vendored, not ours"
  gitignore:
    reason: "git already refuses these"
`)
	writeSeamFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	writeSeamFile(t, root, ".gitignore", "vendor/\n")

	orig := checkIgnored
	checkIgnored = func(string, []string) ([]vcs.IgnoredPath, error) {
		return nil, errors.New("index.lock held by another process")
	}
	t.Cleanup(func() { checkIgnored = orig })

	// A glob-hidden ghost still answers: the verdict never needed git.
	var stdout, stderr bytes.Buffer
	code := runRulesFor([]string{"-C", root, "third_party/newfile.go"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("glob-hidden ghost must answer despite the batch failure, exit %d\nstderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "third_party/**") {
		t.Fatalf("expected the glob answer:\n%s", stdout.String())
	}

	// A ghost that needs the gitignore answer refuses loudly.
	stdout.Reset()
	stderr.Reset()
	code = runRulesFor([]string{"-C", root, "vendor/newfile.go"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("gitignore-dependent ghost must refuse on an unanswerable batch, exit %d\nstdout: %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "scan.gitignore") {
		t.Fatalf("refusal must name the channel:\n%s", stderr.String())
	}
}
