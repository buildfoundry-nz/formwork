// The guard that belongs to FORMWORK_GIT_ENV=inherit itself.
//
// The hatch turns off the scrub, and with it the divergence guard env.go arms
// when the scrub removes something — that guard arbitrates between two candidate
// repositories, and under the hatch there is nothing to arbitrate about the
// variables the operator deliberately set. What was not measured until now is
// that turning the arbitration off ALSO turns off the only thing standing
// between formwork and a repository nobody named: with the hatch on and GIT_DIR
// naming an unrelated repository, `check --lane pre-commit --staged` run inside
// a repository with a staged violation reported "0 path(s) requested by
// --staged, 0 file(s) scanned" and exited 0.
//
// THE POLICY. Under the hatch, formwork answers only about the repository -C
// names: the environment is honoured where it names a WORK TREE, that work tree
// IS -C, and -C is not a repository in its own right — and everything else
// refuses. The hatch means "honour the environment I set", not "answer about a
// repository I did not name".
//
// THE THIRD CLAUSE WAS MISSING AND THE PROSE WAS NOT (#306). The first version
// of this paragraph already said the incoherent case must refuse — "-C naming
// one repository while the environment names another" — while the code asked
// only the first two questions, and GIT_WORK_TREE=<root> answers both of them
// whatever GIT_DIR names, because naming a work tree is what MOVES the
// top-level answer. Measured: `check --lane pre-commit` over a committed
// violation went from exit 1 to "1/1 rules passed" at exit 0.
//
// STATING IT AS "-C AND THE ENVIRONMENT MUST AGREE ABOUT THE REPOSITORY" IS WHAT
// WENT WRONG, and the wording is kept here as a warning rather than dropped: it
// invites the question "is root a repository", which is a different question that
// git answers from an ANCESTOR. See below.
//
// THE MOTIVATING LAYOUT SURVIVES IT UNTOUCHED, which is the whole justification
// and is measured rather than argued: TestGitEnvInheritHonoursADetachedWorkTree
// runs the bare-repo-plus-worktree layout through both exec sites,
// TestGitEnvInheritHonoursADetachedWorkTreeInsideAnotherRepository runs it one
// directory deeper, and TestGitEnvInheritHonoursADetachedWorkTreeHoweverRootIsSpelled
// runs the three spellings of -C this repository's history says break a path
// comparison.
//
// THE EXEMPTION IS POSITIVE, AND THE FIRST VERSION OF IT WAS NOT. That version
// exempted a root git resolves no repository for — "the work tree holds no .git,
// so scrubbed there is nothing to disagree" — and it was wrong in both
// directions at once, each measured:
//
//   - Too WIDE. GIT_DIR set ALONE, with -C a plain directory, is not a layout
//     this hatch is for: with no work tree named, git makes the current
//     directory the work tree, so the files come from -C while the index, the
//     ignore rules and the changeset come from the other repository. Root names
//     no repository, so that exemption waved it through — the whole defect, back
//     under a different spelling.
//   - Too NARROW. git's discovery ASCENDS, and a detached work tree routinely
//     sits inside another repository (`~/work/wt` under a dotfiles repo at
//     $HOME). The scrubbed run then answers from the ANCESTOR at exit 0, the two
//     identities differ by construction, and the layout the hatch exists for was
//     REFUSED — a working gate turned into an exit 2. env.go records that same
//     premise as false where it argues the scrub; this is the other side of it.
//
// So the exemption asks what the layout actually is: THE ENVIRONMENT NAMES A
// WORK TREE, AND IT IS -C. GIT_WORK_TREE is what names one — with GIT_DIR alone
// git falls back to the current directory, which is not the operator saying
// anything about a work tree — and git's own answer must then be root itself.
// Everything else refuses, including the incoherent case that started this: -C
// naming one repository while the environment names another. A vacuous pass over
// "0 paths scanned" is not an answer about the named repository either.
package vcs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ensureHatchNamesRootsRepository is the hatch half of the policy scrubenv.go
// records. It decides nothing unless the hatch is on AND one of
// scrubbedRepoVars is set — a history variable (#176) is split out of the copy
// below and then discarded by MovedRepository, because it leaves the repository
// git resolves untouched and so gives this guard no second candidate to
// arbitrate between. Every ordinary run and every hatched run with no repository
// pointer set pays nothing beyond that one ScrubEnviron.
//
// COST, WHICH IS PAID ONLY UNDER THE HATCH WITH ONE OF THE POINTER VARIABLES SET: two
// extra forks per git call for the identity question under each environment,
// THREE wherever the exemption is consulted — which is every call in the layout
// the hatch exists for, because the exemption is what lets that layout through —
// and two more again where the bare retry fires. Counted through a logging shim
// on PATH: `check --lane pre-commit --staged` in the nested detached layout ran
// 8 git commands, being the 2 the caller makes plus 3 apiece. Every other run
// pays one os.LookupEnv.
func ensureHatchNamesRootsRepository(root string) error {
	inherit, err := gitEnvInheriting()
	if err != nil || !inherit {
		return err
	}
	scrubbed, removed := ScrubEnviron(os.Environ())
	// The POINTER family alone, for gitEnvFor's reason: a history variable (#176)
	// leaves the repository git resolves untouched, so there is no second
	// candidate repository to arbitrate between and nothing this guard decides.
	// Under the hatch it is honoured, which is what the hatch means.
	moved := MovedRepository(removed)
	if len(moved) == 0 {
		return nil
	}
	return refuseUnlessHatchAgrees(root, scrubbed, moved, repoIdentity)
}

// refuseUnlessHatchAgrees runs one identity question under the ambient
// environment and under the scrubbed one, and refuses when the two describe
// different repositories — unless the difference IS the layout the hatch exists
// for, which namesRootAsItsWorkTree decides.
//
// THE RETRY IS THE SAME BARE-REPOSITORY SHAPE compareRepoIdentity documents, for
// the same reason: both runs failing identically is not agreement about anything
// when the failure is `--show-toplevel` in a bare repository, where the callers
// that still work would go on against whichever repository the environment
// landed on. q is the full question and the retry is the only recursion. A
// disagreement reached on the SUBSET cannot be the motivating layout — the
// subset carries no work tree to compare against root, and a bare repository at
// -C is not a detached work tree — so it refuses without asking.
//
// A MUTUAL FAILURE OF THE SUBSET REFUSES, and that is the arm this guard was
// missing. A question formwork cannot ASK must not read as agreement, which is
// the posture validateIdentity already takes for an answer it cannot READ —
// twenty lines apart, the two states are the same failure and had opposite
// verdicts. Both runs failing identically is what a git that refuses
// `--path-format` produces, and env.go records that half as unmeasured: measured
// here through a PATH shim, the run went on against whatever the environment
// named and exited 0 over a violation, with nothing in the output to notice it
// by.
//
// THE LAYOUT THE HATCH EXISTS FOR CANNOT REACH THIS ARM, which is what makes
// refusing here safe, and it is a different statement from the one an earlier
// version of this comment made. That version said this arm's silence used to be
// what let the detached work tree through; it never was — while the guard keyed
// on a probe, that layout returned before this function was called at all, and
// it goes down the DIFFERENCE branch below in any case, since the ambient
// environment answers and the scrubbed one fails. What stops the difference
// branch refusing it is namesRootAsItsWorkTree, not this arm.
//
// The reason refusing here is safe is simpler and is about the layout rather
// than about the code's history: a detached work tree ANSWERS the identity
// question under the ambient environment, so it cannot produce the mutual
// failure this arm decides. Measured: with this arm reverted to `return nil` the
// only red test is the state-3 one, and every detached-work-tree control passes.
// What a mutual failure means is that git could not answer for EITHER
// environment — and on such a git the detached layout is refused too, loudly,
// which is the cost accepted here rather than one hidden.
//
// AND IT IS THE NARROW ARM RATHER THAN THE TIMID ONE. Refusing the whole hatch
// unconditionally would also refuse the coherent case — GIT_DIR naming the very
// repository -C resolves, which is what `submodule foreach` exports — where both
// runs agree and there is nothing to arbitrate. The comparison stays; only its
// mutual-failure verdict changes.
//
// The cost, stated: where root is not a repository under either environment, the
// operator gets this message rather than git's own "not a git repository". Both
// are exit 2, and the caller's next git call would have failed anyway.
func refuseUnlessHatchAgrees(root string, scrubbed, removed []string, q identityQuestion) error {
	ambientOut, ambientCode, ambientErr := gitExitEnv(nil, root, q.args...)
	scrubbedOut, scrubbedCode, scrubbedErr := gitExitEnv(scrubbed, root, q.args...)

	if ambientCode == scrubbedCode && ambientOut == scrubbedOut {
		if ambientErr != nil && scrubbedErr != nil && q.parts != bareRepoIdentity.parts {
			return refuseUnlessHatchAgrees(root, scrubbed, removed, bareRepoIdentity)
		}
		if ambientErr != nil {
			return fmt.Errorf("git: this git cannot answer the question formwork needs to tell which repository it resolves for -C %s, so under %s=%s it cannot say whether %s in the environment names the repository -C does: %w. Unset %s, or use a git that answers `rev-parse --path-format` (2.31 or newer) — moving -C does not help, because this question fails whatever it names",
				root, GitEnvVar, gitEnvInherit, strings.Join(removed, ", "), ambientErr, strings.Join(removed, ", "))
		}
		// Agreeing on something that is not an answer is not agreement — an
		// unrecognised option comes back ECHOED at exit 0, identically from both
		// runs (validateIdentity records the measurement). Under the hatch that
		// would leave this guard silently inert on a git that does not understand
		// the question, which is the state it exists to make impossible.
		if _, err := validateIdentity(q, ambientOut); err != nil {
			return fmt.Errorf("git: this git cannot answer the question formwork needs to tell which repository it resolves for -C %s, so under %s=%s it cannot say whether %s in the environment names the repository -C does: %w. Unset %s, or use a git that answers `rev-parse --path-format` (2.31 or newer) — moving -C does not help, because this question fails whatever it names",
				root, GitEnvVar, gitEnvInherit, strings.Join(removed, ", "), err, strings.Join(removed, ", "))
		}
		return nil
	}
	// The one difference that is a layout rather than a mistake: the operator
	// named a work tree, and it is the directory -C names.
	if namesRootAsItsWorkTree(root, removed) {
		return nil
	}
	// Either the two runs describe different repositories, or the environment
	// decides whether git answers at all. Both are the same refusal: -C named one
	// repository and the environment named another.
	return fmt.Errorf("git: %s in the environment names a different repository from -C %s — with the environment, %s; from -C alone, %s. %s=%s honours the environment rather than removing %s, but formwork will not answer about a repository -C did not name: point -C at the work tree the environment describes, or unset %s",
		strings.Join(removed, ", "), root,
		renderIdentity(ambientOut, ambientErr), renderIdentity(scrubbedOut, scrubbedErr),
		GitEnvVar, gitEnvInherit, strings.Join(removed, ", "), strings.Join(removed, ", "))
}

// namesRootAsItsWorkTree reports whether the environment names a work tree and
// that work tree IS root — the detached bare-repo-plus-worktree layout, stated
// as a property of the answers rather than of where the work tree happens to
// sit.
//
// GIT_WORK_TREE MUST BE SET AND NON-EMPTY, and that is the load-bearing half.
// Measured on git 2.50.1, a NON-BARE GIT_DIR alone answers `./` here too,
// because with no work tree named git falls back to the current directory — so
// the answer alone cannot tell "the operator named this tree" from "git had
// nowhere else to point", and that redirect would exempt itself. (A bare GIT_DIR
// with no work tree named is exit 128, `this operation must be run in a work
// tree`, and fails the exemption on the error rather than on the answer — one
// spelling of the family, not the family.) The layout this
// exists for names its work tree; a `core.worktree` in the bare repository's own
// config does not, and is refused rather than guessed at.
//
// IT ASKS IN GIT'S OWN FRAME, WHICH IS WHY IT IS NOT A PATH COMPARISON. `-C root
// rev-parse --path-format=relative --show-toplevel` answers `./` when root IS
// the work tree, `../` from a subdirectory of it, `../other` when root is
// outside it, and exit 128 when there is no work tree at all — all measured on
// git 2.50.1, and `./` measured also through a symlinked root, a trailing slash
// and a `sub/..` that traverses a real directory. Every normalisation formwork
// could do in Go is a chance to disagree with the kernel about one of those
// three; git answering about its own resolution cannot.
//
// `--show-prefix` IS THE OBVIOUS TEST AND IT IS WRONG. Measured with
// GIT_WORK_TREE naming another directory and -C outside it entirely, the prefix
// is EMPTY at exit 0 — the same answer a root that IS the work tree gives. An
// exemption keyed on it would wave through exactly the shape
// TestGitEnvInheritRefusesAWorkTreeRootDidNotName pins, so the next reader
// reaching for it has the counterexample here.
//
// IT FAILS CLOSED ON A GIT THAT DOES NOT KNOW THE OPTION. Measured, an
// unrecognised `--path-formatX=relative` is ECHOED and the absolute top level
// printed after it: two lines, neither of them `./`, so the exemption does not
// fire and the caller gets the refusal rather than a bypass.
//
// AND ROOT MUST NOT BE A REPOSITORY IN ITS OWN RIGHT (#306), which is the third
// question and the one the answer above cannot ask. `./` says "git resolved
// root as the work tree", and GIT_WORK_TREE=<root> MAKES that true — so on its
// own it cannot tell the layout this exists for from a repository being read
// out of another repository's index. rootHoldsItsOwnGit is the difference: the
// work tree of a detached layout holds no .git of any kind, which is what makes
// that layout inexpressible without the hatch, while the hostile shape's root
// has a repository it could have been asked about directly.
//
// core.bare IS THE OBVIOUS DISCRIMINATOR AND IT IS WRONG, so the counterexample
// is kept here for the next reader reaching for it. "The legitimate layout is a
// BARE repository plus a work tree" is true of the first motivating fixture and
// false of the second: nestedRepos builds its git directory with a plain
// `git init`, so requiring core.bare=true reddens
// TestGitEnvInheritHonoursADetachedWorkTreeInsideAnotherRepository — measured,
// not reasoned about. A detached work tree does not care whether the repository
// driving it is bare.
//
// A LINKED WORKTREE HOLDS A .git AND IS NOT REFUSED BY THIS, because it never
// arrives: with the environment naming its own git directory the ambient and
// scrubbed identity answers are byte-identical, so refuseUnlessHatchAgrees
// returns at its equality branch and this function is not called at all. That
// is a property of the layout rather than a hope, and
// TestGitEnvInheritHonoursALinkedWorktreeWhoseRootHoldsAGitOfItsOwn is what
// holds it — its ambient relative top level is `./` too, so nothing else in the
// suite would notice the exemption starting to decide that layout.
func namesRootAsItsWorkTree(root string, removed []string) bool {
	if wt, ok := os.LookupEnv("GIT_WORK_TREE"); !ok || wt == "" {
		return false
	}
	if !slices.Contains(removed, "GIT_WORK_TREE") {
		return false
	}
	if rootHoldsItsOwnGit(root) {
		return false
	}
	out, _, err := gitExitEnv(nil, root, "rev-parse", "--path-format=relative", "--show-toplevel")
	if err != nil {
		return false
	}
	return strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r") == "./"
}

// rootHoldsItsOwnGit reports whether root carries a `.git` entry — the property
// that makes it a repository formwork could have been asked about directly, and
// therefore not a work tree the hatch has to be told about.
//
// IT ANSWERS TRUE FOR EVERYTHING IT CANNOT RULE OUT, which is the direction
// that costs a refusal rather than a bypass: only a definite "no such file"
// releases the exemption, so a root formwork cannot look inside does not read
// as "no repository here".
//
// THAT ARM IS NOT SEPARATELY PINNED, and it is stated rather than left for a
// reader to discover, because every root that produces it is refused by the git
// question above this call whatever this answers. Lstat fails with something
// other than ErrNotExist when root is unreadable, is a FILE (ENOTDIR), or is a
// symlink loop — and `-C root rev-parse` fails on each of those too, so
// namesRootAsItsWorkTree returns false either way. An empty root is the same
// story: `git -C ""` cannot run, and the concatenation below asks about "/.git"
// rather than reaching for a special case that no test could drive. What the
// conservative spelling buys is that a future caller reaching this with a root
// git CAN resolve does not get an exemption out of a failed stat.
//
// os.Lstat, NOT os.Stat, and the difference is a `.git` SYMLINK whose target
// does not resolve. Stat reports that as ErrNotExist — the exemption would
// fire on a root that plainly carries a .git — where Lstat sees the entry
// itself. detachedWorkTree's fixture states the property as "holds no .git of
// any kind", and Lstat is the spelling of "of any kind".
//
// THE PATH IS BUILT FOR THE KERNEL TO RESOLVE, NOT filepath.Clean, which is why
// it is not filepath.Join. Join cleans lexically, and this function is reached
// with -C spelled however the caller spelled it —
// TestGitEnvInheritHonoursADetachedWorkTreeHoweverRootIsSpelled drives a
// trailing slash, a symlink, a `sub/..` and a case variant through it. Lexical
// `..` is the SECOND regression EnsureTopLevel's note records — "`filepath.Abs`
// before `EvalSymlinks` strips `x/..` lexically while the kernel follows `x`
// first" — and it is the one spelling where Join and the kernel can land in
// different directories: `link/..` cleans to `.` in Go while the kernel follows
// `link` first. Concatenation hands every one of those spellings to the kernel,
// which resolves them the same way git just did.
func rootHoldsItsOwnGit(root string) bool {
	_, err := os.Lstat(root + string(filepath.Separator) + ".git")
	return !errors.Is(err, fs.ErrNotExist)
}
