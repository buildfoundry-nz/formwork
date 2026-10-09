package config

import (
	"strings"
	"testing"

	_ "github.com/buildfoundry-nz/formwork/internal/preprocess"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
)

// A library rule cannot carry a credential: its holder count is a fact about
// the consuming repo, which a pack cannot know.
func TestLibraryRuleRefusesACredential(t *testing.T) {
	data := []byte(`rules:
  - id: lib-owned
    type: forbidden-pattern
    scope: {include: ["**/*.go"]}
    params:
      pattern: x
      require_absent: [{credential: '^// owner', holders: 1}]
    cure: c
`)
	err := loadRuleFile(&Config{}, map[string]string{}, map[string]int{}, "library:generic/lib.yaml", data, t.TempDir(), "generic")
	if err == nil || !strings.Contains(err.Error(), "a library rule cannot carry a credential") {
		t.Fatalf("want the library refusal, got %v", err)
	}
	// The same rule declared locally loads.
	if err := loadRuleFile(&Config{}, map[string]string{}, map[string]int{}, "local.yaml", data, t.TempDir(), ""); err != nil {
		t.Fatalf("a local rule may carry a credential: %v", err)
	}
}
