package vacuity

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type nopChecker struct{}

func (nopChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

type heavyChecker struct{ nopChecker }

func (heavyChecker) Cost() rules.Cost { return rules.CostHeavy }

func loadHeavy(t *testing.T) *config.Rule {
	t.Helper()
	r, err := config.New("h", "command", "error", "", []string{"src/**"}, nil, nil, heavyChecker{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
