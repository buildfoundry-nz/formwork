package vcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitEnvVar is the escape hatch for the scrub policy (scrubenv.go), and
// gitEnvInherit is the only value it accepts.
//
// It is an environment variable rather than a flag because the problem it
// solves lives in the environment: the layout it exists for is expressed by
// exporting GIT_DIR and GIT_WORK_TREE, so the correction belongs beside them
// and needs no plumbing through every subcommand's flag set.
const (
	GitEnvVar     = "FORMWORK_GIT_ENV"
	gitEnvInherit = "inherit"
)

// gitEnv is the environment for one git command: the caller's, less
// scrubbedGitVars, split out by ScrubEnviron (scrubenv.go). It also returns the
// names it removed — empty when none of them were set, which is the ordinary
// case and the one gitEnvFor uses to skip its probe entirely. A nil env means
// "inherit the parent's environment unchanged" — os/exec's own meaning for a nil
// Env — which is what the hatch asks for.
//
// It reads the environment on every call rather than resolving once. Nothing
// here is hot enough to matter beside forking git, and a cached answer would
// make this package's behaviour depend on when it was first used, which no
// caller can see.
func gitEnv() (kept, removed []string, err error) {
	inherit, err := gitEnvInheriting()
	if err != nil || inherit {
		return nil, nil, err
	}
	kept, removed = ScrubEnviron(os.Environ())
	return kept, removed, nil
}

// identityQuestion is one spelling of "what repository did git resolve", with
// the number of ABSOLUTE PATHS a successful answer must carry.
//
// THE COUNT IS PART OF THE QUESTION, not a derived convenience, because
// validating the answer is what keeps an unusable one from reading as agreement.
// See validateIdentity.
type identityQuestion struct {
	args  []string
	parts int
}

// repoIdentity asks git what repository it resolved: the git directory that
// answers, the directory its worktrees share, and the work tree the answers are
// about. bareRepoIdentity is the same question less the part a bare repository
// cannot answer.
//
// ALL THREE PARTS ARE NEEDED, because the three scrubbed variables move
// different ones and a comparison is blind to whatever it does not ask.
// Measured on git 2.50.1, each against `-C A`:
//
//   - GIT_DIR moves the git directory — but naming another repository B moves the
//     COMMON directory to B as well, so --git-common-dir alone already refuses
//     that case and --git-dir is not what catches it. The shape where only the git
//     directory moves is a LINKED WORKTREE, whose git directory is
//     A/.git/worktrees/<name> while its common directory is A/.git, shared with
//     the main worktree. With -C A and GIT_DIR naming it, two of the three parts
//     come back byte-identical and the index does not: ambient `ls-files` lists
//     the linked worktree's files as well, scrubbed lists only the main
//     worktree's. That is the whole of --git-dir's job here, and
//     TestScrubDoesNotAnswerFromAnAmbientLinkedWorktreeGitDir is the only test
//     that fails without it.
//   - GIT_WORK_TREE moves ONLY the top level — the git directory is byte-identical
//     with and without it. That is enough to change every answer read from the
//     working tree: naming an unrelated directory, `check-ignore secret.txt`
//     answered "ignored" citing THAT directory's .gitignore where the scrubbed run
//     said not ignored, and `ls-files --others --ignored` listed its file.
//   - GIT_COMMON_DIR moves ONLY the common directory — git dir and top level are
//     both byte-identical. `info/exclude` lives in the common directory, so
//     `check-ignore loose.txt` answered "ignored" citing the OTHER repository's
//     info/exclude where the scrubbed run said not ignored.
//
// They come from ONE invocation, so the parts cannot describe different
// resolutions and it costs one fork rather than three.
//
// --path-format=absolute IS LOAD-BEARING, and the failure it prevents is a false
// POSITIVE. `--git-dir` and `--git-common-dir` answer a relative ".git" when git
// resolved the repository by discovery, and echo the variable's spelling when one
// supplied it — so an operator exporting GIT_DIR that names the repository -C
// already resolves would get ".git" from one run and an absolute path from the
// other, and a comparison on that refuses a layout where the two agree about
// everything (TestScrubIsSilentWhenGitDirNamesTheSameRepository pins it).
// Resolving the relative spelling in Go instead was rejected: it would compare
// two strings this package built rather than two answers git gave, and a root
// spelled with a trailing slash or reached through a symlink would then differ
// from itself — the mistake EnsureTopLevel's history records three times over.
//
// IT PUTS A FLOOR UNDER git AT 2.31, AND THE ANSWER IS VALIDATED RATHER THAN
// THE VERSION CHECKED. What a pre-2.31 git does with `--path-format` is NOT
// MEASURED here — no such git was available — and the shape it would take is not
// guessable from 2.50.1, which ECHOES an unrecognised option and exits 0 rather
// than refusing it (`rev-parse --absolute-git-dirX` prints `--absolute-git-dirX`,
// exit 0, measured). An earlier version of this comment asserted `error: unknown
// option`, exit 129; that was measured against a shim written to reject the
// option, which models a shim and not an old git.
//
// It does not matter, and that is the point: an old git is refused whichever way
// it behaves, by one of TWO arms rather than by validateIdentity alone. Echoing
// produces a non-absolute first line, which validateIdentity refuses. REFUSING
// produces a failure both runs share, which validateIdentity never sees —
// compareRepoIdentity calls it only where both runs SUCCEEDED and agreed — so
// that half is refused by the mutual-failure arm there instead. An earlier
// version of this paragraph claimed both shapes were covered here; the second
// was not covered at all, and returned "no disagreement" for a question that was
// never asked.
//
// A BARE REPOSITORY CANNOT ANSWER `--show-toplevel` (exit 128, "this operation
// must be run in a work tree"), which fails the whole invocation. The bare
// variant drops that part and keeps the two a bare repository does answer.
//
// IT MATTERS ONLY WHEN BOTH ENDS ARE BARE. With a non-bare repository at either
// end the invocation succeeds on that side, so the exit statuses differ and the
// ordinary comparison already refuses. Bare-to-bare is the one shape where both
// runs fail identically — which would otherwise read as "nothing changed" while
// the environments name different repositories. The exposure it closes is narrow
// and stated as such: of this package's three root-relative readers, only
// TrackedUnder runs at all in a bare repository (`check-ignore` and `ls-files
// --others --ignored` are exit 128 there, measured), and a bare repository
// normally carries no index, so what the retry prevents is formwork PROCEEDING
// against a repository nobody named rather than a demonstrated wrong answer.
// They are variables rather than constants only so SetRepoIdentityForTest can
// drive an unanswerable question through them; nothing in production reassigns
// either.
var (
	repoIdentity = identityQuestion{
		args:  []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--show-toplevel"},
		parts: 3,
	}
	bareRepoIdentity = identityQuestion{
		args:  []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir"},
		parts: 2,
	}
)

// validateIdentity turns git's stdout into the paths it was asked for, or an
// error saying it is not an answer.
//
// AN ANSWER FORMWORK CANNOT READ MUST NOT READ AS AGREEMENT, which is the whole
// reason this exists rather than the caller comparing raw stdout. `git rev-parse`
// ECHOES an option it does not recognise and exits 0 (`rev-parse
// --absolute-git-dirX` prints `--absolute-git-dirX`, exit 0, measured on 2.50.1)
// — so on any git lacking a flag this package passes, BOTH runs come back with
// the same flag string, compare equal, and the guard is silently inert while the
// scrub goes on substituting. That is #167 restored in full by a toolchain
// difference, with nothing in the output to notice it by.
//
// THE TEST IS `filepath.IsAbs` ON EVERY LINE, plus the count. Every part of the
// question is asked under --path-format=absolute, so an absolute path is the only
// shape a real answer can take, and an echoed option (`--path-format=absolute`,
// `--git-dir`) fails it. Requiring the count as well catches the shape where a
// git answers some parts and echoes others.
//
// The renderer below already declined to mislabel an unrecognised shape; this is
// the same posture applied to the VERDICT, which is where it decides anything.
func validateIdentity(q identityQuestion, out string) ([]string, error) {
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		lines = append(lines, strings.TrimSuffix(l, "\r"))
	}
	if len(lines) != q.parts {
		return nil, fmt.Errorf("`git %s` answered %d line(s), want %d", strings.Join(q.args, " "), len(lines), q.parts)
	}
	for _, l := range lines {
		if !filepath.IsAbs(l) {
			return nil, fmt.Errorf("`git %s` answered %q, which is not an absolute path — this git may not understand every option formwork asked for", strings.Join(q.args, " "), l)
		}
	}
	return lines, nil
}

// gitEnvFor is gitEnv plus the guard that makes the scrub honest, and it is what
// both of this package's exec sites call.
//
// WHAT IT REFUSES, AND WHY THE SCRUB ALONE WAS NOT ENOUGH. Removing GIT_DIR &c.
// was justified on the premise that the one layout it breaks fails loudly:
// scrubbed, a detached git dir plus work tree goes `fatal: not a git
// repository`. That premise holds only while no ANCESTOR of root is a
// repository. Where one is, git's ordinary upward discovery answers from the
// ancestor and exits 0 — so the scrub does not remove a wrong answer, it
// substitutes a different wrong one in silence. Measured on git 2.50.1: with a
// plain directory inside repository A as root and GIT_DIR naming repository B,
// `check` went from a correct exit 1 over a committed violation to exit 0,
// because A's .gitignore pruned the file B tracks.
//
// IT REFUSES ON EFFECT, NOT ON PRESENCE — the same axis D9 put the config family
// on, and the reason `submodule foreach` still costs nothing: that flow exports
// GIT_DIR=.git with no GIT_WORK_TREE and re-resolves to the identical git
// directory through the gitlink, so the two answers agree and nothing is said.
// TestSubmoduleForeachEnvironmentSurvivesTheScrub measures that, and
// TestScrubDoesNotAnswerFromAnAncestorRepository the other direction.
//
// IT REFUSES RATHER THAN CHOOSING. Which repository the operator meant is not a
// question this package can answer — one side is what -C names, the other is
// what the environment names, and picking either silently is how this defect got
// here. The message carries both and names the hatch.
//
// NOT AT EnsureTopLevel's ALTITUDE, AND IT COVERS MORE THAN THAT FUNCTION EVER
// DID. An earlier version of this paragraph said EnsureTopLevel "already refuses
// this shape" and that StagedPaths, RangePaths and TrackedPaths inherited the
// refusal. That is true of ONE spelling only. EnsureTopLevel decides on
// `rev-parse --show-prefix` plus a directory-identity check, and measured on git
// 2.50.1 with -C at a repository's own top level:
//
//   - an ambient GIT_DIR naming a SIBLING repository leaves the prefix empty and
//     the top level equal to -C, so it was accepted while the index came from the
//     sibling;
//   - so does an ambient GIT_DIR naming a LINKED WORKTREE;
//   - GIT_WORK_TREE it does catch, but on the identity check rather than the
//     prefix — the reported top level is the named directory, not -C.
//
// So the changeset family inherited a refusal for the ancestor spelling and
// nothing for the other two. The exec sites are the only altitude that covers
// any of the three variables' non-ancestor spellings, and they are additionally
// the only one that reaches TrackedUnder, IgnoredUnder and CheckIgnored — which
// deliberately do not call EnsureTopLevel at all, being root-relative by design
// and required to keep answering for a root that is a repository SUBDIRECTORY.
//
// EVERY ERROR IT RETURNS IS TAGGED ErrGitEnv, which is what lets a caller tell
// this policy's refusal from an answer git gave: see the sentinel below.
func gitEnvFor(root string) ([]string, error) {
	env, removed, err := gitEnv()
	if err != nil {
		return nil, gitEnvRefusal{err}
	}
	// THE HATCH HAS A GUARD OF ITS OWN, AND IT IS NOT THIS ONE. This guard
	// arbitrates between two candidate repositories when formwork cannot tell
	// which the operator meant, and under the hatch there is nothing to
	// arbitrate — the operator named one, in the only way the layout it exists
	// for can be expressed, and refusing on this comparison would leave that
	// layout with no working spelling at all. What the hatch must still refuse is
	// every case that is NOT that layout — the environment naming a work tree
	// that is not the one -C names, or naming no work tree at all; hatch.go is
	// that policy, and it runs here because the scrub's own comparison below is
	// skipped whenever the hatch is on.
	if err := ensureHatchNamesRootsRepository(root); err != nil {
		return nil, gitEnvRefusal{err}
	}
	// Nothing in the POINTER family was removed, so nothing can have moved which
	// repository git answers about. That is BOTH the ordinary run, where none of
	// those three variables is set, and the hatch, where gitEnv returns a nil env
	// and removes nothing at all. A history variable (#176) reaches here with
	// removed non-empty and moved empty: it is scrubbed and nothing further is
	// owed, because both environments resolve the same repository and the probe
	// would compare two identical answers at the cost of two forks.
	moved := MovedRepository(removed)
	if len(moved) == 0 {
		return env, nil
	}
	if err := ensureScrubKeptTheRepository(root, env, moved); err != nil {
		return nil, gitEnvRefusal{err}
	}
	return env, nil
}

// ErrGitEnv marks an error as gitEnvFor's own refusal rather than an answer git
// gave. errors.Is against it is the only way to tell the two apart, because
// those refusals carry git's failures inside their text.
//
// IT EXISTS FOR CALLERS THAT EXEMPT A GIT FAILURE FROM BEING AN ERROR. Such an
// exemption is always argued from something git DID — internal/hooks' verify
// exempts the HooksPath call for a worktree git has already reported prunable,
// on the ground that git's refusal to answer for that directory IS the
// diagnosis it already made at exit 0. No such argument survives here: where
// this policy refuses, git was never run, so the exemption would render an
// engine error as a wiring problem.
//
// A CALLER THAT DOES NOT DISTINGUISH NEEDS NOTHING. Every error gitEnvFor
// returns still reads exactly as it did — the tag adds a predicate, not a
// wrapper an operator sees — so the ordinary path, where a git failure is an
// error either way, is unchanged.
var ErrGitEnv = errors.New("the git environment policy refused")

// gitEnvRefusal tags one refusal without touching what it says. The message IS
// the diagnosis here — it names both repositories and the hatch — so wrapping it
// in a second sentence would put a formwork-internal label in front of it.
type gitEnvRefusal struct{ error }

// Is answers for the sentinel only; Unwrap keeps the chain, so a caller matching
// on something the refusal already wraps still matches.
func (gitEnvRefusal) Is(target error) bool { return target == ErrGitEnv }
func (e gitEnvRefusal) Unwrap() error      { return e.error }

// ensureScrubKeptTheRepository asks git which repository it resolves for root,
// once with the environment as the caller has it and once with scrubbedGitVars
// removed, and refuses when the two disagree.
//
// IT COSTS TWO FORKS PER GIT CALL — four in the bare-to-bare case that takes the
// retry — AND ONLY WHERE ONE OF THE POINTER VARIABLES IS SET. A history variable
// (#176) is scrubbed without arming this, because it cannot move the answer this
// question asks (MovedRepository).
// gitEnvFor skips it entirely otherwise, which is every ordinary run — the
// scrubbedGitVars paragraph above records that git's hook environment carries
// none of the three pointers, and `submodule foreach`, the one flow that sets GIT_DIR, is
// rare by construction. Armed, it triples this package's git calls: counted
// through a logging shim on PATH, `check` over a corpus declaring scan.gitignore
// went 2 to 6 and `check --staged` 4 to 12. The multiplier is on the number of
// git CALLS, which is a small constant, not on the number of files.
//
// There is deliberately NO CACHE. gitEnv reads the environment on every call for
// an argued reason, and a verdict memoised on root would contradict it — the
// environment can change within a process — while saving single-digit forks on a
// path nothing hot reaches.
//
// THE COMPARISON IS ON THE EXIT STATUS AS WELL AS THE OUTPUT, which is what lets
// one predicate cover three states. Two successes agreeing is silence. One of
// each is a DIFFERENCE and is refused as one: an environment that decides
// whether git works at all is exactly what this asks about. Two failures with
// the same status is the third, and it is the only one needing a second look —
// git could not answer, and the reason may be that this root is not a repository
// (the caller's own call is about to say so) or merely that it is BARE, where
// `ls-files` still answers and the run would go on against whichever repository
// the scrub landed on. So that case re-asks without the part a bare repository
// refuses, rather than concluding anything from the first failure. What that
// buys is bounded — see bareRepoIdentity.
func ensureScrubKeptTheRepository(root string, scrubbed, removed []string) error {
	return compareRepoIdentity(root, scrubbed, removed, repoIdentity)
}

// compareRepoIdentity runs one identity question under both environments and
// reports a disagreement. On a mutual failure it retries with the bare-safe
// subset, once — q is the full question and the retry is the only recursion.
func compareRepoIdentity(root string, scrubbed, removed []string, q identityQuestion) error {
	ambientOut, ambientCode, ambientErr := gitExitEnv(nil, root, q.args...)
	scrubbedOut, scrubbedCode, scrubbedErr := gitExitEnv(scrubbed, root, q.args...)

	if ambientCode == scrubbedCode && ambientOut == scrubbedOut {
		// gitExitEnv returns no stdout on a failure, so equality here also covers
		// "both runs failed the same way". Retry the bare-safe subset exactly once,
		// which is what distinguishes a bare repository from a non-repository:
		// the subset answers for the first and fails identically again for the
		// second, landing back here with nothing to report.
		if ambientErr != nil && scrubbedErr != nil && q.parts != bareRepoIdentity.parts {
			return compareRepoIdentity(root, scrubbed, removed, bareRepoIdentity)
		}
		if ambientErr != nil {
			// A QUESTION FORMWORK CANNOT ASK MUST NOT READ AS AGREEMENT, which is
			// the posture validateIdentity below already takes for an answer it
			// cannot READ. Both runs failing identically is what a git that REFUSES
			// `--path-format` produces rather than echoing it, and reaching here
			// means the bare-safe retry above failed the same way — so there is no
			// answer under either environment and nothing was compared. Returning nil
			// here left the scrub free to substitute an ancestor repository's answer
			// in silence, which is the whole of what this guard exists to prevent:
			// measured through a PATH shim, TrackedUnder answered from the ancestor
			// where the environment named a repository tracking the file
			// (env_test.go).
			//
			// THE HATCH IS NOT OFFERED AS A CURE, unlike the refusals either side of
			// this one. hatch.go refuses this identical failure in its own guard, so
			// FORMWORK_GIT_ENV=inherit refuses the state as well — advice that does
			// not cure. Unsetting the variables does cure it: with none of them set
			// the probe is skipped entirely (gitEnvFor).
			return fmt.Errorf("git: this git cannot answer the question formwork needs to tell which repository it resolves for -C %s, so it cannot tell whether %s in the environment moves the answer: %w. Unset %s, or use a git that answers `rev-parse --path-format` (2.31 or newer) — moving -C does not help, because this question fails whatever it names",
				root, strings.Join(removed, ", "), ambientErr, strings.Join(removed, ", "))
		}
		// The two runs agree — but agreeing on something that is not an answer is
		// not agreement, and git says so at exit 0. Validated only here, once,
		// because a run that FAILED is already covered above and a run that
		// DIFFERED is refused below on its own evidence.
		if _, err := validateIdentity(q, ambientOut); err != nil {
			return fmt.Errorf("git: formwork could not ask git which repository it resolves for -C %s, so it cannot tell whether %s in the environment moves the answer: %w. Unset %s, or set %s=%s to use the environment's",
				root, strings.Join(removed, ", "), err, strings.Join(removed, ", "), GitEnvVar, gitEnvInherit)
		}
		return nil
	}
	return fmt.Errorf("git: %s in the environment moves the repository git resolves for -C %s — with it, %s; without it, %s. formwork will not choose between them: unset %s, or set %s=%s to use the environment's",
		strings.Join(removed, ", "), root,
		renderIdentity(ambientOut, ambientErr), renderIdentity(scrubbedOut, scrubbedErr),
		strings.Join(removed, ", "), GitEnvVar, gitEnvInherit)
}

// renderIdentity renders one run of an identity question for an operator.
//
// IT LABELS THE PARTS rather than printing git's raw lines. They move
// independently — GIT_DIR moves the git directory, GIT_WORK_TREE only the work
// tree, GIT_COMMON_DIR only the shared directory — so an unlabelled list leaves
// the reader to guess which one changed, and for the last two the git
// directories are identical and the message would read as if nothing had. An
// answer longer or shorter than the labels is quoted whole rather than
// mislabelled.
//
// IT IS INDEXED OFF THE SLICE, NOT OFF q.parts, and that is a correctness
// requirement rather than a style choice. An earlier version switched on
// bareRepoIdentity.parts and repoIdentity.parts while hard-coding two and three
// index accesses in the bodies — safe only while those fields happened to be 2
// and 3. Changing the question to two parts made `case 1:` read lines[1] and
// PANIC, which a mutation run found; a panic here would turn a refusal into a
// crash, and the exit-code contract has no state for that.
//
// IT DOES NOT VALIDATE, deliberately: it renders the losing side of a
// disagreement, where a malformed answer is evidence to show the operator rather
// than a verdict to draw. validateIdentity is where an unreadable answer decides
// something.
func renderIdentity(out string, err error) string {
	if err != nil {
		return "git failed: " + err.Error()
	}
	labels := []string{"git directory", "shared directory", "work tree"}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		lines = append(lines, strings.TrimSuffix(l, "\r"))
	}
	if len(lines) == 0 || len(lines) > len(labels) {
		return fmt.Sprintf("git answered %q", out)
	}
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = labels[i] + " " + l
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// gitEnvInheriting reports whether the operator asked formwork to leave the
// environment alone, and refuses any other spelling.
//
// UNSET AND SET-BUT-EMPTY ARE DIFFERENT ANSWERS, which is why this reads
// os.LookupEnv: os.Getenv collapses them, and `FORMWORK_GIT_ENV=` would then
// read as "not set" — a deliberate act silently answered as the default.
//
// Refusing an unrecognised value rather than falling back is the fail-closed
// direction on the axis that matters here, which is not the scrub. Falling back
// would leave an operator who mistyped the hatch believing it was on while
// formwork scrubbed anyway, debugging a failure whose cause is invisible.
//
// WHAT THAT FAILURE IS DEPENDS ON THE LAYOUT, and an earlier version of this
// comment named only the loud one. Scrubbed, a detached git dir plus work tree
// goes `fatal: not a git repository` ONLY while no ancestor of root is a
// repository; where one is, git discovers it and answers at exit 0. That second
// case is why gitEnvFor exists — it is the one a mistyped hatch would otherwise
// leave silent. The refusal names the accepted spelling, so it cures itself in
// one reading.
func gitEnvInheriting() (bool, error) {
	val, set := os.LookupEnv(GitEnvVar)
	if !set {
		return false, nil
	}
	if val != gitEnvInherit {
		return false, fmt.Errorf("%s=%q is not a value formwork understands: the only accepted value is %q, which makes formwork honour the ambient git environment unchanged. Unset %s to get the default, which removes %s from the environment of the git commands formwork itself runs. A `command:` rule is not one of those: it runs your argv with the environment as you set it, and formwork refuses the run rather than scrubbing it where one of these variables could move the tool's answer",
			GitEnvVar, val, gitEnvInherit, GitEnvVar, strings.Join(scrubbedGitVars, ", "))
	}
	return true, nil
}

// GitEnvNotice returns the line an operator must see when the hatch is in
// force, empty when it is not, and an error for a value gitEnvInheriting
// refuses.
//
// IT RETURNS THE TEXT RATHER THAN PRINTING IT. The hatch is a fact about the
// environment, which this package owns; a stream to write it to is a fact about
// the command surface, which it does not — nothing else in this package writes
// to stdout or stderr, and giving it a first opinion about output would put a
// diagnostic in front of every library consumer.
//
// The announcement is owed because the hatch turns a guard off from the
// environment, so an invocation running under it is indistinguishable, in a
// shell or a CI log, from one that is not.
//
// THE TEXT CARVES OUT `command:` RULES, AND SO DOES gitEnvInheriting's REFUSAL
// ABOVE (#335). Both used to say the variables are removed from "every git
// command formwork runs", which was one command short of true from the day it
// was written: internal/rules/command builds its own exec.Command with cmd.Dir
// and no cmd.Env, so a `command:` rule shelling out to git keeps every one of
// them whatever this package decides. Measured against a repository whose only
// rule echoes its environment: with GIT_DIR and GIT_WORK_TREE set to that
// repository the tool printed both intact, and the rule was not refused.
//
// THE SENTENCE WAS STALE IN THE OTHER DIRECTION TOO, and the earlier version of
// this paragraph said so while deferring the correction to "#177's fix", on the
// grounds that operator-facing text should not move ahead of the fix that
// decides what it says. #177 and #213 both closed COMPLETED on 2026-08-24, and
// what they added for a command rule is a REFUSAL rather than a scrub:
// ensureRepositoryAgreement asks vcs.EnsureNoInheritedHistoryEnv for the
// object-store six and vcs.CommonDir for the pointer three. Measured, again in
// that repository: an ambient GIT_ALTERNATE_OBJECT_DIRECTORIES and a GIT_DIR
// naming an unrelated repository each stop the rule at exit 2, naming the
// variable. So the old sentence described neither the behaviour it had nor the
// one that closed the hole, and the correction it waited for had landed.
//
// WHAT EACH TEXT NOW SAYS is scoped to "the git commands formwork ITSELF runs"
// and then names the carve-out in the words docs/quickstart.md already uses to
// operators. TestTheHatchTextDoesNotClaimTheScrubReachesACommandRule pins both
// texts, and refuses both spellings of the universal so an edit cannot restore
// it by reflex.
func GitEnvNotice() (string, error) {
	inherit, err := gitEnvInheriting()
	if err != nil || !inherit {
		return "", err
	}
	return fmt.Sprintf("%s=%s — honouring the ambient git environment: %s are NOT removed from the environment of the git commands formwork itself runs, so they may redirect formwork to a different repository than the one named by -C (%s), or to a different history inside the right one (%s). A `command:` rule never had them removed — it runs your argv with the environment as you set it — and the object-store refusal that stands in for the scrub there (#213) is off under this hatch too",
		GitEnvVar, gitEnvInherit, strings.Join(scrubbedGitVars, ", "),
		strings.Join(scrubbedRepoVars, ", "), strings.Join(scrubbedHistoryVars, ", ")), nil
}

// EnsureNoInheritedHistoryEnv refuses when any of the OBJECT-STORE family is
// set in the ambient environment (#213).
//
// It exists for internal/rules/command, and it is deliberately a REFUSAL rather
// than the scrub this package applies to its own git calls (#176). The two
// callers are not in the same position. internal/vcs runs formwork's OWN git
// commands, so removing a variable it never meant to honour is free. `command`
// runs the OPERATOR's argv under the disclosed contract that the environment is
// inherited unchanged; deleting variables there would be formwork deciding on
// the tool's behalf, and would change what an already-working rule sees.
//
// WHY A REFUSAL IS NEEDED AT ALL, given no comparison can see these. That is
// exactly why. scrubbedHistoryVars' own comment says no identity comparison can
// see them, so command's ensureRepositoryAgreement — an identity question — is
// structurally blind here. Measured before this existed: a rule
// `cmd: [git, cat-file, -e, SHA]` for a SHA present only in another repository
// FAILS at exit 1 with a clean environment and PASSES at exit 0 under an ambient
// GIT_ALTERNATE_OBJECT_DIRECTORIES naming that repository's object store. A pass
// earned over objects nobody named is the failure this refuses.
//
// PRESENCE IS THE TRIGGER, not a comparison, because there is nothing to compare
// against — and that is affordable only because these are rare where formwork
// runs: gitEnvFor's measurement records that git does not set any of the six in
// this position. Where an operator does mean one — the receive-pack quarantine
// is the real case — GitEnvVar=inherit is the answer, the same hatch #176 and
// #177 use rather than a second one that would have to be discovered separately.
//
// The error is tagged ErrGitEnv like every other refusal in this file, so a
// caller can tell policy from a git failure, and it NAMES the variable: an
// operator cannot cure what the message does not identify.
func EnsureNoInheritedHistoryEnv() error {
	inheriting, err := gitEnvInheriting()
	if err != nil {
		// TAGGED, because gitEnvInheriting returns a bare error and every path
		// that reached it before this function existed tagged it downstream —
		// CommonDir did. Returning it raw made a mistyped hatch stop carrying
		// ErrGitEnv the moment this guard ran first, so a caller could no longer
		// tell policy from a git failure. Caught by
		// TestCommandRefusesAMistypedHatch, which predates this change.
		return gitEnvRefusal{err}
	}
	if inheriting {
		return nil
	}
	var found []string
	for _, name := range scrubbedHistoryVars {
		if _, ok := os.LookupEnv(name); ok {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return gitEnvRefusal{fmt.Errorf(
		"%s is set in the environment: it moves which commits and objects git answers from INSIDE this repository, so a tool this rule runs can be answered from history nobody named, and no identity check can see the difference. Unset it, or set %s=inherit to take that decision deliberately",
		strings.Join(found, ", "), GitEnvVar)}
}
