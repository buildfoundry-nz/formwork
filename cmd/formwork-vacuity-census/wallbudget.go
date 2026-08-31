package main

import (
	"fmt"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// scopeWallBudget is the mechanical ceiling on the census's O(rules×files)
// scope-membership phase (#12419). Measured at the #12419 closeout on the
// real corpus (1823 rules × 20333 scanned files): 5.73s serial, 0.76s
// through the parallel index, on a 12-core dev host. Ten seconds leaves the
// indexed path an order of magnitude of headroom here and several times the
// measurement on a 4-vCPU CI runner, while the serial loop it replaced lands
// over the line there. The value is pinned by the formwork rule
// vacuity-census-wall-budget-pinned: never raise it — remove work instead.
const scopeWallBudget = 10 * time.Second

// buildScopesWithBudget computes every rule's in-scope file set through
// buildScopeIndex and refuses the result when the scope-membership phase
// runs over budget — the wall-clock half of the #12419 lockdown. The census
// is on every Architecture Guardrails run, so an over-budget phase fails the
// census (exit 1) rather than silently re-admitting an unbounded scan.
func buildScopesWithBudget(rules []*config.Rule, files []*scan.File, budget time.Duration) (map[string][]*scan.File, error) {
	start := time.Now()
	scopes := buildScopeIndex(rules, files)
	if elapsed := time.Since(start); elapsed > budget {
		return nil, fmt.Errorf("scope-membership phase took %s, over the %s wall budget (#12419): "+
			"the census builds per-rule scopes through the parallel index (buildScopeIndex) — a serial "+
			"O(rules×files) loop no longer fits the Architecture Guardrails path. Keep the phase on the "+
			"index; do not raise the budget, it is pinned by the formwork rule vacuity-census-wall-budget-pinned",
			elapsed, budget)
	}
	return scopes, nil
}
