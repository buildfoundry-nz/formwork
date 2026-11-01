package sqltext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
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

// A rule scoped to the package that writes a template judges what the template
// builds, though every call is elsewhere: the definition is read once per
// argument set its callers pass. The instance from the store's own caller is
// judged at the definition line; a rule there sees no unread SQL.
func TestARuleScopedToATemplatesPackageJudgesItsInstances(t *testing.T) {
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"store/frag.go": "package store\n\n" +
			"func DeleteFrom(table string) string {\n\treturn \"DELETE FROM \" + table + \" WHERE true\"\n}\n",
		"routes/r.go": "package routes\n\nimport \"example.com/m/store\"\n\n" +
			"func Q() string { return store.DeleteFrom(\"takeoffqs.boqs\") }\n",
	}
	c := mustChecker(t, "table: 'takeoffqs\\.boqs'\nforbid:\n  - 'WHERE true'\n")
	ms := check(t, c, walked(t, "store/frag.go", files))
	if len(ms) != 1 || ms[0].Line != 4 || strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("want the instance judged at the definition (line 4): %+v", ms)
	}
}

// A rule whose statements are composed at run time on purpose can name that:
// unread SQL then stays out of its findings, and the lint census lists it. What
// the rule can read, it still judges.
func TestUnreadCensusStillJudgesWhatItCanRead(t *testing.T) {
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"db/q.go": "package db\n\n" +
			"func Dynamic(tbl string) string { return \"SELECT id FROM \" + tbl + \" WHERE org_id = $1\" }\n\n" +
			"func Readable() string { return \"SELECT id FROM takeoffqs.boqs\" }\n",
	}
	strict := check(t, mustChecker(t, "table: 'takeoffqs\\.boqs'\nrequire:\n  - 'org_id'\n"), walked(t, "db/q.go", files))
	if len(strict) != 2 {
		t.Fatalf("the default fails on the unread composition and the readable statement: %+v", strict)
	}
	census := check(t, mustChecker(t, "table: 'takeoffqs\\.boqs'\nrequire:\n  - 'org_id'\nunread: census\n"), walked(t, "db/q.go", files))
	if len(census) != 1 || census[0].Line != 5 || strings.Contains(census[0].Message, "could not be read") {
		t.Fatalf("want only the readable statement's finding (line 5): %+v", census)
	}
	explicit := check(t, mustChecker(t, "table: 'takeoffqs\\.boqs'\nrequire:\n  - 'org_id'\nunread: fail\n"), walked(t, "db/q.go", files))
	if len(explicit) != 2 {
		t.Fatalf("unread: fail is the default: %+v", explicit)
	}
}

func TestUnreadRefusesAnyOtherValue(t *testing.T) {
	factory, ok := rules.Lookup("sql/statement-predicate")
	if !ok {
		t.Fatal(`type "sql/statement-predicate" not registered`)
	}
	_, err := factory(paramsNode(t, "table: 't'\nrequire:\n  - 'x'\nunread: ignore\n"))
	if err == nil || !strings.Contains(err.Error(), "params.unread") {
		t.Fatalf("an unknown unread mode must be refused, err = %v", err)
	}
}

// A rule banning hand-written text cannot tell it from the same text folded out
// of the one package meant to mint it. seam_constants names that package: its
// constant reads as a hole, the rest of the statement is still judged, and the
// same text anywhere else still fires.
func TestSeamConstantsReadAsAHoleAndNothingElseDoes(t *testing.T) {
	files := map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.22\n",
		"seam/s.go":   "package seam\n\nconst Approve = \"status = 'approved'\"\n",
		"other/o.go":  "package other\n\nconst Approve = \"status = 'approved'\"\n",
		"db/mint.go":  "package db\n\nimport \"example.com/m/seam\"\n\nconst minted = \"UPDATE t SET \" + seam.Approve + \" WHERE id = $1\"\n",
		"db/alias.go": "package db\n\nimport \"example.com/m/other\"\n\nconst aliased = \"UPDATE t SET \" + other.Approve + \" WHERE id = $1\"\n",
		"db/hand.go":  "package db\n\nconst hand = \"UPDATE t SET status = 'approved' WHERE id = $1\"\n",
		"db/rest.go":  "package db\n\nimport \"example.com/m/seam\"\n\nconst rest = \"UPDATE t SET \" + seam.Approve + \", note = 'x' WHERE id = $1\"\n",
		"seam/t.go":   "package seam\n\nfunc Tuple(a string) string { return a + \" = 'approved'\" }\n",
		"other/t.go":  "package other\n\nfunc Tuple(a string) string { return a + \" = 'approved'\" }\n",
		"db/tmpl.go":  "package db\n\nimport \"example.com/m/seam\"\n\nfunc tmpl() string { return \"UPDATE t SET \" + seam.Tuple(\"status\") + \" WHERE id = $1\" }\n",
		"db/tmpl2.go": "package db\n\nimport \"example.com/m/other\"\n\nfunc tmpl2() string { return \"UPDATE t SET \" + other.Tuple(\"status\") + \" WHERE id = $1\" }\n",
	}
	const params = "table: 'UPDATE t'\nforbid:\n  - \"status = 'approved'\"\n  - \"note = 'x'\"\n"
	seamed := params + "seam_constants: [seam]\n"
	for _, tc := range []struct {
		file          string
		plain, withIt int
	}{
		{"db/mint.go", 1, 0},
		{"db/alias.go", 1, 1},
		{"db/hand.go", 1, 1},
		{"db/rest.go", 1, 1},
		{"db/tmpl.go", 1, 0},
		{"db/tmpl2.go", 1, 1},
	} {
		if got := check(t, mustChecker(t, params), walked(t, tc.file, files)); len(got) != tc.plain {
			t.Errorf("%s without a seam: want %d finding(s), got %+v", tc.file, tc.plain, got)
		}
		if got := check(t, mustChecker(t, seamed), walked(t, tc.file, files)); len(got) != tc.withIt {
			t.Errorf("%s with the seam named: want %d finding(s), got %+v", tc.file, tc.withIt, got)
		}
	}
}

func TestSeamConstantsRefuseAPathThatIsNotACleanRelativeDirectory(t *testing.T) {
	factory, ok := rules.Lookup("sql/statement-predicate")
	if !ok {
		t.Fatal(`type "sql/statement-predicate" not registered`)
	}
	for _, dir := range []string{"''", "'.'", "/abs", "'../up'", "'a/../b'", "'a/'"} {
		_, err := factory(paramsNode(t, "table: 't'\nrequire:\n  - 'x'\nseam_constants: ["+dir+"]\n"))
		if err == nil || !strings.Contains(err.Error(), "seam_constants") {
			t.Errorf("seam_constants [%s] must be refused, err = %v", dir, err)
		}
	}
}
