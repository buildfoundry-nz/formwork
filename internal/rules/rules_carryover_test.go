package rules_test

import (
	"sort"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"gopkg.in/yaml.v3"
)

func TestDecodeParamsZeroKindNodeLeavesOutUntouched(t *testing.T) {
	type params struct {
		Pattern string `yaml:"pattern"`
	}
	p := params{Pattern: "preset"}
	if err := rules.DecodeParams(&yaml.Node{}, &p); err != nil {
		t.Fatal(err)
	}
	if p.Pattern != "preset" {
		t.Fatalf("zero-kind node mutated out: %q", p.Pattern)
	}
}

func TestTypeNamesSortedAndContainsRegistered(t *testing.T) {
	rules.Register("test-typenames-probe", func(*yaml.Node) (rules.Checker, error) {
		return nopChecker{}, nil
	})
	names := rules.TypeNames()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("TypeNames not sorted: %v", names)
	}
	found := false
	for _, n := range names {
		if n == "test-typenames-probe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered type missing from TypeNames: %v", names)
	}
}
