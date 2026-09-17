package command_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"gopkg.in/yaml.v3"
)

// A command rule declares the class it belongs to (#22). Undeclared stays
// heavy — the fail-closed answer that keeps every existing corpus loading and
// running exactly where it ran before.
func TestCommandCost_DeclaredClassIsReported(t *testing.T) {
	cases := map[string]rules.Cost{
		"cmd: [true]":              rules.CostHeavy,
		"cmd: [true]\ncost: range": rules.CostRange,
		"cmd: [true]\ncost: tree":  rules.CostTree,
		"cmd: [true]\ncost: heavy": rules.CostHeavy,
	}
	for params, want := range cases {
		if got := rules.CostOf(build(t, params)); got != want {
			t.Errorf("%q: CostOf = %s, want %s", params, got, want)
		}
	}
}

// A command execs, so it can never be fast; and an unknown class is a config
// error, never a silent heavy.
func TestCommandCost_RefusesFastAndUnknown(t *testing.T) {
	f, ok := rules.Lookup("command")
	if !ok {
		t.Fatal("command type not registered")
	}
	for _, params := range []string{"cmd: [true]\ncost: fast", "cmd: [true]\ncost: cheap"} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
			t.Fatal(err)
		}
		_, err := f(doc.Content[0])
		if err == nil || !strings.Contains(err.Error(), "cost") {
			t.Errorf("%q must be refused naming cost; got %v", params, err)
		}
	}
}
