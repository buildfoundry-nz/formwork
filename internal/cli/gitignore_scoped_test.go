package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/cli"
)

// A path-scoped command asks git about the paths it was given, never for every
// ignored path in the working tree (#126): that listing was ~7 s of a one-file
// check-content on an 83k-file tree. The answers are the ones the whole listing
// gave — vendor/gen.go is still NOT SCANNED by its .gitignore line — and the
// whole-tree check, which walks every path, still lists. git is wrapped on PATH
// so the test reads the argv of every call the run makes.
func TestPathScopedCommandsNeverListTheWorkingTree(t *testing.T) {
	root := gitignoreQueryRepo(t)
	calls := wrapGit(t)
	for _, tc := range []struct {
		name      string
		args      []string
		stdin     string
		wantOut   string
		wantWhole bool
	}{
		{"rules-for", []string{"rules-for", "-C", root, "vendor/gen.go"}, "", ".gitignore:1:vendor/", false},
		{"check-content", []string{"check-content", "-C", root, "--path", "vendor/gen.go"}, "banana\n", `"not_scanned"`, false},
		{"check-content --batch", []string{"check-content", "-C", root, "--batch"}, `{"path":"vendor/gen.go","content":"banana\n"}` + "\n", `"not_scanned"`, false},
		{"check", []string{"check", "-C", root}, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(calls, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run(tc.args, strings.NewReader(tc.stdin), &stdout, &stderr)
			if code == 2 {
				t.Fatalf("exit 2:\n%s%s", stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantOut) {
				t.Fatalf("output missing %q:\n%s", tc.wantOut, stdout.String())
			}
			log, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if whole := strings.Contains(string(log), "--others"); whole != tc.wantWhole {
				t.Fatalf("listed the whole working tree = %v, want %v; git calls:\n%s", whole, tc.wantWhole, log)
			}
		})
	}
}

// wrapGit puts a git first on PATH that appends each call's argv to the
// returned file before running the real git.
func wrapGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	bin, calls := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + calls + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}
