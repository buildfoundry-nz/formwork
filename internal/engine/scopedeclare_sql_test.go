package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// sqlHomeTree writes a repo whose SQL rule has a home in q/q.go, and whose other
// statement there splices another package's constant.
func sqlHomeTree(t *testing.T, sibling string) ([]*config.Rule, *scan.FileSet) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: reads-widgets-scoped\n    type: sql/statement-predicate\n    severity: error\n" +
			"    scope:\n      include: ['**/*.go']\n      not_declaring: [{marker: 'func Home(', holders: 1}]\n" +
			"    params:\n      table: widgets\n      require: ['org_id']\n    cure: scope it\n",
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"status/s.go": "package status\n\nconst Excluded = `('rejected')`\n",
		"q/q.go": "package q\n\nimport \"example.com/m/status\"\n\ntype DB struct{}\n\nfunc (DB) Query(string) {}\n\n" +
			"func Home(d DB) {\n\td.Query(`SELECT 1 FROM widgets`)\n}\n\n" + sibling + "\n\nvar _ = status.Excluded\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Rules, fset
}

// A home is masked with the file's imports, which are text a pattern rule would
// otherwise match. A SQL rule reads Go, and needs them: masked, another package's
// constant in the file's other statements reads as unread.
func TestRunNotDeclaringKeepsImportsForASQLRule(t *testing.T) {
	rs, fset := sqlHomeTree(t, "func Other(d DB) {\n\td.Query(`SELECT 1 FROM widgets WHERE org_id = $1 AND s NOT IN `+status.Excluded)\n}")
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %+v, want none: the sibling's constant is readable and the home is masked", got)
	}
}

// The control: with the constant read, the rule judges the statement it spells.
func TestRunNotDeclaringStillJudgesTheSiblingStatement(t *testing.T) {
	rs, fset := sqlHomeTree(t, "func Other(d DB) {\n\td.Query(`SELECT 1 FROM widgets WHERE s NOT IN `+status.Excluded)\n}")
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Message, "org_id") {
		t.Fatalf("findings = %+v, want the sibling's missing org_id and nothing else", got)
	}
}

// A pattern rule still has the imports masked: they cannot sit inside a home.
func TestRunNotDeclaringMasksImportsForAPatternRule(t *testing.T) {
	rs, fset := declTree(t, "      not_declaring: [{marker: 'func Owner(', holders: 1}]\n", map[string]string{
		"owner.go": "package p\n\nimport \"example.com/WIDGET\"\n\nfunc Owner() { WIDGET.Do() }\n",
	})
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %+v, want none: the import belongs to the home", got)
	}
}
