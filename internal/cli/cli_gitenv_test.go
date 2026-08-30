package cli_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Honouring FORMWORK_GIT_ENV must be ANNOUNCED. The hatch turns off a guard,
// and it is set in the environment rather than on the command line — so the
// invocation that runs under it looks, in CI logs and in a shell, exactly like
// one that does not. An operator who exported it six months ago has nothing
// else to read.
//
// It goes to STDERR because stdout is machine-readable on several subcommands
// (`-format json`, and scope's key=value lines); a diagnostic there would
// corrupt a consumer's parse.
func TestGitEnvHatchIsAnnouncedOnStderr(t *testing.T) {
	t.Setenv(vcs.GitEnvVar, "inherit")

	code, _, stderr := runCLI(t, "check", "-C", filepath.Join("testdata", "toyrepo"))

	// The toyrepo has a violation; the hatch changes nothing about the verdict.
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the announcement must not change the verdict", code)
	}
	for _, want := range []string{vcs.GitEnvVar, "inherit", "GIT_DIR"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q; got:\n%s", want, stderr)
		}
	}
}

// The default is silent. A notice printed on every ordinary run is one an
// operator learns to skip past, which would cost the announcement above its
// only job.
func TestGitEnvDefaultIsSilent(t *testing.T) {
	_, _, stderr := runCLI(t, "check", "-C", filepath.Join("testdata", "toyrepo"))
	if strings.Contains(stderr, vcs.GitEnvVar) {
		t.Errorf("stderr mentions %s on an ordinary run:\n%s", vcs.GitEnvVar, stderr)
	}
}

// ancestorRepro builds #167's ancestor layout and returns the root to check.
//
// Removing GIT_DIR was justified on the premise that the layout it breaks fails
// loudly. It does not when an ANCESTOR of -C is a repository: git's ordinary
// upward discovery answers from the ancestor and exits 0, so the scrub swapped
// one wrong repository for another in silence. Here the ancestor's .gitignore
// names bad.go, and the repository GIT_DIR names has bad.go TRACKED — git
// refuses to call a tracked path ignored, so the two repositories give opposite
// answers about the one file that carries the violation.
//
// Measured on the binary built from this branch before the guard: exit 0,
// "1 files ignored", over a committed violation.
func ancestorRepro(t *testing.T) (wt, gitDir string) {
	t.Helper()
	ancestor := t.TempDir()
	gitInit(t, ancestor)
	mustWrite(t, filepath.Join(ancestor, ".gitignore"), "bad.go\n")
	gitRun(t, ancestor, "add", ".gitignore")

	// The work tree of a SECOND repository, sitting inside the first. It is a
	// plain directory: nothing here declares a repository, which is what leaves
	// git free to walk up into the ancestor once GIT_DIR is gone.
	wt = filepath.Join(ancestor, "wt")
	mustWrite(t, filepath.Join(wt, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n")
	mustWrite(t, filepath.Join(wt, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(wt, "bad.go"), "package bad\n// banana\n")

	elsewhere := t.TempDir()
	gitInit(t, elsewhere)
	gitDir = filepath.Join(elsewhere, ".git")
	t.Setenv("GIT_DIR", gitDir)
	gitRun(t, wt, "add", "-A")
	return wt, gitDir
}

// THE REPRODUCTION, END TO END. The committed violation must be REPORTED, and
// the substitution disclosed.
//
// EXIT 1, NOT 2, AND THE DIFFERENCE IS NOT AN OVERSIGHT. vcs refuses to choose a
// repository and returns an error; scan.gitignore's consumer already has an
// argued policy for a gitignore answer it cannot get (cli.go: prune NOTHING,
// print the reason, scan the whole tree), and that policy is fail-closed FOR
// THIS RULE — the resulting scan is a SUPERSET of the declared one, and a
// forbidden-pattern fires on the PRESENCE of a match, so a superset can only add
// findings. The violation is found, the verdict is exit 1, and the substitution
// is disclosed on stderr.
//
// THAT REASONING DOES NOT GENERALISE, and an earlier version of this comment
// claimed it did ("cannot let a rule pass that would otherwise fail", full
// stop). It is exactly inverted for a check that fires on ABSENCE:
// scope.min_files fails when too FEW files matched, so the files the fallback
// declined to prune can satisfy a floor the declared corpus does not — measured,
// exit 1 to exit 0. `lint`'s empty-scope is the same shape. Both now refuse at
// exit 2 rather than being softened; see
// TestCheckRefusesAScopeFloorOverAnUnprunedGitignoreFallback below and
// internal/meta.
//
// What the guard removed is the silent exit 0. The mode that CANNOT degrade into
// a superset is asserted below.
func TestCheckReportsTheViolationWhenTheScrubWouldAnswerFromAnAncestor(t *testing.T) {
	wt, gitDir := ancestorRepro(t)

	code, out, stderr := runCLI(t, "check", "-C", wt)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the committed violation must be reported\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	if !strings.Contains(out, "bad.go") {
		t.Errorf("the violation is missing from the report:\n%s", out)
	}
	if strings.Contains(out, "files ignored") {
		t.Errorf("a file was pruned on an answer formwork could not attribute to a repository:\n%s", out)
	}
	// The operator has to be able to act on it without reading formwork's source:
	// both repositories, the variable, and the hatch.
	for _, want := range []string{"GIT_DIR", gitDir, vcs.GitEnvVar, "inherit"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q; got:\n%s", want, stderr)
		}
	}
}

// The file-set modes take their scan from git, so there is no superset to fall
// back to: a changeset read from the wrong repository is simply the wrong
// changeset. Exit 2 — formwork could not answer, which the contract puts at an
// engine fault rather than a verdict.
func TestCheckStagedIsExit2WhenTheScrubWouldAnswerFromAnAncestor(t *testing.T) {
	wt, gitDir := ancestorRepro(t)

	code, out, stderr := runCLI(t, "check", "--staged", "-C", wt)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	for _, want := range []string{"GIT_DIR", gitDir, vcs.GitEnvVar} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q; got:\n%s", want, stderr)
		}
	}
}

// stagedRepro builds a repository with a STAGED violation and returns it
// alongside the git directory of an unrelated second repository. Every rule the
// corpus declares matches the staged file, so a run that reads the right
// changeset cannot pass.
func stagedRepro(t *testing.T) (root, gitDir string) {
	t.Helper()
	root = t.TempDir()
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, "staged.txt"), "banana\n")
	gitRun(t, root, "add", "-A")

	elsewhere := t.TempDir()
	gitInit(t, elsewhere)
	return root, filepath.Join(elsewhere, ".git")
}

// THE HATCH IS NOT AN OFF-SWITCH FOR THE FILE-SET MODES' REPOSITORY.
//
// Measured on the binary built from this branch before the agreement guard, with
// the hatch on and GIT_DIR naming the unrelated repository: `0 path(s) requested
// by --staged, 0 file(s) scanned`, `1/1 rules passed`, exit 0 — over a staged
// violation. The changeset came from a repository the operator never named, and
// an empty one reads as nothing to check.
//
// The healthy verdict is asserted FIRST, so a fixture that passes for a reason
// of its own cannot be mistaken for the guard firing.
func TestCheckStagedRefusesWhenTheHatchNamesAnotherRepository(t *testing.T) {
	root, gitDir := stagedRepro(t)

	code, out, stderr := runCLI(t, "check", "--lane", "pre-commit", "--staged", "-C", root)
	if code != 1 {
		t.Fatalf("healthy exit = %d, want 1 — the staged violation must be reported before the guard is tested\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}

	t.Setenv(vcs.GitEnvVar, "inherit")
	t.Setenv("GIT_DIR", gitDir)
	code, out, stderr = runCLI(t, "check", "--lane", "pre-commit", "--staged", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the run judged a changeset read from a repository -C did not name\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	for _, want := range []string{"GIT_DIR", gitDir, vcs.GitEnvVar} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q; got:\n%s", want, stderr)
		}
	}
}

// THE CONTROL, END TO END: the layout the hatch exists for keeps its verdict.
// Without this the test above is satisfied by refusing every hatched run, which
// would take the hatch's only reason to exist away with it.
//
// The bare repository and the work tree are joined by the two variables and
// nothing else — `wt` holds no .git — so this is the one layout that has no
// other spelling.
func TestCheckStagedUnderTheHatchStillJudgesADetachedWorkTree(t *testing.T) {
	base := t.TempDir()
	bare := filepath.Join(base, "bare.git")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(base, "wt")
	mustWrite(t, filepath.Join(wt, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(wt, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(wt, "staged.txt"), "banana\n")
	t.Setenv("GIT_DIR", bare)
	t.Setenv("GIT_WORK_TREE", wt)
	gitRun(t, wt, "add", "-A")
	t.Setenv(vcs.GitEnvVar, "inherit")

	code, out, stderr := runCLI(t, "check", "--lane", "pre-commit", "--staged", "-C", wt)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the layout the hatch exists for must keep working\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	if !strings.Contains(out, "staged.txt") {
		t.Errorf("the staged violation is missing from the report:\n%s", out)
	}
}

// An unrecognised value is exit 2 — an engine/config error — and it is decided
// BEFORE the subcommand runs, so it does not depend on whether that subcommand
// happens to invoke git. `version` runs no git at all and must still refuse.
func TestGitEnvUnrecognisedValueIsExit2(t *testing.T) {
	for _, val := range []string{"", "yes", "INHERIT"} {
		t.Run("value="+val, func(t *testing.T) {
			t.Setenv(vcs.GitEnvVar, val)
			code, _, stderr := runCLI(t, "version")
			if code != 2 {
				t.Fatalf("exit = %d, want 2 for %s=%q", code, vcs.GitEnvVar, val)
			}
			if !strings.Contains(stderr, vcs.GitEnvVar) {
				t.Errorf("stderr does not name %s:\n%s", vcs.GitEnvVar, stderr)
			}
		})
	}
}

// A SUPERSET SCAN SATISFIES A FLOOR THAT THE DECLARED CORPUS DOES NOT.
//
// scope.min_files (#23) exists to turn a shrunken scope from a disclosure into a
// verdict: fewer than N files in scope is an error. It therefore fires on
// ABSENCE, and the unresolved-gitignore fallback — prune nothing, scan the whole
// tree — adds exactly the files that make the absence go away. Measured before
// the refusal: healthy `3 file(s) scanned`, the floor fires, exit 1; with the
// fallback in force `8 file(s) scanned`, `[needs-corpus] OK`, exit 0.
//
// The healthy verdict is asserted FIRST, so a fixture that fails the floor for
// some reason of its own cannot pass as if the guard had fired.
func TestCheckRefusesAScopeFloorOverAnUnprunedGitignoreFallback(t *testing.T) {
	root, gitDir := floorRepro(t)

	code, out, stderr := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("healthy exit = %d, want 1 — the floor must fire before the fallback is tested\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}

	t.Setenv("GIT_DIR", gitDir)
	code, out, stderr = runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the fallback satisfied a floor the declared corpus does not\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	if !strings.Contains(stderr, "min_files") {
		t.Errorf("stderr does not name the feature it refused over:\n%s", stderr)
	}
}

// The other side of the same predicate: with no floor armed, the fallback stays
// a warning and the run keeps its verdict. Without this the test above is
// satisfied by refusing every unresolved gitignore, which would be a different
// and much broader change.
func TestCheckStillReportsUnderTheFallbackWithNoFloorArmed(t *testing.T) {
	root, gitDir := floorRepro(t)
	// Same corpus, floor disarmed.
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: needs-corpus\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: banana}\n")

	t.Setenv("GIT_DIR", gitDir)
	code, out, stderr := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a run with no floor armed keeps the fallback\nstdout:\n%s\nstderr:\n%s", code, out, stderr)
	}
	if !strings.Contains(stderr, "could not determine") {
		t.Errorf("the fallback must still be disclosed:\n%s", stderr)
	}
}

// floorRepro builds a corpus whose rule arms a scope floor of 3 over a src/
// holding 2 matching files, with 5 more sitting in a gitignored gen/. Healthy,
// the floor fires; unpruned, the gen/ files satisfy it. Returns the root and the
// git dir that triggers the environment refusal underneath.
func floorRepro(t *testing.T) (root, gitDir string) {
	t.Helper()
	root = t.TempDir()
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: needs-corpus\n    type: forbidden-pattern\n    scope: {include: ['**/*.go'], min_files: 3}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, ".gitignore"), "gen/\n")
	for i := range 2 {
		mustWrite(t, filepath.Join(root, "src", fmt.Sprintf("s%d.go", i)), "package s\n")
	}
	for i := range 5 {
		mustWrite(t, filepath.Join(root, "gen", fmt.Sprintf("g%d.go", i)), "package g\n")
	}
	gitRun(t, root, "add", "-A")

	elsewhere := t.TempDir()
	gitInit(t, elsewhere)
	return root, filepath.Join(elsewhere, ".git")
}
