package main

import (
	"fmt"
	"io"
	"os"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// minCorpusForCanaries is the rule count above which a corpus is taken to be a
// real guardrail tree rather than a fixture. At or below it, absent canaries are
// normal (a synthetic tree has none) and the self-test carries calibration
// alone; above it, losing EVERY canary means the census is looking at something
// other than the corpus it was calibrated for, and that is fatal.
const minCorpusForCanaries = 50

// selfTest proves the matcher can do the one thing whose failure produced the
// false census: expand `**`. It builds rules and a file tree in memory where the
// answers are known, so it calibrates on ANY tree — including a synthetic
// fixture corpus that has no named canary in it.
func selfTest() error {
	files := []string{
		"packages/feature_measure/lib/presentation/measure_page.dart",
		"packages/tqs_core/lib/cache/session_cache_lru.dart",
		"frontend/lib/main.dart",
		"api-factory/services/core-api/routes/admin/admin_jobs.go",
		"README.md",
	}
	fset := &scan.FileSet{Root: "."}
	for _, p := range files {
		fset.Files = append(fset.Files, scan.NewMemFile(p, []byte("x\n")))
	}

	cases := []struct {
		name    string
		include []string
		exclude []string
		want    int
	}{
		{"doublestar mid-pattern", []string{"packages/*/lib/**/*.dart"}, nil, 2},
		{"doublestar suffix", []string{"api-factory/**"}, nil, 1},
		{"single star is not doublestar", []string{"packages/*/*.dart"}, nil, 0},
		{"exclude subtracts", []string{"**/*.dart"}, []string{"frontend/**"}, 2},
		{"literal path", []string{"README.md"}, nil, 1},
	}
	for _, c := range cases {
		r, err := config.New("selftest", "required-pattern", finding.SeverityError, "",
			c.include, c.exclude, nil, nil)
		if err != nil {
			return fmt.Errorf("self-test %q: building rule: %w", c.name, err)
		}
		got := 0
		for _, f := range fset.Files {
			if r.Applies(f.Path()) {
				got++
			}
		}
		if got != c.want {
			return fmt.Errorf("self-test %q: scope %v matched %d file(s), want %d — "+
				"the scope matcher is not behaving as formwork's engine does, so every count this census "+
				"prints would be unfalsifiable", c.name, c.include, got, c.want)
		}
	}
	return nil
}

// calibrate validates the instrument before any number is believed: first the
// built-in matcher self-test, then the per-glob self-test, then the named
// known-live rules. Returns the process exit code (0 = calibrated, 2 =
// environment/target rot).
func calibrate(cfg *config.Config, scopes map[string][]*scan.File, gm *globMeasure, fset *scan.FileSet, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "%s instrument calibration\n", tag)

	if err := selfTest(); err != nil {
		fmt.Fprintf(stdout, "  matcher self-test                  FAILED\n")
		fmt.Fprintf(stderr, "%s FAIL: %v\n", tag, err)
		return 2
	}
	fmt.Fprintf(stdout, "  matcher self-test                  ** expands, excludes subtract — ok\n")

	// The class-2 probes are the arms this census is leaned on for, and inside
	// a proof scratch pruned to one rule they have no content rules left to
	// classify — so this corpus-independent block is the only thing a mutation
	// to them can bite (#15917).
	if err := class2SelfTest(); err != nil {
		fmt.Fprintf(stdout, "  class-2 probe self-test            FAILED\n")
		fmt.Fprintf(stderr, "%s FAIL: %v\n", tag, err)
		return 2
	}
	fmt.Fprintf(stdout, "  class-2 probe self-test            comment plane and detector witness discriminate — ok\n")

	if code := perGlobSelfTest(gm, fset, stdout, stderr); code != 0 {
		return code
	}

	present, under := 0, 0
	for _, c := range calibration {
		n, found := 0, false
		for _, r := range cfg.Rules {
			if r.ID == c.id {
				n, found = len(scopes[r.ID]), true
			}
		}
		switch {
		case !found:
			fmt.Fprintf(stdout, "  %-34s absent from this corpus\n", c.id)
		case n < c.minHit:
			fmt.Fprintf(stdout, "  %-34s %d in-scope files (want >= %d) — NOT CALIBRATED\n", c.id, n, c.minHit)
			present++
			under++
		default:
			fmt.Fprintf(stdout, "  %-34s %d in-scope files — live\n", c.id, n)
			present++
		}
	}
	if under > 0 {
		fmt.Fprintf(stderr, "%s FAIL: %d known-live rule(s) reported fewer files than they guard; "+
			"every zero below would be unfalsifiable. If a canary was legitimately retired, replace it in the "+
			"calibration list in tools/formwork-vacuity-census/main.go — never lower its floor.\n", tag, under)
		return 2
	}
	if present == 0 && len(cfg.Rules) > minCorpusForCanaries {
		fmt.Fprintf(stderr, "%s FAIL: a %d-rule corpus with none of the named known-live rules in it. "+
			"Either the census is pointed at the wrong tree or every canary has been renamed; "+
			"update the calibration list in tools/formwork-vacuity-census/main.go.\n", tag, len(cfg.Rules))
		return 2
	}
	return 0
}

// impossibleGlobEnv overrides the deliberately-impossible self-test glob. It
// exists for exactly one caller: the lockdown synth
// TestVacuityCensus_ExitsTwoWhenGlobMatcherSelfTestFails, which points it at a
// glob that matches everything to prove this failure arm is load-bearing.
const impossibleGlobEnv = "FORMWORK_VACUITY_CENSUS_SELFTEST_IMPOSSIBLE_GLOB"

// perGlobSelfTest is the per-glob arm's half of calibration (#10626), the
// direct answer to #10083's 133-false-zeros lesson one level down: a per-glob
// census that reports zero for a glob covering thousands of live files has
// reproduced the shell-census bug inside its own fix. Two probes, and the
// census exits 2 — reporting nothing — if either fails:
//
//   - a deliberately-impossible glob must match ZERO files on any tree;
//   - dart-file-size-cap's `**/*.dart` include glob, when that rule is in the
//     corpus, must match a FOUR-FIGURE count. Absent from the corpus (a
//     synthetic fixture tree) it is simply noted, like the named canaries.
func perGlobSelfTest(gm *globMeasure, fset *scan.FileSet, stdout, stderr io.Writer) int {
	impossible := os.Getenv(impossibleGlobEnv)
	if impossible == "" {
		impossible = "**/*.formwork-vacuity-census-impossible"
	}
	paths := make([]string, 0, len(fset.Files))
	for _, f := range fset.Files {
		paths = append(paths, f.Path())
	}
	n, err := countGlobMatches(impossible, paths)
	if err != nil || n != 0 {
		fmt.Fprintf(stdout, "  per-glob self-test                 FAILED\n")
		fmt.Fprintf(stderr, "%s FAIL: the deliberately-impossible glob %q matched %d file(s) (err=%v) — "+
			"the per-glob matcher is not behaving as formwork's engine does, so every per-glob zero "+
			"this census prints would be unfalsifiable.\n", tag, impossible, n, err)
		return 2
	}
	fmt.Fprintf(stdout, "  per-glob self-test                 impossible glob matches 0 — ok\n")

	const canaryID, canaryGlob = "dart-file-size-cap", "**/*.dart"
	globs, present := gm.include[canaryID]
	if !present {
		fmt.Fprintf(stdout, "  %-34s absent from this corpus (per-glob)\n", canaryID+" "+canaryGlob)
		return 0
	}
	for _, g := range globs {
		if g.glob != canaryGlob {
			continue
		}
		if g.n < 1000 {
			fmt.Fprintf(stdout, "  %-34s %d files (want >= 1000) — NOT CALIBRATED\n", canaryID+" "+canaryGlob, g.n)
			fmt.Fprintf(stderr, "%s FAIL: %s's %q glob matched %d files; every per-glob zero below "+
				"would be unfalsifiable. If the canary glob legitimately changed, update the per-glob "+
				"self-test in tools/formwork-vacuity-census/calibrate.go — never lower its floor.\n",
				tag, canaryID, canaryGlob, g.n)
			return 2
		}
		fmt.Fprintf(stdout, "  %-34s %d files — live (per-glob)\n", canaryID+" "+canaryGlob, g.n)
		return 0
	}
	// The rule is present but no longer declares the expected glob: the
	// canary moved, and calibration must be re-anchored rather than assumed.
	fmt.Fprintf(stdout, "  %-34s glob not on the rule — NOT CALIBRATED\n", canaryID+" "+canaryGlob)
	fmt.Fprintf(stderr, "%s FAIL: %s no longer declares an %q include glob; re-anchor the per-glob "+
		"self-test in tools/formwork-vacuity-census/calibrate.go.\n", tag, canaryID, canaryGlob)
	return 2
}
