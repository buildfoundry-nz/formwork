// compileonce_test.go — formwork test must compile a type:command go-run
// detector once per rule and reuse the binary across that rule's arms.
//
// Without this, every fire-*/pass-* arm pays a cold `go run` (or `go -C … run`)
// even when the detector bytes are identical across arms — the dominant cost
// on corpora that copy scripts/dev into each fixture tree.
package fixturetest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// installGoShim writes a `go` wrapper that appends every argv to logPath, then
// execs realGo. The test puts shimDir first on PATH so formwork's command
// rules (and the engine's own go build) are observed.
func installGoShim(t *testing.T, shimDir, realGo, logPath string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"exec \"" + realGo + "\" \"$@\"\n"
	path := filepath.Join(shimDir, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func countDetectorCompiles(log string, needles ...string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// A compile is `go run …` or `go build …` (including `go -C dir run|build`).
		fields := strings.Fields(line)
		isCompile := false
		for _, f := range fields {
			if f == "run" || f == "build" {
				isCompile = true
				break
			}
		}
		if !isCompile {
			continue
		}
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				n++
				break
			}
		}
	}
	return n
}

// detectorMain is a single-file detector (corpus majority shape: go run file.go).
// Reads MARKER in the working directory: "fire" → exit 1 + print; else exit 0.
const detectorMain = `package main
import ("fmt"; "os")
func main() {
	b, _ := os.ReadFile("MARKER")
	s := string(b)
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	if s == "fire" {
		fmt.Fprintln(os.Stderr, "detector-fired")
		os.Exit(1)
	}
	os.Exit(0)
}
`

func writeDetectorFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(detectorMain), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDetectorModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/det\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(detectorMain), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompileOnceGoRunAcrossArms(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	shimDir := filepath.Join(root, "shim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "go-invocations.log")
	installGoShim(t, shimDir, realGo, logPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/det.yaml": `rules:
  - id: det-once
    type: command
    severity: error
    scope:
      include: ["MARKER"]
    params:
      cmd: [go, run, scripts/dev/check-det.go]
    cure: "detector fired"
`,
		".formwork/fixtures/det-once/fire-1/MARKER": "fire\n",
		".formwork/fixtures/det-once/fire-1.want":   "- detector-fired\n",
		".formwork/fixtures/det-once/pass-1/MARKER": "pass\n",
		".formwork/fixtures/det-once/fire-2/MARKER": "fire\n",
		".formwork/fixtures/det-once/fire-2.want":   "- detector-fired\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, arm := range []string{"fire-1", "pass-1", "fire-2"} {
		writeDetectorFile(t, filepath.Join(root, ".formwork", "fixtures", "det-once", arm, "scripts", "dev", "check-det.go"))
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("Run error: %v\n%s", err, sb.String())
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
	if !strings.Contains(sb.String(), "[det-once] OK — 3 fixture(s)") {
		t.Fatalf("expected 3 arms OK, got:\n%s", sb.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	compiles := countDetectorCompiles(string(logBytes), "check-det.go", "scripts/dev/check-det.go")
	if compiles != 1 {
		t.Fatalf("detector compiled %d times, want exactly 1 (compile-once across arms); go log:\n%s",
			compiles, logBytes)
	}
}

func TestCompileOnceGoDashCAcrossArms(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	shimDir := filepath.Join(root, "shim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "go-invocations.log")
	installGoShim(t, shimDir, realGo, logPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/det.yaml": `rules:
  - id: det-c-once
    type: command
    severity: error
    scope:
      include: ["MARKER"]
    params:
      cmd: [go, -C, detector, run, .]
    cure: "detector fired"
`,
		".formwork/fixtures/det-c-once/fire-1/MARKER": "fire\n",
		".formwork/fixtures/det-c-once/fire-1.want":   "- detector-fired\n",
		".formwork/fixtures/det-c-once/pass-1/MARKER": "pass\n",
		".formwork/fixtures/det-c-once/fire-2/MARKER": "fire\n",
		".formwork/fixtures/det-c-once/fire-2.want":   "- detector-fired\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, arm := range []string{"fire-1", "pass-1", "fire-2"} {
		// Under go -C detector, program cwd is <arm>/detector, so MARKER must live there.
		writeDetectorModule(t, filepath.Join(root, ".formwork", "fixtures", "det-c-once", arm, "detector"))
		if err := os.WriteFile(filepath.Join(root, ".formwork", "fixtures", "det-c-once", arm, "detector", "MARKER"),
			[]byte(map[string]string{"fire-1": "fire\n", "fire-2": "fire\n", "pass-1": "pass\n"}[arm]), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("Run error: %v\n%s", err, sb.String())
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, sb.String())
	}
	if !strings.Contains(sb.String(), "[det-c-once] OK — 3 fixture(s)") {
		t.Fatalf("expected 3 arms OK, got:\n%s", sb.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	compiles := countDetectorCompiles(string(logBytes), "detector")
	if compiles != 1 {
		t.Fatalf("detector compiled %d times, want exactly 1; go log:\n%s", compiles, logBytes)
	}
}

func TestCompileOnceBuildFailureSurfacesOnArm(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	shimDir := filepath.Join(root, "shim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "go-invocations.log")
	installGoShim(t, shimDir, realGo, logPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/det.yaml": `rules:
  - id: det-broken
    type: command
    severity: error
    scope:
      include: ["MARKER"]
    params:
      cmd: [go, run, scripts/dev/check-det.go]
    cure: "broken"
`,
		// Pass arm: a detector that does not build must surface as an arm
		// problem (unexpected finding / build output), never a silent OK.
		".formwork/fixtures/det-broken/pass-1/MARKER": "pass\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	broken := filepath.Join(root, ".formwork", "fixtures", "det-broken", "pass-1", "scripts", "dev", "check-det.go")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("package main\nfunc main() { notARealIdent() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatalf("build failure must not be an infrastructure abort: %v\n%s", err, sb.String())
	}
	if failed == 0 {
		t.Fatalf("broken detector must not silently pass:\n%s", sb.String())
	}
	out := sb.String()
	// Pass-arm printer names the unexpected finding; a silent OK is the defect.
	if !strings.Contains(out, "unexpected finding") {
		t.Fatalf("arm problem must surface as an unexpected finding, got:\n%s", out)
	}
}
