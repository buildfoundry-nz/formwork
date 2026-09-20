package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"gopkg.in/yaml.v3"
)

// rootWithFile builds a tree holding one real file, because the findings
// contract resolves a location against the tree under evaluation.
func rootWithFile(t *testing.T, rel string) string {
	t.Helper()
	root := t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// findingsAt builds the rule and finalizes it against root, failing the test
// on an evaluation error: these arms are about WHAT is reported, so a rule
// that could not run at all is never the answer.
func findingsAt(t *testing.T, params, root string) []rules.Match {
	t.Helper()
	got, err := finalizeAt(t, build(t, params), root)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return got
}

// A rule that DECLARES the contract gets located findings, so the journal
// recording them can say which file each was about — the whole point of
// TakeoffQS #18214.
func TestCommandOutput_DeclaredContractYieldsLocatedFindings(t *testing.T) {
	root := rootWithFile(t, "pkg/thing.go")
	got := findingsAt(t, `cmd: [sh, -c, "echo 'pkg/thing.go:12: holds a frozen symbol'; exit 1"]
output: findings-v1`, root)

	var located []rules.Match
	for _, m := range got {
		if m.Path != "" {
			located = append(located, m)
		}
	}
	if len(located) != 1 {
		t.Fatalf("got %d located finding(s), want 1:\n%+v", len(located), got)
	}
	if located[0].Path != "pkg/thing.go" || located[0].Line != 12 {
		t.Fatalf("located = %+v, want pkg/thing.go line 12", located[0])
	}
	if located[0].Message != "holds a frozen symbol" {
		t.Fatalf("message = %q, want the text after the location", located[0].Message)
	}
}

// The exit fact must survive. A located finding says what is wrong with a
// file; it does not say the detector exited 1 where 0 was wanted, and a
// contract that loses the verdict to gain a path has traded down.
func TestCommandOutput_ExitVerdictIsStillReported(t *testing.T) {
	root := rootWithFile(t, "pkg/thing.go")
	got := findingsAt(t, `cmd: [sh, -c, "echo 'pkg/thing.go:12: holds a frozen symbol'; exit 1"]
output: findings-v1`, root)

	var pathless string
	for _, m := range got {
		if m.Path == "" {
			pathless = m.Message
		}
	}
	if pathless == "" {
		t.Fatalf("no pathless finding carrying the exit verdict:\n%+v", got)
	}
	if !strings.Contains(pathless, "exited 1") || !strings.Contains(pathless, "want 0") {
		t.Fatalf("pathless finding = %q, want it to name the exit and the expectation", pathless)
	}
}

// A rule that declares NOTHING behaves exactly as it does today: one message,
// no path. Every corpus predating this contract must load and report
// unchanged, which is what makes adoption per-rule rather than a migration.
func TestCommandOutput_UndeclaredIsUnchanged(t *testing.T) {
	root := rootWithFile(t, "pkg/thing.go")
	got := findingsAt(t, `cmd: [sh, -c, "echo 'pkg/thing.go:12: holds a frozen symbol'; exit 1"]`, root)

	if len(got) != 1 {
		t.Fatalf("got %d finding(s), want exactly 1 for an undeclared rule:\n%+v", len(got), got)
	}
	if got[0].Path != "" || got[0].Line != 0 {
		t.Fatalf("finding = %+v, want no location — an undeclared rule must not start parsing", got[0])
	}
}

// An unknown output format is refused at load. Accepting it silently would
// hand a rule author a declaration that does nothing — the same class of
// defect as an env assignment the runner quietly drops.
func TestCommandOutput_UnknownFormatIsRefusedAtLoad(t *testing.T) {
	f, ok := rules.Lookup("command")
	if !ok {
		t.Fatal("command type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("cmd: [true]\noutput: findings-v2"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("an unknown output format must be refused at load, not ignored")
	} else if !strings.Contains(err.Error(), "output") {
		t.Fatalf("refusal must name the field; got %v", err)
	}
}
