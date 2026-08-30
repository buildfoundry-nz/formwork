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

// detachedWorkTree builds THE layout the hatch exists for — a bare repository
// and a work tree that does not contain it, joined only by GIT_DIR and
// GIT_WORK_TREE — and returns the work tree, with both variables set.
//
// It is a fixture and a claim at once: `wt` holds no .git of any kind, so
// scrubbed the two variables there is no repository at that path at all. That
// is what makes the layout inexpressible without the hatch.
func detachedWorkTree(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	bare := filepath.Join(base, "bare.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", bare)
	t.Setenv("GIT_WORK_TREE", wt)

	write(t, wt, "wt_only.go", "package wt\n")
	run(t, wt, "add", "-A")
	run(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "-c", "commit.gpgsign=false",
		"commit", "-q", "-m", "wt")
	return wt
}

// THE CONTROL, AND THE POLICY'S WHOLE JUSTIFICATION. The agreement guard below
// is only defensible while this layout keeps working under the hatch, because
// it has no other spelling — so it is measured here rather than reasoned about.
//
// It replaces a fixture that used two ordinary sibling repositories and asserted
// that the hatch made -C A answer from B. That shape is the one the guard now
// refuses, and it never was the layout this comment describes.
func TestGitEnvInheritHonoursADetachedWorkTree(t *testing.T) {
	t.Run("gitExit", func(t *testing.T) {
		wt := detachedWorkTree(t)
		t.Setenv(vcs.GitEnvVar, "inherit")

		got, err := vcs.TrackedUnder(wt)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"wt_only.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("TrackedUnder = %v, want %v — the hatch did not restore the ambient environment", got, want)
		}
	})

	t.Run("gitStdin", func(t *testing.T) {
		wt := detachedWorkTree(t)
		write(t, wt, ".gitignore", "secret.txt\n")
		write(t, wt, "secret.txt", "x\n")
		t.Setenv(vcs.GitEnvVar, "inherit")

		got, err := vcs.CheckIgnored(wt, []string{"secret.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Path != "secret.txt" {
			t.Fatalf("CheckIgnored = %v, want secret.txt — the hatch did not restore the ambient environment", got)
		}
	})
}

// THE MOTIVATING LAYOUT IS ORDINARILY NESTED, and that is the case the first
// version of this guard refused.
//
// A detached work tree lives somewhere on disk, and somewhere on disk is
// routinely inside another repository — `~/work/wt` beneath a dotfiles
// repository at `$HOME` is the ordinary spelling, not an exotic one. The work
// tree still holds no .git of its own; what changes is that git's discovery
// ASCENDS, so a probe asking "does git resolve a repository for root without the
// environment" is answered by the ANCESTOR at exit 0. env.go's own comment
// records that premise as false for the scrub; the hatch's first guard was
// written on it anyway, armed on every nested layout, and refused it — the two
// identities differ by construction there, because the environment names the
// bare repository and -C alone names the ancestor.
//
// Measured on the binary at that commit, over this shape with a staged
// violation: exit 2, refused, where the binary before the guard reported the
// finding and exited 1.
//
// nestedRepos is TestScrubDoesNotAnswerFromAnAncestorRepository's fixture,
// reused rather than rebuilt: the ancestor relationship is the whole content of
// both tests, and the scrub's guard was given this shape while the hatch's was
// not.
func TestGitEnvInheritHonoursADetachedWorkTreeInsideAnotherRepository(t *testing.T) {
	wt, gitDirB := nestedRepos(t)
	t.Setenv("GIT_DIR", gitDirB)
	t.Setenv("GIT_WORK_TREE", wt)
	t.Setenv(vcs.GitEnvVar, "inherit")

	// B's index is the discriminator: the ancestor tracks nothing under wt, so
	// only an answer that honoured the environment can carry bad.go.
	got, err := vcs.TrackedUnder(wt)
	if err != nil {
		t.Fatalf("TrackedUnder: %v — the hatch refused the layout it exists for, because an ANCESTOR of the work tree is a repository", err)
	}
	if want := []string{"bad.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder = %v, want %v — the hatch did not restore the ambient environment", got, want)
	}

	// The ancestor's .gitignore names bad.go and the environment's work tree
	// holds no .gitignore at all, so honouring the environment means NOT ignored.
	// A refusal here is the same regression at the other exec site, which builds
	// its own exec.Command.
	ignored, err := vcs.CheckIgnored(wt, []string{"bad.go"})
	if err != nil {
		t.Fatalf("CheckIgnored: %v — the hatch refused the layout it exists for at the second exec site", err)
	}
	if len(ignored) != 0 {
		t.Errorf("CheckIgnored = %v, want none — that answer came from the ancestor's .gitignore rather than the work tree the environment names", ignored)
	}
}

// GIT_DIR ALONE IS NOT THE MOTIVATING LAYOUT, whatever root is. With no work
// tree named, git makes the CURRENT DIRECTORY the work tree — so every answer
// read from the working tree is about root while the index, the ignore rules and
// the changeset come from the repository the variable names. The layout the
// hatch exists for names its work tree; this does not.
//
// The fixture makes root a plain directory precisely because that is the shape
// an exemption keyed on "root names no repository" waves through: measured that
// way, `check` reported the ancestor-free tree as clean at exit 0 while
// TrackedUnder answered with the OTHER repository's file.
func TestGitEnvInheritRefusesAGitDirWithNoWorkTreeNamed(t *testing.T) {
	_, b := twoRepos(t)
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
	t.Setenv(vcs.GitEnvVar, "inherit")

	got, err := vcs.TrackedUnder(plain)
	if err == nil {
		t.Fatalf("TrackedUnder = %v with no error — GIT_DIR alone answered about a repository -C did not name", got)
	}
	wantHatchRefusal(t, err, plain, filepath.Join(b, ".git"), "GIT_DIR")
}

// The hatch is not a general off-switch for the question "which repository is
// this about". Where -C names a repository IN ITS OWN RIGHT and the environment
// names a different one, there is no environment to honour: the two describe
// different repositories and formwork was asked about the one on the command
// line.
//
// Both exec sites, because a guard at one of them leaves CheckIgnored — which
// builds its own exec.Command — answering from the other repository.
func TestGitEnvInheritRefusesARepositoryRootDidNotName(t *testing.T) {
	t.Run("gitExit", func(t *testing.T) {
		a, b := twoRepos(t)
		t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
		t.Setenv(vcs.GitEnvVar, "inherit")

		got, err := vcs.TrackedUnder(a)
		if err == nil {
			t.Fatalf("TrackedUnder(A) = %v with no error — the hatch answered about a repository -C did not name", got)
		}
		wantHatchRefusal(t, err, a, filepath.Join(b, ".git"), "GIT_DIR")
	})

	t.Run("gitStdin", func(t *testing.T) {
		a, b := twoRepos(t)
		write(t, b, ".git/info/exclude", "secret.txt\n")
		write(t, a, "secret.txt", "x\n")
		t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
		t.Setenv(vcs.GitEnvVar, "inherit")

		got, err := vcs.CheckIgnored(a, []string{"secret.txt"})
		if err == nil {
			t.Fatalf("CheckIgnored = %v with no error — the hatch answered about a repository -C did not name", got)
		}
		wantHatchRefusal(t, err, a, filepath.Join(b, ".git"), "GIT_DIR")
	})
}

// GIT_WORK_TREE alone moves no git directory, so a comparison asking only
// "which git directory" would wave this through — and what it waves through is
// an answer read from the other repository's working tree.
//
// Measured on git 2.50.1 over this fixture, with B's .gitignore naming
// secret.txt and A holding a secret.txt of its own: `check-ignore` from -C A
// answers IGNORED, citing B's .gitignore, where the scrubbed run answers not
// ignored. Under the hatch that difference is what prunes A's file from the scan
// with nothing said.
func TestGitEnvInheritRefusesAWorkTreeRootDidNotName(t *testing.T) {
	a, b := twoRepos(t)
	write(t, b, ".gitignore", "secret.txt\n")
	write(t, a, "secret.txt", "x\n")
	t.Setenv("GIT_WORK_TREE", b)
	t.Setenv(vcs.GitEnvVar, "inherit")

	got, err := vcs.CheckIgnored(a, []string{"secret.txt"})
	if err == nil {
		t.Fatalf("CheckIgnored = %v with no error — the hatch answered from a work tree -C did not name", got)
	}
	wantHatchRefusal(t, err, a, b, "GIT_WORK_TREE")
}

// wantHatchRefusal asserts the refusal is one an operator can act on without
// reading formwork's source: it names the variables in force, the root they
// disagree with, and both repositories.
//
// IT ALSO ASSERTS WHAT THE MESSAGE MUST NOT SAY. The scrub's own refusal ends by
// offering the hatch as a cure; here the hatch is already on, so that sentence
// would send the operator to set a variable they have set. A wrong cure is worse
// than none — it costs a round of debugging to find out it changes nothing.
func wantHatchRefusal(t *testing.T, err error, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the refusal does not mention %q:\n%v", s, err)
		}
	}
	if !strings.Contains(err.Error(), vcs.GitEnvVar) {
		t.Errorf("the refusal does not name %s, so it does not say which policy is in force:\n%v", vcs.GitEnvVar, err)
	}
	if strings.Contains(err.Error(), "or set "+vcs.GitEnvVar) {
		t.Errorf("the refusal offers the hatch as a cure while the hatch is what is in force:\n%v", err)
	}
}

// THE SPELLING OF -C MUST NOT DECIDE WHETHER THE LAYOUT IS HONOURED.
//
// EnsureTopLevel's note (vcs.go) records three regressions from comparing paths
// in Go, and each was a DIFFERENT comparison rather than a different spelling:
// raw equality refused the default "." from the repo root and broke every git
// hook (#142); `filepath.Abs` before `EvalSymlinks` strips `x/..` lexically
// while the kernel follows `x` first, reporting a subdirectory as the top level;
// and `EvalSymlinks` then `Abs` still compared spelling, so a case-variant root
// on a case-insensitive filesystem was refused. An earlier version of this
// comment cited that note for a trailing slash and a symlinked root — neither is
// what it records. The trailing SPACE hazard is TopLevel's, about a directory
// legally named `foo `, and a symlinked root appears there as a case that
// correctly PASSES.
//
// So the table below is the spellings those comparisons make dangerous, the
// last of them taken straight from the note's third regression. The exemption
// asks git, in git's frame, whether the work tree it resolves IS the directory
// -C names, so every one of them is the same answer by construction rather than
// by a normalisation formwork got right: measured on git 2.50.1, each answers
// `./` to `rev-parse --path-format=relative --show-toplevel`.
//
// TWO OF THESE FOUR DISCRIMINATE AND TWO DO NOT, which is worth saying so the
// table is not read as four load-bearing pins. Run against the path comparison
// this replaced, the trailing slash, the symlink and the `sub/..` all still
// pass — EvalSymlinks handles them — while the CASE VARIANT fails, as does the
// default root in the test below. They are kept because a future comparison
// need not resolve symlinks the way that one did, and because the cheap half of
// a regression table is the half nobody had to debug.
func TestGitEnvInheritHonoursADetachedWorkTreeHoweverRootIsSpelled(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spell func(t *testing.T, wt string) string
	}{
		{"trailing slash", func(_ *testing.T, wt string) string { return wt + string(filepath.Separator) }},
		{"through a symlink", func(t *testing.T, wt string) string {
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(wt, link); err != nil {
				t.Fatal(err)
			}
			return link
		}},
		{"sub/.. through a real subdirectory", func(t *testing.T, wt string) string {
			if err := os.MkdirAll(filepath.Join(wt, "s"), 0o755); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(wt, "s", "..")
		}},
		{"a case variant of the work tree", func(t *testing.T, wt string) string {
			variant := filepath.Join(filepath.Dir(wt), strings.ToUpper(filepath.Base(wt)))
			if _, err := os.Stat(variant); err != nil {
				// A case-SENSITIVE filesystem, where this spelling names nothing
				// and the test would fail for a reason that is not the exemption.
				// Skipped with the substrate named rather than silently: what it
				// leaves unmeasured HERE is measured wherever the suite runs on a
				// case-insensitive filesystem, which is the default on macOS.
				t.Skipf("case-insensitive filesystem needed: %s does not resolve (%v)", variant, err)
			}
			return variant
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt := detachedWorkTree(t)
			root := tc.spell(t, wt)
			t.Setenv(vcs.GitEnvVar, "inherit")

			got, err := vcs.TrackedUnder(root)
			if err != nil {
				t.Fatalf("the layout the hatch exists for is refused when -C is spelled %s: %v", tc.name, err)
			}
			if want := []string{"wt_only.go"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("TrackedUnder = %v, want %v", got, want)
			}
		})
	}
}

// gitRefusingPathFormat puts a git on PATH that answers every question the real
// git does EXCEPT one carrying --path-format, which it refuses at exit 129.
//
// It models the half env.go records as unmeasured. git 2.50.1 ECHOES an
// unrecognised option and exits 0 — the loud half, which validateIdentity
// refuses — but nothing measured says every git does, and a git that refuses
// instead fails BOTH runs identically, which is the shape that reads as
// agreement. No real git available reproduces it, so the shim is the fixture,
// and it is a pass-through rather than a stub precisely so the caller's own
// question still gets a real answer: that is what makes the run a fail-open
// rather than an error.
func gitRefusingPathFormat(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	shim := "#!/bin/sh\nfor a in \"$@\"; do\n  case \"$a\" in --path-format=*) echo \"error: unknown option \\`$a'\" >&2; exit 129;; esac\ndone\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// A QUESTION FORMWORK CANNOT ASK MUST NOT READ AS AGREEMENT — the posture
// validateIdentity already takes for an answer it cannot READ, applied to one it
// cannot OBTAIN. Both runs fail identically on such a git, so the comparison
// sees no difference and said nothing, while the caller's own call went on being
// answered by whatever the environment named.
//
// Measured with the shim below, root a repository and GIT_DIR naming another:
// before this arm, TrackedUnder returned the OTHER repository's file at no
// error.
func TestGitEnvInheritRefusesWhenGitCannotAnswerTheQuestion(t *testing.T) {
	a, b := twoRepos(t)
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
	t.Setenv(vcs.GitEnvVar, "inherit")
	gitRefusingPathFormat(t) // after the fixture, so setup uses the real git

	got, err := vcs.TrackedUnder(a)
	if err == nil {
		t.Fatalf("TrackedUnder = %v with no error — a git that cannot answer the question left the guard inert", got)
	}
	if !strings.Contains(err.Error(), vcs.GitEnvVar) {
		t.Errorf("the refusal does not name %s, so it does not say which policy is in force:\n%v", vcs.GitEnvVar, err)
	}
	if !strings.Contains(err.Error(), a) {
		t.Errorf("the refusal does not name the root it could not answer for:\n%v", err)
	}
	// A CURE THAT DOES NOT CURE IS WORSE THAN NONE, and this message is the one
	// state where the other refusal's cure is false: measured, following "point
	// -C at the work tree the environment describes" here is exit 2 again,
	// because the question fails whatever -C names, while unsetting the variables
	// is exit 1. Asserted as an ABSENCE so the next edit cannot reintroduce it by
	// reusing the neighbouring message's wording, which is how it got here.
	if strings.Contains(err.Error(), "Point -C") {
		t.Errorf("the refusal offers a -C cure that does not cure in this state:\n%v", err)
	}
}

// THE DEFAULT ROOT IS THE SPELLING THAT MATTERS MOST, and it is the one an
// absolute -C cannot stand in for.
//
// internal/hooks' generated shim runs `formwork check --lane pre-commit
// --staged` with NO -C at all, deliberately: git runs hooks with the working
// directory set to the repository root, so formwork's default root of "." is
// already correct. Every commit in a detached work tree under the hatch
// therefore arrives here spelled ".", and a comparison that resolved paths in Go
// refused it — measured, exit 2 for "." against exit 1 for the absolute
// spelling of the same directory, same fixture, same commit.
//
// Asking git in its own frame is what makes the two the same question: git
// resolves root itself, so "." and the absolute path and the /private-resolved
// path on macOS all answer `./`. That is the property this test pins, and it is
// why the assertion below is paired with the absolute spelling rather than
// standing alone — the pair fails asymmetrically under a path comparison and
// passes together under the question.
func TestGitEnvInheritHonoursADetachedWorkTreeAtTheDefaultRoot(t *testing.T) {
	wt := detachedWorkTree(t)
	t.Setenv(vcs.GitEnvVar, "inherit")

	// The absolute spelling first: a failure here is the exemption being broken
	// outright, which is a different fault from the one this test is about.
	if _, err := vcs.TrackedUnder(wt); err != nil {
		t.Fatalf("the absolute spelling is refused, so this test cannot say anything about the default root: %v", err)
	}

	// t.Chdir, NOT os.Chdir, and the mutation depends on it: t.Chdir sets $PWD,
	// and `filepath.Abs(".")` answers from $PWD when it names the same directory
	// as ".". Measured on macOS, that is the unresolved /var/… spelling where
	// git answers /private/var/… — which is what made the old path comparison
	// refuse this root. Under os.Chdir the two can agree and the mutation stops
	// reproducing, so this is not a call to "simplify".
	t.Chdir(wt)
	got, err := vcs.TrackedUnder(".")
	if err != nil {
		t.Fatalf("the default root is refused where the absolute spelling of the same directory is honoured: %v", err)
	}
	if want := []string{"wt_only.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder(\".\") = %v, want %v", got, want)
	}
}

// GIT_WORK_TREE=<root> IS NOT A LAYOUT WHEN ROOT IS ITS OWN REPOSITORY (#306).
//
// namesRootAsItsWorkTree asked two questions — GIT_WORK_TREE is set and in the
// removed set, and git resolves root as its work tree — and naming a work tree
// is exactly what MOVES the top-level answer, so GIT_WORK_TREE=<root> satisfies
// both whatever GIT_DIR names. The exemption then fired on the shape the file's
// own policy paragraph says must refuse: "-C naming one repository while the
// environment names another".
//
// WHAT IT COST, MEASURED ON THE BINARY BEFORE THE FIX rather than argued: a
// repository with `scan.gitignore` declared, `.gitignore` naming `build/`, and a
// force-added COMMITTED `build/bad.txt` containing the forbidden token.
// `check -C r --lane pre-commit` is exit 1 with the finding. Under
// `FORMWORK_GIT_ENV=inherit GIT_DIR=<B>/.git GIT_WORK_TREE=<r>` the same command
// printed "scan.gitignore: 1 dirs pruned", "1/1 rules passed", exit 0 — because
// the tracked question was answered from B's index, where build/bad.txt is
// untracked and therefore ignorable. `--staged`, which is what internal/hooks'
// generated shim runs, reported "0 path(s) requested by --staged" at exit 0.
//
// TWO EXEC SITES, for TestGitEnvInheritRefusesARepositoryRootDidNotName's
// reason: CheckIgnored builds its own exec.Command, so a guard proved at one of
// them leaves the other answering from B.
//
// THE SAME BINARY ALREADY REFUSED THIS STATE ON THE INSTALL SIDE — `hooks
// install` and `hooks verify` both exit 2 here (internal/hooks' preflight, #179)
// — so before this test one formwork held two verdicts about one environment.
func TestGitEnvInheritRefusesAWorkTreeThatIsRootsOwnRepository(t *testing.T) {
	t.Run("gitExit", func(t *testing.T) {
		a, b := twoRepos(t)
		t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
		t.Setenv("GIT_WORK_TREE", a)
		t.Setenv(vcs.GitEnvVar, "inherit")

		// A tracks a_only.go and B tracks b_only.go, so the answer names which
		// index replied: measured under this environment, `ls-files` in A lists
		// b_only.go.
		got, err := vcs.TrackedUnder(a)
		if err == nil {
			t.Fatalf("TrackedUnder(A) = %v with no error — GIT_WORK_TREE naming -C itself exempted a GIT_DIR that names another repository, so A's own repository went unread", got)
		}
		wantHatchRefusal(t, err, a, filepath.Join(b, ".git"), "GIT_DIR", "GIT_WORK_TREE")
	})

	t.Run("gitStdin", func(t *testing.T) {
		a, b := twoRepos(t)
		// B's info/exclude, NOT B's .gitignore, and the difference is the whole
		// fixture: GIT_WORK_TREE makes A the work tree, so the per-tree ignore
		// files are read from A and only the git directory's own exclude file is
		// still B's. Measured — `check-ignore -v` from -C A under this
		// environment answers ignored, citing B/.git/info/exclude, where the
		// scrubbed run answers not ignored.
		write(t, b, ".git/info/exclude", "secret.txt\n")
		write(t, a, "secret.txt", "x\n")
		t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
		t.Setenv("GIT_WORK_TREE", a)
		t.Setenv(vcs.GitEnvVar, "inherit")

		got, err := vcs.CheckIgnored(a, []string{"secret.txt"})
		if err == nil {
			t.Fatalf("CheckIgnored = %v with no error — B's info/exclude decided whether a file in A's own repository is governed", got)
		}
		wantHatchRefusal(t, err, a, filepath.Join(b, ".git"), "GIT_DIR", "GIT_WORK_TREE")
	})
}

// A LINKED WORKTREE IS THE ROOT THAT HOLDS A .git AND MUST STILL BE HONOURED,
// and it is honoured by AGREEMENT rather than by the exemption (#306).
//
// The refusal above is keyed on root being a repository in its own right, and a
// linked worktree's root holds a `.git` FILE — `gitdir: <main>/.git/worktrees/W`
// — so a reader could reasonably fear this layout was refused with it. It is
// not, and the reason is structural rather than lucky: with the environment
// naming the linked worktree's own git directory, the ambient and scrubbed
// identity answers are BYTE-IDENTICAL (measured on git 2.50.1: git directory
// <main>/.git/worktrees/W, shared directory <main>/.git, work tree W, from both
// runs), so refuseUnlessHatchAgrees returns at its equality branch and
// namesRootAsItsWorkTree is never called.
//
// SO THIS PINS THE EARLY RETURN, which nothing else did. It is the one test that
// distinguishes "the exemption carries this layout" from "agreement does": the
// ambient relative top level here is `./`, exactly as it is in the refused shape
// above, so a fix that reached the exemption for this root would refuse a
// perfectly coherent worktree.
//
// BOTH SPELLINGS, because they exercise different halves. GIT_DIR alone cannot
// reach the exemption at all (namesRootAsItsWorkTree returns false on the
// unset variable), so only the second row would notice the exemption being
// consulted; the first row is what says the agreement branch is doing the work
// rather than the variable being absent.
func TestGitEnvInheritHonoursALinkedWorktreeWhoseRootHoldsAGitOfItsOwn(t *testing.T) {
	main := commitRepo(t)
	linked := filepath.Join(t.TempDir(), "W")
	run(t, main, "worktree", "add", "-q", "-b", "wbranch", linked)
	write(t, linked, "w_only.go", "package w\n")
	run(t, linked, "add", "-A")
	run(t, linked, "commit", "-q", "-m", "w")

	// Read the linked git directory from git rather than assembling
	// `.git/worktrees/W`, for TestScrubDoesNotAnswerFromAnAmbientLinkedWorktreeGitDir's
	// reason: the layout is git's to name.
	out, err := exec.Command("git", "-C", linked, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		t.Fatalf("resolving the linked worktree's git dir: %v", err)
	}
	linkedGitDir := strings.TrimSpace(string(out))

	for _, tc := range []struct {
		name     string
		workTree bool
	}{
		{"GIT_DIR alone", false},
		{"GIT_DIR and GIT_WORK_TREE naming the worktree", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIT_DIR", linkedGitDir)
			if tc.workTree {
				t.Setenv("GIT_WORK_TREE", linked)
			}
			t.Setenv(vcs.GitEnvVar, "inherit")

			got, err := vcs.TrackedUnder(linked)
			if err != nil {
				t.Fatalf("a linked worktree of a non-bare repository is refused under the hatch: %v", err)
			}
			if want := []string{"a.txt", "w_only.go"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("TrackedUnder = %v, want %v — the worktree's own index did not answer", got, want)
			}
		})
	}
}
