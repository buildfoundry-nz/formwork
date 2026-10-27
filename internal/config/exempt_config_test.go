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
`

func TestLoadPreprocessAndExcept(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml":  exemptRules,
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
	if r.Preprocess != "" || r.Marker || len(r.ExceptPaths) != 0 {
		t.Fatalf("defaults wrong: preprocess=%q marker=%v except.paths=%v", r.Preprocess, r.Marker, r.ExceptPaths)
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

// except.allowlist is not an exemption channel: a list of paths whose
// findings are forgiven is refused at load, so a corpus carrying one exits 2
// rather than running with the list silently ignored. The file is present, so
// the refusal is the key's, not a missing-file error.
func TestLoadRefusesExceptAllowlist(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: bad\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: x}\n" +
			"    except: {allowlist: allowlists/legacy.txt}\n",
		".formwork/allowlists/legacy.txt": "src/old.go\n",
	})
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "field allowlist not found") {
		t.Fatalf("except.allowlist accepted: %v", err)
	}
}
