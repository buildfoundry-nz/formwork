// alsopresent_samefile_test.go — #189.
//
// also_present gates the obligation on the unit carrying some OTHER marker.
// That is coherent for any unit holding a text span, and it was refused for
// every `where:` except same-func — so a package-level const could not be
// gated at all and a downstream lockdown missed the shape it was written for.
//
// same-file's unit is the file, which holds a span. same-dir's unit is a
// DIRECTORY spanning several files, assembled in Finalize, which holds no
// single span — so the refusal is kept there deliberately. Lifting it
// blanket-wise would be the fail-open direction: a gate nobody can evaluate
// must refuse at config time rather than resolve to "not owed".
package pairconsistency_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func checkerErr(t *testing.T, params string) error {
	t.Helper()
	factory, ok := rules.Lookup("pair-consistency")
	if !ok {
		t.Fatal("type \"pair-consistency\" not registered")
	}
	_, err := factory(paramsNode(t, params))
	return err
}

// The gate is absent from the file, so the trigger's obligation is not owed
// even though the companion is missing.
func TestSameFileAlsoPresentGatesTheObligation(t *testing.T) {
	c := mustChecker(t, "trigger: 'REGISTER'\nrequires: 'HANDLER'\nalso_present: 'GATED'\n")
	f := scan.NewMemFile("a.go", []byte("package p\nvar x = REGISTER\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("also_present absent from the file: the obligation must not be owed, got %d: %+v", len(ms), ms)
	}
}

// The gate is present and the companion is not: the obligation is owed and
// reported. Without this, the test above is satisfied by a rule that never
// fires at all.
func TestSameFileAlsoPresentPresentStillReports(t *testing.T) {
	c := mustChecker(t, "trigger: 'REGISTER'\nrequires: 'HANDLER'\nalso_present: 'GATED'\n")
	f := scan.NewMemFile("a.go", []byte("package p\n// GATED\nvar x = REGISTER\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("also_present present, companion missing: want 1 finding, got %d: %+v", len(ms), ms)
	}
}

// same-dir keeps the refusal, and the message must name the mode so the
// operator learns which unit cannot carry the gate.
func TestSameDirStillRefusesAlsoPresent(t *testing.T) {
	err := checkerErr(t, "trigger: 'REGISTER'\nrequires: 'HANDLER'\nalso_present: 'GATED'\nwhere: same-dir\n")
	if err == nil {
		t.Fatal("also_present with where: same-dir must be refused at config time")
	}
	if !strings.Contains(err.Error(), "same-dir") {
		t.Fatalf("the refusal must name the mode it cannot serve, got: %v", err)
	}
}
