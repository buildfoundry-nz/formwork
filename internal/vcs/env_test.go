package vcs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// twoRepos builds two unrelated repositories, A and B, each with one committed
// file named after itself, and returns their paths. Every scrub test is a
// question of the form "root says A, the environment says B — who wins", so the
// two answers are disjoint by construction and an assertion cannot pass by
// accident.
func twoRepos(t *testing.T) (a, b string) {
	t.Helper()
	a, b = initRepo(t), initRepo(t)
	write(t, a, "a_only.go", "package a\n")
	run(t, a, "add", "-A")
	run(t, a, "commit", "-q", "-m", "a")
	write(t, b, "b_only.go", "package b\n")
	run(t, b, "add", "-A")
	run(t, b, "commit", "-q", "-m", "b")
	return a, b
}

// nestedRepos builds the relationship twoRepos cannot express: repository A,
// and inside A's working tree a plain directory `wt` that is NOT a repository.
// It returns wt and the git directory of a second repository B, which lives
// outside A entirely and tracks bad.go — a file A's .gitignore names.
//
// THE NESTING IS THE WHOLE POINT. With disjoint siblings, removing GIT_DIR
// leaves git discovering the repository at -C itself, so the caller gets an
// answer about the tree it named. With an ancestor above -C, git's ordinary
// upward discovery answers from THAT — a third repository nobody named — and
// exits 0 while doing it.
func nestedRepos(t *testing.T) (wt, gitDirB string) {
	t.Helper()
	a, b := initRepo(t), initRepo(t)
	write(t, a, ".gitignore", "bad.go\n")
	run(t, a, "add", "-A")
	run(t, a, "commit", "-q", "-m", "a")

	wt = filepath.Join(a, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, b, "bad.go", "package bad\n")
	run(t, b, "add", "-A")
	run(t, b, "commit", "-q", "-m", "b")
	return wt, filepath.Join(b, ".git")
}

// THE SCRUB MUST NOT SUBSTITUTE AN ANCESTOR'S ANSWER FOR THE ONE IT REMOVED.
//
// Scrubbing was justified on the premise that the layout it breaks fails loudly
// — `fatal: not a git repository`, exit 2. That holds only while no ancestor of
// -C is a repository. Where one is, git's upward discovery answers from the
// ancestor at exit 0, so the scrub does not remove a wrong answer, it replaces
// it with a different one and says nothing.
//
// All three consumers here are reachable because none of them calls
// EnsureTopLevel — the asymmetry that makes this a class rather than an
// instance. EnsureTopLevel does refuse THIS spelling, the ancestor one, on a
// non-empty `--show-prefix`, and StagedPaths, RangePaths and TrackedPaths
// inherit that; the ...Under family and CheckIgnored are root-relative by design
// and cannot. What that inheritance does NOT cover is the sibling and
// linked-worktree spellings, which leave the prefix empty — see env.go's
// altitude paragraph.
func TestScrubDoesNotAnswerFromAnAncestorRepository(t *testing.T) {
	wt, gitDirB := nestedRepos(t)
	t.Setenv("GIT_DIR", gitDirB)

	// #90's scan-ignore-tracked reads this, and the ancestor answers "nothing is
	// tracked under wt" where B tracks bad.go — OK where FAIL was owed.
	if got, err := vcs.TrackedUnder(wt); err == nil {
		t.Errorf("TrackedUnder = %v, want a refusal — the scrub answered from an ancestor repository", got)
	}
	// scan.gitignore reads this, and the ancestor's .gitignore prunes a file B
	// tracks — a green check over a committed violation.
	if got, err := vcs.IgnoredUnder(wt); err == nil {
		t.Errorf("IgnoredUnder = %v, want a refusal — the scrub answered from an ancestor repository", got)
	}
	// rules-for reads this, and would cite an ancestor's .gitignore line number
	// as the reason a governed file is ungoverned.
	got, err := vcs.CheckIgnored(wt, []string{"bad.go"})
	if err == nil {
		t.Fatalf("CheckIgnored = %v, want a refusal — the scrub answered from an ancestor repository", got)
	}
	// The operator has to be able to act on it: both repositories, and the hatch.
	for _, want := range []string{gitDirB, "GIT_DIR", vcs.GitEnvVar, "inherit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
}

// GIT_WORK_TREE MOVES A DIFFERENT HALF OF THE ANSWER, and a guard that compares
// only the git directory is silent on all of it.
//
// Measured on git 2.50.1: with `-C A` and GIT_WORK_TREE naming an unrelated
// directory, `rev-parse --absolute-git-dir` is BYTE-IDENTICAL with and without
// the variable — git still discovers A's own .git — while the top level moves to
// the named directory, and every answer read from the working tree moves with
// it. Here the discriminator is secret.txt, named by a .gitignore that exists
// only in the other directory: ambient, git calls it ignored and cites that
// file; scrubbed, not ignored. A path wrongly reported ignored is pruned from
// the scan, which is the fail-open direction.
func TestScrubDoesNotAnswerFromAnAmbientWorkTree(t *testing.T) {
	a := initRepo(t)
	write(t, a, "a_only.go", "package a\n")
	run(t, a, "add", "-A")
	run(t, a, "commit", "-q", "-m", "a")

	other := t.TempDir()
	write(t, other, ".gitignore", "secret.txt\n")
	write(t, other, "secret.txt", "x\n")
	write(t, a, "secret.txt", "x\n")
	t.Setenv("GIT_WORK_TREE", other)

	if got, err := vcs.CheckIgnored(a, []string{"secret.txt"}); err == nil {
		t.Errorf("CheckIgnored = %v, want a refusal — an ambient GIT_WORK_TREE supplied another directory's .gitignore", got)
	}
	if got, err := vcs.IgnoredUnder(a); err == nil {
		t.Errorf("IgnoredUnder = %v, want a refusal — an ambient GIT_WORK_TREE moved the tree being enumerated", got)
	}
}

// GIT_COMMON_DIR MOVES A THIRD THING, and moves neither of the other two.
//
// Measured on git 2.50.1: with `-C A` and GIT_COMMON_DIR naming another
// repository's git directory, BOTH `--git-dir` and `--show-toplevel` are
// byte-identical with and without the variable. `info/exclude` lives in the
// common directory, so the ignore answer moves anyway — here loose.txt is named
// only by the other repository's info/exclude, and git calls it ignored while
// citing that file.
func TestScrubDoesNotAnswerFromAnAmbientCommonDir(t *testing.T) {
	a, b := twoRepos(t)
	write(t, b, ".git/info/exclude", "loose.txt\n")
	write(t, a, "loose.txt", "x\n")
	t.Setenv("GIT_COMMON_DIR", filepath.Join(b, ".git"))

	if got, err := vcs.CheckIgnored(a, []string{"loose.txt"}); err == nil {
		t.Errorf("CheckIgnored = %v, want a refusal — an ambient GIT_COMMON_DIR supplied another repository's info/exclude", got)
	}
}

// A LINKED WORKTREE IS THE ONLY SHAPE WHERE `--git-dir` EARNS ITS PLACE, and
// without this test dropping it from the probe changed nothing anywhere.
//
// The ordinary `GIT_DIR=B/.git` case moves the git directory AND the common
// directory together, so `--git-common-dir` alone already refuses it — which is
// why the whole suite stayed green with `--git-dir` deleted from both argument
// sets. A linked worktree separates them: its git directory is
// `A/.git/worktrees/<name>` while the common directory is `A/.git`, shared with
// the main worktree. Measured on git 2.50.1, `-C A` with GIT_DIR naming the
// linked worktree's git dir:
//
//	ambient   --git-dir A/.git/worktrees/W  --git-common-dir A/.git  --show-toplevel A
//	scrubbed  --git-dir A/.git              --git-common-dir A/.git  --show-toplevel A
//
// Two of the three parts are byte-identical. The substitution behind them is
// not: the index lives in the per-worktree git directory, so ambient `ls-files`
// lists main_only.go AND w_only.go where scrubbed lists only main_only.go. That
// is TrackedUnder, which is what #90's scan-ignore-tracked reads — and the
// scrubbed answer is the SMALLER one, so a tracked file hidden by scan.ignore
// goes unreported.
func TestScrubDoesNotAnswerFromAnAmbientLinkedWorktreeGitDir(t *testing.T) {
	a := initRepo(t)
	write(t, a, "main_only.go", "package m\n")
	run(t, a, "add", "-A")
	run(t, a, "commit", "-q", "-m", "m")

	wt := filepath.Join(t.TempDir(), "W")
	run(t, a, "worktree", "add", "-q", "-b", "wbranch", wt)
	write(t, wt, "w_only.go", "package w\n")
	run(t, wt, "add", "-A")
	run(t, wt, "commit", "-q", "-m", "w")

	// Read the linked git directory from git rather than assembling
	// `.git/worktrees/W` here: the layout is git's to name, and a test that
	// hard-codes it would fail for a reason unrelated to the guard if it changed.
	out, err := exec.Command("git", "-C", wt, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		t.Fatalf("resolving the linked worktree's git dir: %v", err)
	}
	t.Setenv("GIT_DIR", strings.TrimSpace(string(out)))

	if got, err := vcs.TrackedUnder(a); err == nil {
		t.Fatalf("TrackedUnder(A) = %v, want a refusal — an ambient linked-worktree GIT_DIR moved the index while the common directory and top level stayed identical", got)
	}
}

// A BARE REPOSITORY IS WHERE THE WORK-TREE QUESTION CANNOT BE ASKED, and the
// guard must not go inert there.
//
// `rev-parse --show-toplevel` is exit 128 in a bare repository, which fails the
// whole identity invocation. BOTH ENDS MUST BE BARE for that to be the state
// under test: with a non-bare repository at either end, GIT_DIR without
// GIT_WORK_TREE makes git treat the CWD as the top level and the invocation
// succeeds there, so the exit statuses differ and the ordinary comparison
// already refuses. Bare-to-bare is the one shape where both runs fail
// identically, which the comparison would read as "nothing changed" — and it is
// not: the bare-safe retry shows the two environments naming different
// repositories.
//
// WHAT THIS CLOSES IS THE SUBSTITUTION, NOT A DEMONSTRATED WRONG FILE LIST.
// `ls-files` answers (exit 0) in a bare repository where check-ignore and
// `ls-files --others --ignored` are both exit 128, so TrackedUnder is the only
// one of the three that still runs — but a bare repository normally carries no
// index, so both answers here are empty. The claim is that formwork does not
// proceed against a repository the operator did not name, not that this fixture
// exhibits a wrong answer.
func TestScrubDoesNotGoInertWhenBothEndsAreBare(t *testing.T) {
	parent := t.TempDir()
	a, b := filepath.Join(parent, "a.git"), filepath.Join(parent, "b.git")
	run(t, parent, "init", "-q", "--bare", a)
	run(t, parent, "init", "-q", "--bare", b)
	t.Setenv("GIT_DIR", b)

	if got, err := vcs.TrackedUnder(a); err == nil {
		t.Errorf("TrackedUnder(bare a) = %v, want a refusal — the guard went inert where --show-toplevel cannot be asked", got)
	}
}

// AN ANSWER FORMWORK CANNOT READ IS NOT AGREEMENT, and git reports it at exit 0.
//
// `git rev-parse` ECHOES an option it does not recognise and exits 0 — measured
// on 2.50.1, `rev-parse --absolute-git-dirX` prints `--absolute-git-dirX`. So on
// any git missing a flag this package passes, BOTH runs come back with the same
// flag string, the comparison calls that agreement, and the guard goes silently
// inert while the scrub keeps substituting: #167 restored in full by a toolchain
// difference, with nothing in the output to notice it by.
//
// The unknown option is driven through the export_test seam because no git here
// reproduces it — this one understands every option the real question uses, and a
// test cannot install an old git. What it reproduces is the SHAPE such a git
// would present.
//
// THE FIXTURE IS OTHERWISE HEALTHY: GIT_DIR names A's own repository, which
// TestScrubIsSilentWhenGitDirNamesTheSameRepository proves is silent with the
// real question. So the refusal here can only come from the answer being
// unreadable, not from a repository disagreement.
func TestScrubRefusesAnIdentityAnswerItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		parts int
	}{
		{"unknown option is echoed", []string{"rev-parse", "--absolute-git-dirX"}, 1},
		// One part answered and one echoed, with BOTH runs agreeing — which is the
		// state an old git produces and the one the comparison alone waves through.
		// --path-format=absolute keeps the answered part identical across the two
		// environments, so nothing but the echoed line is left to refuse on.
		{"one part answered, one echoed", []string{"rev-parse", "--path-format=absolute", "--git-dir", "--not-an-option"}, 2},
		{"fewer lines than parts", []string{"rev-parse", "--path-format=absolute", "--git-dir"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := twoRepos(t)
			restore := vcs.SetRepoIdentityForTest(tc.args, tc.parts)
			defer restore()
			t.Setenv("GIT_DIR", filepath.Join(a, ".git"))

			_, err := vcs.TrackedUnder(a)
			if err == nil {
				t.Fatal("an identity answer formwork cannot read was treated as agreement")
			}
			// It must not claim the repository moved — it does not know that. The
			// honest verdict is that the question could not be asked.
			if !strings.Contains(err.Error(), "could not ask git") {
				t.Errorf("the refusal %q does not say the question could not be asked", err)
			}
			if !strings.Contains(err.Error(), vcs.GitEnvVar) {
				t.Errorf("the refusal %q does not name the hatch", err)
			}
		})
	}
}

// A QUESTION FORMWORK CANNOT ASK MUST NOT READ AS AGREEMENT — the posture the
// test above takes for an answer it cannot READ, applied to one it cannot
// OBTAIN. On a git that REFUSES an option rather than echoing it, both runs fail
// identically, so the comparison sees no difference; this arm used to return nil
// there while the caller's own call went on being answered by whatever the scrub
// landed on.
//
// THE HATCH'S GUARD ALREADY REFUSED THIS STATE
// (TestGitEnvInheritRefusesWhenGitCannotAnswerTheQuestion, hatch_test.go). Two
// functions, the same failure, opposite verdicts — this is the non-hatch half.
//
// THE LAYOUT IS THE ANCESTOR ONE, so the refusal is owed over a demonstrated
// wrong answer rather than on principle alone: measured with the shim below,
// TrackedUnder(wt) returned the ANCESTOR's answer (nothing tracked under wt)
// where B tracks bad.go, and IgnoredUnder(wt) pruned it by the ancestor's
// .gitignore — the exact fail-open TestScrubDoesNotAnswerFromAnAncestorRepository
// pins with a real git, restored in full by a toolchain difference.
func TestScrubRefusesWhenGitCannotAnswerTheQuestion(t *testing.T) {
	wt, gitDirB := nestedRepos(t)
	t.Setenv("GIT_DIR", gitDirB)
	gitRefusingPathFormat(t) // after the fixture, so setup uses the real git

	got, err := vcs.TrackedUnder(wt)
	if err == nil {
		t.Fatalf("TrackedUnder = %v with no error — a git that cannot answer the question left the guard inert", got)
	}
	if !strings.Contains(err.Error(), wt) {
		t.Errorf("the refusal does not name the root it could not answer for:\n%v", err)
	}
	if !strings.Contains(err.Error(), "GIT_DIR") {
		t.Errorf("the refusal does not name the variable whose effect it could not measure:\n%v", err)
	}
	// A CURE THAT DOES NOT CURE IS WORSE THAN NONE. The neighbouring refusals in
	// this file offer FORMWORK_GIT_ENV=inherit as the way to keep the
	// environment's answer, and in THIS state it is not one: the hatch has a guard
	// of its own that refuses the identical failure (hatch.go's mutual-failure
	// arm, pinned by TestGitEnvInheritRefusesWhenGitCannotAnswerTheQuestion), so
	// following that advice is exit 2 again. Asserted as an ABSENCE, because the
	// wording is one copy-paste away.
	if strings.Contains(err.Error(), vcs.GitEnvVar) {
		t.Errorf("the refusal offers the hatch as a cure, which refuses this same state:\n%v", err)
	}
}

// THE OTHER DIRECTION OF THE SAME GUARD: an operator who exports GIT_DIR naming
// the very repository -C resolves has changed nothing, and must not be refused.
//
// This is the case that decides `--absolute-git-dir` over `--git-dir`. Measured
// on git 2.50.1, at a repository's top level: `--git-dir` answers the relative
// ".git" when git found the repository by discovery, and echoes GIT_DIR's
// absolute spelling when the variable supplied it — one repository, two strings,
// and a comparison on that spelling refuses a layout where both runs agree.
func TestScrubIsSilentWhenGitDirNamesTheSameRepository(t *testing.T) {
	a, _ := twoRepos(t)
	t.Setenv("GIT_DIR", filepath.Join(a, ".git"))

	got, err := vcs.TrackedUnder(a)
	if err != nil {
		t.Fatalf("TrackedUnder(A) with GIT_DIR naming A's own repository: %v", err)
	}
	if want := []string{"a_only.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder(A) = %v, want %v", got, want)
	}
}

// An ambient GIT_DIR beats -C at repository resolution, so before this package
// controlled the environment every git call formwork made could be answered by
// a repository the caller never named. Measured on git 2.50.1: `GIT_DIR=B/.git
// git -C A ls-files` lists B's files. That is member 6 of #167's class at the
// seam it enters through — the gitExit exec site.
//
// THE OWED ANSWER IS A REFUSAL, NOT A SCRUBBED ANSWER, and this test asserted
// the latter until the ancestor case above showed why. Removing GIT_DIR here
// leaves git resolving A's own git directory — a different repository from the
// one the variable named — so the scrub is not inert, it picks a side. It
// happens to pick the side -C names, which is why this read as correct.
//
// SPLITTING THAT FROM THE ANCESTOR CASE WAS TRIED AND ABANDONED. `rev-parse
// --show-toplevel` is the obvious discriminator — it differs below -C and agrees
// here — but it calls a real substitution safe: measured on git 2.50.1, with -C
// at a repository top level and GIT_DIR naming another repository whose index
// force-tracks a file this one gitignores, `check` goes from exit 1 to exit 0
// while the two environments report the identical top level. A predicate that
// silent on that is not a guard.
func TestGitCallsRefuseAnAmbientGitDirThatMovesTheRepository(t *testing.T) {
	a, b := twoRepos(t)
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))

	if got, err := vcs.TrackedUnder(a); err == nil {
		t.Fatalf("TrackedUnder(A) = %v, want a refusal — GIT_DIR named a repository formwork silently discarded", got)
	}
}

// The gitStdin exec site (ignored.go) is a SEPARATE exec.Command and does not
// route through gitExit, so a guard on one leaves the other live. CheckIgnored
// is the caller that reaches it.
//
// The discriminator is B's .git/info/exclude, which lives inside the git dir
// and so travels with GIT_DIR. Measured on git 2.50.1: with GIT_DIR pointing at
// B, `git -C A check-ignore` reports A's secret.txt as ignored and attributes
// it to B's exclude file. The fail-open direction is concrete — a path wrongly
// reported ignored is pruned from the scan — and the refusal is what keeps
// either repository from answering for the other.
func TestCheckIgnoredRefusesAnAmbientGitDirThatMovesTheRepository(t *testing.T) {
	a, b := twoRepos(t)
	write(t, b, ".git/info/exclude", "secret.txt\n")
	write(t, a, "secret.txt", "x\n")
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))

	if got, err := vcs.CheckIgnored(a, []string{"secret.txt"}); err == nil {
		t.Fatalf("CheckIgnored(A, secret.txt) = %v, want a refusal — an ambient GIT_DIR supplied another repository's exclude file", got)
	}
}

// GIT_INDEX_FILE MUST SURVIVE, and this is the test that says so.
//
// A blanket scrub of git's environment would create a fail-open rather than
// close one. During a partial commit git builds a temporary index holding only
// the named paths and points GIT_INDEX_FILE at it, then runs pre-commit — so
// the index formwork must read under --staged is the one named there, not
// .git/index. This fixture reproduces that layout directly: the real index
// stages two.txt, the temporary index stages one.txt, and the sets are disjoint
// so the wrong answer cannot look like the right one. Scrubbed, StagedPaths
// reports two.txt — a file that is not in the commit — and misses one.txt,
// which is (plan A1, re-measured on git 2.50.1).
func TestGitCallsPreserveGitIndexFile(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "one.txt", "one\n")
	write(t, dir, "two.txt", "two\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "one.txt", "one changed\n")
	write(t, dir, "two.txt", "two changed\n")
	run(t, dir, "add", "two.txt") // the real index

	// Build the temporary index exactly as `git commit -- one.txt` does.
	alt := filepath.Join(t.TempDir(), "next-index")
	for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "one.txt"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+alt)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building the temp index with git %v: %v\n%s", args, err, out)
		}
	}
	t.Setenv("GIT_INDEX_FILE", alt)

	got, err := vcs.StagedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("StagedPaths = %v, want %v — GIT_INDEX_FILE did not reach git, so --staged read the wrong index", got, want)
	}
}

// The escape hatch, at BOTH exec sites, is measured in hatch_test.go — against
// the layout it exists for rather than against two sibling repositories, which
// is a shape the hatch's agreement guard now refuses.

// Any value but the one accepted spelling is an error, not a silent fallback to
// the default. Fail-closed on input formwork does not understand is this
// repository's posture everywhere else (strict YAML decoding, unknown rule ids,
// unknown -format), and the alternative here discards a deliberate act: an
// operator who typed the hatch and got a fallback would debug a git failure
// while believing the hatch was on.
//
// SET-BUT-EMPTY IS THE ONE THAT MATTERS. os.LookupEnv is what tells it from
// unset; os.Getenv cannot, and would wave it through as the default.
func TestGitEnvRefusesAnUnrecognisedValue(t *testing.T) {
	for _, val := range []string{"", "1", "true", "INHERIT", "Inherit", "inherit ", "scrub", "inherited"} {
		t.Run("value="+val, func(t *testing.T) {
			dir := initRepo(t)
			t.Setenv(vcs.GitEnvVar, val)

			if _, err := vcs.TrackedUnder(dir); err == nil {
				t.Fatalf("TrackedUnder with %s=%q returned no error, want a refusal", vcs.GitEnvVar, val)
			}
			if _, err := vcs.CheckIgnored(dir, []string{"x"}); err == nil {
				t.Fatalf("CheckIgnored with %s=%q returned no error, want a refusal", vcs.GitEnvVar, val)
			}
			// RepoConfig reaches gitExit through scopedConfig, which splits
			// git's exit status three ways and tests `code == 1` — "unset" —
			// BEFORE it tests the error. A refusal must not land in that
			// branch: "the operator's environment is unusable" and "this
			// repository does not set the key" are the two answers that must
			// never read alike, because the second is what lets install and
			// verify decide the wiring is theirs to take over.
			if _, set, err := vcs.RepoConfig(dir, "core.hooksPath"); err == nil {
				t.Fatalf("RepoConfig with %s=%q returned set=%v and no error, want a refusal", vcs.GitEnvVar, val, set)
			}
			notice, err := vcs.GitEnvNotice()
			if err == nil {
				t.Fatalf("GitEnvNotice with %s=%q returned no error, want a refusal", vcs.GitEnvVar, val)
			}
			if notice != "" {
				t.Errorf("GitEnvNotice returned both a notice %q and an error", notice)
			}
			if !strings.Contains(err.Error(), "inherit") {
				t.Errorf("the refusal %q does not name the accepted spelling, so it does not tell the operator how to cure it", err)
			}
		})
	}
}

// The notice is a fact about policy, and vcs owns the fact while the CLI owns
// the printing — this package writes to no stream of its own. It must be empty
// when the hatch is off, or an operator would learn to ignore it.
func TestGitEnvNotice(t *testing.T) {
	t.Run("unset is silent", func(t *testing.T) {
		os.Unsetenv(vcs.GitEnvVar)
		notice, err := vcs.GitEnvNotice()
		if err != nil {
			t.Fatal(err)
		}
		if notice != "" {
			t.Fatalf("GitEnvNotice with %s unset = %q, want silence", vcs.GitEnvVar, notice)
		}
	})

	t.Run("inherit announces", func(t *testing.T) {
		t.Setenv(vcs.GitEnvVar, "inherit")
		notice, err := vcs.GitEnvNotice()
		if err != nil {
			t.Fatal(err)
		}
		// An operator who set this in CI months ago has to be able to read the
		// line and know what it changed, so it names both the variable that
		// turned it on and the variables that are consequently in force.
		for _, want := range []string{vcs.GitEnvVar, "inherit", "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"} {
			if !strings.Contains(notice, want) {
				t.Errorf("notice %q does not mention %q", notice, want)
			}
		}
	})
}

// THE TWO OPERATOR-FACING TEXTS MUST NOT CLAIM THE SCRUB REACHES EVERY GIT
// COMMAND FORMWORK RUNS (#335). It does not, and this package's own comment said
// so — "ONE COMMAND SHORT OF TRUE" — while deferring the correction to #177,
// which has since closed COMPLETED along with #213.
//
// WHAT AN OPERATOR IS TOLD AND WHAT THEY GET. internal/rules/command builds its
// own exec.Command with cmd.Dir and no cmd.Env, so a `command:` rule's tool runs
// with these variables exactly as the operator set them. Measured through a
// throwaway repository whose only rule is a `command:` rule echoing its
// environment: with GIT_DIR and GIT_WORK_TREE set to that repository, the tool
// printed both intact and the rule was not refused.
// internal/rules/command's TestCommandStillInheritsTheEnvironment pins that
// behaviour, which is correct — what #213 added there is a REFUSAL, not a scrub,
// so the sentence describing a scrub was never true of a command rule and is
// stale in the other direction now as well.
//
// THE ASSERTION IS TWO-SIDED, and the negative half is the one that matters.
// "removed from the environment of every git command formwork runs" is the
// sentence an operator reads as "my command rule's git is scrubbed"; requiring
// only that the carve-out be present would leave the false universal standing
// beside it, and a future edit restores exactly that sentence by reflex. Both
// spellings this package has ever used are refused.
func TestTheHatchTextDoesNotClaimTheScrubReachesACommandRule(t *testing.T) {
	falseUniversals := []string{
		"every git command formwork runs",
		"the git commands formwork runs",
	}
	// The carve-out docs/quickstart.md already makes to operators, in the same
	// words, so the terminal and the guide cannot describe different products.
	carveOut := []string{"`command:` rule", "the environment as you set it"}

	t.Run("the refusal for an unrecognised value", func(t *testing.T) {
		t.Setenv(vcs.GitEnvVar, "Inherit")
		notice, err := vcs.GitEnvNotice()
		if err == nil {
			t.Fatalf("GitEnvNotice with %s=%q returned notice %q and no error, want a refusal to read", vcs.GitEnvVar, "Inherit", notice)
		}
		assertHatchTextCarvesOutCommandRules(t, "the "+vcs.GitEnvVar+" refusal", err.Error(), falseUniversals, carveOut)
	})

	t.Run("the notice under the hatch", func(t *testing.T) {
		t.Setenv(vcs.GitEnvVar, "inherit")
		notice, err := vcs.GitEnvNotice()
		if err != nil {
			t.Fatal(err)
		}
		if notice == "" {
			t.Fatalf("GitEnvNotice under the hatch returned nothing, so there is no text here to judge")
		}
		assertHatchTextCarvesOutCommandRules(t, "the "+vcs.GitEnvVar+"=inherit notice", notice, falseUniversals, carveOut)
	})
}

func assertHatchTextCarvesOutCommandRules(t *testing.T, what, text string, falseUniversals, carveOut []string) {
	t.Helper()
	for _, claim := range falseUniversals {
		if strings.Contains(text, claim) {
			t.Errorf("%s says the variables are removed from %q. internal/rules/command sets no cmd.Env, so a `command:` rule's tool runs with every one of them as the operator set them — the sentence is false, and it is the one an operator reads as protection they do not have.\n%s", what, claim, text)
		}
	}
	for _, want := range carveOut {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain %q, so it does not tell the operator which git commands the sentence excludes.\n%s", what, want, text)
		}
	}
}

// THE LOAD-BEARING JUSTIFICATION FOR SCRUBBING GIT_DIR AT ALL.
//
// git originates GIT_DIR in exactly one place — `submodule foreach`, which sets
// it to the relative `.git` and runs the command in the submodule's working
// tree. Scrubbing is safe there only because git re-discovers the same
// repository through the gitlink, and this test measures that rather than
// asserting it: the scrubbed answer must equal the answer the hatch produces
// from the untouched environment, and be non-empty so two failures cannot
// agree vacuously.
func TestSubmoduleForeachEnvironmentSurvivesTheScrub(t *testing.T) {
	super, sub := initRepo(t), initRepo(t)
	write(t, sub, "s.go", "package sub\n")
	run(t, sub, "add", "-A")
	run(t, sub, "commit", "-q", "-m", "sub")
	write(t, super, "t.go", "package top\n")
	run(t, super, "add", "-A")
	run(t, super, "commit", "-q", "-m", "top")
	// file:// submodules are refused by default since the 2022 security fix, and
	// arming the guard is what this -c does.
	//
	// IT MUST BE -c, NOT THE SUPERPROJECT'S CONFIG. Measured on git 2.50.1, over
	// this exact fixture: with `git -C super config protocol.file.allow always`
	// and nothing else, `submodule add` is exit 128 `fatal: transport 'file' not
	// allowed`; with -c on the command it is exit 0. The clone `submodule add`
	// spawns does not consult the superproject's config file, and -c reaches it
	// because git exports -c settings to its child processes in
	// GIT_CONFIG_PARAMETERS (measured: a pre-commit hook run under
	// `git -c protocol.file.allow=always commit` sees it there).
	//
	// That first spelling was what this test used. It is a CONFIG-SCOPING fault
	// rather than a platform one, so it failed identically everywhere and the
	// skip below took the whole test out of every run this branch has had.
	//
	// A FAILURE HERE IS FATAL, NOT A SKIP. This is the only measurement behind
	// scrubbing GIT_DIR at all (env.go), and a t.Skipf on `submodule add` is
	// reachable for reasons that have nothing to do with the environment — as it
	// just was — while `go test` prints a skipped package as `ok`. There is no
	// substrate question left for a skip to answer: every test in this package
	// already requires git, and the -c above is what the transport needed.
	add := exec.Command("git", "-c", "protocol.file.allow=always", "-C", super, "submodule", "add", "-q", sub, "vendored")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("submodule add failed, so the justification for scrubbing GIT_DIR is unmeasured: %v\n%s", err, out)
	}
	run(t, super, "commit", "-q", "-m", "addsub")
	vendored := filepath.Join(super, "vendored")

	// Exactly what `submodule foreach` exports: GIT_DIR=.git, relative, with the
	// command running in the submodule's working tree.
	t.Setenv("GIT_DIR", ".git")

	scrubbed, err := vcs.TrackedUnder(vendored)
	if err != nil {
		t.Fatalf("scrubbed: %v", err)
	}
	t.Setenv(vcs.GitEnvVar, "inherit")
	ambient, err := vcs.TrackedUnder(vendored)
	if err != nil {
		t.Fatalf("ambient: %v", err)
	}
	if len(scrubbed) == 0 {
		t.Fatal("scrubbed answer is empty — the comparison below would pass vacuously")
	}
	if !reflect.DeepEqual(scrubbed, ambient) {
		t.Fatalf("scrubbed = %v, ambient = %v — scrubbing changed what `submodule foreach` sees", scrubbed, ambient)
	}
}
