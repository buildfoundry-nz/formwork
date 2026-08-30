package meta

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// fixturecoverage.go holds the fixture-coverage check and the two predicates
// only it uses. A pure split out of lint.go, which #53's declared-exemption
// arm pushed to 760 lines against the repo's own 750-line hard cap — and that
// cap says never widen it, because consumers vendoring this source enforce it.
// Same move internal/scan made for restrict.go.

// fixtureCoverage emits the fixture-coverage verdict.
func fixtureCoverage(cfg *config.Config, root string, w io.Writer, failed, total *int) error {
	var coverage []string
	for _, r := range cfg.Rules {
		// Whole-run external-tool rules (command, git-diff) can be exempt from
		// fixtures — their behaviour depends on external tools and git state a
		// fixture tree cannot reproduce. But the exemption must be DECLARED,
		// not inferred from cost (#53).
		//
		// Keyed on cost alone, "cannot be fixtured by construction" and "nobody
		// bothered" were the same state, and `formwork test` printed
		// `SKIP — no fixtures` for both. Command rules are the escape hatch
		// reached for the highest-stakes lockdowns, so the rules carrying no
		// firing proof at all were the ones that most needed it: downstream, 58
		// were in that state and the run exited 0.
		//
		// A declared exemption is also enumerated by the census, because an
		// escape hatch that silenced this check without appearing anywhere
		// would just be a quieter spelling of the same defect.
		//
		// TrimSpace, not `== ""`: `fixture_exempt: "   "` declares nothing, and
		// the gate's whole claim is that the gap is a DECISION (#336). It is
		// also the predicate this repo already owns for the same job twice —
		// internal/config/scan.go:94 and :115 refuse a scan reason on it, and
		// lintpolicy.go:100-106 next door refuses a lint.yaml skip on it.
		if isExternalTool(r) {
			if strings.TrimSpace(r.FixtureExempt) == "" {
				fire, pass, err := fixtureCounts(filepath.Join(root, ".formwork", "fixtures", r.ID))
				if err != nil {
					return err
				}
				switch {
				case fire == 0 && pass == 0:
					coverage = append(coverage, fmt.Sprintf(
						"%s: heavy rule with no fixtures and no declared exemption — add fixtures, or declare `fixture_exempt: <why a fixture tree cannot drive this>` so the gap is a decision instead of an accident",
						r.ID))
				case fire == 0:
					coverage = append(coverage, fmt.Sprintf(
						"%s: heavy rule has a pass fixture but no fire fixture — nothing proves the detector can fire at all (want .formwork/fixtures/%s/fire-*/)",
						r.ID, r.ID))
				case pass == 0:
					// THE PAIR IS THE PROOF, and for a heavy rule it is the only
					// proof there is (#230). A detector that CANNOT RUN —
					// `go run ./tools/detector/main.go` where that path does not
					// exist — exits non-zero in every fixture, so the fire
					// fixture is satisfied for the wrong reason and reports a
					// detector that never executed as a passing proof. Only the
					// pass fixture catches it, by seeing that same non-zero exit
					// as an unexpected finding.
					//
					// The declarative branch below has always judged fire and
					// pass independently. This branch asked `fire == 0 && pass
					// == 0`, so a heavy rule carrying a fire fixture and no pass
					// fixture reported healthy — the fig-leaf shape #230 was
					// filed about, one level up from where it was looked for.
					//
					// A DECLARED exemption still governs: it is a recorded
					// decision (#53) and this check is above that test, not
					// inside it.
					coverage = append(coverage, fmt.Sprintf(
						"%s: heavy rule has a fire fixture but no pass fixture — a detector that cannot run exits non-zero in the fire fixture too, so only the pass fixture proves it RAN (want .formwork/fixtures/%s/pass-*/)",
						r.ID, r.ID))
				}
			}
			continue
		}
		fire, pass, err := fixtureCounts(filepath.Join(root, ".formwork", "fixtures", r.ID))
		if err != nil {
			return err
		}
		if fire == 0 {
			coverage = append(coverage, fmt.Sprintf("%s: no fire fixture (want .formwork/fixtures/%s/fire-*/)", r.ID, r.ID))
		}
		if pass == 0 {
			coverage = append(coverage, fmt.Sprintf("%s: no pass fixture (want .formwork/fixtures/%s/pass-*/)", r.ID, r.ID))
		}
	}
	*failed += emit(w, "fixture-coverage", coverage)
	*total++
	return nil
}

// isExternalTool reports whether r is a heavy, whole-run external-tool rule
// (command, git-diff): heavy rules shell out to tools/git and are tracked as
// escape hatches, not by fixtures or empty-scope rot. Keying on cost (not the
// ErrFinalizer interface) lets fast rules that merely need the repo root —
// doc-path-exists, baseline — use ErrFinalizer without being exempted.
func isExternalTool(r *config.Rule) bool {
	return r.Cost() == rules.CostHeavy
}

func fixtureCounts(ruleDir string) (fire, pass int, err error) {
	entries, err := os.ReadDir(ruleDir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		switch {
		case strings.HasPrefix(e.Name(), "fire-"):
			fire++
		case strings.HasPrefix(e.Name(), "pass-"):
			pass++
		}
	}
	return fire, pass, nil
}
