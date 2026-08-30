package vcs

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// THIS FILE IS THE OTHER HALF OF env.go's POLICY, and the two are deliberately
// different in kind. env.go REMOVES two families from every git command — the
// repository pointers and the history/object-store family (#176) — because
// neither an ambient repository pointer nor an ambient rewrite of the history
// inside it is an intent this package can honour. The variables below are
// removed from NOTHING a caller runs: they carry
// configuration an operator supplied on purpose, there is no correct spelling to
// normalise them into, and silently discarding one would answer a question
// nobody asked. What this file offers instead is a MEASUREMENT — run one git
// question twice, with the family and without it — so a caller can refuse on
// what the environment DID rather than on which variables happen to be set.
//
// WHY MEASURE INSTEAD OF LIST. The guard this replaces tested os.LookupEnv over
// seven names swept out of git's documentation, and it was wrong in both
// directions at once. GIT_CONFIG_COUNT=0 and an empty GIT_CONFIG_NOSYSTEM change
// nothing git does, and it refused a healthy repository over them; meanwhile
// GIT_CONFIG_PARAMETERS — git's own `-c` propagation channel — appears in
// neither git(1)'s ENVIRONMENT section nor git-config(1) (swept on git 2.50.1,
// both), so no reading of the documentation would have added it, and it walked
// `hooks install` straight over a live husky wiring. Comparing two answers turns
// "did I enumerate every variable" into "did the environment change this
// answer", which is the question a caller actually has.
//
// THE LIST STILL BOUNDS THE MEASUREMENT, and that is stated rather than glossed:
// a variable outside configEnvPrefix is in both runs, so a difference it causes
// cannot show up as a difference here. What the prefix buys is that the bound is
// on a NAMESPACE git owns rather than on a set of names someone had to think of.

// configEnvPrefix is the whole family, spelled as a namespace.
//
// Every variable through which git takes CONFIG-FILE content or a `-c`
// propagation from the environment begins with
// it: GIT_CONFIG, GIT_CONFIG_GLOBAL, GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM,
// GIT_CONFIG_PARAMETERS, GIT_CONFIG_COUNT and the GIT_CONFIG_KEY_<n> /
// GIT_CONFIG_VALUE_<n> pairs it enumerates, plus the historical
// GIT_CONFIG_NOGLOBAL. A prefix rather than a list because the list is the shape
// that already failed: a name git adds later is inside the namespace and outside
// any list written today.
//
// THAT IS NARROWER THAN "EVERYTHING GIT READS TO TAKE CONFIGURATION FROM THE
// ENVIRONMENT", which is what an earlier version of this sentence said and is
// false: GIT_PAGER, GIT_EDITOR, GIT_ASKPASS and GIT_SSH_COMMAND each override a
// config key (core.pager, core.editor, core.askPass, core.sshCommand) and none
// of them is inside this namespace. The consequence here is nil — each of those
// names a PROGRAM git may run for a pager, an editor, a credential prompt or a
// transport, and none can change what `rev-parse` reports for the questions
// below — so the bound is still the right one to draw. It is a bound, not an
// absolute, and the paragraph above already says a variable outside it cannot
// show up as a difference.
//
// IT TAKES THE WHOLE DECLARATION OR NONE OF IT. GIT_CONFIG_COUNT=1 is one half
// of a statement whose other half is GIT_CONFIG_KEY_0/GIT_CONFIG_VALUE_0.
// Measured on git 2.50.1: removing the count alone is enough to make git ignore
// the keys — with GIT_CONFIG_KEY_0=core.hooksPath and no count, `rev-parse
// --git-path hooks` answered the repository's own value. The prefix removes both
// halves anyway, so the probe never hands git a partial declaration and never
// depends on that measurement staying true.
const configEnvPrefix = "GIT_CONFIG"

// ConfigEnvQuestion is one question this package asks git, in the form a caller
// names when it wants to know whether the environment moved the answer.
//
// THE ARGV IS UNEXPORTED AND SHARED WITH THE FUNCTION THAT ASKS IT. A caller
// outside this package cannot spell a question of its own, which keeps two
// things true: a probe cannot drift into asking about something adjacent to what
// the caller actually reads, and the questions stay enumerable here rather than
// scattered across consumers. The zero value asks nothing and MeasureConfigEnv
// refuses it — a question with no argv would run bare `git` twice and report
// "nothing changed" for every environment there is.
type ConfigEnvQuestion struct {
	// What names the question in git's terms, for a message an operator reads.
	What string
	args []string
}

// The questions, each paired with the exported function that asks it. Sharing
// the argv is what makes the pairing a fact rather than a comment.
var (
	// HooksPathQuestion is HooksPath's: where git will run hooks.
	HooksPathQuestion = ConfigEnvQuestion{What: "where git runs hooks", args: hooksPathArgs}
	// TopLevelQuestion is TopLevel's: which directory is the repository top
	// level, and how far below it the caller's root sits.
	TopLevelQuestion = ConfigEnvQuestion{What: "the repository top level", args: topLevelArgs}
	// CommonDirQuestion is CommonDir's: the git directory every worktree shares,
	// which is where the hooks git runs by default live.
	CommonDirQuestion = ConfigEnvQuestion{What: "the git directory worktrees share", args: commonDirArgs}
)

// ConfigEnvEffect is what one question answered twice reports.
//
// Ambient and Scrubbed are for the operator to READ, not for a caller to compare
// — Changed is the comparison, made on git's exit status as well as its output,
// and a run that failed renders here as its error rather than as an empty
// answer. Comparing the display strings instead would call two different
// failures the same answer.
type ConfigEnvEffect struct {
	// Question is the What of the question that was asked.
	Question string
	// Changed reports that the two runs disagreed.
	Changed bool
	// Ambient is git's answer with the environment MeasureConfigEnv was given —
	// the caller's, less env.go's scrubbedGitVars, because the ambient run is
	// built from gitEnv() so that both runs differ in the config family alone.
	// Under FORMWORK_GIT_ENV=inherit gitEnv removes nothing, and it is then the
	// caller's environment unchanged.
	Ambient string
	// Scrubbed is git's answer with the GIT_CONFIG family removed.
	Scrubbed string
	// Removed names the variables the probe actually removed — the ones both
	// present in the environment and inside the family. Empty when none were set,
	// which is ORDINARILY the state where Changed cannot be true: with nothing
	// removed the two runs are the same invocation under the same environment.
	// It is not a guarantee, and an earlier version of this sentence stated it as
	// one — git answering the same question differently across two consecutive
	// forks reaches empty-and-Changed, and internal/hooks' configEnvLine has a
	// live branch for exactly that state rather than a message naming no variable.
	Removed []string
}

// MeasureConfigEnv asks git one question twice — once with the environment as it
// is, once with the GIT_CONFIG family removed — and reports whether the answers
// differ.
//
// IT CHANGES NOTHING ABOUT A REAL RUN. The removal lives inside the second
// invocation's own environment; os.Environ is not modified, no other git command
// this package runs is affected, and a caller that ignores the result behaves
// exactly as before. That is why FORMWORK_GIT_ENV=inherit does not turn this
// off: the hatch exists so an operator can stop env.go REMOVING variables from
// the commands formwork runs, and nothing here removes one from a command
// anybody asked for.
//
// AN ERROR MEANS THE COMPARISON WAS NOT PERFORMED, which is not the same as
// "nothing changed" and must not be read as one: both runs failing is the state
// where git could not answer at all. Where the two runs fail DIFFERENTLY — one
// exit status, one answer — that is a difference, and it is reported as one; an
// environment the answer depends on for whether git works at all is exactly what
// a caller is asking about.
func MeasureConfigEnv(root string, q ConfigEnvQuestion) (ConfigEnvEffect, error) {
	if len(q.args) == 0 {
		return ConfigEnvEffect{}, fmt.Errorf("vcs: a config-environment measurement needs one of this package's questions, not the zero value")
	}
	eff := ConfigEnvEffect{Question: q.What}

	// gitEnv, not gitEnvFor: this measures the CONFIG family, and running the
	// repository-pointer guard here would report a difference on the other axis
	// as if it were this question's answer.
	//
	// NO RUN PASSES BECAUSE OF THAT, on either branch. A moved config answer is
	// already a refusal by the caller; an unmoved one lets the caller carry on to
	// an ordinary git call, where gitEnvFor decides the repository-pointer
	// question — including under the hatch, where hatch.go's own guard runs. It
	// allows two states deliberately: the layout the hatch exists for, and the
	// coherent one where the ambient environment names the repository -C resolves
	// anyway, which is what `submodule foreach` exports and where there is
	// nothing to arbitrate. Neither is a bypass. That sentence was written when
	// the hatch skipped every such guard, and a `hooks install` under it was a
	// run that passed for exactly this reason.
	// What it does mean is that a caller which refuses HERE renders answers taken
	// under whatever repository the ambient environment named — a wrong
	// repository in a message that is already an exit 2, never a verdict anybody
	// acts on.
	ambientEnv, _, err := gitEnv()
	if err != nil {
		return ConfigEnvEffect{}, err
	}
	probeEnv, removed := withoutConfigEnv(ambientEnv)
	eff.Removed = removed

	ambientOut, ambientCode, ambientErr := gitExitEnv(ambientEnv, root, q.args...)
	probeOut, probeCode, probeErr := gitExitEnv(probeEnv, root, q.args...)
	if ambientErr != nil && probeErr != nil && ambientCode == probeCode {
		// Both runs failed the same way, so there is no answer to compare and the
		// failure is the caller's to report at the call it makes itself. Returning
		// the ambient error keeps the exit-code contract's "a git failure is an
		// engine error" (vcs.go) rather than inventing a verdict from it.
		return ConfigEnvEffect{}, ambientErr
	}
	eff.Ambient = answerOrError(ambientOut, ambientErr)
	eff.Scrubbed = answerOrError(probeOut, probeErr)
	eff.Changed = ambientCode != probeCode || ambientOut != probeOut
	return eff, nil
}

// answerOrError renders one run for an operator: git's answer, or why there
// isn't one. The trailing newline goes because this lands mid-sentence; nothing
// else is trimmed, for the reason this package's parsers do not trim (vcs.go).
func answerOrError(out string, err error) string {
	if err != nil {
		return "git failed: " + err.Error()
	}
	return strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
}

// withoutConfigEnv returns env less the GIT_CONFIG family, and the names it
// removed.
//
// A nil env is os/exec's "inherit the parent's environment", which is what
// gitEnv returns under FORMWORK_GIT_ENV=inherit. Inheriting is not something
// this probe can do — it has to hand git a filtered environment — so the
// parent's is read explicitly, which reproduces the same set of variables that
// inheriting would have given the ambient run.
func withoutConfigEnv(env []string) (kept, removed []string) {
	if env == nil {
		env = os.Environ()
	}
	kept = make([]string, 0, len(env))
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		// An entry with no "=" is not a variable this can be about — the same
		// reading env.go's filter takes, and for the same reason. Names are
		// compared exactly, which is what the environment is on unix; a Windows
		// port needs a case fold here, as env.go's does.
		if ok && strings.HasPrefix(name, configEnvPrefix) {
			removed = append(removed, name)
			continue
		}
		kept = append(kept, kv)
	}
	slices.Sort(removed) // named in operator-facing text
	return kept, removed
}
