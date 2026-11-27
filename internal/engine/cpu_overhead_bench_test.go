package engine_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func BenchmarkCPUOverhead(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("cpu=%v", enabled), func(b *testing.B) {
			files := map[string]string{}
			for i := 0; i < 24; i++ {
				files[fmt.Sprintf("f-%02d.txt", i)] = "x"
			}
			fset := memFileSet(files)
			newRule := func(i int) *config.Rule {
				checker := &fakeChecker{match: func(*scan.File) []rules.Match {
					cpuBenchSink = spinForCPU(50_000)
					return nil
				}}
				r, err := config.New(fmt.Sprintf("rule-%02d", i), "fake", finding.SeverityError, "fix it", []string{"**/*.txt"}, nil, checker)
				if err != nil {
					b.Fatal(err)
				}
				return r
			}
			var pairs atomic.Int64
			restore := engine.SetCPUMeasurementForTest(enabled, &pairs)
			b.Cleanup(restore)
			rls := make([]*config.Rule, 24)
			for i := range rls {
				rls[i] = newRule(i)
			}
			run := func() engine.Timings {
				_, timings, err := engine.RunTimed(rls, fset, 1, nil)
				if err != nil {
					b.Fatal(err)
				}
				return timings
			}
			warmup := run()
			if enabled != (warmup.CPU != nil) {
				b.Fatalf("CPU measurement switch did not change collection: enabled=%v CPU=%#v", enabled, warmup.CPU)
			}
			pairs.Store(0)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
			b.StopTimer()
			b.ReportMetric(float64(pairs.Load())/float64(b.N), "pairs/op")
			b.ReportMetric(float64(pairs.Load())/float64(b.N*len(rls)*len(fset.Files)), "pairs/check")
		})
	}
}

var cpuBenchSink uint64

func spinForCPU(n uint64) uint64 {
	x := cpuBenchSink + uint64(time.Now().Nanosecond()%3)
	for i := uint64(0); i < n; i++ {
		x = x*1664525 + 1013904223
	}
	return x
}
