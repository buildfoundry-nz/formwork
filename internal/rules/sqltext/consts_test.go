package sqltext_test

import (
	"os"
	"path/filepath"
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
