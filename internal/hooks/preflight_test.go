package hooks_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/hooks"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Install's pre-flight: the states where install must REFUSE rather than take
// over hook wiring that is already there (#146 D1/D2/D7).
//
// Every refusal here is asserted twice — once on the message, which is the only
// thing the operator gets, and once on a SNAPSHOT of the repository taken before
// the run. The snapshot is the half that cannot go vacuous: an absence assertion
// ("no shim at path P") keeps passing after the thing it watched is deleted, and
// it says nothing about a partial write somewhere else.

// treeSnapshot records every path under root with its permissions and a hash of
// its contents. Not mtimes: git rewrites those for reasons that have nothing to
// do with install, and a snapshot that fails for a reason the test is not about
// gets deleted rather than debugged.
//
// The walk deliberately includes .git, so the repository's config file is part
// of the snapshot rather than a second assertion that could be forgotten —
// wantUnchanged fails if that key is missing, so a future exclusion cannot
// silently turn "changed nothing" into "changed nothing outside .git".
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			out[rel] = fmt.Sprintf("dir %04o", fi.Mode().Perm())
		case !fi.Mode().IsRegular():
			out[rel] = fi.Mode().String()
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[rel] = fmt.Sprintf("%04o %x", fi.Mode().Perm(), sha256.Sum256(b))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

const configKey = ".git/config"

// wantUnchanged is wantUnchangedTree plus the assertion that the snapshot
// covered the repository's config file at all. Every use of it is about a
// refusal that must not have SET anything, and a snapshot that quietly stopped
// covering .git would still pass while core.hooksPath was rewritten.
func wantUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if _, ok := before[configKey]; !ok {
		t.Fatalf("the snapshot does not cover %s, so it cannot say the repository's config was left alone", configKey)
	}
	wantUnchangedTree(t, before, after)
}

// wantUnchangedTree is the same comparison for a directory that has no
// .git/config of its own — a linked worktree, whose .git is a file.
func wantUnchangedTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	if reflect.DeepEqual(before, after) {
		return
	}
	var diffs []string
	for k, b := range before {
		if a, ok := after[k]; !ok {
			diffs = append(diffs, "removed "+k)
		} else if a != b {
			diffs = append(diffs, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diffs = append(diffs, "added "+k)
		}
	}
	sort.Strings(diffs)
	t.Fatalf("a refusal changed the repository:\n  %s", strings.Join(diffs, "\n  "))
}

func wantErrContains(t *testing.T, err error, subs ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("install did not refuse")
	}
	for _, s := range subs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the refusal does not mention %q:\n%v", s, err)
		}
	}
}

func wantErrLacks(t *testing.T, err error, subs ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("install did not refuse")
	}
	for _, s := range subs {
		if strings.Contains(err.Error(), s) {
			t.Errorf("the refusal must not mention %q:\n%v", s, err)
		}
	}
}

// resolved is the spelling git reports for a path. On macOS a t.TempDir under
// /var arrives back as /private/var, so a test comparing git's answer against
// the path it handed in compares two spellings of one directory.
func resolved(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return p
}

// --- D1: a root that is not the git top level --------------------------------

// core.hooksPath is repo-relative and git resolves it FROM THE TOP LEVEL, so
// `hooks install -C <subdir>` writes shims into a directory git never looks in
// and sets a config value pointing at another one. Both commands then agreed the
// wiring was healthy while no hook ran at all.
//
// The refusal covers every lane, including the whole-tree ones whose shims would
// technically work from a subdirectory: a policy that holds for pre-push and not
// for pre-commit is a trap, and the repair — teaching the shim to pass -C — is
// blocked anyway, because `check --lane <l> --staged -C <subdir>` exits 2 by
// design (vcs.StagedPaths calls vcs.EnsureTopLevel, internal/vcs/vcs.go:21,263).
func TestInstallRefusesASubdirectoryRoot(t *testing.T) {
	dir := repo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := laneCfg("pre-commit", "pre-push")
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(sub, cfg, false)
	wantErrContains(t, err, resolved(t, dir), "sub")
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// D1'S OTHER HALF: THE PREFIX IS EMPTY AND ROOT IS STILL NOT THE WORK TREE.
//
// Install performs two writes — shims into <root>/.formwork/hooks, and
// core.hooksPath into the config of the repository git resolves for root — and
// they describe one directory only while root IS that repository's work tree.
// The subdirectory refusal above decides on `--show-prefix`, which does not
// cover this: measured on git 2.50.1, with the repository's own config setting
// `core.worktree` to another directory, `rev-parse --show-toplevel --show-prefix`
// from the directory holding .git answers that other directory and an EMPTY
// prefix.
//
// Measured on the binary built from this branch before this refusal, with no git
// variable set anywhere: `formwork hooks install -C <repo>` exited 0 reporting
// "installed git hooks: pre-commit", wrote the shims beside .git, and set
// core.hooksPath = .formwork/hooks. git runs hooks with the working directory
// set to the work tree and resolves that relative value there, so a commit in
// the work tree ran no hook at all — a wiring reported as installed, gating
// nothing.
func TestInstallRefusesWhenItsTwoWritesLandInDifferentTrees(t *testing.T) {
	dir := repo(t)
	elsewhere := t.TempDir()
	gitT(t, dir, "config", "core.worktree", elsewhere)
	cfg := laneCfg("pre-commit")
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(dir, cfg, false)
	// Both write targets, so the operator can see which two directories
	// disagreed rather than being told only that something did.
	wantErrContains(t, err, resolved(t, elsewhere), managedDir(dir))
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// THE REFUSAL ABOVE GOES INERT WHERE THE FILESYSTEM CANNOT DISTINGUISH
// DIRECTORIES, and this measures that rather than leaving it a sentence in a
// comment.
//
// os.SameFile compares file IDs, and a substrate that does not provide usable
// ones (SMB/9p/FUSE, container bind mounts, overlayfs without xino) answers
// "same" for every pair — so the comparison does not fail closed there, it stops
// deciding anything. The identical fixture that refuses above installs cleanly
// here, over a repository whose work tree is somewhere else entirely.
//
// IT IS NOT THE SHAPE ITS vcs SIBLING TAKES, and the difference is the point.
// TestEnsureTopLevelRefusesSubdirWhenIdentityCannotDistinguish forces the same
// answer and asserts the refusal STILL fires, because EnsureTopLevel has a
// second arm that never leaves git's frame. This refusal has no such arm to fall
// back on: git's own answer in this state is that the prefix is empty, which is
// why the identity comparison is here at all. So this test asserts the
// limitation as it stands. Closing it needs a question asked in git's frame
// rather than a stronger comparison of paths, and that is a change to this
// refusal rather than to the seam, and it is a separate change rather than one
// to smuggle in beside a test.
func TestWriteTargetRefusalGoesInertWhereIdentityCannotDistinguish(t *testing.T) {
	dir := repo(t)
	elsewhere := t.TempDir()
	gitT(t, dir, "config", "core.worktree", elsewhere)

	// The control first: without the forced answer this fixture is refused, so a
	// pass below cannot come from the fixture having stopped reproducing.
	if _, err := hooks.Install(dir, laneCfg("pre-commit"), false); err == nil {
		t.Fatal("the fixture no longer reproduces: install accepted a root that is not the work tree")
	}

	restore := hooks.SetSameFileForTest(func(a, b os.FileInfo) bool { return true })
	t.Cleanup(restore)

	installed, err := hooks.Install(dir, laneCfg("pre-commit"), false)
	if err != nil {
		t.Fatalf("install refused with the identity comparison forced degenerate, so it is not what this arm decides on: %v", err)
	}
	if len(installed) == 0 {
		t.Fatal("install reported nothing wired, so this test is not measuring the arm it names")
	}
}

// --- D2: hook wiring THIS PROJECT already declares ---------------------------
//
// No flag unlocks any of these. A core.hooksPath in the repository's own config,
// or a hook git is running from its default directory, is a decision the project
// made, and formwork does not overrule it — it says what is there and how to
// call formwork from it.

// The `.husky` case, measured as the defect: install overwrote core.hooksPath
// and husky's hooks stopped running, with nothing said.
func TestInstallRefusesWiringThisRepositoryDeclares(t *testing.T) {
	dir := repo(t)
	gitT(t, dir, "config", "core.hooksPath", ".husky")
	cfg := laneCfg("pre-commit", "pre-push")
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(dir, cfg, false)
	wantErrContains(t, err, ".husky",
		"formwork check --lane pre-commit --staged", // the exact line to add, per lane
		"formwork check --lane pre-push")
	// D7's escape is for a wiring wider than this repository. This one is the
	// project's own, and offering a flag here would be offering to overrule it.
	wantErrLacks(t, err, "--override-global")
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// A WORKTREE-SCOPED core.hooksPath is the repository's own declaration, and it
// is invisible to `git config --local --get` — measured on git 2.50.1: with
// extensions.worktreeConfig on, the local read exits 1 while `rev-parse
// --git-path hooks` answers the config.worktree value. A detector reading local
// scope alone calls this somebody else's wiring; the refusal it then produces is
// D7's, which offers --override-global for a setting the project itself made.
func TestInstallRefusesWorktreeScopedWiringAsTheProjectsOwn(t *testing.T) {
	dir := repo(t)
	gitT(t, dir, "config", "extensions.worktreeConfig", "true")
	gitT(t, dir, "config", "--worktree", "core.hooksPath", ".wtpath")
	cfg := laneCfg("pre-commit")
	before := treeSnapshot(t, dir)

	_, err := hooks.Install(dir, cfg, false)
	wantErrContains(t, err, ".wtpath")
	wantErrLacks(t, err, "--override-global")
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// Setting core.hooksPath overrides the WHOLE default directory, so pointing it
// at formwork's silently disables every hook in there — including hook names
// formwork does not model.
func TestInstallRefusesOverTheHooksGitIsRunning(t *testing.T) {
	dir := repo(t)
	theirs := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(theirs, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	writeShimFile(t, filepath.Join(theirs, "commit-msg"), "#!/bin/sh\necho theirs\n", 0o755)
	cfg := laneCfg("pre-commit")
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(dir, cfg, false)
	// Both hooks, not just the one formwork would have wired: commit-msg is a
	// name formwork never installs and it stops running all the same.
	wantErrContains(t, err, theirs, "pre-commit", "commit-msg",
		"formwork check --lane pre-commit --staged")
	wantErrLacks(t, err, "--override-global")
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// R3, and it is the regression this refusal must not have. Once core.hooksPath
// points at formwork's directory those files are inert — git does not run them —
// so refusing over them would stop a repository whose shim was deleted from
// getting it back, leaving NO gate running at all.
func TestInstallRepairsAShimWhileDeadHooksSitInTheDefaultDirectory(t *testing.T) {
	dir := repo(t)
	writeShimFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	// Set by hand rather than by installing: the fixture is a repository ALREADY
	// wired to formwork, and it must not depend on the behaviour of the command
	// under test.
	gitT(t, dir, "config", "core.hooksPath", ".formwork/hooks")
	cfg := laneCfg("pre-commit")

	installed, err := hooks.Install(dir, cfg, false)
	if err != nil {
		t.Fatalf("install refused to repair a repository it already manages: %v", err)
	}
	if len(installed) != 1 || installed[0] != "pre-commit" {
		t.Fatalf("installed = %v, want [pre-commit]", installed)
	}
	if _, err := os.Stat(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
		t.Fatalf("no shim on disk after the repair: %v", err)
	}
}

// "Is this formwork's own directory" is asked of the RESOLVED path, not the
// declared string, and these are the spellings that make the difference: a
// trailing slash and an absolute path both name .formwork/hooks. Compared as
// strings they do not, and a repository spelled either way would be told its own
// formwork wiring belongs to someone else and could never be repaired.
func TestInstallRepairsAWiringDeclaredInAnotherSpelling(t *testing.T) {
	for _, spelling := range []string{".formwork/hooks/", "%ROOT%/.formwork/hooks"} {
		t.Run(spelling, func(t *testing.T) {
			dir := repo(t)
			gitT(t, dir, "config", "core.hooksPath", strings.ReplaceAll(spelling, "%ROOT%", dir))

			if _, err := hooks.Install(dir, laneCfg("pre-commit"), false); err != nil {
				t.Fatalf("install refused a repository already wired to formwork: %v", err)
			}
			if _, err := os.Stat(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
				t.Fatalf("no shim on disk: %v", err)
			}
		})
	}
}

// R4's three axes, from install's side. None of these files is protection: git
// prints a hint and runs nothing for a pre-commit the committing user cannot
// execute, and it never runs a file whose name is not a hook name. Refusing over
// them is a false positive nobody can clear — the repository looks permanently
// un-installable.
func TestInstallIsNotBlockedByFilesGitWillNotRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root passes access(X_OK) on any execute bit, so the non-executable row cannot be constructed")
	}
	dir := repo(t)
	hd := filepath.Join(dir, ".git", "hooks")
	if n := len(sampleHookNames(t, hd)); n == 0 {
		t.Fatal("this git init shipped no *.sample hooks, so the name axis is unexercised here")
	}
	writeShimFile(t, filepath.Join(hd, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o644)  // git will not run it
	writeShimFile(t, filepath.Join(hd, "pre-commit.bak"), "#!/bin/sh\necho old\n", 0o755) // name git never runs
	writeShimFile(t, filepath.Join(hd, "README"), "how we do hooks here\n", 0o755)        // ditto
	writeShimFile(t, filepath.Join(hd, ".gitkeep"), "", 0o644)                            // ditto
	if err := os.MkdirAll(filepath.Join(hd, "pre-merge-commit"), 0o755); err != nil {     // not a file
		t.Fatal(err)
	}
	cfg := laneCfg("pre-commit")

	if _, err := hooks.Install(dir, cfg, false); err != nil {
		t.Fatalf("install refused over files git will not run: %v", err)
	}
}

// R5. In a linked worktree the per-worktree git directory has no hooks of its
// own; the ones git will run live under the directory every worktree SHARES. A
// pre-flight reading the per-worktree answer concludes there is nothing there,
// installs, and switches off the operator's real pre-commit.
func TestInstallRefusesFromALinkedWorktreeOverTheSharedDirsHooks(t *testing.T) {
	dir := repo(t)
	commitEverything(t, dir)
	theirs := filepath.Join(dir, ".git", "hooks")
	writeShimFile(t, filepath.Join(theirs, "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
	wt := filepath.Join(t.TempDir(), "linked")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	cfg := laneCfg("pre-commit")
	beforeMain, beforeWt := treeSnapshot(t, dir), treeSnapshot(t, wt)

	_, err := hooks.Install(wt, cfg, false)
	wantErrContains(t, err, "pre-commit")
	wantUnchanged(t, beforeMain, treeSnapshot(t, dir))
	wantUnchangedTree(t, beforeWt, treeSnapshot(t, wt))
}

// --- the refusals at git's altitude ------------------------------------------

// theirHook blocks every commit and leaves a marker behind, so a test can tell
// "the operator's hook ran and said no" from "git failed for some other reason"
// — the distinction exit status alone cannot make. git runs hooks with the
// working directory set to the repository top level, which is where the marker
// lands.
const theirHook = "#!/bin/sh\ntouch hook-ran\nexit 1\n"

const hookMarker = "hook-ran"

// wantHookStillRuns stages a file, attempts a commit, and fails unless the
// commit was refused BY THE HOOK.
func wantHookStillRuns(t *testing.T, dir, when string) {
	t.Helper()
	marker := filepath.Join(dir, hookMarker)
	if err := os.RemoveAll(marker); err != nil {
		t.Fatal(err)
	}
	mustWriteAt(t, filepath.Join(dir, "f.txt"), when+"\n")
	gitT(t, dir, "add", "-A")
	out, err := exec.Command("git", "-C", dir, "commit", "-m", when).CombinedOutput()
	if err == nil {
		t.Fatalf("%s: git committed, so the operator's pre-commit did not run:\n%s", when, out)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("%s: the commit failed, but the hook left no marker — it was blocked for some other reason:\n%s", when, out)
	}
}

// THE POINT OF A REFUSAL IS THAT THE DEVELOPER IS NO WORSE OFF. Each fixture has
// a working hook before install runs; the refusal must leave it working, which
// is asserted with a real commit rather than by reading files back.
func TestARefusalLeavesTheOperatorsHookRunning(t *testing.T) {
	for _, tc := range []struct {
		name string
		// wire puts the fixture's own hook wiring in place and returns the
		// directory git will run hooks from, and the root install is invoked
		// with.
		wire func(t *testing.T, dir string) (hookDir, installRoot string)
	}{
		{"a subdirectory root", func(t *testing.T, dir string) (string, string) {
			sub := filepath.Join(dir, "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, ".git", "hooks"), sub
		}},
		{"the hooks git is running", func(t *testing.T, dir string) (string, string) {
			return filepath.Join(dir, ".git", "hooks"), dir
		}},
		{"a wider-scope wiring", func(t *testing.T, dir string) (string, string) {
			globalHooksPath(t, ".husky")
			return filepath.Join(dir, ".husky"), dir
		}},
		// The include row (#173, D11), and this table is where it belongs
		// because reading the config back cannot answer the question here. The
		// include sits after `[core]`, where `git config --local` writes, and
		// config is last-one-wins — so `core.hooksPath` in the body of the file
		// says nothing about which value governs. A commit that the operator's
		// hook blocks does.
		{"a wiring declared through an include", func(t *testing.T, dir string) (string, string) {
			writeShimFile(t, filepath.Join(dir, ".git", "team.cfg"), "[core]\n\thooksPath = team-hooks\n", 0o644)
			appendConfig(t, filepath.Join(dir, ".git", "config"), "[include]\n\tpath = team.cfg\n")
			return filepath.Join(dir, "team-hooks"), dir
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			hookDir, root := tc.wire(t, dir)
			writeShimFile(t, filepath.Join(hookDir, "pre-commit"), theirHook, 0o755)
			wantHookStillRuns(t, dir, "before install")

			if _, err := hooks.Install(root, laneCfg("pre-commit"), false); err == nil {
				t.Fatal("install did not refuse")
			}
			wantHookStillRuns(t, dir, "after the refusal")
		})
	}
}

// --- D7: a wiring wider than this repository ---------------------------------
//
// A machine-wide hook runner is a DEFAULT, and repo-local override is precisely
// what git provides local config for — so unlike D2 this refusal has an escape.
// Same mechanism, different owner, different answer.

// globalHooksPath gives this test process its own HOME holding one
// `.gitconfig`, which declares one core.hooksPath, and registers the assertion
// that formwork left that file BYTE-IDENTICAL.
//
// The check is a t.Cleanup rather than a line at the end of each test: "formwork
// never writes global config" is a claim about every run, and one asserted by
// remembering to assert it is prose.
//
// IT USED TO SET GIT_CONFIG_GLOBAL, which is one line shorter and is now the
// wrong fixture: that variable is a member of the family gitenv.go measures, so
// install refuses it wherever it moves the answer — and moving the answer is
// this fixture's entire job. The refusal would be about the fixture rather than
// about D7, and the tests below would pass on a message that never mentions a
// wider-scope wiring. $HOME/.gitconfig is what a machine-wide hooks runner
// actually is: a FILE, which the developer committing here reads too, and which
// no environment variable supplied. XDG_CONFIG_HOME moves with HOME because git
// reads $XDG_CONFIG_HOME/git/config as well, so leaving the caller's would let
// the machine running the tests answer for this repository.
func globalHooksPath(t *testing.T, value string) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".gitconfig")
	body := "[core]\n\thooksPath = " + value + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("git's global config is unreadable after the run: %v", err)
			return
		}
		if string(got) != body {
			t.Errorf("formwork wrote to git's global config\nbefore: %q\nafter:  %q", body, got)
		}
	})
	return path
}

// The refusal, and the two things it must not do: overrule the wider wiring
// silently, and say anything about where that wiring came from. Formwork reports
// what GIT will do in the repository it was pointed at; the operator's machine
// stays their business.
func TestInstallRefusesAWiderScopeWiring(t *testing.T) {
	dir := repo(t)
	globalPath := globalHooksPath(t, ".husky")
	cfg := laneCfg("pre-commit")
	before := treeSnapshot(t, dir)

	installed, err := hooks.Install(dir, cfg, false)
	wantErrContains(t, err,
		filepath.Join(dir, ".husky"), // git's resolved path, which is all formwork knows
		"--override-global",
		"formwork check --lane pre-commit --staged") // or chain it from the runner in charge
	wantErrLacks(t, err, globalPath, "global config", "~/.gitconfig")
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// The other direction, without which the decision is untested: the flag installs
// normally, and what it writes is REPO-LOCAL. The global file's bytes are
// asserted by globalHooksPath's cleanup.
func TestInstallWithOverrideGlobalWiresThisRepositoryOnly(t *testing.T) {
	dir := repo(t)
	globalHooksPath(t, ".husky")
	cfg := laneCfg("pre-commit")

	installed, err := hooks.Install(dir, cfg, true)
	if err != nil {
		t.Fatalf("--override-global did not install: %v", err)
	}
	if len(installed) != 1 || installed[0] != "pre-commit" {
		t.Fatalf("installed = %v, want [pre-commit]", installed)
	}
	if _, err := os.Stat(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
		t.Fatalf("no shim on disk: %v", err)
	}
	val, set, err := vcs.RepoConfig(dir, "core.hooksPath")
	if err != nil {
		t.Fatal(err)
	}
	if !set || val != ".formwork/hooks" {
		t.Fatalf("core.hooksPath in this repository = %q (set=%v), want .formwork/hooks", val, set)
	}
}

// R2's trap. `git config --get` returns the EFFECTIVE value across system,
// global and local scope, so a wider-scope core.hooksPath that happens to equal
// formwork's own directory reads as "already formwork-managed" — and install
// proceeds over a wiring this repository never declared, leaving a repository
// whose gate silently depends on one machine's global config.
func TestInstallRefusesAWiderScopeValueEqualToFormworksOwnDirectory(t *testing.T) {
	dir := repo(t)
	globalHooksPath(t, ".formwork/hooks")
	cfg := laneCfg("pre-commit")
	before := treeSnapshot(t, dir)

	_, err := hooks.Install(dir, cfg, false)
	wantErrContains(t, err, "--override-global")
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// --override-global is an answer to D7 and to nothing else. D2's refusals are
// about wiring this project declared, and a flag that quietly cleared them would
// be the overwrite this whole change exists to stop — reached by typing one
// word that says something else.
func TestOverrideGlobalDoesNotUnlockTheProjectsOwnRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire func(t *testing.T, dir string) (installRoot string)
	}{
		{"a core.hooksPath this repository declares", func(t *testing.T, dir string) string {
			gitT(t, dir, "config", "core.hooksPath", ".husky")
			return dir
		}},
		{"the hooks git is running", func(t *testing.T, dir string) string {
			writeShimFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho theirs\n", 0o755)
			return dir
		}},
		{"a subdirectory root", func(t *testing.T, dir string) string {
			sub := filepath.Join(dir, "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			return sub
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t)
			root := tc.wire(t, dir)
			before := treeSnapshot(t, dir)

			_, err := hooks.Install(root, laneCfg("pre-commit"), true)
			if err == nil {
				t.Fatal("--override-global unlocked a refusal that is not D7's")
			}
			wantUnchanged(t, before, treeSnapshot(t, dir))
		})
	}
}
