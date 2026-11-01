package cli

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type plannerFinalizer struct{}

func (plannerFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (plannerFinalizer) Finalize() []rules.Match                     { return nil }

type plannerWrapper struct{ inner rules.Checker }

func (w plannerWrapper) CheckFile(f *scan.File) ([]rules.Match, error) {
	return w.inner.CheckFile(f)
}
func (plannerWrapper) ContentOnly(string) bool        { return true }
func (w plannerWrapper) UnwrapChecker() rules.Checker { return w.inner }

func TestPlanCheckContentDefersWrappedFinalizer(t *testing.T) {
	r, err := config.New("wrapped", "test", finding.SeverityError, "", []string{"**"}, nil, plannerWrapper{inner: plannerFinalizer{}})
	if err != nil {
		t.Fatal(err)
	}
	eligible, notEvaluated, _, deferred, _ := planCheckContent([]*config.Rule{r}, "a.txt", nil)
	if len(eligible) != 0 || deferred != 1 {
		t.Fatalf("wrapped finalizer reached eligible set: eligible=%v deferred=%d", eligible, deferred)
	}
	if len(notEvaluated) != 1 || notEvaluated[0].Reason != "finalizer" {
		t.Fatalf("wrapped finalizer reason = %+v", notEvaluated)
	}
}
