package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// scope.min_files is the arming end of #160's disclosure (#23). An empty scope
// stays a pass by default because it is legitimate — a rule scoped to a path the
// repo has not created yet is not a defect — so the floor is opt-in per rule,
// copying set-relation's min_count: permissive default, validated at load.
//
// These pin the LOAD half: what the key decodes to, and what it refuses.

func minFilesRepo(t *testing.T, scope string) string {
	t.Helper()
	return writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: no-widget\n    type: forbidden-pattern\n" +
			"    scope: " + scope + "\n    params: {pattern: WIDGET}\n",
	})
}

func TestLoadScopeMinFilesDefaultsToZero(t *testing.T) {
	cfg, err := config.Load(minFilesRepo(t, "{include: ['**/*.go']}"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Rules[0].MinFiles(); got != 0 {
		t.Fatalf("MinFiles() = %d, want 0 — an undeclared floor must leave the rule exactly as it was", got)
	}
}

func TestLoadScopeMinFilesIsCarried(t *testing.T) {
	cfg, err := config.Load(minFilesRepo(t, "{include: ['**/*.go'], min_files: 7}"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Rules[0].MinFiles(); got != 7 {
		t.Fatalf("MinFiles() = %d, want 7", got)
	}
}

// Strict decoding, the substance of adding config surface: a floor the operator
// cannot have meant must be exit 2 at load, never a silently-disarmed rule. A
// negative floor is satisfiable by every possible file count, so accepting it
// would ship a rule that LOOKS armed and can never fire — the repo's signature
// defect wearing a config key.
func TestLoadRejectsBadScopeMinFiles(t *testing.T) {
	cases := []struct{ name, scope, wantErr string }{
		{"negative", "{include: ['**/*.go'], min_files: -1}", "min_files"},
		{"fractional float", "{include: ['**/*.go'], min_files: 1.5}", "min_files"},
		// A float that happens to be whole is the same defect wearing a
		// convincing disguise: decoded into an int it would read as 2 silently.
		{"whole float", "{include: ['**/*.go'], min_files: 2.0}", "min_files"},
		{"string", "{include: ['**/*.go'], min_files: many}", "min_files"},
		{"boolean", "{include: ['**/*.go'], min_files: true}", "min_files"},
		{"sequence", "{include: ['**/*.go'], min_files: [1]}", "min_files"},
		{"explicit null", "{include: ['**/*.go'], min_files: }", "min_files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.Load(minFilesRepo(t, c.scope))
			if err == nil {
				t.Fatalf("scope %s loaded without error — an unusable floor must be exit 2", c.scope)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to name %q", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), "no-widget") {
				t.Fatalf("err = %v, want it to name the offending rule", err)
			}
		})
	}
}

// The programmatic constructor (engine/report tests build rules without YAML)
// gets the same validation, because the field is unexported: SetMinFiles is the
// only way in, and it refuses the value compile refuses. A caller that could
// bypass the check would be a second, unvalidated door onto the same invariant.
func TestSetMinFilesRefusesNegative(t *testing.T) {
	r, err := config.New("r", "forbidden-pattern", "error", "", []string{"**/*.go"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetMinFiles(-1); err == nil {
		t.Fatal("SetMinFiles(-1) returned nil — a negative floor can never fire")
	}
	if got := r.MinFiles(); got != 0 {
		t.Fatalf("MinFiles() = %d after a refused set, want 0", got)
	}
	if err := r.SetMinFiles(3); err != nil {
		t.Fatalf("SetMinFiles(3): %v", err)
	}
	if got := r.MinFiles(); got != 3 {
		t.Fatalf("MinFiles() = %d, want 3", got)
	}
}
