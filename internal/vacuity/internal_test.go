package vacuity

import (
	"errors"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// A rule whose anchor params cannot be read is a config defect. Judge must
// return the error (exit 2), never fold it into "judged". A real rule cannot
// reach this branch — the type's own factory rejects what yaml cannot read —
// so the reader is injected.
func TestUnreadableAnchorParamsIsAnError(t *testing.T) {
	r, err := config.New("x", "go/guard-precedes-call", "error", "", []string{"**"}, nil, nil, nopChecker{})
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	_, err = blind(r, &scan.FileSet{}, func(*config.Rule) (anchorParamSet, error) {
		return anchorParamSet{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the anchor-params error", err)
	}
}

// Heavy rules are out of Vacuous's class: their tool never consults scope, so
// the universal-scope probe must not run (Judge screens escapes first, so only
// a direct caller of Vacuous can reach this).
func TestVacuousSkipsHeavyRules(t *testing.T) {
	r := loadHeavy(t)
	got, err := Vacuous(r, &scan.FileSet{}, nil)
	if err != nil || got {
		t.Fatalf("Vacuous(heavy) = %v, %v; want false, nil", got, err)
	}
}

// A probe that cannot be built is an error, not a verdict either way.
func TestVacuousProbeFailureIsAnError(t *testing.T) {
	r, err := config.New("x", "forbidden-pattern", "error", "", []string{"src/**"}, nil, nil, nopChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Vacuous(r, &scan.FileSet{}, nil); err == nil {
		t.Fatal("Vacuous swallowed a probe failure (rule has no factory to refresh)")
	}
}
