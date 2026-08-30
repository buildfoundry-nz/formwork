package engine_test

import (
	"os"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	fwrules "github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"

	// register every rule type so the ported config loads
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filesize"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

// BenchmarkRealTree runs the fast (scan-class) ported rules over the real
// tree of the validating port — the same workload the perf comparison
// measured — so a CPU profile pinpoints the dispatch hotspot. Env-gated so
// it never runs in CI.
func BenchmarkRealTree(b *testing.B) {
	cfgDir := os.Getenv("FW_BENCH_CONFIG")
	treeDir := os.Getenv("FW_BENCH_TREE")
	if cfgDir == "" || treeDir == "" {
		b.Skip("set FW_BENCH_CONFIG and FW_BENCH_TREE to run")
	}
	cfg, err := config.Load(cfgDir)
	if err != nil {
		b.Fatal(err)
	}
	fset, err := scan.Walk(treeDir)
	if err != nil {
		b.Fatal(err)
	}
	var fast []*config.Rule
	for _, r := range cfg.Rules {
		if r.Cost() == fwrules.CostFast {
			fast = append(fast, r)
		}
	}
	b.Logf("rules=%d fast=%d files=%d", len(cfg.Rules), len(fast), len(fset.Files))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Run(fast, fset, 0); err != nil {
			b.Fatal(err)
		}
	}
}
