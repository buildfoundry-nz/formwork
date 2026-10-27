package config_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type benchChecker struct{}

func (benchChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

// Applies runs for every (file, rule) pair the engine considers, and in a
// large corpus almost every pair is a miss. This measures that path over
// globs and paths shaped like a real monorepo's.
func BenchmarkRuleAppliesMostlyMisses(b *testing.B) {
	r, err := config.New("bench", "fake", finding.SeverityError, "",
		[]string{"api-factory/services/core-api/internal/**/*.go", "packages/*/lib/**/providers/**/*.dart", "scripts/dev/*census*/**/*.go"},
		[]string{"**/*_test.go", "**/*.g.dart", "shared/generated/**"}, benchChecker{})
	if err != nil {
		b.Fatal(err)
	}
	paths := []string{
		"packages/feature_account/lib/data/account_repository.dart",
		"api-factory/services/core-api/routes/projects.go",
		"docs/plans/README.md",
		"frontend/lib/routing/router.dart",
		"api-factory/services/core-api/internal/metrics/rule_engine.go",
		"scripts/dev/check-precommit-gate-lanes-partitioned.go",
		"schema/proto/takeoffqs/api/v1/projects.proto",
		"packages/tqs_widgets/lib/presentation/buttons/app_button.dart",
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range paths {
			r.Applies(p)
		}
	}
}

// Applies matches include and exclude without re-validating them, which is
// sound only because New refuses an invalid one. The include half is pinned
// through Load ("bad glob"); this pins both halves at New itself.
func TestNewRefusesAnInvalidStoredGlob(t *testing.T) {
	for name, globs := range map[string][2][]string{
		"include": {{"["}, nil},
		"exclude": {{"**"}, {"["}},
	} {
		if _, err := config.New("r", "fake", finding.SeverityError, "", globs[0], globs[1], benchChecker{}); err == nil {
			t.Errorf("New accepted an invalid %s glob; Applies matches stored globs unvalidated", name)
		}
	}
}
