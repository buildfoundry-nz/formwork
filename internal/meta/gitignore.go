package meta

import (
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// GitIgnoreState is what the engine managed to learn about scan.gitignore for
// one run. The three states must stay distinct all the way to the report:
// "off" and "nothing was ignored" and "the question could not be answered"
// look identical in a bare count, and collapsing them is exactly the false
// clean this repo treats as its signature defect.
type GitIgnoreState int

const (
	// GitIgnoreOff — the key is not declared. Nothing is pruned and nothing is
	// reported; an undeclared repo keeps its "escape hatches: none" contract.
	GitIgnoreOff GitIgnoreState = iota
	// GitIgnoreOn — git answered, and Set holds what it confirmed ignored.
	GitIgnoreOn
	// GitIgnoreUnknown — the key is declared but git could not answer (no
	// repository, no git binary, a broken index). Set is nil.
	GitIgnoreUnknown
)

// GitIgnoreResult is the resolved prune set plus how it was arrived at.
type GitIgnoreResult struct {
	State GitIgnoreState
	Set   *scan.GitIgnored // nil unless State is GitIgnoreOn
	Err   error            // why, when State is GitIgnoreUnknown
}

// Opts turns the result into the walk options for this run.
func (r GitIgnoreResult) Opts(ignore []string) scan.Opts {
	return scan.Opts{Ignore: ignore, GitIgnored: r.Set}
}

// ResolveGitIgnore asks git which paths under root it ignores, when the config
// declares scan.gitignore.
//
// WHY A GIT FAILURE IS NOT AN ERROR HERE, WHEN IT IS EVERYWHERE ELSE IN THIS
// ENGINE. The vcs package's contract is that callers must never soften a git
// failure, because every other consumer uses git to NARROW what is scanned —
// --staged, --range, the #90 tracked-set assertion — and a silent fallback
// there would gate a commit against the wrong, smaller file set. This consumer
// narrows too, but the fallback runs the other way: pruning nothing yields a
// scan that is a strict SUPERSET of the declared one, so no rule can pass that
// would otherwise have failed. Turning that into exit 2 would invent a new
// hard failure for the ordinary case of running over an exported tarball or a
// non-repo corpus — a false blocker, which is the very complaint this feature
// exists to answer (raised by the validating port).
//
// The error is not swallowed: it is carried on the result so the census can
// name it, and callers surface it. Unanswered is reported as unanswered.
//
// This lives in one place, reached by both `check` and `lint`, so the
// fail-closed decision cannot drift into two spellings.
func ResolveGitIgnore(cfg *config.Config, root string) GitIgnoreResult {
	if cfg == nil || cfg.Gitignore == nil {
		return GitIgnoreResult{State: GitIgnoreOff}
	}
	recs, err := vcs.IgnoredUnder(root)
	if err != nil {
		return GitIgnoreResult{State: GitIgnoreUnknown, Err: err}
	}
	return GitIgnoreResult{State: GitIgnoreOn, Set: GitIgnoredSet(recs)}
}

// GitIgnoredSet keys confirmed-ignored records for the walk. This is the ONE
// construction of a prune set from vcs records — the walk snapshot and
// rules-for's ghost frame both build through it, so a future filter or key
// normalization cannot be applied to one and silently missed by the other
// (#125 round-2 finding 5).
func GitIgnoredSet(recs []vcs.IgnoredPath) *scan.GitIgnored {
	set := scan.NewGitIgnored()
	for _, r := range recs {
		set.Add(r.Path, r.Dir, r.Rule())
	}
	return set
}
