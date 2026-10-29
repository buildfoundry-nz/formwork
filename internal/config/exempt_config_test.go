package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

func TestLoadPreprocess(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: no-secret\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.go']}\n    preprocess: decomment-go\n    params: {pattern: SECRET}\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Rules[0].Preprocess; got != "decomment-go" {
		t.Fatalf("Preprocess = %q", got)
	}
}

func TestLoadDefaultsAreRaw(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: plain\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**']}\n    params: {pattern: x}\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Rules[0].Preprocess; got != "" {
		t.Fatalf("Preprocess = %q, want raw", got)
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

// There is no except: block. Each of its former keys excused code from a rule
// (a path list, an inline-comment opt-in, an allowlist file), and a rule
// declaring any of them is refused at load, so a corpus carrying one exits 2
// rather than running with the excusal silently ignored.
func TestLoadRefusesTheExceptBlock(t *testing.T) {
	for name, except := range map[string]string{
		"paths":     "{paths: ['vendor/**']}",
		"marker":    "{marker: true}",
		"allowlist": "{allowlist: allowlists/legacy.txt}",
	} {
		t.Run(name, func(t *testing.T) {
			root := writeRepo(t, map[string]string{
				".formwork/formwork.yaml": validRoot,
				".formwork/rules/r.yaml": "rules:\n  - id: bad\n    type: forbidden-pattern\n" +
					"    scope: {include: ['**']}\n    params: {pattern: x}\n" +
					"    except: " + except + "\n",
				".formwork/allowlists/legacy.txt": "src/old.go\n",
			})
			_, err := config.Load(root)
			if err == nil || !strings.Contains(err.Error(), "field except not found") {
				t.Fatalf("except.%s accepted: %v", name, err)
			}
		})
	}
}
