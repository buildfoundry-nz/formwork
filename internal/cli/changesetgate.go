package cli

import (
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// configDir is where config.Load reads every rule file and formwork.yaml.
const configDir = ".formwork"

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
// A gated rule still runs when either of these holds, and each is the
// fail-closed answer:
//   - a touched path is under .formwork/, which holds every rule definition,
//     so the rule itself may have changed;
//   - a touched path is one its scope admits.
//
// Admission is the path test Rule.Applies, without scope.not_declaring: that
// test needs file content, and a touched path that declares itself out is run
// rather than skipped.
func gateOnChangeset(rls []*config.Rule, touched []string) (run []*config.Rule, skipped []report.SkippedRule) {
	for _, p := range touched {
		if p == configDir || strings.HasPrefix(p, configDir+"/") {
			return rls, nil
		}
	}
	for _, r := range rls {
		if !changesetGated(r) || reachedBy(r, touched) {
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

func reachedBy(r *config.Rule, touched []string) bool {
	for _, p := range touched {
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
