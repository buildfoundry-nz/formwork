package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

const exemptRules = `rules:
  - id: no-secret
    type: forbidden-pattern
    scope:
      include: ["**/*.go"]
    preprocess: decomment-go
    params:
      pattern: 'SECRET'
    except:
      paths: ["vendor/**"]
      marker: true
      allowlist: allowlists/legacy.txt
`

func TestLoadPreprocessAndExcept(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":         validRoot,
		".formwork/rules/r.yaml":          exemptRules,
		".formwork/allowlists/legacy.txt": "# grandfathered\n\nsrc/old.go\ndocs/gone.md\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]
	if r.Preprocess != "decomment-go" {
		t.Fatalf("Preprocess = %q", r.Preprocess)
	}
	if !r.Marker {
		t.Fatal("Marker not set")
	}
	if got := r.ExceptPaths; len(got) != 1 || got[0] != "vendor/**" {
		t.Fatalf("ExceptPaths = %v", got)
	}
	al := r.Allowlist
	if al == nil || al.File != "allowlists/legacy.txt" {
		t.Fatalf("Allowlist = %+v", al)
	}
	want := []config.AllowlistEntry{
		{Path: "src/old.go", Line: 3},
		{Path: "docs/gone.md", Line: 4},
	}
	if len(al.Entries) != len(want) {
		t.Fatalf("Entries = %+v", al.Entries)
	}
	for i := range want {
		if al.Entries[i] != want[i] {
			t.Fatalf("Entries[%d] = %+v, want %+v", i, al.Entries[i], want[i])
		}
	}
}

func TestLoadDefaultsAreRawAndUnexempted(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: plain\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: x}\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]
	if r.Preprocess != "" || r.Marker || r.Allowlist != nil {
		t.Fatalf("defaults wrong: preprocess=%q marker=%v allowlist=%+v", r.Preprocess, r.Marker, r.Allowlist)
	}
}

func TestLoadUnknownPreprocessorErrors(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: bad\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    preprocess: no-such\n    params: {pattern: x}\n",
	})
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), `unknown preprocessor "no-such"`) {
		t.Fatalf("unknown preprocessor accepted: %v", err)
	}
}

func TestLoadMissingAllowlistErrors(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: bad\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: x}\n" +
			"    except: {allowlist: allowlists/nope.txt}\n",
	})
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "allowlists/nope.txt") {
		t.Fatalf("missing allowlist accepted: %v", err)
	}
}
