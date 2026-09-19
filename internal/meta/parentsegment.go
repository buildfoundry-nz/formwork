// parentsegment.go — `formwork lint`'s command-argv-no-parent-segment check
// (#28).
//
// A command rule that names its tree with a path relative to the caller's
// working directory is read differently on every plane the engine evaluates.
// `--root ../../..` means the repository under `check`, where the working
// directory is the repository; under `test` the fixture runner makes the
// fixture tree the working directory and the same three segments climb back
// out into the repository, so the fixture judges the real tree. {{root}} and
// {{repo}} say it once and the engine resolves them per plane.
//
// WHY LINT AND NOT A LOAD REFUSAL. Refusing at load is the stronger shape and
// was the first one tried: a rule that does not load cannot read the wrong
// tree even once. It also makes every corpus written before the tokens
// unreadable — and the tools that read an old corpus are the ones that most
// need to. The vacuity census loads the corpus as it stood at a change's
// merge base, to tell a rule this change ADDED from one it merely edited; a
// base it cannot parse is a transition it cannot compute, and its own comment
// says an unparseable base must never resolve to "nothing changed". A load
// refusal therefore broke the census for every branch cut before the
// migration, and for any commit older than it.
//
// Lint runs on every pull request, so the shape still cannot merge. What it
// no longer does is make history unreadable.
package meta

import (
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// argvReader is the method set this check needs: a checker that can state the
// command it would run, without running it.
type argvReader interface {
	Argv() []string
}

// commandArgvProblems reports every rule whose argv names a parent directory.
func commandArgvProblems(rls []*config.Rule) (problems []string) {
	for _, r := range rls {
		if r.Library != "" {
			continue
		}
		a, ok := r.Checker.(argvReader)
		if !ok {
			continue
		}
		if arg, bad := command.ParentSegmentArg(a.Argv()); bad {
			problems = append(problems, command.ParentSegmentProblem(r.ID, arg))
		}
	}
	return problems
}

// anyCommandArgv reports whether the corpus has a command rule at all, so the
// check enters lint's denominator only where it has a subject — the same
// conditional shape the lane and trigger checks use.
func anyCommandArgv(rls []*config.Rule) bool {
	for _, r := range rls {
		if r.Library != "" {
			continue
		}
		if _, ok := r.Checker.(argvReader); ok {
			return true
		}
	}
	return false
}
