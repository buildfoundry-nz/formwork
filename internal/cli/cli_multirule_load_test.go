package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestTestManyRulesLoadsCorpusOnce pins that N selected rules share one corpus
// load rather than one load per rule.
func TestTestManyRulesLoadsCorpusOnce(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".formwork/formwork.yaml", "version: 1\n")
	write(".formwork/rules/r.yaml",
		"rules:\n"+
			"  - id: r1\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: alpha}\n"+
			"  - id: r2\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: beta}\n")
	write(".formwork/fixtures/r1/fire-1/f.txt", "alpha want: r1\n")
	write(".formwork/fixtures/r1/pass-1/f.txt", "clean\n")
	write(".formwork/fixtures/r2/fire-1/f.txt", "beta want: r2\n")
	write(".formwork/fixtures/r2/pass-1/f.txt", "clean\n")

	loads := 0
	loadHook = func() { loads++ }
	t.Cleanup(func() { loadHook = nil })

	if code := runTest([]string{"-C", root, "--rule", "r1", "--rule", "r2"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if loads != 1 {
		t.Errorf("corpus loaded %d times for 2 rules, want 1", loads)
	}
}
