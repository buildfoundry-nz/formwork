package sqltext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// walked writes files under a temp root and returns rel from its walk, so the
// rule reads constants from the tree a real run hands it.
func walked(t *testing.T, rel string, files map[string]string) *scan.File {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return fs.File(rel)
}

// A statement composed from a constant declared in another file is judged
// whole. Both directions of the old fragment reading are pinned: the WHERE a
// constant supplies satisfies a `require` the literal alone lacked, and the
// clause a constant hides fires a `forbid` the literal alone did not show.
func TestAStatementComposedFromConstantsIsJudgedWhole(t *testing.T) {
	files := map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"db/where.go": "package db\n\nconst byOrg = \" WHERE org_id = $1 AND id = $2\"\nconst everyRow = \" WHERE true\"\n",
		"db/q.go": "package db\n\n" +
			"func Scoped() string { return \"UPDATE takeoffqs.boqs SET status = $3\" + byOrg }\n\n" +
			"func Unscoped() string { return \"DELETE FROM takeoffqs.boqs\" + everyRow }\n",
	}
	c := mustChecker(t, "table: 'takeoffqs\\.boqs'\nrequire:\n  - 'org_id'\nforbid:\n  - 'WHERE true'\n")
	ms := check(t, c, walked(t, "db/q.go", files))
	if len(ms) != 1 || ms[0].Line != 5 {
		t.Fatalf("want the one statement whose constant hides `WHERE true` (line 5), "+
			"and not the one whose constant supplies org_id: %+v", ms)
	}
}

// FAILING CLOSED. A composition carrying SQL that the extractor could not read
// is a finding, whatever table it names: the part it could not read may be the
// table, so the rule cannot select it out, and a statement the rule cannot
// select is one it cannot judge. A composition that is not SQL is not a
// finding, and neither is one made of constants, which the rule reads.
func TestSQLTheRuleCannotReadIsAFinding(t *testing.T) {
	files := map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.22\n",
		"db/cols.go": "package db\n\nconst cols = \"id, org_id\"\n",
		"db/q.go": "package db\n\n" +
			"func Dynamic(tbl string) string { return \"SELECT id FROM \" + tbl + \" WHERE org_id = $1\" }\n\n" +
			"func Message(id string) string { return \"could not reach the pallet service for \" + id }\n\n" +
			"func Constant() string { return \"SELECT \" + cols + \" FROM takeoffqs.boqs WHERE org_id = $1\" }\n",
	}
	c := mustChecker(t, "table: 'takeoffqs\\.boqs'\nrequire:\n  - 'org_id'\n")
	ms := check(t, c, walked(t, "db/q.go", files))
	if len(ms) != 1 || ms[0].Line != 3 || !strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("want the one composition the rule cannot read (line 3), and neither the "+
			"message nor the composition of constants: %+v", ms)
	}
}
