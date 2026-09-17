package cli

import (
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// Narrowing the selected rules by cost class, with disclosure. Two operator
// flags do it and they are different in kind: --skip-escapes drops every
// escape (anything that is not fast) regardless of its declared class;
// --cost-max keeps the rules at or below one class in the fast < range <
// tree < heavy order (#22), so a pre-push hook can afford the range-scoped
// escapes (seconds) without the whole-tree ones (minutes).
//
// Both drops are named in the scan summary, never silent: the dropped rule
// never reaches the engine, so its checker cannot report the skip, and an
// empty not-run section would read as "every rule ran". Dropping ALL selected
// rules is a named exit 2 in runCheck; dropping SOME must not be quieter than
// that. Lane selection deliberately gets no such line: a lane not choosing a
// rule is selection working as asked, not a rule dropped out from under the
// run.

// dropEscapes is --skip-escapes: keep the fast rules, disclose the rest.
func dropEscapes(rls []*config.Rule) (kept []*config.Rule, dropped []report.SkippedRule) {
	kept = rls[:0:0]
	for _, r := range rls {
		if r.Cost() == rules.CostFast {
			kept = append(kept, r)
			continue
		}
		reason := fmt.Sprintf("did not run: --skip-escapes dropped this heavy %s rule; the whole-tree CI run is its backstop", r.Type)
		dropped = append(dropped, report.SkippedRule{
			RuleID: r.ID, Channel: report.SkipChannelSkipEscapes, Reason: withFloor(r, reason),
		})
	}
	return kept, dropped
}

// dropAboveCost is --cost-max: keep the rules at or below max, disclose the
// rest on their own channel so a consumer can tell "the operator dropped
// every escape" from "the operator dropped the heavy ones".
func dropAboveCost(rls []*config.Rule, max rules.Cost) (kept []*config.Rule, dropped []report.SkippedRule) {
	limit := rules.Rank(max)
	kept = rls[:0:0]
	for _, r := range rls {
		if rules.Rank(r.Cost()) <= limit {
			kept = append(kept, r)
			continue
		}
		reason := fmt.Sprintf("did not run: --cost-max %s dropped this %s-class %s rule; the whole-tree CI run is its backstop", max, r.Cost(), r.Type)
		dropped = append(dropped, report.SkippedRule{
			RuleID: r.ID, Channel: report.SkipChannelCostMax, Reason: withFloor(r, reason),
		})
	}
	return kept, dropped
}

// withFloor appends the rule's scope.min_files floor to a drop reason. The
// floor goes with the rule, and that is disclosed rather than evaluated: the
// cost argument for the drop does not reach a floor — it is glob matching
// over a file set already in hand — but emitting its finding here would exit
// 1 having printed nothing, because report.Human renders findings by
// iterating the rules it was handed and this one is no longer among them. A
// silent failure is worse than a disclosed gap (#23, fix round 1).
func withFloor(r *config.Rule, reason string) string {
	if floor := r.MinFiles(); floor > 0 {
		return reason + fmt.Sprintf(" — its scope.min_files floor of %d went unevaluated with it", floor)
	}
	return reason
}
