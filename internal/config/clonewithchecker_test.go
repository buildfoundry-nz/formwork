package config_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type fakeChecker struct{ id string }

func (fakeChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func TestCloneWithChecker(t *testing.T) {
	a, b := fakeChecker{"a"}, fakeChecker{"b"}
	r, err := config.New("r1", "forbidden-pattern", finding.SeverityError, "cure",
		[]string{"**/*.go"}, []string{"**/*_test.go"}, nil, a)
	if err != nil {
		t.Fatal(err)
	}

	clone := r.CloneWithChecker(b)

	if clone.Checker != rules.Checker(b) {
		t.Fatalf("clone.Checker not replaced")
	}
	if r.Checker != rules.Checker(a) {
		t.Fatalf("original mutated: %v", r.Checker)
	}
	if !clone.Applies("main.go") || clone.Applies("main_test.go") || clone.Applies("x.txt") {
		t.Fatalf("scope not preserved on clone")
	}
	if clone.ID != "r1" {
		t.Fatalf("clone.ID = %q", clone.ID)
	}
}
