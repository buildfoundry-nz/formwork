package pairconsistency_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// --- obligation: countable -------------------------------------------------
//
// Default pair-consistency is unit-presence: one requires match clears every
// trigger in the unit. That is correct for many rules and must stay the default.
// obligation: countable demands count(requires) ≥ count(trigger) inside the
// unit so a second trigger cannot free-ride on a single companion (V18).

const countableSameFuncParams = "trigger: 'projectMetricUnionSQL'\n" +
	"requires: 'projectreadscope\\.Memo\\('\n" +
	"where: same-func\n" +
	"obligation: countable\n"

// TestPairConsistencyCountableTwoTriggersOneCompanionFails is fire-N: two
// trigger occurrences and one companion in one function must produce a finding
// under obligation:countable (presence mode would pass).
func TestPairConsistencyCountableTwoTriggersOneCompanionFails(t *testing.T) {
	src := `package p

func load(ctx, tx interface{}) error {
	return projectreadscope.Memo(ctx, "k", func() error {
		_ = projectMetricUnionSQL // first
		_ = projectMetricUnionSQL // second free-rides without a second Memo
		tx.Query()
		return nil
	})
}
`
	c := mustChecker(t, countableSameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("countable: two triggers + one companion must fire once, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "load") {
		t.Fatalf("finding must name the func, got %q", ms[0].Message)
	}
	if !strings.Contains(ms[0].Message, "countable") && !strings.Contains(ms[0].Message, "2") {
		t.Fatalf("message should surface the count residual, got %q", ms[0].Message)
	}
}

// TestPairConsistencyCountableMatchedCountsPass: equal trigger/requires counts
// satisfy countable mode.
func TestPairConsistencyCountableMatchedCountsPass(t *testing.T) {
	src := `package p

func load(ctx, tx interface{}) error {
	_ = projectreadscope.Memo(ctx, "k1", func() error {
		_ = projectMetricUnionSQL
		return nil
	})
	_ = projectreadscope.Memo(ctx, "k2", func() error {
		_ = projectMetricUnionSQL
		return nil
	})
	return nil
}
`
	c := mustChecker(t, countableSameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("countable: two triggers + two companions must pass, got %+v", ms)
	}
}

// TestPairConsistencyDefaultPresenceStillFreeRides pins that omitting
// obligation keeps unit-presence semantics: two triggers + one companion pass.
func TestPairConsistencyDefaultPresenceStillFreeRides(t *testing.T) {
	src := `package p

func load(ctx, tx interface{}) error {
	return projectreadscope.Memo(ctx, "k", func() error {
		_ = projectMetricUnionSQL
		_ = projectMetricUnionSQL
		return nil
	})
}
`
	c := mustChecker(t, sameFuncParams) // no obligation → presence
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("default presence must still allow free-ride, got %+v", ms)
	}
}

// TestPairConsistencyCountableSameFileTwoTriggersOneCompanionFails is the
// same-file arm of countable mode.
func TestPairConsistencyCountableSameFileTwoTriggersOneCompanionFails(t *testing.T) {
	c := mustChecker(t, "trigger: 'open'\nrequires: 'close'\nwhere: same-file\nobligation: countable\n")
	ms, err := c.CheckFile(scan.NewMemFile("res.txt", []byte("open one\nopen two\nclose\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("countable same-file: two open + one close must fire, got %+v", ms)
	}
}

// TestPairConsistencyRejectsUnknownObligation pins config-time rejection.
func TestPairConsistencyRejectsUnknownObligation(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")
	_, err := factory(paramsNode(t, "trigger: t\nrequires: r\nobligation: pairwise-magic\n"))
	if err == nil {
		t.Fatal("unknown obligation accepted")
	}
	if !strings.Contains(err.Error(), "obligation") {
		t.Fatalf("error must name obligation, got %v", err)
	}
}

// TestPairConsistencyCountableRejectsSameDir: countable needs a unit whose
// triggers and companions can be counted together; same-dir accumulates only
// presence today and must refuse the mode rather than silently degrade.
func TestPairConsistencyCountableRejectsSameDir(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")
	_, err := factory(paramsNode(t, "trigger: t\nrequires: r\nwhere: same-dir\nobligation: countable\n"))
	if err == nil {
		t.Fatal("countable+same-dir accepted")
	}
}
