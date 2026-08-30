package rules_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type fakePrefiltered struct{ lit string }

func (fakePrefiltered) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (f fakePrefiltered) Prefilter() string                         { return f.lit }
func (fakePrefiltered) WithoutPrefilter() rules.Checker             { return fakePrefiltered{} }

type plainChecker struct{}

func (plainChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func TestPrefilterOf(t *testing.T) {
	if lit, ok := rules.PrefilterOf(fakePrefiltered{"scope_tok"}); !ok || lit != "scope_tok" {
		t.Fatalf("prefiltered: got (%q,%v), want (scope_tok,true)", lit, ok)
	}
	if lit, ok := rules.PrefilterOf(fakePrefiltered{""}); ok || lit != "" {
		t.Fatalf("empty prefilter must report ok=false: got (%q,%v)", lit, ok)
	}
	if _, ok := rules.PrefilterOf(plainChecker{}); ok {
		t.Fatalf("non-Prefiltered checker must report ok=false")
	}
}
