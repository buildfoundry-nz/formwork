// history_env_test.go — #213.
//
// #177 closed the half an identity comparison can see: a command rule refuses
// when an ambient POINTER variable (GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR)
// resolves a repository other than the one the engine is checking.
//
// The OBJECT-STORE family (#176) was still inherited, and no comparison in this
// package could catch it: those six move which commits and objects git answers
// from INSIDE the repository root names, so the git directory, the shared
// directory and the work tree are all byte-identical. vcs/env.go says so about
// its own scrub — "no identity comparison can see them".
//
// Measured on this tree before the fix, with two repositories and a rule
// `cmd: [git, cat-file, -e, <sha that exists only in other>]`:
//
//	clean environment                        -> [hist] FAIL, exit 1   (correct)
//	GIT_ALTERNATE_OBJECT_DIRECTORIES=other   -> [hist] OK,   exit 0   (silent green)
//
// A pass earned over objects from a repository nobody named — this repo's
// signature defect, in the half #177's guard cannot reach.
//
// REFUSE, NOT SCRUB, and the reason is the escape hatch's contract. `command`
// runs the OPERATOR's argv and this package removes nothing from its
// environment; deleting variables would be formwork deciding on the tool's
// behalf, and would change what an already-working rule sees. Refusing changes
// no tool's environment, is loud (exit 2, never a finding), sits in the same
// function as #177's pointer refusal, and leaves the existing
// FORMWORK_GIT_ENV=inherit hatch as the way to say "I meant it".
package command_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Every one of the six, because a guard written against the one in the bug
// report would pass on the other five.
func TestCommandRefusesEveryInheritedHistoryVariable(t *testing.T) {
	for _, name := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_GRAFT_FILE",
		"GIT_NO_REPLACE_OBJECTS",
		"GIT_OBJECT_DIRECTORY",
		"GIT_REPLACE_REF_BASE",
		"GIT_SHALLOW_FILE",
	} {
		t.Run(name, func(t *testing.T) {
			engineRoot := initRepo(t, "engine")
			other := initRepo(t, "other")
			t.Setenv(name, other)

			c := build(t, "cmd: [git, rev-parse, --absolute-git-dir]")
			m, err := finalizeAt(t, c, engineRoot)
			if err == nil {
				t.Fatalf("%s was inherited and the rule ran anyway (matches=%v) — a tool "+
					"reading git can be answered from another repository's objects", name, m)
			}
			if !errors.Is(err, vcs.ErrGitEnv) {
				t.Fatalf("%s: refusal must carry vcs.ErrGitEnv so a caller can tell policy "+
					"from a git failure, got %v", name, err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("%s: the refusal must NAME the variable it refused on — an "+
					"operator cannot cure what the message does not identify; got %v", name, err)
			}
		})
	}
}

// The narrowing that keeps this from failing every ordinary run: with none of
// the six set, nothing is refused. Without this a guard that refused
// unconditionally would pass the test above and break every command rule.
func TestCommandRunsWithNoHistoryVariableSet(t *testing.T) {
	for _, name := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
		"GIT_OBJECT_DIRECTORY", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE",
	} {
		if v, ok := os.LookupEnv(name); ok {
			t.Skipf("%s is set in the ambient environment (%q); this case needs it unset", name, v)
		}
	}
	engineRoot := initRepo(t, "engine")
	c := build(t, "cmd: [git, rev-parse, --absolute-git-dir]")
	if _, err := finalizeAt(t, c, engineRoot); err != nil {
		t.Fatalf("no history variable is set, so nothing should be refused: %v", err)
	}
}

// The hatch is how an operator says "I meant it" — the receive-pack quarantine
// case is real, and two of the six are meaningful there. This must be the SAME
// hatch #176/#177 use, not a second one: a per-family opt-out would mean an
// operator who set FORMWORK_GIT_ENV=inherit still hit a refusal from a family
// they had already spoken about.
func TestCommandHistoryRefusalHonoursTheInheritHatch(t *testing.T) {
	engineRoot := initRepo(t, "engine")
	other := initRepo(t, "other")
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", other)
	t.Setenv(vcs.GitEnvVar, "inherit")

	c := build(t, "cmd: [git, rev-parse, --absolute-git-dir]")
	if _, err := finalizeAt(t, c, engineRoot); err != nil {
		t.Fatalf("under %s=inherit the operator has taken the decision; the history "+
			"family must not be refused on top of it: %v", vcs.GitEnvVar, err)
	}
}
