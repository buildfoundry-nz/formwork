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
func shardRules(rls []*config.Rule, prior engine.Timing, k, n int) []*config.Rule {
	home := shardHomes(rls, prior, n)
	var out []*config.Rule
	for _, r := range rls {
		if home[r.ID] == k {
			out = append(out, r)
		}
	}
	return out
}

func shardHomes(rls []*config.Rule, prior engine.Timing, n int) map[string]int {
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
