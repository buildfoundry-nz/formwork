// shard.go — `check --shard K/N`: run one of N disjoint shards of the selected
// rules, so one whole-corpus sweep can be spread over N machines.
package cli

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

const shardUsage = "run only shard `K/N` of the selected rules: every rule id lands in exactly one of N shards " +
	"(balanced longest-first by --durations, then by declared cost:), so N runs together cover the " +
	"selection once. The JSON report names the rules the shard ran"

const shardPinUsage = "with --shard, place every selected rule the named lane selects on one shard, `LANE=K`, " +
	"before the rest are balanced around it — so the rules that need a toolchain only one matrix job " +
	"installs all run in that job. Repeatable, one pin per lane; every shard job must pass the same pins"

// shardPin is one --shard-pin LANE=K: the rules lane selects all run on shard k.
type shardPin struct {
	lane string
	k    int
}

// parseShardPin reads one `LANE=K`. K is canonical like --shard's (decimal, no
// sign, no leading zero), because it is a value N matrix jobs must agree on.
func parseShardPin(spec string) (shardPin, error) {
	lane, ks, ok := strings.Cut(spec, "=")
	if !ok || lane == "" || strings.TrimSpace(lane) != lane {
		return shardPin{}, fmt.Errorf("--shard-pin %q is not LANE=K (e.g. dart=1)", spec)
	}
	k, err := canonicalPositive(ks)
	if err != nil {
		return shardPin{}, fmt.Errorf("--shard-pin %q is not LANE=K with K a positive decimal integer (e.g. dart=1)", spec)
	}
	return shardPin{lane: lane, k: k}, nil
}

// pinnedHomes resolves the pins against the selected rules: the shard each
// pinned id must live on. Every arm that cannot be honoured is a refusal, not
// a quiet fallback to the unpinned partition, because a job that installs a
// toolchain for its pinned rules is only right while the pinned rules are
// really there:
//
//   - a pin with no --shard, or a K beyond N, names a shard that is not run;
//   - a lane named twice, or a lane that does not exist, is a typo;
//   - a pin whose lane selects none of the selected rules places nothing, and
//     the job that set up for those rules would set up for nothing;
//   - an id two pins place on different shards cannot live on both.
func pinnedHomes(cfg *config.Config, selected []*config.Rule, pins []shardPin, n int) (map[string]int, error) {
	if len(pins) == 0 {
		return nil, nil
	}
	inSelection := map[string]bool{}
	for _, r := range selected {
		inSelection[r.ID] = true
	}
	homes := map[string]int{}
	byLane := map[string]bool{}
	for _, p := range pins {
		if byLane[p.lane] {
			return nil, fmt.Errorf("--shard-pin names lane %q twice; one lane has one shard", p.lane)
		}
		byLane[p.lane] = true
		if p.k > n {
			return nil, fmt.Errorf("--shard-pin %s=%d names shard %d of %d; K runs from 1 to N", p.lane, p.k, p.k, n)
		}
		laneRules, err := cfg.RulesForLane(p.lane)
		if err != nil {
			return nil, fmt.Errorf("--shard-pin %s=%d: %w", p.lane, p.k, err)
		}
		placed := 0
		for _, r := range laneRules {
			if !inSelection[r.ID] {
				continue
			}
			if prev, ok := homes[r.ID]; ok && prev != p.k {
				return nil, fmt.Errorf("--shard-pin places rule %q on shard %d and on shard %d; an id lives on one shard", r.ID, prev, p.k)
			}
			homes[r.ID] = p.k
			placed++
		}
		if placed == 0 {
			return nil, fmt.Errorf("--shard-pin %s=%d pins no rule: lane %q selects none of the selected rules, "+
				"so shard %d would carry nothing it was pinned for", p.lane, p.k, p.lane, p.k)
		}
	}
	return homes, nil
}

// parseShard reads a `K/N` spec. Only the canonical spelling is accepted —
// decimal, no sign, no leading zero, no padding — because the spec is the
// whole contract between N matrix jobs, and two spellings of one shard are two
// strings a CI lockdown has to know are equal.
func parseShard(spec string) (k, n int, err error) {
	ks, ns, ok := strings.Cut(spec, "/")
	if !ok {
		return 0, 0, fmt.Errorf("--shard %q is not K/N (e.g. 2/4)", spec)
	}
	k, kerr := canonicalPositive(ks)
	n, nerr := canonicalPositive(ns)
	if kerr != nil || nerr != nil {
		return 0, 0, fmt.Errorf("--shard %q is not K/N with K and N positive decimal integers (e.g. 2/4)", spec)
	}
	if k > n {
		return 0, 0, fmt.Errorf("--shard %q names shard %d of %d; K runs from 1 to N", spec, k, n)
	}
	return k, n, nil
}

func canonicalPositive(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 || strconv.Itoa(v) != s {
		return 0, fmt.Errorf("%q is not a positive decimal integer", s)
	}
	return v, nil
}

// shardRules returns the rules of shard k (1-based) of n, in declaration order.
//
// THE PARTITION IS OVER RULE IDS, and is a pure function of (rules, prior, n):
// each of the N matrix jobs computes it independently, so anything else it
// read — the clock, map order, the shard index — would let two jobs disagree
// about where a rule lives, and a rule both jobs place elsewhere runs nowhere.
// Rules that share an id stay together, so one id's findings never split
// across two reports.
//
// Ids are placed one at a time onto the least-loaded shard (ties: fewer ids,
// then lower index), heaviest first by the keys dispatchOrder ranks a pool by
// — measured duration, then declared cost: class — and then by id. With no
// hint every load is zero and the placement is a round-robin over that order,
// so shards balance by count and the heavy classes spread across them. A rule
// the hint does not name is weighed at the smallest positive duration it does
// name, not at zero: it is new or below the report's recording floor, and
// weighing it at nothing would stack every such rule on one shard.
//
// pinned (--shard-pin) is placed first: each pinned id goes to its named shard
// and counts toward that shard's load, and only then are the other ids placed
// heaviest first, so the balance is computed around the pinned work rather
// than beside it. With no pins the partition is exactly the unpinned one.
func shardRules(rls []*config.Rule, prior engine.Timing, pinned map[string]int, k, n int) []*config.Rule {
	home := shardHomes(rls, prior, pinned, n)
	var out []*config.Rule
	for _, r := range rls {
		if home[r.ID] == k {
			out = append(out, r)
		}
	}
	return out
}

func shardHomes(rls []*config.Rule, prior engine.Timing, pinned map[string]int, n int) map[string]int {
	rank := map[string]int{}
	for _, r := range rls {
		rank[r.ID] = max(rank[r.ID], rules.Rank(r.Cost()))
	}
	unknown := time.Duration(0)
	for _, d := range prior {
		if d > 0 && (unknown == 0 || d < unknown) {
			unknown = d
		}
	}
	weight := func(id string) time.Duration {
		if d, ok := prior[id]; ok {
			return d
		}
		return unknown
	}
	order := slices.Sorted(maps.Keys(rank))
	slices.SortStableFunc(order, func(a, b string) int {
		return cmp.Or(cmp.Compare(weight(b), weight(a)), cmp.Compare(rank[b], rank[a]))
	})
	home := make(map[string]int, len(order))
	load := make([]time.Duration, n)
	count := make([]int, n)
	for _, id := range order {
		if k, ok := pinned[id]; ok {
			load[k-1] += weight(id)
			count[k-1]++
			home[id] = k
		}
	}
	for _, id := range order {
		if _, ok := pinned[id]; ok {
			continue
		}
		best := 0
		for s := 1; s < n; s++ {
			if load[s] < load[best] || (load[s] == load[best] && count[s] < count[best]) {
				best = s
			}
		}
		load[best] += weight(id)
		count[best]++
		home[id] = best + 1
	}
	return home
}

// shardedIDs is the sorted, de-duplicated id list a shard's report names, so
// a merge of N reports can prove each id ran exactly once.
func shardedIDs(rls []*config.Rule) []string {
	ids := map[string]bool{}
	for _, r := range rls {
		ids[r.ID] = true
	}
	return slices.Sorted(maps.Keys(ids))
}
