package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

const subjectRules = "rules:\n" +
	"  - id: no-widget-prod\n    type: forbidden-pattern\n    scope: {include: ['**/*.go'], subject: production}\n    params: {pattern: WIDGET}\n" +
	"  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n"

// A production subject reads no file the repository's tests: names; every
// other rule reads tests.
func TestProductionSubjectLeavesOutTestCodeAlone(t *testing.T) {
	cfg, err := config.Load(writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   "version: 1\ntests: ['**/*_test.go']\n",
		".formwork/rules/main.yaml": subjectRules,
	}))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*config.Rule{}
	for _, r := range cfg.Rules {
		byID[r.ID] = r
	}
	prod, all := byID["no-widget-prod"], byID["no-widget"]
	if !prod.SubjectIsProduction() || all.SubjectIsProduction() {
		t.Fatal("scope.subject: production must set the subject on that rule alone")
	}
	for _, tc := range []struct {
		path      string
		prod, all bool
	}{
		{"a/a.go", true, true},
		{"a/a_test.go", false, true},
	} {
		if got := prod.Applies(tc.path); got != tc.prod {
			t.Errorf("production rule Applies(%s) = %v, want %v", tc.path, got, tc.prod)
		}
		if got := all.Applies(tc.path); got != tc.all {
			t.Errorf("default rule Applies(%s) = %v, want %v", tc.path, got, tc.all)
		}
	}
}

func TestLoadRejectsBadSubject(t *testing.T) {
	for _, tc := range []struct{ name, yaml, rules, want string }{
		{"production without tests:", "version: 1\n", subjectRules, "needs formwork.yaml `tests:`"},
		{"unknown subject", "version: 1\ntests: ['**/*_test.go']\n",
			"rules:\n  - id: r\n    type: forbidden-pattern\n    scope: {include: ['**/*.go'], subject: tests}\n    params: {pattern: X}\n", "unknown subject"},
		{"empty test glob", "version: 1\ntests: ['']\n", subjectRules, "glob is empty"},
		{"duplicate test glob", "version: 1\ntests: ['**/*_test.go', '**/*_test.go']\n", subjectRules, "declared twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(writeRepo(t, map[string]string{
				".formwork/formwork.yaml":   tc.yaml,
				".formwork/rules/main.yaml": tc.rules,
			}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
