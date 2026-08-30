package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Every branch of requestedButAbsent's classification in one call. Most of them
// also have end-to-end rows in cli_absent_test.go; ONE cannot — a path that IS a
// regular file on disk and still is not in the scan. Its real causes are
// filesystem name mapping (git's NFC spelling against an NFD directory entry —
// #98 describes that shape) and case folding, neither of which reproduces on
// every platform CI runs. The BRANCH is reachable regardless of what any
// particular issue's state is, so it is pinned where it can be: by handing the
// function a scan that omits a file which exists.
func TestRequestedButAbsentClassifiesEveryOutcome(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("scanned.go")
	write("unscanned.go")
	write("gen/hidden.go")
	write(".formwork/rules/r.yaml")
	write("sub/.formwork")
	for _, d := range []string{"realsub", "phantomdir"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The scan produced exactly one of them.
	got := &scan.FileSet{Root: root, Files: []*scan.File{scan.NewMemFile("scanned.go", nil)}}
	opts := scan.Opts{Ignore: []string{"gen/**"}}
	requested := []string{
		"scanned.go",             // produced — nothing to explain
		".formwork/rules/r.yaml", // a built-in skip ANCESTOR — never scannable
		"sub/.formwork",          // a regular file NAMED .formwork — the walk scans it
		"realsub",                // git calls it a gitlink: a pointer, carved out
		"uninit",                 // a gitlink whose directory was never created
		"phantomdir",             // a directory git recorded NOTHING for: refused
		"gen/hidden.go",          // on disk, hidden by a declared glob
		"gone.go",                // never on disk
		"script.sh",              // an EXECUTABLE blob: still a file, still refused
		"unscanned.go",           // on disk, regular, no channel explains it
	}
	// git's answer, which is the only pointer oracle in either mode. The two
	// gitlinks differ only in whether their directory exists, and both must be
	// carved out — that difference is what the worktree oracle got wrong.
	gitModes := map[string]string{
		"realsub":       vcs.ModeGitlink,
		"uninit":        vcs.ModeGitlink,
		"sub/.formwork": vcs.ModeBlob,
		"gone.go":       vcs.ModeBlob,
		// 100755 is a file git can hand the toolchain, not a pointer. Listing it
		// beside the pointers is what stops the carve-out being written as
		// "anything that is not 100644".
		"script.sh":     vcs.ModeExecutable,
		"unscanned.go":  vcs.ModeBlob,
		"gen/hidden.go": vcs.ModeBlob,
		// phantomdir is deliberately absent: an unrecorded mode is not a pointer.
	}

	absent, hidden := requestedButAbsent(root, requested, got, opts, gitModes)

	wantHidden := map[string]string{"gen/hidden.go": "scan.ignore (gen/**)"}
	if len(hidden) != len(wantHidden) {
		t.Fatalf("hidden = %+v, want exactly %v", hidden, wantHidden)
	}
	if hidden[0].path != "gen/hidden.go" || hidden[0].channel != wantHidden["gen/hidden.go"] {
		t.Errorf("hidden[0] = %+v, want gen/hidden.go via %q", hidden[0], wantHidden["gen/hidden.go"])
	}

	wantAbsent := map[string]string{
		"gone.go":       reasonNotOnDisk,
		"phantomdir":    reasonNotRegular,
		"script.sh":     reasonNotOnDisk,
		"sub/.formwork": reasonUnscanned,
		"unscanned.go":  reasonUnscanned,
	}
	if len(absent) != len(wantAbsent) {
		t.Fatalf("absent = %+v, want exactly %v", absent, wantAbsent)
	}
	// Sorted by path.
	for i, want := range []string{"gone.go", "phantomdir", "script.sh", "sub/.formwork", "unscanned.go"} {
		if absent[i].path != want {
			t.Fatalf("absent must be sorted by path, got %+v", absent)
		}
	}
	for _, a := range absent {
		if a.reason != wantAbsent[a.path] {
			t.Errorf("%s: reason = %q, want %q", a.path, a.reason, wantAbsent[a.path])
		}
	}
}

// initRepoWith makes root a git repository holding one committed regular blob.
// Both arms now ask git about a tree — the index under --staged, the range's end
// under --range — so both need a real repository to answer from; the cli_test
// package's gitInit is not visible here.
func initRepoWith(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@example.com"},
		{"config", "user.name", "Test"},
		// Same detached-maintenance disarm gitInit carries (see cli_test.go):
		// this helper commits, so without it the same TempDir race is open.
		{"config", "maintenance.auto", "false"},
		{"add", rel}, {"commit", "-qm", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// Without git's answer the --staged carve-out cannot be taken at all, and taking
// it anyway on a guess is precisely the fail-open this accounting exists to
// close. The error must propagate — never a silent fall back to the worktree
// oracle git was asked to replace.
func TestRefuseUnaccountedPathsFailsClosedWhenGitCannotAnswer(t *testing.T) {
	root := t.TempDir() // deliberately not a repository
	if err := os.WriteFile(filepath.Join(root, "onDisk.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	refuse, err := refuseUnaccountedPaths(&buf, root, "--staged", "", true,
		[]string{"onDisk.go"}, &scan.FileSet{Root: root}, scan.Opts{})
	if err == nil {
		t.Fatalf("want an error when git cannot answer; got refuse=%v, stderr:\n%s", refuse, buf.String())
	}
	if !strings.Contains(err.Error(), "staged mode") {
		t.Errorf("the error must say what could not be determined: %v", err)
	}
}

// The same contract on the --range arm, which asks a different git command and
// so could swallow a failure independently. It is reachable only from inside
// the package: end to end, vcs.RangePaths fails on the same unresolvable range
// one screen earlier, so `check` never gets this far.
func TestRefuseUnaccountedPathsFailsClosedWhenTheRangeCannotBeRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "onDisk.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initRepoWith(t, root, "onDisk.go")
	var buf bytes.Buffer
	refuse, err := refuseUnaccountedPaths(&buf, root, "--range nope..alsonope", "nope..alsonope", false,
		[]string{"onDisk.go"}, &scan.FileSet{Root: root}, scan.Opts{})
	if err == nil {
		t.Fatalf("want an error when git cannot resolve the range; got refuse=%v, stderr:\n%s", refuse, buf.String())
	}
	if !strings.Contains(err.Error(), "end-of-range mode") {
		t.Errorf("the error must say what could not be determined: %v", err)
	}
}

// Both restore-cures say "put the file back", which is only advice when a file
// IS missing. A path that is on disk and merely unscanned must get the refusal
// without them — sending that operator to `git restore` points them at a file
// they already have and away from the spelling that is the real cause.
func TestRefuseUnaccountedPathsWithholdsTheRestoreCureWhenNothingIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name, flag, rangeSpec string
		staged                bool
	}{
		{"staged", "--staged", "", true},
		{"range", "--range HEAD..HEAD", "HEAD..HEAD", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "onDisk.go"), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			initRepoWith(t, root, "onDisk.go")
			var buf bytes.Buffer
			refuse, err := refuseUnaccountedPaths(&buf, root, tc.flag, tc.rangeSpec, tc.staged,
				[]string{"onDisk.go"}, &scan.FileSet{Root: root}, scan.Opts{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !refuse {
				t.Fatal("an unscanned path must still be refused")
			}
			out := buf.String()
			if !strings.Contains(out, reasonUnscanned) {
				t.Errorf("the refusal must still name the reason:\n%s", out)
			}
			for _, cure := range []string{"restore it", "restore them", "would commit unchecked"} {
				if strings.Contains(out, cure) {
					t.Errorf("%q is false for a file that is on disk:\n%s", cure, out)
				}
			}
		})
	}
}

// The sixth outcome: a stat that fails for a reason other than "not there". It
// must be reported, not skipped — the whole point of this accounting is to
// leave no requested path unexplained, and "I could not tell" is not an
// explanation. A skip here would be the fail-open in miniature.
func TestRequestedButAbsentReportsAnUnreadableStat(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions, so the stat cannot be made to fail")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore before TempDir's cleanup, which cannot remove an unsearchable dir.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	absent, hidden := requestedButAbsent(root, []string{"locked/x.go"}, &scan.FileSet{Root: root}, scan.Opts{}, nil)
	if len(hidden) != 0 {
		t.Fatalf("hidden = %+v, want none — no channel was declared", hidden)
	}
	if len(absent) != 1 {
		t.Fatalf("absent = %+v, want the unreadable path reported", absent)
	}
	if !strings.HasPrefix(absent[0].reason, "could not be examined on disk:") {
		t.Errorf("reason = %q, want the stat failure carried through", absent[0].reason)
	}
}
