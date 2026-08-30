// Install's pre-flight: the states where formwork must refuse to wire hooks at
// all, rather than take over wiring that is already there (#146 D1/D2/D7).
//
// IT IS A SEPARATE FUNCTION FOR A REASON THAT IS NOT TIDINESS. "A refusal
// changes nothing" is true HERE and false one function away: install still
// writes the healthy lanes' shims when another git-hook lane selects no rules,
// and returns that diagnosis alongside the list of what it wired, because
// refusing the whole install over a stale lane left developers with no gate at
// all (install.go argues that ordering at its site). Two `if`s inside Install
// would have made "changes nothing" read as a property of the command. A
// function returning a bare error, called before the first write, cannot be
// mistaken for the loop that returns (wired, err).
package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// preflight is THE ONLY STATE-DEPENDENT REFUSAL THAT ABORTS THE WHOLE INSTALL.
// It returns an error for a state install must not proceed from, and it neither
// writes a file nor changes a setting on any path — including its own error
// paths, which is what makes the caller's `return nil, err` a complete account
// of what happened.
//
// It is not the command's only whole-install abort: install.go refuses a config
// naming no git-hook lane at all, before it calls here. That one is a judgement
// about the config as written rather than about the repository's state, and
// neither of the two is the empty-lane diagnosis — which deliberately wires the
// healthy lanes and reports.
// overrideGlobal is the operator's `--override-global`: yes, this repository is
// different from whatever the machine says. It answers D7 and nothing else —
// wiring the project itself declared is not something a flag clears.
func preflight(root string, cfg *config.Config, overrideGlobal bool) error {
	// FIRST, because every refusal below reads an answer this could have moved —
	// including the top level subdirectoryRefusal decides on. gitenv.go holds the
	// construction; what it buys here is that the two questions below describe
	// the same configuration, which is the premise the D2/D7 split rests on.
	//
	// Measured on git 2.50.1, over a repository whose local config says `.husky`:
	// with GIT_CONFIG_PARAMETERS supplying core.hooksPath=.formwork/hooks, `live`
	// reads as formwork's own directory while `declared` stays `.husky`, so
	// declaredWiringRefusal concluded install was repairing its own wiring and
	// overwrote the operator's at exit 0 (#167 member 1).
	if err := configEnvRefusal(root); err != nil {
		return err
	}
	// ONE QUESTION, TWO VERDICTS. Both refusals below are about where git
	// resolves core.hooksPath from, and asking twice would let them decide on
	// answers that describe different states.
	top, prefix, err := vcs.TopLevel(root)
	if err != nil {
		return err
	}
	if err := subdirectoryRefusal(root, top, prefix); err != nil {
		return err
	}
	if err := writeTargetRefusal(root, top); err != nil {
		return err
	}
	// LAST OF THE THREE ROOT REFUSALS, because the two above give a better
	// message for the states they own and this one only fires under the hatch,
	// where their answers are correct and still not enough (#179).
	if err := unregisteredWorkTreeRefusal(root); err != nil {
		return err
	}
	// THE TWO QUESTIONS, and neither of them opens the operator's global or
	// system config. What git will DO is scope-agnostic by construction; whether
	// THIS REPOSITORY declared it is a scoped read. Each is asked once here and
	// passed down, so no decision below re-asks git and acts on a second answer.
	//
	// ASKING EACH ONCE DOES NOT MAKE THE TWO AGREE, and the pair must not be read
	// as one coherent picture of the configuration. They consult different
	// sources: `rev-parse --git-path hooks` honours git's environment config
	// overrides and `git config --local --get` does not, so the two can be made to
	// describe different configurations — which is why the environment refusal
	// above runs before either is asked. That closes the environment's entrance
	// to the divergence; the entrance from inside the repository is an
	// `include.path` in its own config, which git resolves for the first question
	// and `--local --get` does not follow for the second — refused below by
	// includedWiringRefusal rather than decided (#173).
	live, err := vcs.HooksPath(root)
	if err != nil {
		return err
	}
	declared, set, err := vcs.RepoConfig(root, "core.hooksPath")
	if err != nil {
		return err
	}
	lanes := Expected(cfg)
	if set {
		// The repository declared the wiring, so it is the project's, whatever
		// its value — D2, no flag.
		if err := declaredWiringRefusal(root, live, declared, lanes); err != nil {
			return err
		}
	} else {
		// UNSET IS TWO STATES, and only one of them is D7's. The scoped read
		// above does not follow include directives, so it answers unset both for
		// a repository that declared nothing and for one that declared through
		// an `include.path`. The second must be separated out FIRST: D7's
		// message asserts that a setting outside this repository owns the
		// wiring, which is false there, and its flag then overwrites it.
		if err := includedWiringRefusal(root, live, lanes); err != nil {
			return err
		}
		if err := widerScopeRefusal(root, live, lanes, overrideGlobal); err != nil {
			return err
		}
	}
	return runningHooksRefusal(root, live, lanes)
}

// subdirectoryRefusal is D1: install must run at the git top level.
//
// core.hooksPath is repo-relative and git resolves it FROM THE TOP LEVEL, so
// installing from a subdirectory writes shims into one directory and points git
// at another — a hook that is never found, reported as installed.
//
// IT APPLIES TO EVERY LANE, including the whole-tree ones whose shims would run
// correctly from anywhere. A policy that holds for pre-push and not for
// pre-commit is a trap: the operator learns "install works here" from the lane
// that tolerates it.
//
// IT DECIDES ON `rev-parse --show-prefix`, WHICH AN AMBIENT GIT_DIR USED TO
// EMPTY: git then reports the subdirectory itself as the top level. Measured
// before internal/vcs controlled the environment — with GIT_DIR set, install
// exited 0, wrote the shims under the subdirectory, and set a core.hooksPath git
// resolves from the real top level to a directory that does not exist, which is
// precisely the state below (#167 member 5). vcs removes GIT_DIR from every git
// command formwork runs, so the answer this reads is about the tree named by
// -C; TestInstallStillRefusesASubdirectoryUnderAnAmbientGitDir re-measures that
// rather than trusting this paragraph, and the caller's environment refusal
// covers what remains — an answer moved by configuration is refused before this
// runs.
//
// The remedy is to install from the top level, and there is no partial repair on
// offer — teaching the shim to pass `-C <subdir>` does not work, because the
// staged file-set refuses a subdirectory root: vcs.StagedPaths calls
// vcs.EnsureTopLevel (both in internal/vcs/vcs.go — named rather than cited by
// line, because the commit that first wrote this citation also moved one of the
// two and shipped it stale), which exits 2 for exactly this frame, deliberately
// (#142). A top-level shim is not a compromise
// either — it gates the subdirectory's files along with everything else.
func subdirectoryRefusal(root, top, prefix string) error {
	if prefix == "" {
		return nil
	}
	return fmt.Errorf("hooks install must run at the repository top level (%s), not %s — git resolves core.hooksPath from the top level, so shims written under the subdirectory %q are in a directory git never looks in; install from there, the top-level shim still gates this subdirectory's files",
		top, root, strings.TrimSuffix(prefix, "/"))
}

// unregisteredWorkTreeRefusal is #179: under FORMWORK_GIT_ENV=inherit, git's
// answers describe the work tree the environment named, and the wiring install
// writes has to keep working once that environment is gone.
//
// WHAT WENT WRONG IS NOT A GUARD BEING WRONG. With GIT_WORK_TREE naming a
// SUBDIRECTORY of the repository, git reports that subdirectory as the work
// tree, `--show-prefix` is legitimately empty, and D1 and writeTargetRefusal
// above both agree — correctly, about the repository git was asked about.
// Install then wrote its shims under the subdirectory, wrote the repo-relative
// core.hooksPath into the repository's config, and exited 0. A plain `git
// commit` carries none of those variables: it resolves that relative value from
// the REAL top level, finds nothing, and commits ungated. Measured on git 2.50.1
// — exit 0, "installed git hooks: pre-commit", and a violating commit accepted.
// This is #150's residue surviving inside the hatch's positive exemption, which
// #167 D10 deliberately kept.
//
// THE QUESTION IS GIT'S WORKTREE REGISTRY, AND THE ENVIRONMENT DOES NOT MOVE IT.
// The obvious shape is a second git run with the pointer variables removed, and
// it is not needed: measured on git 2.50.1 in exactly that layout, `worktree
// list --porcelain` answers the repository's real top level while GIT_WORK_TREE
// names the subdirectory. So the registry already answers what a plain `git
// commit` would resolve, from inside the environment being honoured, and it is
// the same list verify walks (vcs.Worktrees) rather than a second mechanism.
//
// THE DETACHED LAYOUT THE HATCH EXISTS FOR PASSES ON THE SAME ANSWER, which is
// why this is one rule and not two. A bare repository plus a detached work tree
// has no work-tree entry at all — measured, `worktree list --porcelain` there
// reports the bare git directory and the word `bare` — so the registry
// contradicts nothing about root, and there is nothing to refuse. A repository
// whose registry DOES name work trees and does not name root is the other case,
// and root is then a directory a plain `git commit` never resolves hooks from.
//
// IT IS ARMED ONLY UNDER THE HATCH, and that is a scope decision rather than a
// half-measure. Off the hatch, internal/vcs removes the pointer family from
// every git command, so root cannot be a work tree the environment invented, and
// a root BELOW the work tree is D1's. What is left is a path comparison, and a
// path comparison is what #142 r2 broke on a case-variant root — arming it on
// every install would risk refusing a healthy repository over a spelling, to
// close a hole that only the hatch opens.
//
// vcs.GitEnvNotice IS THE HATCH PREDICATE because it is the only exported one:
// its contract is a line to print when the hatch is in force and the empty
// string when it is not, and its error is the refusal of an unusable value,
// which install owes the operator anyway. Reading FORMWORK_GIT_ENV here instead
// would put a second copy of that vocabulary in this package.
func unregisteredWorkTreeRefusal(root string) error {
	notice, err := vcs.GitEnvNotice()
	if err != nil || notice == "" {
		return err
	}
	wts, err := vcs.Worktrees(root)
	if err != nil {
		return err
	}
	rootKey := resolvedRootKey(root)
	var named []string
	for _, wt := range wts {
		if wt.Bare {
			continue
		}
		if resolvedRootKey(wt.Path) == rootKey {
			return nil
		}
		named = append(named, wt.Path)
	}
	if len(named) == 0 {
		return nil // the detached layout: git's registry names no work tree at all
	}
	return fmt.Errorf("hooks install will not wire %s: git runs here only because the ambient environment says so, and this repository's own worktrees are %s. core.hooksPath is repo-relative and git resolves it from the worktree top level, so the shims would go under %s while a plain `git commit` — which carries none of those variables — looks under the worktrees above and finds nothing, with install reporting success. Run it with -C naming one of those worktrees, or unset %s so formwork resolves this repository the way `git commit` does",
		root, strings.Join(named, ", "), root, vcs.GitEnvVar)
}

// writeTargetRefusal compares install's TWO WRITES and refuses unless they land
// in the same tree: the shims go into <root>/.formwork/hooks, and core.hooksPath
// goes into the config of the repository git resolves for root, which git then
// resolves FROM THAT REPOSITORY'S WORK TREE. So root must be that work tree.
//
// IT IS NOT THE SUBDIRECTORY REFUSAL AND IT IS NOT THE ENVIRONMENT'S. The
// caller's `--show-prefix` arm above covers root sitting BELOW the work tree;
// this covers root sitting OUTSIDE it with the prefix empty, which is what
// `core.worktree` in the repository's own config produces — measured on git
// 2.50.1, `rev-parse --show-toplevel --show-prefix` there answers the other
// directory and an empty prefix, and install went on to report a wiring that
// gated nothing (TestInstallRefusesWhenItsTwoWritesLandInDifferentTrees carries
// the measurement). Nothing about it involves an ambient variable: it holds with
// the environment untouched, which is why it is a comparison of install's own
// two write targets rather than a second opinion about which repository git
// resolved.
//
// THE COMPARISON IS DIRECTORY IDENTITY THROUGH os.Stat, so the root spelled `.`,
// spelled through a symlink, or spelled in another case on a case-insensitive
// filesystem all pass — each names the work tree and git agrees. That is also
// this check's one limitation, MEASURED rather than papered over: os.SameFile
// compares file IDs, and on a substrate that does not provide usable ones every
// directory compares equal and this goes inert. It runs through the sameFile
// seam below for that reason —
// TestWriteTargetRefusalGoesInertWhereIdentityCannotDistinguish forces the
// degenerate answer and installs cleanly over the fixture that is refused here,
// so the sentence is a fact about this function rather than a caveat about
// os.SameFile. The prefix arm the caller runs first never leaves git's frame and
// is immune; this arm is not written that way, because git's own answer to THAT
// question here is that the prefix is empty.
//
// A STAT FAILURE REFUSES. The two directories are what install is about to write
// into; not being able to look at one of them is not a state to install from.
func writeTargetRefusal(root, top string) error {
	topInfo, err := os.Stat(top)
	if err != nil {
		return fmt.Errorf("hooks install cannot stat the work tree %s that git reports for %s: %w", top, root, err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("hooks install cannot stat %s: %w", root, err)
	}
	if sameFile(topInfo, rootInfo) {
		return nil
	}
	managed, err := managedHooksDir(root)
	if err != nil {
		return err
	}
	return fmt.Errorf("hooks install would write its shims into %s and set core.hooksPath in the config of the repository at %s, whose work tree git reports as %s — git runs hooks from the work tree, so it would look for them in %s and find nothing while install reported success; run it with -C naming the work tree git reports",
		managed, root, top, filepath.Join(top, filepath.FromSlash(hooksDir)))
}

// sameFile is os.SameFile behind a variable, so a test can force the answer a
// filesystem without usable file IDs gives — "same" for every pair, which takes
// the refusal above off rather than making it refuse.
//
// The seam is the one vcs.go carries, added here for the same reason and with a
// different outcome: there the degenerate answer is caught by a second arm that
// never leaves git's frame, and the test asserts the refusal still fires; here
// there is no such arm, because git's own answer in this state is that the
// prefix is empty. So what the seam buys is a MEASUREMENT of the limitation
// writeTargetRefusal states —
// TestWriteTargetRefusalGoesInertWhereIdentityCannotDistinguish — rather than a
// guard against it.
var sameFile = os.SameFile

// declaredWiringRefusal is one half of D2: this repository's own config names a
// hooks directory that is not formwork's, so the project has hook wiring and
// formwork does not take it over.
//
// NO FLAG IS THE WHOLE POINT. Whatever is here, the project put it here, and a
// tool whose opinions are about how it works rather than where it works does not
// offer to overrule that. The way forward is the other direction: call formwork
// from the hook runner that is already in charge, which is what the message
// spells out line by line.
//
// live is the directory git will run hooks from; declared is what this
// repository's config says; lanes are its git-hook lanes, for the remedy.
//
// THE COMPARISON IS AGAINST THE RESOLVED DIRECTORY, not the declared string:
// `.formwork/hooks/`, an absolute spelling of it, and a symlinked root all name
// formwork's own directory, and a repository that already wired formwork must
// stay installable.
func declaredWiringRefusal(root, live, declared string, lanes []string) error {
	managed, err := managedHooksDir(root)
	if err != nil {
		return err
	}
	// A comparison formwork could not make is not "this is somebody else's
	// wiring" and not "this is formwork's own" — it is neither, and this branch
	// decides whether to REPAIR the wiring or to refuse to touch it. Both
	// verdicts are wrong on a question nobody answered, so it takes neither
	// (#171).
	same, err := sameDirPath(live, managed)
	if err != nil {
		return fmt.Errorf("this repository declares core.hooksPath = %s and git runs hooks from %s, and formwork cannot tell whether that is its own managed directory %s: %w — it will not decide whether to repair or to leave alone on a question it could not answer",
			declared, live, managed, err)
	}
	if same {
		// Formwork's own directory, declared here: install repairs it. That
		// reading holds only while live and declared describe the same
		// configuration — where they do not, this arm repairs on a `live` somebody
		// else supplied and overwrites the `declared` wiring it was meant to
		// protect. The caller refuses the environment's way of separating them
		// before this runs, and an include's where the config body declares
		// nothing at all. WHAT IS LEFT is an include that OVERRIDES a value the
		// body does declare: `--local --get` answers, so the caller takes this
		// branch, and `live` is then the included value. Where that value is
		// formwork's own directory this arm repairs — rewriting the body's
		// declaration, which the include had already made inert. Separating that
		// case needs a different question from D11's (`--get-all` in both
		// spellings, counted), and it is the residue #173 leaves rather than
		// something this arm decides correctly.
		return nil
	}
	return fmt.Errorf("this repository declares core.hooksPath = %s, so git runs hooks from %s — formwork will not take over hook wiring the project already set up; %s",
		declared, live, chainingRemedy(live, lanes))
}

// includedWiringRefusal is D11 (#173): this repository's config declares
// core.hooksPath through an `include.path` directive, so formwork cannot say
// whose wiring it is, and refuses to guess.
//
// IT IS A REFUSAL TO ANSWER, WHICH IS WHY IT HAS NO FLAG AND OFFERS NO VALUE.
// The two states it sits between are D2's (the project's own) and D7's
// (something wider), and an include is compatible with both. Deciding between
// them would mean either reading the included file — configuration outside the
// repository formwork was pointed at — or a rule about which include targets
// count, and measured on git 2.50.1 an include escapes the repository through
// BOTH spellings, an absolute path to a file elsewhere on the machine and a
// relative `../../up.cfg`, so "relative includes are the project's" is not that
// rule.
//
// THE MESSAGE PRINTS GIT'S RESOLVED PATH AND NOT THE DECLARED VALUE, unlike
// D2's. vcs.RepoConfigWithIncludes deliberately returns no value, for the reason
// above; `live` is what git will DO here, which formwork asked git directly.
//
// IT RUNS BEFORE D7 AND CANNOT BE REORDERED AFTER IT. Both are reached on the
// same scoped read reporting unset, and without --override-global D7 refuses
// first — with the sentence that is false here, which is #173 restored.
func includedWiringRefusal(root, live string, lanes []string) error {
	viaInclude, err := vcs.RepoConfigWithIncludes(root, "core.hooksPath")
	if err != nil {
		return err
	}
	if !viaInclude {
		return nil
	}
	return fmt.Errorf("git runs hooks from %s, and this repository's config declares core.hooksPath through an `include.path` directive rather than in the body of its own config file — an include can name a file anywhere on the machine, so formwork cannot tell whether that wiring is this project's own or something wider, and it will not guess. There is no flag for this state. Either move that declaration out of the included file into this repository's own config, where formwork can see whose it is, or %s",
		live, chainingRemedy(live, lanes))
}

// widerScopeRefusal is D7: git is running hooks from somewhere this repository
// never asked for, so a setting outside it owns the wiring, and installing would
// switch that off in this repository with nothing said.
//
// IT IS A DIFFERENT REFUSAL FROM D2 AND HAS A DIFFERENT ANSWER. What D2 finds is
// a decision the project made; what this finds is a DEFAULT, and repo-local
// override is exactly what git provides local config for — so the operator can
// say "yes, this repository is different" in one word. The message therefore
// offers two ways forward where D2 offers one.
//
// NEITHER QUESTION READS GLOBAL OR SYSTEM CONFIG. The caller asked git what it
// will DO (scope-agnostic) and whether THIS REPOSITORY declared it (scoped), and
// those two answers are all D7 needs. The message names git's resolved path and
// nothing else: what the operator's machine says is their business, not
// formwork's to report.
//
// THE SCOPED READ IS NARROWER THAN "DID THIS REPOSITORY DECLARE ONE", which is
// why this refusal is no longer reached on that read alone. vcs.RepoConfig reads
// with `git config --local --get`, which defaults to --no-includes, so a
// core.hooksPath this repository declares in its own .git/config via
// `include.path` reads back unset while git runs hooks from it (measured). This
// refusal's sentence — that a setting outside this repository owns the wiring —
// was then false, and --override-global, documented as never clearing D2, took
// the project's own wiring over: #173. includedWiringRefusal above separates
// that state and stops before this one. What survives the split and arrives here
// is an unset scoped read WITH no included declaration either, which is D7's
// premise stated exactly.
//
// A RESOLUTION FAILURE INSIDE sameDirPath IS NO LONGER A VERDICT ANYWHERE ON
// THE INSTALL SIDE. It used to answer "not the same directory" when it could not
// resolve a path, which this refusal read as a reason to stop while
// preexistingHooks — the next call the pre-flight makes, over the same two
// directories — read it as a reason NOT to refuse. That opposite polarity was
// the fail-open #171 records, and it is closed at the comparison itself:
// sameDirPath returns the failure now, and each of this file's three callers
// refuses on it. What is left here is a decision about the FLAG, argued at the
// branch below.
func widerScopeRefusal(root, live string, lanes []string, overrideGlobal bool) error {
	dflt, err := defaultHooksDir(root)
	if err != nil {
		return err
	}
	// THE FLAG IS CHECKED BEFORE THE COMPARISON AND AFTER THE GIT CALL, which is
	// the order the old `sameDirPath(...) || overrideGlobal` had: git's failure
	// aborted whatever the flag said, and the comparison's verdict was discarded
	// once the flag was set. Keeping both is what makes #171's error return a
	// change to this refusal's unanswerable case only. The flag ANSWERS this
	// question — the operator has said this repository is different from whatever
	// is wider — so there is nothing here for an unresolvable comparison to
	// decide. It does not answer the next one: runningHooksRefusal asks its own,
	// over the same two directories, and refuses there where it cannot resolve
	// them.
	if overrideGlobal {
		return nil
	}
	same, err := sameDirPath(live, dflt)
	if err != nil {
		return fmt.Errorf("git runs hooks from %s, and formwork cannot tell whether that is git's own default hooks directory %s: %w — it will not wire hooks over a wiring it could not identify",
			live, dflt, err)
	}
	if same {
		return nil
	}
	return fmt.Errorf("git runs hooks from %s, which this repository does not declare — a setting outside this repository owns the hook wiring here, and installing would override it for this repository. Either run `formwork hooks install --override-global`, which sets core.hooksPath in this repository only and changes nothing outside it, or %s",
		live, chainingRemedy(live, lanes))
}

// runningHooksRefusal is the other half of D2: hooks git is running right now,
// from the directory it runs them from by default. Setting core.hooksPath
// overrides that whole directory, so every one of them stops — including hook
// names formwork does not model.
//
// It runs after both branches above rather than inside either, because "is git
// running hooks from its default directory right now" does not depend on who
// declared the path. preexistingHooks' R3 guard is what makes that safe in every
// branch: where git runs hooks somewhere else, the files in the default
// directory are inert and this finds nothing — which is also why an install that
// only had to repair a missing shim is not blocked by them.
func runningHooksRefusal(root, live string, lanes []string) error {
	dir, names, err := preexistingHooks(root, live)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	// Setting core.hooksPath overrides the WHOLE directory, so this refusal is
	// about every name in it and not only the ones formwork wires: an operator's
	// commit-msg stops running just as silently as their pre-commit.
	return fmt.Errorf("git already runs %s from %s — setting core.hooksPath would override that whole directory and every hook in it would stop running, so formwork will not do it; %s",
		strings.Join(names, ", "), dir, chainingRemedy(dir, lanes))
}

// chainingRemedy renders the exact lines that call formwork's lanes from hooks
// the operator owns, in the directory git is running them from.
//
// The command comes from checkCommand, the same renderer the generated shim
// uses, so the advice cannot drift from what formwork itself would run.
func chainingRemedy(dir string, lanes []string) string {
	var b strings.Builder
	b.WriteString("to run formwork's lanes from your own hooks, add the matching line to each hook in " + dir + ":")
	for _, lane := range lanes {
		b.WriteString("\n    " + lane + ": " + checkCommand(lane))
	}
	return b.String()
}

// managedHooksDir is the directory formwork manages, absolute, so it can be
// compared against the absolute path git reports (vcs.HooksPath).
//
// The joined form is relative whenever root is, and root is relative on the
// commonest invocation there is: the CLI's default of ".". Comparing that
// against git's absolute answer finds two different directories where there is
// one.
func managedHooksDir(root string) (string, error) {
	return filepath.Abs(filepath.Join(root, filepath.FromSlash(hooksDir)))
}
