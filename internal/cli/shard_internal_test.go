// shard_internal_test.go — the partition behind `check --shard K/N`.
//
// Sharding is only safe if it is a PARTITION: every rule id in exactly one
// shard, and every shard job computing the same answer from the same inputs.
// A rule in no shard is a rule CI silently stopped running; a rule in two is a
// finding reported twice. These tests are the guarantee the CI matrix leans on
// when it runs shards 1..N as separate machines.
package cli

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
)

func shardTestRules(n int) []*config.Rule {
	rls := make([]*config.Rule, 0, n+1)
	for i := range n {
		rls = append(rls, &config.Rule{ID: fmt.Sprintf("rule-%03d", i)})
	}
	// A repeated id: rules sharing an id must travel together, or one id's
	// findings would be split across two shard reports.
	return append(rls, &config.Rule{ID: "rule-000"})
}

func shardTestTiming(rls []*config.Rule) engine.Timing {
	prior := engine.Timing{}
	for i, r := range rls {
		// Only some rules measured, so the unmeasured fallback is exercised too.
		if i%3 != 0 {
			prior[r.ID] = time.Duration((i*37)%101+1) * time.Millisecond
		}
	}
	return prior
}

// TestShardPartitionCoversEveryRuleExactlyOnce is T1 of TakeoffQS#25657: for
// N in 1..8, with and without a durations hint, the union of shards 1..N is
// the whole rule set, the shards are pairwise disjoint, an id never splits,
// and a second computation gives the same partition.
func TestShardPartitionCoversEveryRuleExactlyOnce(t *testing.T) {
	rls := shardTestRules(41)
	for _, prior := range []engine.Timing{nil, shardTestTiming(rls)} {
		for n := 1; n <= 8; n++ {
			seen := map[*config.Rule]int{}
			idShard := map[string]int{}
			for k := 1; k <= n; k++ {
				got := shardRules(rls, prior, nil, k, n)
				again := shardRules(rls, prior, nil, k, n)
				if !slices.Equal(got, again) {
					t.Fatalf("n=%d k=%d: the partition is not deterministic", n, k)
				}
				for _, r := range got {
					seen[r]++
					if prev, ok := idShard[r.ID]; ok && prev != k {
						t.Fatalf("n=%d: id %q split across shards %d and %d", n, r.ID, prev, k)
					}
					idShard[r.ID] = k
				}
			}
			for _, r := range rls {
				if seen[r] != 1 {
					t.Fatalf("n=%d hinted=%v: rule %q ran in %d shard(s), want exactly 1", n, prior != nil, r.ID, seen[r])
				}
			}
		}
	}
}

// TestShardPartitionKeepsDeclarationOrder: a shard is a selection, not a
// re-ordering. The engine's own dispatch order (declared cost, or the
// durations hint) must see the rules exactly as the corpus declared them.
func TestShardPartitionKeepsDeclarationOrder(t *testing.T) {
	rls := shardTestRules(20)
	for k := 1; k <= 3; k++ {
		got := shardRules(rls, nil, nil, k, 3)
		idx := make([]int, len(got))
		for i, r := range got {
			idx[i] = slices.Index(rls, r)
		}
		if !slices.IsSorted(idx) {
			t.Fatalf("shard %d/3 re-ordered the rules: declaration indices %v", k, idx)
		}
	}
}

// TestShardPartitionBalancesMeasuredCost: with a durations hint the partition
// is longest-first onto the least-loaded shard, so one heavy rule does not
// share a shard with the next heaviest while another shard idles.
func TestShardPartitionBalancesMeasuredCost(t *testing.T) {
	rls := []*config.Rule{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	prior := engine.Timing{"a": 10 * time.Second, "b": 9 * time.Second, "c": 2 * time.Second, "d": 1 * time.Second}
	ids := func(rs []*config.Rule) []string {
		out := []string{}
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	if got := ids(shardRules(rls, prior, nil, 1, 2)); !slices.Equal(got, []string{"a", "d"}) {
		t.Fatalf("shard 1/2 = %v, want [a d] (10s+1s against 9s+2s)", got)
	}
	if got := ids(shardRules(rls, prior, nil, 2, 2)); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("shard 2/2 = %v, want [b c]", got)
	}
}

// TestShardPartitionSpreadsUnmeasuredRules: a rule the durations report does
// not name is new or below the report's recording floor, not free. Weighing it
// at zero would stack every such rule on whichever shard the measured ones
// left lightest — one long shard, which is the contention this flag removes.
func TestShardPartitionSpreadsUnmeasuredRules(t *testing.T) {
	rls := shardTestRules(40)[:40]
	prior := engine.Timing{"rule-000": 5 * time.Second}
	for k := 1; k <= 4; k++ {
		if got := len(shardRules(rls, prior, nil, k, 4)); got < 9 {
			t.Fatalf("shard %d/4 holds %d of 40 rules; unmeasured rules piled onto other shards", k, got)
		}
	}
}

func TestParseShardSpec(t *testing.T) {
	for _, ok := range []struct {
		spec string
		k, n int
	}{{"1/1", 1, 1}, {"3/4", 3, 4}, {"8/8", 8, 8}} {
		k, n, err := parseShard(ok.spec)
		if err != nil || k != ok.k || n != ok.n {
			t.Fatalf("parseShard(%q) = %d, %d, %v; want %d, %d, nil", ok.spec, k, n, err, ok.k, ok.n)
		}
	}
	for _, bad := range []string{"", "1", "0/4", "5/4", "1/0", "-1/4", "a/b", "1/4/5", "1-4", " 1/4", "01/4"} {
		if _, _, err := parseShard(bad); err == nil {
			t.Fatalf("parseShard(%q) accepted a spec that is not K/N with 1 <= K <= N", bad)
		}
	}
}

// TestShardPinPlacesPinnedIdsFirst: --shard-pin puts every pinned id on its
// named shard and balances the rest AROUND that load, so the pinned shard
// takes less of the remaining work rather than all of it on top.
func TestShardPinPlacesPinnedIdsFirst(t *testing.T) {
	rls := []*config.Rule{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	prior := engine.Timing{"a": 10 * time.Second, "b": 9 * time.Second, "c": 2 * time.Second, "d": 1 * time.Second}
	pinned := map[string]int{"c": 1}
	ids := func(rs []*config.Rule) []string {
		out := []string{}
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	// c (2s) is placed on shard 1 first; a (10s) then goes to the empty shard
	// 2, b (9s) to shard 1 (2s < 10s), d (1s) to shard 2 (10s < 11s).
	if got := ids(shardRules(rls, prior, pinned, 1, 2)); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("shard 1/2 = %v, want [b c]", got)
	}
	if got := ids(shardRules(rls, prior, pinned, 2, 2)); !slices.Equal(got, []string{"a", "d"}) {
		t.Fatalf("shard 2/2 = %v, want [a d]", got)
	}
}

// TestShardPinStillPartitions: pins move ids between shards but never drop or
// double one. For N in 2..8 the pinned ids all sit on the pinned shard, and
// the union of every shard is the whole set, each id exactly once.
func TestShardPinStillPartitions(t *testing.T) {
	rls := shardTestRules(41)
	pinned := map[string]int{}
	for _, r := range rls[:5] {
		pinned[r.ID] = 2
	}
	for _, prior := range []engine.Timing{nil, shardTestTiming(rls)} {
		for n := 2; n <= 8; n++ {
			seen := map[*config.Rule]int{}
			for k := 1; k <= n; k++ {
				for _, r := range shardRules(rls, prior, pinned, k, n) {
					seen[r]++
					if _, ok := pinned[r.ID]; ok && k != 2 {
						t.Fatalf("n=%d: pinned id %q landed on shard %d, want 2", n, r.ID, k)
					}
				}
			}
			for _, r := range rls {
				if seen[r] != 1 {
					t.Fatalf("n=%d hinted=%v: rule %q ran in %d shard(s), want exactly 1", n, prior != nil, r.ID, seen[r])
				}
			}
		}
	}
}

func TestParseShardPinSpec(t *testing.T) {
	p, err := parseShardPin("dart=3")
	if err != nil || p.lane != "dart" || p.k != 3 {
		t.Fatalf("parseShardPin(dart=3) = %+v, %v; want {dart 3}, nil", p, err)
	}
	for _, bad := range []string{"", "dart", "=1", "dart=", "dart=0", "dart=01", "dart=-1", "dart=x", " dart=1", "dart=1/2"} {
		if _, err := parseShardPin(bad); err == nil {
			t.Fatalf("parseShardPin(%q) accepted a spec that is not LANE=K", bad)
		}
	}
}
