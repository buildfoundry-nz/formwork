// The ambient git configuration environment, for both commands in this package
// (#167 D9). Verify's R7 guard used to live in verify.go and install had no
// equivalent at all; this file is the shared replacement, and the asymmetry it
// removes was backwards — the READ command refused while the command that
// rewrites `.git/config` did not.
package hooks

import (
	"errors"
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// configEnvQuestions are the questions this package asks git ABOUT THE ROOT IT
// WAS INVOKED AT, and the only thing measured about the environment there. A
// root it merely reaches — a linked worktree — is measured separately, at that
// root: see configEnvProblemsAt.
//
// WHY A LIST OF QUESTIONS AND NOT A LIST OF VARIABLES. The guard this replaces
// enumerated variables, and an enumeration of variables is unbounded — git's
// documentation does not name GIT_CONFIG_PARAMETERS anywhere, so no reading of
// it produces the one that mattered. The questions are bounded by something this
// repository owns: they are questions vcs owns the argv of, so each is provably
// the same question the function beside it asks.
//
// THE MEASURED SET IS NOT EVERY GIT CALL THESE COMMANDS MAKE, and the claim
// that it was — "they are the git calls these two commands make, which grep
// finds and a reviewer can count" — is what let a gap read as closed. `grep -n
// 'vcs\.[A-Z][A-Za-z]*(' internal/hooks/*.go` finds nine call sites outside
// this file and outside _test.go. Four are covered by the three questions here:
// vcs.HooksPath in Verify and in preflight, vcs.TopLevel in preflight,
// vcs.CommonDir in defaultHooksDir. Of the other five:
//
//   - vcs.HooksPath(wt.Path) in worktreeProblems asks THIS question about a
//     DIFFERENT root, so no measurement taken here answers for it. It is
//     measured at that root, by configEnvProblemsAt.
//   - vcs.SetConfig in Install is a write, not a question.
//   - vcs.RepoConfig (preflight), vcs.RepoConfigWithIncludes
//     (includedWiringRefusal) and vcs.Worktrees (worktreeProblems) are
//     unmeasured. Measured on git 2.50.1 in a repository with a local
//     core.hooksPath and two worktrees, GIT_CONFIG_PARAMETERS and
//     GIT_CONFIG_GLOBAL left `config --local [--includes] --get core.hooksPath`
//     and `worktree list --porcelain` byte-identical, and GIT_CONFIG makes the
//     config reads exit 129 ("only one config file at a time") — an error, so
//     exit 2 rather than a verdict. That is one measurement of today's git and
//     not a guarantee about them.
//
// THE THREE ARE NOT EQUALLY PROVEN, and that is stated rather than implied.
// HooksPathQuestion has a reproduction (gitenv_test.go, from #167): a
// GIT_CONFIG_PARAMETERS naming core.hooksPath moves it, and both commands acted
// on the moved answer. For the other two no config key was found on git 2.50.1
// that an ambient variable can use to move them — `core.worktree` and
// `core.bare` supplied through GIT_CONFIG_PARAMETERS left `rev-parse
// --show-toplevel --show-prefix` and `--git-common-dir` byte-identical, measured
// — so they are probed for the shape of the guarantee rather than against a
// known attack: "no answer this decision rests on was moved" is a claim about
// the decision, and dropping the rows nobody could move today would make it a
// claim about one measurement instead.
var configEnvQuestions = []vcs.ConfigEnvQuestion{
	vcs.HooksPathQuestion,
	vcs.TopLevelQuestion,
	vcs.CommonDirQuestion,
}

// hooksPathOnly is what verify asks about a LINKED worktree: the one question it
// puts to git about that directory. See configEnvProblemsAt.
var hooksPathOnly = []vcs.ConfigEnvQuestion{vcs.HooksPathQuestion}

// configEnvEffects returns one effect per question the ambient git
// configuration environment moves at root, and nothing at all when it moves
// none.
//
// ROOT IS A PARAMETER BECAUSE THE ANSWER IS. vcs.MeasureConfigEnv runs git at
// the directory it is handed, and git resolves core.hooksPath per worktree —
// under extensions.worktreeConfig a linked worktree carries one of its own. A
// measurement taken at one root therefore says nothing about a question asked at
// another, which is the gap this parameter closes: verify measured at root and
// then called vcs.HooksPath on every linked worktree's path.
//
// A MEASUREMENT FAILURE IS AN ERROR, never an empty result. It means git could
// not be asked, and every caller below treats "formwork could not find out" as a
// reason to stop rather than as a clean bill of health — verify's R6 (a git
// failure is exit 2, not a wiring diagnosis) and install's pre-flight, which
// must not write on a question nobody answered.
func configEnvEffects(root string, questions []vcs.ConfigEnvQuestion) ([]vcs.ConfigEnvEffect, error) {
	var moved []vcs.ConfigEnvEffect
	for _, q := range questions {
		eff, err := vcs.MeasureConfigEnv(root, q)
		if err != nil {
			return nil, err
		}
		if eff.Changed {
			moved = append(moved, eff)
		}
	}
	return moved, nil
}

// configEnvLine renders one moved answer, ending in the caller's own verdict.
//
// BOTH ANSWERS ARE PRINTED, because the finding is a disagreement and one half
// of it is not evidence of anything. The variables are named as a group rather
// than blamed individually: the measurement removes the family in one step, so
// which member of it did the moving is not something this has asked git.
func configEnvLine(eff vcs.ConfigEnvEffect, verdict string) string {
	if len(eff.Removed) == 0 {
		// Not reachable through vcs.MeasureConfigEnv today, and therefore
		// untested — with nothing removed the two runs are the same invocation
		// under the same environment, so a difference between them is git
		// answering inconsistently rather than anything the environment did. Kept
		// because the alternative is a sentence naming no variable at all
		// ("  is set in the environment"), which reads as a formwork bug and tells
		// the operator nothing to do.
		return fmt.Sprintf("git reported %s differently on two consecutive runs of the same command in the same environment — with one, %s; with the other, %s. %s",
			eff.Question, eff.Ambient, eff.Scrubbed, verdict)
	}
	return fmt.Sprintf("%s is set in the environment and changes what git reports for %s — with it, %s; without it, %s. A plain `git commit` here runs without it, so %s. Re-run with %s unset; if that configuration is meant to hold in this repository, set it in the repository's own config, where git uses it for every commit rather than only where those variables are set",
		strings.Join(eff.Removed, ", "), eff.Question, eff.Ambient, eff.Scrubbed, verdict, strings.Join(eff.Removed, ", "))
}

// configEnvProblems is verify's side: each moved answer is a wiring problem
// (exit 1), not an error.
//
// R6 GOVERNS THE OTHER RETURN AND NOT THIS ONE. An error here means verify could
// not ask git, which is exit 2; a moved answer is something verify DID find out,
// and the finding is that this environment cannot certify anything — the
// developer whose commit is meant to be gated will not have it set.
func configEnvProblems(root string) ([]string, error) {
	return configEnvProblemsAt(root, configEnvQuestions, "")
}

// configEnvProblemsAt is the same, for a root verify reaches that is not the one
// it was invoked at, with prefix labelling whose answer moved.
//
// IT ASKS ONLY THE QUESTIONS ITS CALLER ACTS ON THERE, which for a linked
// worktree is HooksPathQuestion alone — worktreeProblems' vcs.HooksPath(wt.Path)
// is the only thing it asks git about that directory. Measuring the other two
// would fork four more times per worktree to report a moved answer nothing
// reads.
//
// THE COST WAS MEASURED BEFORE THE SET WAS CHOSEN, because vcs.MeasureConfigEnv
// forks twice per question and a repository with N worktrees pays 2N. Two
// binaries off this branch, one with the call below and one without, timed 20
// runs each of `hooks verify` on the two-worktree reproduction repository: 110.0
// ms/run against 107.5 — about 2.5ms for the one linked worktree actually
// checked (the main worktree's entry is deduped against root, which was measured
// there). All three questions would be ~7.5ms. That is small enough that the
// narrow set is a choice about what the answer MEANS rather than one forced by
// the clock.
func configEnvProblemsAt(root string, questions []vcs.ConfigEnvQuestion, prefix string) ([]string, error) {
	moved, err := configEnvEffects(root, questions)
	if err != nil {
		return nil, err
	}
	var probs []string
	for _, eff := range moved {
		probs = append(probs, prefix+configEnvLine(eff, "formwork cannot certify this wiring"))
	}
	return probs, nil
}

// configEnvRefusal is install's side: a moved answer aborts the whole install,
// before the first write.
//
// IT IS A REFUSAL AND NOT A FLAG-CLEARABLE ONE. --override-global answers "this
// repository is different from what my machine says", which is a statement about
// this repository; an environment that moves git's answer is not about this
// repository at all, so there is nothing for that flag to authorise. D2's
// argument, applied to a different mover of the same answer.
func configEnvRefusal(root string) error {
	moved, err := configEnvEffects(root, configEnvQuestions)
	if err != nil {
		return err
	}
	if len(moved) == 0 {
		return nil
	}
	var lines []string
	for _, eff := range moved {
		lines = append(lines, configEnvLine(eff, "formwork will not wire hooks from an environment that answers differently"))
	}
	return errors.New(strings.Join(lines, "\n"))
}
