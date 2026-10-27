package fixturetest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vacuity"
)

// A vacuity probe that cannot run must make Results return an error (exit 2),
// never be read as a judged or passing arm. A real rule cannot make the probe
// fail from here, so the judge is injected.
func TestJudgeErrorFailsResults(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".formwork/formwork.yaml":            "version: 1\n",
		".formwork/rules/r.yaml":             "rules:\n  - id: ban\n    type: forbidden-pattern\n    scope:\n      include: [\"**/*.go\"]\n    params:\n      pattern: 'BANNED'\n",
		".formwork/fixtures/ban/pass-1/a.go": "package a\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("probe exploded")
	old := judgeVacuity
	judgeVacuity = func(*config.Rule, *scan.FileSet) (vacuity.Verdict, error) { return vacuity.Verdict{}, boom }
	t.Cleanup(func() { judgeVacuity = old })

	_, err = Results(cfg, []string{"ban"}, root, 1)
	if !errors.Is(err, boom) {
		t.Fatalf("Results err = %v, want the probe error", err)
	}
}

// Only a pass arm that otherwise passed is probed. A fire arm's Vacuity field
// is blanked by newArmResult regardless, so the output cannot show a wasted
// probe — the call count can.
func TestOnlyBehavingPassArmsAreProbed(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		".formwork/formwork.yaml":            "version: 1\n",
		".formwork/rules/r.yaml":             "rules:\n  - id: ban\n    type: forbidden-pattern\n    scope:\n      include: [\"**/*.go\"]\n    params:\n      pattern: 'BANNED'\n",
		".formwork/fixtures/ban/fire-1/a.go": "package a\n\nvar x = BANNED // want: ban\n",
		".formwork/fixtures/ban/pass-1/a.go": "package a\n",
		".formwork/fixtures/ban/pass-2/a.go": "package a\n\nvar y = BANNED\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	old := judgeVacuity
	judgeVacuity = func(r *config.Rule, fs *scan.FileSet) (vacuity.Verdict, error) {
		calls++
		return old(r, fs)
	}
	t.Cleanup(func() { judgeVacuity = old })

	if _, err := Results(cfg, []string{"ban"}, root, 1); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("judge called %d times, want 1 (pass-1 only: fire-1 is a fire arm, pass-2 already fails)", calls)
	}
}
