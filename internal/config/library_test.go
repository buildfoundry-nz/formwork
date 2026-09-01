package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	_ "github.com/buildfoundry-nz/formwork/internal/preprocess"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
)

func TestLoadLibraryMergesGenericPack(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nlibrary: [generic]\n",
		".formwork/rules/local.yaml": `rules:
  - id: local-only
    type: forbidden-pattern
    scope: {include: ["**/*.go"]}
    params: {pattern: "FIXME"}
`,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range cfg.Rules {
		got[r.ID] = r.Library
	}
	if got["dart-no-test-skip"] != "generic" {
		t.Fatalf("dart-no-test-skip library = %q, want generic", got["dart-no-test-skip"])
	}
	if got["go-weak-types-any"] != "generic" {
		t.Fatalf("go-weak-types-any library = %q, want generic", got["go-weak-types-any"])
	}
	if got["local-only"] != "" {
		t.Fatalf("local-only library = %q, want empty", got["local-only"])
	}
}

func TestLoadLibraryLocalWinsByID(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nlibrary: [generic]\n",
		".formwork/rules/override.yaml": `rules:
  - id: dart-no-test-skip
    type: forbidden-pattern
    scope: {include: ["spec/**/*_test.dart"]}
    params: {pattern: '(^|[^A-Za-z0-9_.])skip:'}
    cure: rebound locally
`,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var found *config.Rule
	for _, r := range cfg.Rules {
		if r.ID == "dart-no-test-skip" {
			found = r
			break
		}
	}
	if found == nil {
		t.Fatal("dart-no-test-skip missing after override")
	}
	if found.Library != "" {
		t.Fatalf("Library = %q, want empty (local wins)", found.Library)
	}
	if found.Cure != "rebound locally" {
		t.Fatalf("Cure = %q, want the local restatement", found.Cure)
	}
	if !found.Applies("spec/x_test.dart") {
		t.Fatal("local include spec/**/*_test.dart did not apply")
	}
	if found.Applies("test/x_test.dart") {
		t.Fatal("library include leaked through a local override")
	}
}

func TestLoadUnknownLibraryNamesTheRoster(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nlibrary: [not-a-pack]\n",
	})
	_, err := config.Load(root)
	if err == nil {
		t.Fatal("expected unknown library to fail")
	}
	if !strings.Contains(err.Error(), "unknown library") {
		t.Fatalf("err = %v, want unknown library", err)
	}
	if !strings.Contains(err.Error(), "generic") {
		t.Fatalf("err = %v, want it to list generic", err)
	}
}

func TestLoadDuplicateLibraryPackIsRefused(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nlibrary: [generic, generic]\n",
	})
	_, err := config.Load(root)
	if err == nil {
		t.Fatal("expected duplicate pack to fail")
	}
	if !strings.Contains(err.Error(), "duplicate pack") {
		t.Fatalf("err = %v, want duplicate pack", err)
	}
}
