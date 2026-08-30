package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// introspectRules is a two-rule config: one with params, one without —
// the two shapes Params()/ParamsYAML() must distinguish.
const introspectRules = `rules:
  - id: with-params
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'pgxpool\.New'
  - id: without-params
    type: required-pattern
    severity: warn
    scope:
      include: ["cmd/**"]
      exclude: ["cmd/gen/**"]
    params:
      pattern: 'package '
`

func loadIntrospect(t *testing.T) *config.Config {
	t.Helper()
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   "version: 1\n",
		".formwork/rules/main.yaml": introspectRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestRuleIncludeExposesScope pins the read-only accessor explain/rules-for
// need: the include globs as configured, mirroring Exclude().
func TestRuleIncludeExposesScope(t *testing.T) {
	cfg := loadIntrospect(t)
	r := cfg.Rules[0] // with-params (sorted by id: with-params < without-params)
	if r.ID != "with-params" {
		t.Fatalf("expected with-params first, got %s", r.ID)
	}
	inc := r.Include()
	if len(inc) != 1 || inc[0] != "**/*.go" {
		t.Fatalf("Include() = %v, want [**/*.go]", inc)
	}
}

// TestRuleParamsDecodeAndYAML pins both params renderings: structured (for
// JSON output) and re-marshaled YAML (for human output). Both must surface
// the configured param, and a decode is never silently empty for a rule that
// declared params.
func TestRuleParamsDecodeAndYAML(t *testing.T) {
	cfg := loadIntrospect(t)
	r := cfg.Rules[0]
	v, err := r.Params()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("Params() = %T, want map[string]any", v)
	}
	if m["pattern"] != `pgxpool\.New` {
		t.Fatalf("Params()[pattern] = %v", m["pattern"])
	}
	y, err := r.ParamsYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "pattern:") {
		t.Fatalf("ParamsYAML() missing param name: %q", y)
	}
}

// TestRuleParamsAbsent pins the no-params shape: nil value and empty YAML,
// no error — absence is a legitimate state, not a failure. Built via
// config.New, which constructs a Rule with no retained params node (the same
// state a YAML rule without params: reaches).
func TestRuleParamsAbsent(t *testing.T) {
	cfg := loadIntrospect(t)
	r, err := config.New("bare", "probe", cfg.Rules[0].Severity, "", []string{"assets/**"}, nil, nil, cfg.Rules[0].Checker)
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.Params()
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("Params() on a params-free rule = %v, want nil", v)
	}
	y, err := r.ParamsYAML()
	if err != nil {
		t.Fatal(err)
	}
	if y != "" {
		t.Fatalf("ParamsYAML() on a params-free rule = %q, want empty", y)
	}
}
