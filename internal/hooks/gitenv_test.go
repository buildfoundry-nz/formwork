package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/hooks"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// The ambient git configuration environment, at both commands (#167, D9).
//
// Verify used to refuse on PRESENCE: seven variable names, os.LookupEnv, refuse
// if any is set. That was wrong in both directions at once, and this file pins
// both corrections — the variable no list named walks past it, and the variables
// git ignores walk into it. Install had no guard at all, which is the half that
// wrote to the repository.

// paramsRepo is the reproduction from #167 in fixture form: a repository whose
// own config names `.husky`, with a hook there that git really runs, and an
// environment whose `-c` propagation says the hooks live in formwork's directory
// instead. Measured on git 2.50.1: `rev-parse --git-path hooks` answers the
// environment while `git config --local --get core.hooksPath` answers `.husky`,
// which is exactly the disagreement install's D2 decision rests on not having.
func paramsRepo(t *testing.T) string {
	t.Helper()
	dir := repo(t)
	gitT(t, dir, "config", "core.hooksPath", ".husky")
	writeShimFile(t, filepath.Join(dir, ".husky", "pre-commit"), theirHook, 0o755)
	return dir
}

// setParams points git's command-line config channel at value, and returns the
// function that takes it back off — needed because the environment reaches every
// git in this process, including the real `git commit` a test makes to prove the
// operator's hook still works.
func setParams(t *testing.T, value string) (unset func()) {
	t.Helper()
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='"+value+"'")
	return func() {
		if err := os.Unsetenv("GIT_CONFIG_PARAMETERS"); err != nil {
			t.Fatal(err)
		}
	}
}

// Member 1 of the class: install took over a live husky wiring at exit 0. The
// two questions the D2 decision compares were answered by different
// configurations, so install read its own directory in one and the project's in
// the other, and concluded it was repairing its own wiring.
func TestInstallRefusesWhenTheEnvironmentMovesGitsAnswer(t *testing.T) {
	dir := paramsRepo(t)
	before := treeSnapshot(t, dir)
	unset := setParams(t, ".formwork/hooks")

	_, err := hooks.Install(dir, laneCfg("pre-commit"), false)
	wantErrContains(t, err, "GIT_CONFIG_PARAMETERS", "where git runs hooks", ".husky", ".formwork/hooks")
	wantUnchanged(t, before, treeSnapshot(t, dir))

	// No worse off, asserted in the environment the developer actually commits
	// in: theirs is the one without the variable, and their hook still runs.
	unset()
	wantHookStillRuns(t, dir, "after the refusal")
}

// --override-global does not clear it, on D2's argument rather than by accident:
// the flag answers "this repository is different from what my machine says", and
// nothing about an environment that moves git's answer is a statement about this
// repository at all.
func TestOverrideGlobalDoesNotClearTheEnvironmentRefusal(t *testing.T) {
	dir := paramsRepo(t)
	before := treeSnapshot(t, dir)
	setParams(t, ".formwork/hooks")

	_, err := hooks.Install(dir, laneCfg("pre-commit"), true)
	wantErrContains(t, err, "GIT_CONFIG_PARAMETERS")
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// Member 2: the same environment at the read command. The fixture is built so
// that certifying is a REAL fail-open and not merely a wrong sentence — the
// shims git would run are missing, and the ones the environment points verify at
// are byte-perfect.
func TestVerifyRefusesToCertifyWhenTheEnvironmentMovesGitsAnswer(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	shim := readShim(t, dir, "pre-commit")

	// What git will really run here: nothing. The shim is gone.
	if err := os.Remove(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
		t.Fatal(err)
	}
	wantProblem(t, mustVerify(t, dir, cfg), "no shim at") // the state, before any environment

	// What the environment makes git report: a directory holding exactly the
	// bytes install writes.
	writeShimFile(t, filepath.Join(dir, ".husky", "pre-commit"), shim, 0o755)
	setParams(t, ".husky")

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a repository whose gate is missing, because the environment pointed it at a different directory")
	}
	wantProblem(t, probs, "GIT_CONFIG_PARAMETERS")
}

// --- the measurement's root is the root it is asked about ---------------------

// wtScopedRepo is a repository whose LINKED worktree carries a core.hooksPath of
// its own, which only git's per-worktree config can express.
//
// The fixture is built so that certifying it is a real fail-open: what git will
// run in the linked worktree is nothing (`.husky` there is empty), while the
// directory the environment redirects the question to holds a byte-perfect shim.
// The main worktree is left genuinely, unconditionally wired — that is what makes
// the environment's effect on it nil and the effect on the worktree invisible to
// a measurement taken at the main worktree.
func wtScopedRepo(t *testing.T) (dir, wt string, cfg *config.Config) {
	t.Helper()
	dir = repo(t)
	cfg = laneCfg("pre-commit")
	commitEverything(t, dir) // a HEAD, which `git worktree add` needs
	mustInstall(t, dir, cfg)
	wt = filepath.Join(t.TempDir(), "linkedwt")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")

	// extensions.worktreeConfig is what makes `--worktree` a scope git will read;
	// without it the write below is `fatal: --worktree cannot be used with
	// multiple working trees`.
	gitT(t, dir, "config", "extensions.worktreeConfig", "true")
	gitT(t, wt, "config", "--worktree", "core.hooksPath", ".husky")

	// The shims stayed untracked (commitEverything ran first), so the branch this
	// worktree is on does not carry them; this puts them there by hand, which is
	// what gives the redirected answer something to certify.
	writeShimFile(t, filepath.Join(wt, ".formwork", "hooks", "pre-commit"), readShim(t, dir, "pre-commit"), 0o755)
	return dir, wt, cfg
}

// wantOneProblem requires a SINGLE problem carrying every substring.
//
// wantProblem called several times would be satisfied by several DIFFERENT
// lines, and for the test below that is the failure mode to exclude: the
// root-level finding names the variable, and a line naming the variable is not
// evidence that any worktree was measured.
func wantOneProblem(t *testing.T, probs []string, subs ...string) {
	t.Helper()
	for _, p := range probs {
		all := true
		for _, s := range subs {
			if !strings.Contains(p, s) {
				all = false
				break
			}
		}
		if all {
			return
		}
	}
	t.Errorf("no single problem mentions all of %q; problems = %#v", subs, probs)
}

// The measurement was per-QUESTION but the answers verify acts on are
// per-ROOT: it measured `--git-path hooks` at root and then called
// vcs.HooksPath on every linked worktree's path, unmeasured.
//
// A value EQUAL to root's own leaves root's two answers byte-identical, so
// nothing is reported there, while the linked worktree's answer moves off the
// `.husky` git would really use. Measured on git 2.50.1 against the binary built
// from this branch: `formwork hooks verify` printed "hooks wired" at exit 0 over
// a worktree where a plain `git commit` runs no gate at all — the same command
// without the variable is exit 1, naming the missing shim.
//
// THE ASSERTION IS ABOUT THE WORKTREE'S OWN ANSWER, in one line, for the reason
// wantOneProblem gives: the control below shows a root-level finding is
// available for the wrong test to pass on.
func TestVerifyRefusesToCertifyWhenTheEnvironmentMovesALinkedWorktreesAnswer(t *testing.T) {
	dir, wt, cfg := wtScopedRepo(t)
	// The state, before any environment: the worktree's gate is missing.
	wantProblem(t, mustVerify(t, dir, cfg), filepath.Join(".husky", "pre-commit"))

	setParams(t, ".formwork/hooks") // byte-equal to root's own local value

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a repository whose linked worktree runs no gate, because the environment moved an answer it never measured there")
	}
	wantOneProblem(t, probs, filepath.Base(wt), "GIT_CONFIG_PARAMETERS", ".husky", ".formwork/hooks")
}

// THE SAME DEFECT AT THE OTHER SPELLING OF core.hooksPath, and the spelling is
// what makes it reachable.
//
// The test above sets the RELATIVE `.formwork/hooks`, which vcs.HooksPath joins
// against each worktree's own path — so root's hooks directory and the linked
// worktree's redirected one are different directories, their dedupe keys never
// collide, and the loop reaches the measurement. An ABSOLUTE core.hooksPath is
// first-class supported surface (verify.go's header: it "stops being a special
// case"), and it names ONE directory for every worktree. Pointed at root's own
// resolved hooks directory, the environment moves the linked worktree's answer
// onto root's key, the seen-set skips the worktree, and the measurement added to
// catch exactly this never runs. Root's own measurement cannot cover it: root's
// local value already IS that path, so its ambient and scrubbed answers are
// byte-identical and it reports nothing.
//
// Measured on git 2.50.1 with the measurement below the dedupe, which is where
// it was: hooks.Verify returned NO problems at all for this fixture — a worktree
// whose gate git resolves to `.husky`, where no shim exists — while the same
// fixture with a clean environment reports that missing shim.
func TestVerifyRefusesToCertifyWhenTheEnvironmentMovesALinkedWorktreesAnswerOntoRootsHooksDirectory(t *testing.T) {
	dir, wt, cfg := wtScopedRepo(t)
	// The absolute spelling of the directory install just wired — same directory,
	// different string, and the one every worktree resolves identically.
	abs := managedDir(dir)
	gitT(t, dir, "config", "core.hooksPath", abs)
	// The state, before any environment: the worktree's gate is missing.
	wantProblem(t, mustVerify(t, dir, cfg), filepath.Join(".husky", "pre-commit"))

	setParams(t, abs) // root's resolved hooks directory, which is what collides

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a repository whose linked worktree runs no gate, because the environment moved that worktree's answer onto root's dedupe key")
	}
	wantOneProblem(t, probs, filepath.Base(wt), "GIT_CONFIG_PARAMETERS", ".husky", abs)
}

// The control, and it is what stops the test above passing for the wrong reason.
// Same repository, same worktree, same variable — pointed at a value that moves
// ROOT's answer too. That case was already reported, by the root-level guard
// alone, so this must stay green when the per-worktree measurement is removed.
func TestVerifyReportsTheEnvironmentWhenItMovesRootsAnswerToo(t *testing.T) {
	dir, _, cfg := wtScopedRepo(t)
	setParams(t, ".other")

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "GIT_CONFIG_PARAMETERS")
}

// THE OTHER HALF OF MOVING THE MEASUREMENT ABOVE THE DEDUPE: `worktree list`
// reports the MAIN worktree as an entry of its own, so a measurement taken for
// every entry asks at root's directory a second time and renders the finding
// twice — once unlabelled, once prefixed with the main worktree's path. Measured
// against this loop before the second seen-set: exactly that pair. One state,
// one line.
func TestVerifyReportsAMovedRootAnswerOnce(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg)) // healthy before the variable

	setParams(t, ".other")

	var n int
	probs := mustVerify(t, dir, cfg)
	for _, p := range probs {
		if strings.Contains(p, "changes what git reports for where git runs hooks") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the environment moved one answer at one directory, reported %d times; problems = %#v", n, probs)
	}
}

// --- GIT_DIR: closed at the seam, not by a second guard here ------------------

// Member 5 used GIT_DIR to empty `rev-parse --show-prefix`, so install accepted
// a subdirectory root, wrote shims there, and set a core.hooksPath git resolves
// from the real top level to a directory that does not exist. What must hold is
// that install REFUSES AND WRITES NOTHING; the diagnosis it reaches for is not
// the guarantee.
//
// THE REASON CHANGED, AND THIS SAYS SO RATHER THAN PINNING THE OLD ONE. The
// refusal used to come from the subdirectory pre-flight, restored by removing
// GIT_DIR. It now comes one layer lower, from vcs: setting GIT_DIR without
// GIT_WORK_TREE makes git treat the CWD as the top level, so with the variable
// git reports the work tree `sub` and without it the repository root — the two
// answers disagree, and vcs refuses to pick one. That refusal is unavoidably
// first, because the subdirectory pre-flight has to ask git a question to reach
// its own verdict, and every such question now runs through the guard.
//
// Under an ambient GIT_DIR there is no layout left where the subdirectory
// diagnosis could fire instead: a subdirectory root is exactly what makes the
// two top levels disagree. The assertions below are therefore the ones that
// carry member 5 — non-zero, nothing written — plus enough of the message for an
// operator to act.
func TestInstallStillRefusesASubdirectoryUnderAnAmbientGitDir(t *testing.T) {
	dir := repo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))

	_, err := hooks.Install(sub, laneCfg("pre-commit"), false)
	if err == nil {
		t.Fatal("install accepted a subdirectory root under an ambient GIT_DIR")
	}
	wantErrContains(t, err, "GIT_DIR")
	wantErrContains(t, err, vcs.GitEnvVar)
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// THE HATCH USED TO CARRY INSTALL INTO A SECOND REPOSITORY. With
// FORMWORK_GIT_ENV=inherit the environment is honoured and the scrub's guard is
// skipped, so install wrote its shims at the -C tree while SetConfig wrote
// core.hooksPath into the repository GIT_DIR named.
//
// Measured on the binary built from this branch before the agreement guard:
// exit 0, "installed git hooks: pre-commit"; -C's repository had no
// core.hooksPath at all, and the OTHER repository — never named on the command
// line — had one pointing at a directory that does not exist there. Both
// repositories end up ungated, and the second one's own hooks are switched off.
//
// BOTH TREES ARE ASSERTED UNCHANGED. Asserting only about -C's would leave the
// half of the defect that writes somewhere else entirely unmeasured.
func TestInstallRefusesWhenTheHatchNamesAnotherRepository(t *testing.T) {
	dir, other := repo(t), repo(t)
	beforeDir, beforeOther := treeSnapshot(t, dir), treeSnapshot(t, other)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv(vcs.GitEnvVar, "inherit")

	installed, err := hooks.Install(dir, laneCfg("pre-commit"), false)
	wantErrContains(t, err, "GIT_DIR", vcs.GitEnvVar)
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, beforeDir, treeSnapshot(t, dir))
	wantUnchanged(t, beforeOther, treeSnapshot(t, other))
}

// AN ENVIRONMENT REFUSAL IS EXIT 2 WHEREVER IT ARRIVES, including through the
// one git failure verify is allowed to render as a problem line.
//
// That exception exists for a prunable worktree, and its justification is that
// git has already diagnosed the registration itself at exit 0 — so its refusal
// to answer for the directory is that diagnosis. For an error raised by
// formwork's own environment policy the justification is simply false: git was
// never run. GIT_DIR naming root's own repository is coherent AT ROOT (verify's
// root-level guards stay silent, which this fixture depends on) and divergent at
// a linked worktree, whose scrubbed resolution is its own git directory — so the
// refusal lands exactly at the HooksPath call the exception wraps.
//
// The control is TestVerifyReportsAPrunableWorktreeWhoseDirectoryIsStillThere
// (foreign_test.go): the same fixture WITHOUT the variable stays exit 1 with
// git's own reason on a problem line, so this pair is what separates "git could
// not answer for a dead registration" from "formwork refused to ask".
func TestVerifyErrorsWhenTheEnvironmentRefusesAtAPrunableWorktree(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	commitEverything(t, dir)
	mustInstall(t, dir, cfg)
	wt := filepath.Join(t.TempDir(), "orphaned")
	gitT(t, dir, "worktree", "add", "-q", wt, "-b", "other")
	// Prunable with every file still in place: git's verdict is on the
	// REGISTRATION, so the directory is there for HooksPath to be asked about.
	if err := os.Remove(filepath.Join(wt, ".git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))

	probs, err := hooks.Verify(dir, cfg)
	if err == nil {
		t.Fatalf("verify rendered an environment refusal as a wiring problem (exit 1); problems = %#v", probs)
	}
	wantErrContains(t, err, "GIT_DIR", vcs.GitEnvVar)
}

// The subdirectory pre-flight itself, with NO ambient variable — the control
// that keeps the test above from being the only evidence that install refuses a
// subdirectory at all. Without this pair, deleting subdirectoryRefusal would
// leave a green suite.
func TestInstallRefusesASubdirectory(t *testing.T) {
	dir := repo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)

	_, err := hooks.Install(sub, laneCfg("pre-commit"), false)
	wantErrContains(t, err, "must run at the repository top level")
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// The other direction, and the reason presence is the wrong test. Each of these
// is set, and each changes nothing git does — measured on git 2.50.1, every row
// leaves `rev-parse --git-path hooks` byte-identical. The presence guard refused
// a healthy repository over them: exit 1, "formwork cannot certify this wiring",
// for a wiring that was perfectly intact.
func TestVerifyCertifiesUnderVariablesGitIgnores(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"GIT_CONFIG_COUNT", "0"},
		{"GIT_CONFIG_NOSYSTEM", ""},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			dir := repo(t)
			cfg := laneCfg("pre-commit")
			mustInstall(t, dir, cfg)
			wantWired(t, mustVerify(t, dir, cfg)) // healthy before the variable

			t.Setenv(tc.name, tc.value)
			wantWired(t, mustVerify(t, dir, cfg))
		})
	}
}

// A replaced global config file is the interesting middle case: it is a member
// of the family, it genuinely can move the answer, and here it does not — it
// declares something with nothing to do with hooks, and this repository's own
// core.hooksPath outranks it either way. Refusing on presence made every test
// fixture that needed a global config unable to run verify at all.
func TestVerifyCertifiesUnderAGlobalConfigThatChangesNothingRelevant(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg))

	f := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(f, []byte("[user]\n\tname = someone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", f)

	wantWired(t, mustVerify(t, dir, cfg))
}

// And the one that pins the difference between "a variable is set" and "the
// answer moved": the SAME variable, pointed at a file that does name a hooks
// directory, is a refusal.
func TestVerifyRefusesAGlobalConfigThatMovesTheHooksPath(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	// No install: with no local core.hooksPath there is nothing to outrank the
	// global file, which is the state where replacing that file changes git's
	// answer.
	f := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(f, []byte("[core]\n\thooksPath = .husky\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", f)

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "GIT_CONFIG_GLOBAL")
	for _, p := range probs {
		if strings.Contains(p, f) {
			t.Errorf("the refusal names a path on the operator's machine, which is their business and not formwork's to report: %q", p)
		}
	}
}

// --- #179: the hatch's positive exemption, and the wiring that outlives it ----

// UNDER THE HATCH, EVERY INSTALL-SIDE GUARD IS ANSWERED CORRECTLY AND THE GATE
// STILL NEVER RUNS. With GIT_WORK_TREE naming a SUBDIRECTORY of the repository,
// git reports that subdirectory as the work tree, `--show-prefix` is legitimately
// empty, and D1 and the write-target refusal both agree root is the top level —
// of the repository git was asked about. Install then writes shims under the
// subdirectory, writes the repo-relative core.hooksPath into the repository's
// config, and exits 0.
//
// A plain `git commit` carries none of those variables. It resolves that relative
// value from the REAL top level, finds nothing, and commits ungated. Measured on
// git 2.50.1 before this refusal: exit 0, "installed git hooks: pre-commit", and
// a violating commit accepted at exit 0 with no rule run.
//
// THE DESIGN CALL IS TO REFUSE, not to install at the top level instead. Install
// answers about the repository -C names, which is what the hatch's own policy
// says (`hatch.go`: "the hatch means honour the environment I set, not answer
// about a repository I did not name") — and writing into a directory the operator
// did not point at, on the strength of an environment variable, is that second
// thing. There is also nothing here to repair silently: the operator has spelled
// a work tree the repository itself does not have.
//
// THE QUESTION IS GIT'S OWN REGISTRY, WHICH THE ENVIRONMENT DOES NOT MOVE.
// Measured on git 2.50.1 in exactly this layout, `worktree list --porcelain`
// answers the repository's real top level and not the value of GIT_WORK_TREE —
// so no scrubbed second run is needed to ask what a plain `git commit` would
// resolve. It is the same list verify already walks.
func TestInstallRefusesAWorkTreeTheRepositoryDoesNotRegister(t *testing.T) {
	dir := repo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	t.Setenv("GIT_WORK_TREE", sub)
	t.Setenv(vcs.GitEnvVar, "inherit")

	installed, err := hooks.Install(sub, laneCfg("pre-commit"), false)
	if err == nil {
		t.Fatal("install wired a subdirectory the repository does not register as a worktree, so a plain `git commit` runs no gate")
	}
	// Both directories, so the operator can see which two disagreed — and the
	// hatch, because unsetting it is one of the two ways out.
	wantErrContains(t, err, resolved(t, dir), sub, vcs.GitEnvVar)
	if len(installed) != 0 {
		t.Errorf("a refusal reported hooks it installed: %v", installed)
	}
	wantUnchanged(t, before, treeSnapshot(t, dir))
}

// THE LAYOUT THE HATCH EXISTS FOR MUST STILL INSTALL. A bare repository plus a
// detached work tree is the case #167 D10 kept the hatch for, and it has no other
// spelling: the work tree holds no .git, so the variables are the only thing that
// names it. git's worktree registry there lists the BARE entry and no work tree
// at all, which is precisely how it differs from the subdirectory above — there
// the registry names a work tree, and it is not root.
//
// Without this pair the refusal above could be satisfied by refusing the hatch
// outright, which #167 D10 rejected.
func TestInstallStillWiresADetachedWorkTreeUnderTheHatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	bare := filepath.Join(parent, "bare.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(parent, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", bare)
	t.Setenv("GIT_WORK_TREE", wt)
	t.Setenv(vcs.GitEnvVar, "inherit")

	installed, err := hooks.Install(wt, laneCfg("pre-commit"), false)
	if err != nil {
		t.Fatalf("install refused the detached work tree the hatch exists for: %v", err)
	}
	if len(installed) != 1 || installed[0] != "pre-commit" {
		t.Fatalf("installed = %v, want [pre-commit]", installed)
	}
	if _, err := os.Stat(filepath.Join(wt, ".formwork", "hooks", "pre-commit")); err != nil {
		t.Fatalf("no shim on disk: %v", err)
	}
}
