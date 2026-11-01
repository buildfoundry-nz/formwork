package cli

import (
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// configDir is where config.Load reads every rule file and formwork.yaml.
const (
	configDir  = ".formwork"
	configFile = configDir + "/formwork.yaml"
)

const changesetSkipReason = "did not run: the changeset adds, edits, deletes or renames no path this rule's scope admits, so it cannot move the verdict; the whole-tree run still judges it"

// changesetGated reports whether a --staged/--range run may skip r when the
// changeset does not reach its scope. Two kinds of rule judge more than the
// changed files, and each opts in through its own declaration:
//   - a whole-tree invariant that is rules.ScopeBound, because it evaluates over
//     the tracked tree (#4) and reads nothing but what its scope admits;
//   - a per-file rule that is rules.SubjectScoped, because its detector judges
//     the whole root whatever files it was handed.
//
// Every other rule is not gated here. A per-file rule without a tree-reading
// finalizer already sees only the changed files, and a command rule gates
// itself through its own when: trigger.
func changesetGated(r *config.Rule) bool {
	if r.WholeTreeInvariant() {
		return rules.IsScopeBound(r.Checker)
	}
	return rules.IsSubjectScoped(r.Checker)
}

// gateOnChangeset drops from rls the gated rules the changeset cannot reach,
// and names each one, in rls order. The rest are returned in order to run as
// before.
//
// touched must be the ANY-STATUS changeset: deletions and both ends of a rename
// are how a change removes a rule's evidence, and a scannable-only list drops
// exactly those paths.
//
// A gated rule still runs when any of these holds, and each is the
// fail-closed answer:
//   - formwork.yaml is touched: every rule is judged under its lanes, scope
//     and scan settings, so every rule runs;
//   - the rule file that declares it is touched, so the rule itself may have
//     changed;
//   - a touched path is one its scope admits.
//
// No other path under .formwork/ moves a check verdict. Loading reads
// formwork.yaml and rules/*.yaml only, the walk never enters the directory,
// and fixtures, mutation specs and the durations hint are other subcommands'
// data. Running every rule on such a change cost a 4,300-rule corpus 494 s
// against 33 s for a docs edit.
//
// Admission is the path test Rule.Applies, without scope.not_declaring: that
// test needs file content, and a touched path that declares itself out is run
// rather than skipped.
func gateOnChangeset(rls []*config.Rule, touched []string) (run []*config.Rule, skipped []report.SkippedRule) {
	edited := make(map[string]bool, len(touched))
	for _, p := range touched {
		if p == configDir || p == configFile {
			return rls, nil
		}
		edited[p] = true
	}
	for _, r := range rls {
		if !changesetGated(r) || (r.File != "" && edited[r.File]) || reachedBy(r, touched) {
			run = append(run, r)
			continue
		}
		skipped = append(skipped, report.SkippedRule{
			RuleID:  r.ID,
			Channel: report.SkipChannelChangeset,
			Reason:  changesetSkipReason,
		})
	}
	return run, skipped
}

// reachedBy reports whether a touched path is one r's scope admits. A
// scope-bound invariant reads only what the walk enumerates, so a path the walk
// never descends to (a fixture under .formwork/, say) cannot reach it, whatever
// its globs match. A census detector reads the root itself, so every admitted
// path counts for it.
func reachedBy(r *config.Rule, touched []string) bool {
	walkOnly := r.WholeTreeInvariant()
	for _, p := range touched {
		if walkOnly && scan.UnderBuiltinSkipDir(p) {
			continue
		}
		if r.Applies(p) {
			return true
		}
	}
	return false
}

// withoutSkipped returns rls minus the rules skipped names, in rls order. It
// returns rls itself when nothing was skipped.
func withoutSkipped(rls []*config.Rule, skipped []report.SkippedRule) []*config.Rule {
	if len(skipped) == 0 {
		return rls
	}
	gone := make(map[string]bool, len(skipped))
	for _, sk := range skipped {
		gone[sk.RuleID] = true
	}
	kept := make([]*config.Rule, 0, len(rls)-len(skipped))
	for _, r := range rls {
		if !gone[r.ID] {
			kept = append(kept, r)
		}
	}
	return kept
}
