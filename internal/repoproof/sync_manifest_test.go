// sync_manifest_test.go — the clone harness refuses a manifest that declares
// no targets. Converted from scripts/sync-manifest-proof.sh.
//
// WHY THIS EXISTS. `make sync` and `make sync-status` read the manifest line
// by line. Both were written as a bare `while read ... done < $(REPOS_FILE)`
// loop, so a manifest with nothing in it ran the loop zero times, printed
// nothing, and exited 0 — "synced everything you asked for" over an empty ask.
// That matters most in the public tree, whose first `make sync` runs against
// no manifest at all.
//
// BOTH DIRECTIONS, because a negative-only test passes just as well against a
// guard that refuses everything. The accept vector pins a throwaway LOCAL git
// repo, so this needs no network and cannot be perturbed by a remote nobody
// controls.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up to the directory holding go.mod. Failing closed rather
// than guessing: a proof that cannot find the tree it is judging must not
// report a pass.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory — cannot locate the repo root")
		}
		dir = parent
	}
}

func needBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		// Fail, never skip. A gate that cannot answer must say so; skipping
		// would let a run with no make report the same green as one that
		// actually exercised the harness.
		t.Fatalf("%s is not on PATH, so this gate cannot answer", name)
	}
}

// runTarget runs one make target against a throwaway manifest and projects dir.
func runTarget(t *testing.T, target, manifest, projects string) (int, string) {
	t.Helper()
	cmd := exec.Command("make", target,
		"REPOS_FILE="+manifest,
		"PROJECTS_DIR="+projects)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("make %s could not run at all: %v", target, err)
	}
	return code, string(out)
}

func TestSyncRefusesAManifestDeclaringNoTargets(t *testing.T) {
	needBinary(t, "make")
	tmp := t.TempDir()

	empty := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	comments := filepath.Join(tmp, "comments.txt")
	if err := os.WriteFile(comments, []byte("# only comments, no targets\n#\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, v := range []struct{ label, manifest string }{
		{"absent", filepath.Join(tmp, "does-not-exist.txt")},
		{"empty", empty},
		{"comments-only", comments},
	} {
		for _, target := range []string{"sync", "sync-status"} {
			code, out := runTarget(t, target, v.manifest, filepath.Join(tmp, "projects"))
			if code == 0 {
				t.Errorf("%s with a %s manifest exited 0 — a vacuous pass over zero targets:\n%s",
					target, v.label, out)
			}
			// Exit code alone is not enough: a raw shell error would satisfy a
			// code-only assertion while telling the reader nothing.
			if !strings.Contains(out, "no targets") && !strings.Contains(out, "declares no targets") {
				t.Errorf("%s with a %s manifest gave no legible reason:\n%s", target, v.label, out)
			}
		}
	}
}

func TestSyncAcceptsAManifestThatDeclaresATarget(t *testing.T) {
	needBinary(t, "make")
	needBinary(t, "git")
	tmp := t.TempDir()

	probe := filepath.Join(tmp, "probe-origin")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main", "."},
		{"config", "user.email", "proof@example.invalid"},
		{"config", "user.name", "sync proof"},
	} {
		c := exec.Command("git", args...)
		c.Dir = probe
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("building the probe repo (git %v): %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(probe, "file.txt"), []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "file.txt"}, {"commit", "--quiet", "-m", "probe commit"}} {
		c := exec.Command("git", args...)
		c.Dir = probe
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("building the probe repo (git %v): %v: %s", args, err, out)
		}
	}
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = probe
	shaOut, err := shaCmd.Output()
	if err != nil {
		t.Fatalf("cannot read the probe SHA: %v", err)
	}
	sha := strings.TrimSpace(string(shaOut))

	manifest := filepath.Join(tmp, "real.txt")
	body := "# a manifest that declares one target\nprobe " + probe + " " + sha + "\n"
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	projects := filepath.Join(tmp, "projects")
	code, out := runTarget(t, "sync", manifest, projects)
	if code != 0 {
		t.Fatalf("sync with a declared target exited %d — the guard fired on a non-empty manifest:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(projects, "probe", ".git")); err != nil {
		t.Fatalf("sync with a declared target did not check the target out: %v\n%s", err, out)
	}
}
