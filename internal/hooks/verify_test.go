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

// Verify's contract, stated once: it must answer "will git run formwork's gate
// here", and every test below is one state where the answer was yes-by-inspection
// and no-in-practice (or the reverse). Each is a row of #146's reproduction
// table; the table itself is in
// docs/superpowers/plans/2026-08-12-hooks-verify-cannot-verify.md.
//
// Helpers live at the bottom of the file. repo() is in hooks_test.go.

// --- helpers ---------------------------------------------------------------

func laneCfg(lanes ...string) *config.Config {
	c := &config.Config{Rules: []*config.Rule{{ID: "r"}}, RuleFiles: 1}
	for _, l := range lanes {
		c.Lanes = append(c.Lanes, config.Lane{Name: l, All: true})
	}
	return c
}

func managedDir(root string) string { return filepath.Join(root, ".formwork", "hooks") }

func mustInstall(t *testing.T, root string, cfg *config.Config) {
	t.Helper()
	if _, err := hooks.Install(root, cfg, false); err != nil {
		t.Fatalf("install: %v", err)
	}
}

// mustVerify fails the test on a git error, so a test asserting on problems can
// never mistake "verify could not ask git" for "verify found nothing".
func mustVerify(t *testing.T, root string, cfg *config.Config) []string {
	t.Helper()
	probs, err := hooks.Verify(root, cfg)
	if err != nil {
		t.Fatalf("verify returned an error: %v", err)
	}
	return probs
}

func wantProblem(t *testing.T, probs []string, sub string) {
	t.Helper()
	for _, p := range probs {
		if strings.Contains(p, sub) {
			return
		}
	}
	t.Errorf("no problem mentions %q; problems = %#v", sub, probs)
}

func wantWired(t *testing.T, probs []string) {
	t.Helper()
	if len(probs) != 0 {
		t.Fatalf("expected the hooks to verify as wired, got problems: %#v", probs)
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeShimFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile's mode applies on create only
		t.Fatal(err)
	}
}

// commitEverything makes the repo have a HEAD, which `git worktree add` needs.
func commitEverything(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", "seed.txt")
	gitT(t, dir, "commit", "-q", "-m", "seed")
}

// --- row 1: the formwork root is a repository subdirectory -----------------

// core.hooksPath is repo-relative and git resolves it from the TOP LEVEL, so
// `hooks install -C <subdir>` writes shims into a directory git will never look
// in. Both commands then agreed the wiring was healthy while no hook ran at all.
//
// THE FIXTURE IS BUILT BY HAND, and it used to call install. Install refuses a
// subdirectory root now (#146 D1), so it can no longer produce this state — but
// verify must still report it, because a binary predating that refusal, or an
// operator who set core.hooksPath themselves, leaves exactly these bytes behind.
// Same reasoning as TestPreexistingHooksIgnoresHooksGitNoLongerRuns in
// foreign_test.go: a fixture that states what it means does not depend on the
// behaviour of the command that is being changed.
func TestVerifyReportsShimsGitWillNeverFind(t *testing.T) {
	top := repo(t)
	sub := filepath.Join(top, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := laneCfg("pre-commit")
	writeShimFile(t, filepath.Join(sub, ".formwork", "hooks", "pre-commit"),
		"#!/bin/sh\nexec formwork check --lane pre-commit --staged\n", 0o755)
	gitT(t, top, "config", "core.hooksPath", ".formwork/hooks")

	probs := mustVerify(t, sub, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim git will never find")
	}
	// The problem must name the directory GIT will use, not the one formwork
	// wrote to — that is the whole diagnosis.
	wantProblem(t, probs, filepath.Join(top, ".formwork", "hooks"))
	wantProblem(t, probs, "pre-commit")
	// ONCE, not twice. `git worktree list` reports the main worktree as well,
	// and its hooks directory is the one already checked above — reported
	// through git's canonical spelling of the path rather than the operator's.
	// Deduping that is not cosmetic: while one defect produces two lines, a
	// mutation that removes one of the two checks leaves the exit code
	// unchanged and the suite cannot see it.
	if n := countProblems(probs, "no shim"); n != 1 {
		t.Errorf("one missing shim must be reported once, got %d: %#v", n, probs)
	}
}

// THE DEFAULT ROOT IS RELATIVE, and every other test in this package passes an
// absolute one. `formwork hooks verify` with no -C passes ".", so vcs.HooksPath
// joined git's answer onto "." and returned `.formwork/hooks` — while the
// worktree loop asked with git's own absolute worktree path and got the absolute
// spelling of the SAME directory. The dedupe compares those two strings, so it
// could not tell they were one directory, and the commonest invocation there is
// reported one missing shim twice.
//
// The assertion is the COUNT. Both spellings name a real defect, both exit 1,
// and an exit-code assertion cannot see the duplicate at all.
func TestVerifyFromTheDefaultRelativeRootReportsOneProblemOnce(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	if err := os.Remove(filepath.Join(managedDir(dir), "pre-commit")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir) // what the CLI does not do, and does not have to: "." is its default

	probs := mustVerify(t, ".", cfg)
	if n := countProblems(probs, "no shim"); n != 1 {
		t.Errorf("one missing shim from the default root must be reported once, got %d: %#v", n, probs)
	}
	// The diagnostic names two directories so the operator can compare them, so
	// both have to arrive in the same spelling: one absolute and one relative
	// reads as the mismatch the line exists to reveal.
	for _, p := range probs {
		which, rest, ok := strings.Cut(p, "formwork manages ")
		if !ok || !strings.HasPrefix(which, "git runs hooks from ") {
			continue
		}
		if !filepath.IsAbs(rest) {
			t.Errorf("the diagnostic compares an absolute path against a relative one: %q", p)
		}
	}
}

func countProblems(probs []string, sub string) int {
	n := 0
	for _, p := range probs {
		if strings.Contains(p, sub) {
			n++
		}
	}
	return n
}

// --- row 2: the shim is not executable --------------------------------------

// git prints a hint and runs nothing. Verify read the file's CONTENT and never
// its mode, so it certified a hook git had already declined to run.
func TestVerifyReportsANonExecutableShim(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	if err := os.Chmod(filepath.Join(managedDir(dir), "pre-commit"), 0o644); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim git will not execute")
	}
	wantProblem(t, probs, "not executable")
}

// The mode bits are not the question git asks. git runs a hook through
// access(X_OK), which for the file's OWNER consults the owner bit alone, so
// `mode&0o111 != 0` — "any execute bit anywhere" — certifies a shim the person
// committing cannot execute. Measured before the fix: at 0655 (rw-r-xr-x) Verify
// returned no problems at all, which cli.go's hooks verify prints as "hooks
// wired" at exit 0, while git declines the hook and the commit lands ungated.
//
// The state does not survive a commit, which is why it needs catching where it
// is made. Measured: `git add` records a 0655 shim as mode 100644, because git
// reads the owner-execute bit — the same bit access(X_OK) asks about — so a
// fresh clone gets 0644 and every reading of the mode agrees. It is the working
// tree of the machine where the chmod happened that goes ungated.
func TestVerifyReportsAShimTheCommittingUserCannotExecute(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root passes access(X_OK) on any execute bit, so the state cannot be constructed")
	}
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	// Group and other may execute; the owner — who is the committing user, and
	// the only one whose answer git asks for — may not.
	if err := os.Chmod(filepath.Join(managedDir(dir), "pre-commit"), 0o655); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim the committing user cannot execute")
	}
	wantProblem(t, probs, "not executable")
}

// --- row 3: the exec line is unreachable ------------------------------------

// `exit 0` above the exec line leaves the substring the old check searched for
// perfectly intact. The byte-compare is what makes "the shim invokes its lane"
// a property of the whole file rather than of one line somewhere in it.
func TestVerifyReportsAShimWhoseExecLineIsUnreachable(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	p := filepath.Join(managedDir(dir), "pre-commit")
	writeShimFile(t, p, "#!/bin/sh\n"+
		"# generated by 'formwork hooks install' — do not edit\n"+
		"exit 0\n"+
		"exec formwork check --lane pre-commit --staged\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim whose exec line cannot be reached")
	}
	wantProblem(t, probs, "does not match")
}

// --- row 10: the shim was rewritten with CRLF -------------------------------

// A CR at the end of the shebang becomes part of the interpreter name, so the
// script cannot be executed at all — fail-closed at commit time, while verify
// said the hooks were wired. The substring match survived because the CR sits
// after the text it looked for.
func TestVerifyReportsACRLFShim(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	p := filepath.Join(managedDir(dir), "pre-commit")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	writeShimFile(t, p, strings.ReplaceAll(string(b), "\n", "\r\n"), 0o755)

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a shim /bin/sh cannot execute")
	}
	wantProblem(t, probs, "does not match")
}

// --- row 4: an orphan shim for a removed lane -------------------------------

// The inverted row: git runs every executable in the hooks directory whose name
// it recognises, so a shim left behind for a deleted lane aborts every push
// forever. Verify only ever looked at the lanes it EXPECTED, so a file it did
// not expect was invisible to it.
func TestVerifyReportsAnOrphanShim(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	writeShimFile(t, filepath.Join(managedDir(dir), "pre-push"),
		"#!/bin/sh\n# generated by 'formwork hooks install' — do not edit\nexec formwork check --lane pre-push\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a directory holding a shim for a lane that no longer exists")
	}
	wantProblem(t, probs, "pre-push")
}

// A file formwork did not write is REPORTED, never treated as its own to
// remove — and a name git does not execute is not a hook at all, so the
// directory's README does not become a permanent exit 1.
func TestVerifyReportsAForeignHookFileButNotAnInertOne(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	writeShimFile(t, filepath.Join(managedDir(dir), "commit-msg"), "#!/bin/sh\necho mine\n", 0o755)
	writeShimFile(t, filepath.Join(managedDir(dir), "README"), "these are generated\n", 0o644)

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "commit-msg")
	for _, p := range probs {
		if strings.Contains(p, "README") {
			t.Errorf("a file git never executes was reported as a hook: %q", p)
		}
	}
}

// An orphan is reported ONCE. #172 split checkHooksDir into shimProblems +
// orphanProblems so install could ask the shim half alone; the orphan tail was
// left on shimProblems as well, so checkHooksDir ran the orphan half twice and
// every orphan came out doubled — once per worktree, since the worktree loop
// calls the same function. No test looked at the COUNT, so the suite was green
// over it.
//
// The count is the assertion, not the presence: TestVerifyReportsAnOrphanShim
// above passes either way.
func TestVerifyReportsAnOrphanOnce(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	writeShimFile(t, filepath.Join(managedDir(dir), "pre-push"), "#!/bin/sh\necho mine\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	n := 0
	for _, p := range probs {
		if strings.Contains(p, "pre-push") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("one undeclared hook reported %d times; problems = %#v", n, probs)
	}
}

// --- the reason on a rejected shim has to be true ---------------------------

// A shim replaced by a symlink to a valid script is rejected — install writes a
// regular file, so formwork cannot certify what the link points at. What it must
// NOT say is that git will not run it: measured, git follows the link and runs
// the target perfectly. A false reason on a true finding is still a false claim.
//
// This is deliberately a different rule from the one applied to FOREIGN hooks in
// foreign.go, which counts a symlink as a real hook. The questions differ: there
// it is "would git run this file" (yes, through a link); here it is "is this the
// file install wrote" (no).
func TestVerifyRejectsASymlinkedShimForTheRightReason(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	p := filepath.Join(managedDir(dir), "pre-commit")
	real := filepath.Join(dir, "real-shim")
	writeShimFile(t, real, "#!/bin/sh\nexec formwork check --lane pre-commit --staged\n", 0o755)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, p); err != nil {
		t.Fatal(err)
	}

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "symlink")
	for _, s := range []string{"git will not run", "not executable", "no shim"} {
		for _, pr := range probs {
			if strings.Contains(pr, s) {
				t.Errorf("the reason is false — git follows the link and runs the target: %q", pr)
			}
		}
	}
}

// --- row 8: git runs hooks from somewhere else entirely ---------------------

// core.hooksPath pointing at another tool's directory (husky's, here). The
// shims formwork wrote are intact and git will never look at them. Verify has
// to answer from the directory GIT names, not from the one formwork manages —
// which is also the only state that tells the two apart, since everywhere else
// they are the same directory.
func TestVerifyReportsWhenGitRunsHooksFromSomewhereElse(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	if err := os.MkdirAll(filepath.Join(dir, ".husky"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "config", "core.hooksPath", ".husky")

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified formwork's shims while git runs hooks from .husky")
	}
	wantProblem(t, probs, filepath.Join(dir, ".husky"))
}

// --- rows 11 and 12: the false positives ------------------------------------

// core.hooksPath spelled as the ABSOLUTE path of the same directory. git runs
// the hook correctly; the old string comparison against ".formwork/hooks"
// reported it unwired. Any tightening of a path comparison makes this worse,
// which is why the verdict is "read the files at the directory git names".
func TestVerifyAcceptsAnAbsoluteHooksPath(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	gitT(t, dir, "config", "core.hooksPath", managedDir(dir))

	wantWired(t, mustVerify(t, dir, cfg))
}

// core.hooksPath with a trailing slash. Same directory, same behaviour from
// git, and rev-parse echoes the slash straight back.
func TestVerifyAcceptsAHooksPathWithATrailingSlash(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	gitT(t, dir, "config", "core.hooksPath", ".formwork/hooks/")

	wantWired(t, mustVerify(t, dir, cfg))
}

// The verdict is FILE-LEVEL, so a hooks directory formwork does not manage
// verifies as wired when the shims in it are the ones install writes. git runs
// them; that is the whole question.
//
// This is the row-11/12 argument taken to its limit, and it is here because the
// two rows above do not reach it: an absolute core.hooksPath and one with a
// trailing slash both normalise to the SAME string as the managed directory, so
// every lexical comparison — Clean, Join, EvalSymlinks — still calls them equal.
// A comparison promoted to a verdict therefore passes rows 11 and 12 and passes
// the five healthy spellings, and nothing in the suite notices. Measured:
// appending `!sameDirPath(live, root/.formwork/hooks)` to problems left every
// test in this package green. Only a directory that is genuinely NOT the managed
// one, holding shims git will genuinely run, tells the file-level verdict apart
// from a path-level one.
func TestVerifyAcceptsShimsInADirectoryFormworkDoesNotManage(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	// The operator's own directory, holding exactly what install writes — read
	// from the managed copy so this test cannot drift from shim()'s bytes.
	b, err := os.ReadFile(filepath.Join(managedDir(dir), "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	writeShimFile(t, filepath.Join(dir, "githooks", "pre-commit"), string(b), 0o755)
	gitT(t, dir, "config", "core.hooksPath", "githooks")

	wantWired(t, mustVerify(t, dir, cfg))
}

// --- requirement 4: an unlistable hooks directory ---------------------------

// Mode 111 is traversable but not listable: every per-lane read succeeds by
// name and only the directory listing fails. Swallowing that error is #146's
// own defect reintroduced by its fix — "hooks wired", exit 0, over an orphan
// shim that aborts every push.
func TestVerifyReportsWhenGitHooksDirIsUnlistable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can list a mode-111 directory, so the state cannot be constructed")
	}
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	hd := managedDir(dir)
	writeShimFile(t, filepath.Join(hd, "pre-push"),
		"#!/bin/sh\n# generated by 'formwork hooks install' — do not edit\nexec formwork check --lane pre-push\n", 0o755)
	if err := os.Chmod(hd, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hd, 0o755) }) // or TempDir cleanup cannot remove it

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified a hooks directory it could not read, hiding an orphan shim")
	}
}

// --- R6: a git failure is an ERROR, not a wiring problem --------------------

// Pointing -C at a directory that is not a repository used to report
// "core.hooksPath is not set (run: formwork hooks install)" at exit 1 — a
// layout diagnosis invented from a tool failure. The exit-code contract puts
// engine faults at 2.
func TestVerifyOnANonRepositoryIsAGitError(t *testing.T) {
	dir := t.TempDir()
	probs, err := hooks.Verify(dir, laneCfg("pre-commit"))
	if err == nil {
		t.Fatalf("expected a git error outside a repository; problems = %#v", probs)
	}
	if len(probs) != 0 {
		t.Errorf("a git failure must not also be reported as wiring problems: %#v", probs)
	}
}

// --- R7: an inherited git environment ---------------------------------------
//
// R7 IS NO LONGER A LIST OF VARIABLE NAMES. It refused whatever was SET, which
// was wrong in both directions — it refused a healthy repository over
// GIT_CONFIG_COUNT=0, and it certified one under GIT_CONFIG_PARAMETERS, a
// variable no reading of git's documentation produces. It now measures the
// answer instead (gitenv.go), and the rows that used to assert refusal on
// presence live in gitenv_test.go asserting it on effect, in both directions.

// GIT_DIR moved the repository out from under every git command formwork ran,
// and R7 answered that by refusing to certify while it was set. internal/vcs
// answers it on EFFECT instead: it removes the variable, then checks that
// removing it did not move which repository git resolves, and refuses when it
// did (env.go). So the variable git itself sets in `submodule foreach` costs an
// operator nothing — it re-resolves identically — while this layout, where it
// names a genuinely different repository, is an error rather than a certificate.
//
// THE ERROR IS THE POINT, NOT A PROBLEM LIST. Which repository the operator
// meant is unknowable here: one is what -C names, the other is what the
// environment names. Certifying either silently is the defect, and a wiring
// problem would be a verdict about a repository formwork had picked on its own.
// Exit 2 — engine fault — is where the contract puts that.
//
// The discriminator is a SECOND repository with no shims at all: the healthy
// answer is asserted first, so a run that errors for some reason of its own
// cannot pass as if the guard had fired.
func TestVerifyRefusesUnderAnAmbientGitDirThatMovesTheRepository(t *testing.T) {
	dir, elsewhere := repo(t), repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg)) // healthy without the variable

	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))
	probs, err := hooks.Verify(dir, cfg)
	if err == nil {
		t.Fatalf("verify certified under a GIT_DIR naming another repository; problems = %#v", probs)
	}
	if len(probs) != 0 {
		t.Errorf("an unanswerable question must not also be reported as wiring problems: %#v", probs)
	}
	for _, want := range []string{"GIT_DIR", vcs.GitEnvVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q, so the operator cannot act on it", err, want)
		}
	}
}

// The injected-config spelling of the family, which is the one measured to
// override a LOCAL core.hooksPath outright: in a repository formwork wired,
// GIT_CONFIG_COUNT=1 naming `.husky` makes `rev-parse --git-path hooks` answer a
// directory nobody's plain `git commit` will use, so the whole inspection below
// it runs against the wrong directory.
//
// The healthy answer is asserted FIRST, so a run that reports a problem for some
// reason of its own cannot pass as if the guard had fired.
func TestVerifyRefusesToCertifyUnderInjectedConfigKeys(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg))

	for k, v := range map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "core.hooksPath",
		"GIT_CONFIG_VALUE_0": ".husky",
	} {
		t.Setenv(k, v)
	}

	probs := mustVerify(t, dir, cfg)
	if len(probs) == 0 {
		t.Fatal("verify certified the wiring while an injected core.hooksPath rewrote git's answer")
	}
	wantProblem(t, probs, "GIT_CONFIG_COUNT")
}

// --- the directory git names is not always one formwork manages -------------

// checkHooksDir is handed whatever `rev-parse --git-path hooks` returned, which
// before any install is git's own default. Calling that ".git/hooks" directory
// "formwork's hooks directory" makes the sentence false for every repository
// with real hooks and no formwork install — a true finding (git does run that
// file, and this config does not declare it) delivered with an invented reason.
func TestVerifyDoesNotCallGitsDefaultDirFormworksOwn(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit")
	// Deliberately no install: git runs hooks from .git/hooks, and formwork has
	// never written anything there.
	writeShimFile(t, filepath.Join(dir, ".git", "hooks", "commit-msg"), "#!/bin/sh\necho theirs\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "commit-msg") // the finding itself stands
	for _, p := range probs {
		if strings.Contains(p, "formwork's hooks directory") {
			t.Errorf("the reason is false — formwork does not manage git's default hooks directory: %q", p)
		}
	}
}

// --- requirement 8: every problem, not the first ----------------------------

// Verify's doc comment promises "every reason the hooks are not wired". Two
// independent defects in one repository must produce two lines: an operator who
// fixes the one they were shown and re-runs is the person this matters to.
func TestVerifyAccumulatesEveryProblem(t *testing.T) {
	dir := repo(t)
	cfg := laneCfg("pre-commit", "pre-push")
	mustInstall(t, dir, cfg)
	if err := os.Chmod(filepath.Join(managedDir(dir), "pre-commit"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeShimFile(t, filepath.Join(managedDir(dir), "pre-push"), "#!/bin/sh\nexit 0\n", 0o755)

	probs := mustVerify(t, dir, cfg)
	wantProblem(t, probs, "pre-commit")
	wantProblem(t, probs, "pre-push")
}

// --- the baseline that must not break ---------------------------------------

// Five spellings of a healthy root, all of which git resolves to the same
// repository. #142 r2 broke the case variant by promoting a path comparison to
// a verdict, so this is a DRIFT guard rather than a test of today's logic:
// under "read the files at the directory git names" there is no path
// comparison left to get wrong, and all five pass without effort.
//
// Its mutation is therefore not one of the row mutations — it is reintroducing
// #142 r2's comparison as a verdict: `rev-parse --show-toplevel` against
// filepath.Abs(root), symlinks resolved on both sides. Measured, that mutation
// fails only the case-variant spelling, and fails no other test in this
// package — so this test is the whole guard for it, and it is inert on a
// case-sensitive filesystem, which is what the t.Log below discloses.
//
// It is NOT the guard for promoting hooksDirDiagnostic to a verdict. Measured,
// that mutation leaves all five spellings green, because each of them makes the
// two directories compare equal; the test that catches it is
// TestVerifyAcceptsShimsInADirectoryFormworkDoesNotManage above, and its own
// comment says why nothing cheaper does.
func TestVerifyAcceptsEveryHealthySpellingOfRoot(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "h-root")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q")
	gitT(t, dir, "config", "user.email", "t@e.com")
	gitT(t, dir, "config", "user.name", "T")
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)

	alias := filepath.Join(parent, "h-alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}

	spellings := []struct{ name, root string }{
		{"plain", dir},
		{"trailing separator", dir + string(filepath.Separator)},
		{"dot-dot through a directory", filepath.Join(dir, "src", "..")},
		{"symlinked alias", alias},
	}
	if caseInsensitive(t, parent) {
		spellings = append(spellings, struct{ name, root string }{"case variant", filepath.Join(parent, "H-ROOT")})
	} else {
		// Stated rather than skipped silently: this guard exists for a macOS
		// regression, and on a case-sensitive filesystem H-ROOT is simply a
		// different path that proves nothing either way.
		t.Log("filesystem is case-sensitive: the case-variant spelling is not exercised here")
	}
	for _, sp := range spellings {
		if probs := mustVerify(t, sp.root, cfg); len(probs) != 0 {
			t.Errorf("%s spelling (%s) reported unwired: %#v", sp.name, sp.root, probs)
		}
	}
}

// caseInsensitive reports whether dir's filesystem folds case, by probing it
// rather than by assuming anything about the operating system.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "case-probe-x")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Stat(filepath.Join(dir, "case-probe-X"))
	return err == nil
}
