package cli

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	"github.com/buildfoundry-nz/formwork/internal/report"
)

func runTest(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("test", "repository root", stderr)
	workers := fs.Int("workers", 0, "worker count (0 or absent = GOMAXPROCS; a negative count is refused)")
	var rules ruleSelection
	fs.Var(&rules, "rule", "run only the named rules' fixtures; repeat the flag or comma-separate ids (default: all rules; any unknown id is exit 2)")
	format := fs.String("format", "human", "output format: human | json")
	cfg, ok := parseAndLoad(fs, args, root, stderr)
	if !ok {
		return 2
	}
	// Ahead of every config-CONTENT guard below, for runCheck's reason: a caller
	// who mistyped a flag needs to hear about the flag, and the zero-rules
	// refusal further down would otherwise answer `--workers -1` with advice
	// about rule files.
	if !workersValueUsable(*workers, stderr) {
		return 2
	}
	// report.ValidFormat also admits "github", which has no meaning for fixture
	// verdicts, so this command validates its own pair.
	switch *format {
	case "", "human", "json":
	default:
		fmt.Fprintf(stderr, "formwork: unknown format %q (want human or json)\n", *format)
		return 2
	}
	// Collect the full id set BEFORE --rule scoping: the orphan check inside
	// Run compares fixture dirs against every rule the corpus declares, so a
	// scoped run must not read sibling rules' fixtures as orphans (#58).
	allIDs := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		allIDs = append(allIDs, r.ID)
	}
	// Zero rules CONFIGURED means there is nothing this command could report on,
	// so "0/0 rules passed, 0 fixture(s) run" at exit 0 is a green verdict over
	// nothing (#151 row 12).
	//
	// But the refusal must not TAKE the better diagnosis with it. Run reports
	// orphan fixture trees — dirs on disk matching no rule id — as FAIL
	// verdicts counted in `failed`. Calling it here with a capturing writer
	// surfaces that specific, actionable diagnosis (it names the dead trees)
	// for the case where rule files vanished but their fixtures did not.
	// Guarding ahead of Run instead replaced "these 3 fixture dirs can never
	// run" with "there are no fixtures", which was both less useful and
	// untrue. Zero rules is still exit 2 (a config error). When rules exist,
	// the same orphans are findings (exit 1) and other rules still run (#9).
	//
	// Deliberately NOT the same condition as "zero fixtures ran". A configured
	// rule with no fixture directory also prints "0/0 rules passed" at exit 0 —
	// but it prints `[id] SKIP — no fixtures` and a skipped count first, and
	// lint's fixture-coverage check fails on it. That one is disclosed, not
	// silent, which is the whole difference.
	if len(cfg.Rules) == 0 {
		var buf bytes.Buffer
		failed, err := fixturetest.Run(cfg, allIDs, *root, *workers, &buf)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		if failed > 0 {
			fmt.Fprint(stderr, buf.String())
			return 2
		}
		fmt.Fprintln(stderr, "formwork:", noRulesReason(cfg, "there are no fixtures to run and nothing this command could report on"))
		return 2
	}
	if len(rules) > 0 {
		scoped, unknown := scopeToRules(cfg, rules)
		for _, id := range unknown {
			fmt.Fprintf(stderr, "formwork: no rule matches --rule %q\n", id)
		}
		if len(unknown) > 0 {
			return 2
		}
		cfg = scoped
	}
	if *format == "json" {
		// Results first, render second: an error returns before a byte reaches
		// stdout, so exit 2 never carries a document.
		results, err := fixturetest.Results(cfg, allIDs, *root, *workers)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		if err := report.TestJSON(stdout, results); err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		for _, rr := range results {
			if rr.Status == fixturetest.StatusFail {
				return 1
			}
		}
		return 0
	}
	failed, err := fixturetest.Run(cfg, allIDs, *root, *workers, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// ruleSelection is the repeatable, comma-splitting value of `test --rule`: each
// occurrence is split on commas, segments are trimmed, empties dropped, and
// repeats removed keeping first-seen order.
type ruleSelection []string

func (l *ruleSelection) String() string { return strings.Join(*l, ",") }

func (l *ruleSelection) Set(v string) error {
	for _, seg := range strings.Split(v, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" || slices.Contains(*l, seg) {
			continue
		}
		*l = append(*l, seg)
	}
	return nil
}

// scopeToRules is scopeToRule for several ids: it returns the shallow copy
// narrowed to the named rules plus every id that matched no rule, in the order
// given. The caller must treat any unknown id as exit 2 — a partial run must
// not read as a full pass.
func scopeToRules(cfg *config.Config, ids []string) (scoped *config.Config, unknown []string) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var kept []*config.Rule
	found := make(map[string]bool, len(ids))
	for _, r := range cfg.Rules {
		if want[r.ID] {
			kept = append(kept, r)
			found[r.ID] = true
		}
	}
	for _, id := range ids {
		if !found[id] {
			unknown = append(unknown, id)
		}
	}
	c := *cfg
	c.Rules = kept
	c.Lanes = nil
	return &c, unknown
}
