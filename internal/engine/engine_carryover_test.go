package engine_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func TestRunDrainsQueueAfterPanic(t *testing.T) {
	var seen atomic.Int64
	counter := &fakeChecker{match: func(f *scan.File) []rules.Match {
		seen.Add(1)
		return nil
	}}
	r1 := mustRule(t, "panicking-rule", finding.SeverityError, []string{"**"}, &fakeChecker{panics: true})
	r2 := mustRule(t, "counting-rule", finding.SeverityError, []string{"**"}, counter)
	files := map[string]string{}
	for i := range 50 {
		files[fmt.Sprintf("f%02d.txt", i)] = "x"
	}
	_, err := engine.Run([]*config.Rule{r1, r2}, memFileSet(files), 4)
	if err == nil {
		t.Fatal("expected error from panicking rule")
	}
	if seen.Load() != 50 {
		t.Fatalf("queue not drained after panic: counting rule saw %d/50 files", seen.Load())
	}
}

func TestRunDiscardsFindingsOnError(t *testing.T) {
	finder := &fakeChecker{match: func(f *scan.File) []rules.Match {
		return []rules.Match{{Line: 1, Message: "hit"}}
	}}
	r1 := mustRule(t, "finding-rule", finding.SeverityError, []string{"**"}, finder)
	r2 := mustRule(t, "erroring-rule", finding.SeverityError, []string{"**"}, &fakeChecker{err: errFake})
	got, err := engine.Run([]*config.Rule{r1, r2}, memFileSet(map[string]string{"a.txt": "x"}), 2)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("findings must be discarded on error, got %+v", got)
	}
}

func TestRunDualRoleCheckerAndFinalizer(t *testing.T) {
	dual := &fakeFinalizer{fakeChecker: fakeChecker{match: func(f *scan.File) []rules.Match {
		return []rules.Match{{Line: 1, Message: "per-file"}}
	}}}
	dual.final = []rules.Match{{Message: "finalized"}}
	r := mustRule(t, "dual-role", finding.SeverityError, []string{"**"}, dual)
	got, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "" || got[1].Path != "a.txt" {
		t.Fatalf("dual-role findings wrong (want scope-level then file-level after sort): %+v", got)
	}
}
