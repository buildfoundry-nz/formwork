package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/engine"
)

// durationsUsage is the `check --durations` flag help, kept beside the code
// that honours it.
//
// The FIRST backquoted word is the argument name flag.PrintDefaults renders
// (flag.UnquoteUsage), so `path` has to come before any other backquoted term
// or the flag prints as `-durations check -format json`. It did.
const durationsUsage = "`path` of a previous whole-tree `check -format json` report: its per-rule " +
	"durations schedule this run's pools longest-first (without it they order by declared cost:). " +
	"Never changes findings, the verdict or the exit code"

// priorDurations resolves `check --durations <path>` into the scheduling hint
// engine.RunTimedHinted takes, printing its own refusal and returning ok=false
// when the run must abort with exit 2.
//
// An empty path is the default and is not an error: no flag, no hint, and the
// pool falls back to the declared `cost:` ordering that needs no state.
//
// EVERY OTHER FAILURE IS A REFUSAL, and that is the point of the function
// rather than an afterthought. The hint cannot change a verdict — dispatch
// order reaches no finding and no exit code — so the temptation is to treat a
// missing, truncated or timing-less report as harmless and carry on. That is
// exactly the shape this repo keeps rediscovering: a supplied flag that is
// silently ignored, leaving the operator believing a run was scheduled a way it
// was not. The failure is not a wrong verdict, it is an unfalsifiable claim
// about the run, and the same refusal is already how --workers, --range and
// --cost-max answer a value they cannot honour. Cheap to diagnose, impossible
// to misread.
//
// fileSetFlag names the --staged/--range flag in force, or "" for a whole-tree
// run. It is a refusal rather than a no-op for the same reason: those modes
// evaluate through engine.Run, which takes no hint and collects no timing
// (0.6.2 scoped timing to whole-tree runs), so the flag would be parsed and
// discarded.
func priorDurations(path, fileSetFlag string, stderr io.Writer) (engine.Timing, bool) {
	if path == "" {
		return nil, true
	}
	if fileSetFlag != "" {
		fmt.Fprintf(stderr, "formwork: --durations is a whole-tree option and %s does not collect or "+
			"consume per-rule timings; drop one of the two flags\n", fileSetFlag)
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "formwork: --durations %s: cannot read the report: %v\n", path, err)
		return nil, false
	}
	// Only the durations object is read. The rest of the report belongs to the
	// run that wrote it, and decoding strictly here would refuse a report from a
	// NEWER binary that added a field — the opposite of what this flag is for,
	// which is feeding one CI run's report to the next.
	var rep struct {
		Durations map[string]int64 `json:"durations"`
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		fmt.Fprintf(stderr, "formwork: --durations %s: not a formwork JSON report: %v\n", path, err)
		return nil, false
	}
	if len(rep.Durations) == 0 {
		fmt.Fprintf(stderr, "formwork: --durations %s: the report carries no `durations` object, so it "+
			"can schedule nothing. Produce it with `formwork check -format json` on a whole-tree run "+
			"(the human and github formats carry no timings, and --staged/--range runs collect none)\n", path)
		return nil, false
	}
	hint := make(engine.Timing, len(rep.Durations))
	// Sorted, so a report carrying several corrupt entries names the same one on
	// every run. Go randomises map iteration, and a refusal that blames a
	// different rule each time is a refusal nobody can act on.
	for _, id := range slices.Sorted(maps.Keys(rep.Durations)) {
		ms := rep.Durations[id]
		if ms < 0 {
			fmt.Fprintf(stderr, "formwork: --durations %s: rule %q reports %d ms; a negative duration is "+
				"not something this engine writes, so the report is corrupt\n", path, id, ms)
			return nil, false
		}
		hint[id] = time.Duration(ms) * time.Millisecond
	}
	return hint, true
}
