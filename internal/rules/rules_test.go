package rules_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type nopChecker struct{}

func (nopChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func TestRegisterLookupRoundtrip(t *testing.T) {
	rules.Register("test-roundtrip", func(*yaml.Node) (rules.Checker, error) {
		return nopChecker{}, nil
	})
	f, ok := rules.Lookup("test-roundtrip")
	if !ok || f == nil {
		t.Fatal("registered factory not found")
	}
	if _, ok := rules.Lookup("never-registered"); ok {
		t.Fatal("lookup of unknown type succeeded")
	}
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	rules.Register("test-duplicate", func(*yaml.Node) (rules.Checker, error) {
		return nopChecker{}, nil
	})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	rules.Register("test-duplicate", func(*yaml.Node) (rules.Checker, error) {
		return nopChecker{}, nil
	})
}

func TestDecodeParamsStrict(t *testing.T) {
	type params struct {
		Pattern string `yaml:"pattern"`
	}
	var p params
	if err := rules.DecodeParams(paramsNode(t, "pattern: abc\n"), &p); err != nil {
		t.Fatalf("valid params rejected: %v", err)
	}
	if p.Pattern != "abc" {
		t.Fatalf("pattern = %q", p.Pattern)
	}
	err := rules.DecodeParams(paramsNode(t, "pattern: abc\nbogus: 1\n"), &p)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown field not rejected: %v", err)
	}
	if err := rules.DecodeParams(nil, &p); err != nil {
		t.Fatalf("nil params node rejected: %v", err)
	}
}
