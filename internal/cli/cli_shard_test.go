// cli_shard_test.go — `check --shard K/N` runs one shard of the selected rules
// (TakeoffQS#25657), so a CI matrix can spread one whole-corpus sweep over N
// machines. The partition itself is pinned in shard_internal_test.go; these
// pin what the command does with it: every shard's report names the rules it
// ran, the reports together cover the corpus exactly once, and a spec the run
// cannot honour is refused rather than quietly running something else.
package cli_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// shardRepo carries six rules, two of which fire, so a shard's findings can be
// checked against the rules that shard says it ran.
func shardRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nlanes:\n  ci: {all: true}\n")
	var b strings.Builder
	b.WriteString("rules:\n")
	for _, id := range []string{"no-alpha", "no-beta", "no-gamma", "no-delta", "no-epsilon", "no-zeta"} {
		b.WriteString("  - id: " + id + "\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: " +
			strings.ToUpper(strings.TrimPrefix(id, "no-")) + "}\n")
	}
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"), b.String())
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"ALPHA DELTA\"\n")
	return root
}

type shardReport struct {
	Findings []struct {
		Rule string `json:"rule"`
	} `json:"findings"`
	Scan struct {
		Shard *struct {
			Index int      `json:"index"`
			Count int      `json:"count"`
			Rules []string `json:"rules"`
		} `json:"shard"`
	} `json:"scan"`
	Summary struct {
		RulesTotal int `json:"rules_total"`
	} `json:"summary"`
}

func TestCheckShardReportsTogetherCoverTheCorpusOnce(t *testing.T) {
	root := shardRepo(t)
	all := []string{"no-alpha", "no-beta", "no-delta", "no-epsilon", "no-gamma", "no-zeta"}
	for n := 1; n <= 3; n++ {
		var union, fired []string
		total := 0
		for k := 1; k <= n; k++ {
			spec := strconv.Itoa(k) + "/" + strconv.Itoa(n)
			code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--shard", spec, "-format", "json")
			if code != 0 && code != 1 {
				t.Fatalf("--shard %s: exit = %d\nstderr:\n%s", spec, code, errOut)
			}
			var rep shardReport
			if err := json.Unmarshal([]byte(out), &rep); err != nil {
				t.Fatalf("--shard %s: %v\n%s", spec, err, out)
			}
			if rep.Scan.Shard == nil || rep.Scan.Shard.Index != k || rep.Scan.Shard.Count != n {
				t.Fatalf("--shard %s: the report must name the shard it ran:\n%s", spec, out)
			}
			if rep.Summary.RulesTotal != len(rep.Scan.Shard.Rules) {
				t.Fatalf("--shard %s: rules_total %d but the shard names %d rules", spec, rep.Summary.RulesTotal, len(rep.Scan.Shard.Rules))
			}
			for _, f := range rep.Findings {
				if !slices.Contains(rep.Scan.Shard.Rules, f.Rule) {
					t.Fatalf("--shard %s: finding from %q, a rule this shard did not run", spec, f.Rule)
				}
				fired = append(fired, f.Rule)
			}
			union = append(union, rep.Scan.Shard.Rules...)
			total += rep.Summary.RulesTotal
		}
		slices.Sort(union)
		if !slices.Equal(union, all) {
			t.Fatalf("n=%d: shards together ran %v, want each of %v exactly once", n, union, all)
		}
		slices.Sort(fired)
		if !slices.Equal(fired, []string{"no-alpha", "no-delta"}) {
			t.Fatalf("n=%d: shards together reported findings from %v, want [no-alpha no-delta]", n, fired)
		}
		if total != len(all) {
			t.Fatalf("n=%d: rules_total summed to %d, want %d", n, total, len(all))
		}
	}
}

func TestCheckShardHeadlineNamesTheShard(t *testing.T) {
	root := shardRepo(t)
	_, out, _ := runCLI(t, "check", "-C", root, "--shard", "2/3")
	if !strings.Contains(out, "shard 2/3") {
		t.Fatalf("a sharded run must say so where it reports what it looked at:\n%s", out)
	}
}

func TestCheckShardBadSpecIsRefused(t *testing.T) {
	root := shardRepo(t)
	for _, spec := range []string{"0/3", "4/3", "1/0", "x", "1-3", ""} {
		code, out, errOut := runCLI(t, "check", "-C", root, "--shard", spec)
		if code != 2 {
			t.Fatalf("--shard %q: exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", spec, code, out, errOut)
		}
		if !strings.Contains(errOut, "--shard") {
			t.Fatalf("--shard %q: the refusal must name the flag:\n%s", spec, errOut)
		}
	}
}

// An empty shard would render "0/0 rules passed" at exit 0 — a CI matrix
// wider than the corpus reading as green over nothing.
func TestCheckEmptyShardIsRefused(t *testing.T) {
	root := shardRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root, "--shard", "7/7")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a shard that selects no rules\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "shard 7/7") {
		t.Fatalf("the refusal must name the shard:\n%s", errOut)
	}
}

// pinRepo is shardRepo with two of its rules tagged into a `pinned` lane, and
// an `empty` lane that selects nothing.
func pinRepo(t *testing.T) string {
	t.Helper()
	root := shardRepo(t)
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  ci: {all: true}\n  pinned: {tags: [pinned]}\n  empty: {tags: [nothing-has-this]}\n  some: {tags: [some]}\n")
	var b strings.Builder
	b.WriteString("rules:\n")
	for _, id := range []string{"no-alpha", "no-beta", "no-gamma", "no-delta", "no-epsilon", "no-zeta"} {
		tags := "[some]"
		if id == "no-beta" || id == "no-zeta" {
			tags = "[pinned]"
		}
		b.WriteString("  - id: " + id + "\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: " +
			strings.ToUpper(strings.TrimPrefix(id, "no-")) + "}\n    tags: " + tags + "\n")
	}
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"), b.String())
	return root
}

// TestCheckShardPinRunsTheLaneOnItsShard: with --shard-pin pinned=K every
// rule the lane selects is in shard K's report, and the N reports still cover
// the corpus exactly once.
func TestCheckShardPinRunsTheLaneOnItsShard(t *testing.T) {
	root := pinRepo(t)
	all := []string{"no-alpha", "no-beta", "no-delta", "no-epsilon", "no-gamma", "no-zeta"}
	for n := 2; n <= 3; n++ {
		for pinK := 1; pinK <= n; pinK++ {
			var union []string
			for k := 1; k <= n; k++ {
				spec := strconv.Itoa(k) + "/" + strconv.Itoa(n)
				code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--shard", spec,
					"--shard-pin", "pinned="+strconv.Itoa(pinK), "-format", "json")
				if code != 0 && code != 1 {
					t.Fatalf("--shard %s --shard-pin pinned=%d: exit = %d\nstderr:\n%s", spec, pinK, code, errOut)
				}
				var rep shardReport
				if err := json.Unmarshal([]byte(out), &rep); err != nil {
					t.Fatalf("--shard %s: %v\n%s", spec, err, out)
				}
				for _, id := range []string{"no-beta", "no-zeta"} {
					if got := slices.Contains(rep.Scan.Shard.Rules, id); got != (k == pinK) {
						t.Fatalf("--shard %s --shard-pin pinned=%d: %s in this shard = %v, want %v", spec, pinK, id, got, k == pinK)
					}
				}
				union = append(union, rep.Scan.Shard.Rules...)
			}
			slices.Sort(union)
			if !slices.Equal(union, all) {
				t.Fatalf("n=%d pin=%d: shards together ran %v, want each of %v exactly once", n, pinK, union, all)
			}
		}
	}
}

// A pin the run cannot honour is refused, never dropped: a CI job that sets up
// a toolchain for its pinned rules is only right while they are really there.
func TestCheckShardPinRefusals(t *testing.T) {
	root := pinRepo(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no --shard", []string{"--shard-pin", "pinned=1"}, "without --shard"},
		{"K beyond N", []string{"--shard", "1/2", "--shard-pin", "pinned=3"}, "pinned=3"},
		{"unknown lane", []string{"--shard", "1/2", "--shard-pin", "nope=1"}, "nope"},
		{"lane named twice", []string{"--shard", "1/2", "--shard-pin", "pinned=1", "--shard-pin", "pinned=2"}, "twice"},
		{"lane pins nothing", []string{"--shard", "1/2", "--shard-pin", "empty=1"}, "pins no rule"},
		{"lane outside the selection", []string{"--lane", "some", "--shard", "1/2", "--shard-pin", "pinned=1"}, "pins no rule"},
		{"malformed", []string{"--shard", "1/2", "--shard-pin", "pinned"}, "--shard-pin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, append([]string{"check", "-C", root}, tc.args...)...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Fatalf("the refusal must name %q:\n%s", tc.want, errOut)
			}
		})
	}
}
