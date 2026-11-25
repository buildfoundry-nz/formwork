package sqltext_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// A prefilter is a pure skip. It must not drop a statement the extractor would
// judge, including one whose table name is assembled rather than written in
// one piece, and it must not parse a file that cannot name that table.
func TestPrefilterKeepsAssembledStatementsAndSkipsTheRest(t *testing.T) {
	const params = "table: 'INSERT\\s+INTO\\s+takeoffqs\\.pages'\n" +
		"forbid:\n  - '[(,\\s]status[,\\s)]'\n" +
		"prefilter: takeoffqs.pages\n"
	c := mustChecker(t, params)

	split := "package db\n\nconst q = \"INSERT INTO \" + \"takeoffqs.\" + \"pages (status) VALUES ($1)\"\n"
	ms := check(t, c, file("split.go", split))
	if len(ms) != 1 || strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("a '+' split of the table must still be judged: %+v", ms)
	}
	plain := c.(rules.Prefiltered).WithoutPrefilter()
	if got := check(t, plain, file("split.go", split)); len(got) != len(ms) {
		t.Fatalf("stripping the prefilter changed the split finding: with %+v without %+v", ms, got)
	}

	sprint := map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.22\n",
		"db/table.go":  "package db\n\nconst pagesTable = \"takeoffqs.pages\"\n",
		"db/insert.go": "package db\n\nimport \"fmt\"\n\nfunc Q() string { return fmt.Sprintf(\"INSERT INTO %s (status) VALUES ($1)\", pagesTable) }\n",
	}
	ms = check(t, c, walked(t, "db/insert.go", sprint))
	if len(ms) != 1 || strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("fmt.Sprintf of a constant table must still be judged: %+v", ms)
	}

	built := "package db\n\nimport \"strings\"\n\nfunc Q() string {\n" +
		"\tvar b strings.Builder\n" +
		"\tb.WriteString(\"INSERT INTO takeoffqs.pages (status) VALUES ($1)\")\n" +
		"\treturn b.String()\n}\n"
	ms = check(t, c, file("build.go", built))
	if len(ms) != 1 {
		t.Fatalf("a builder holding the insert must still be judged: %+v", ms)
	}

	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"store/ins.go": "package store\n\nfunc InsertPages(cols string) string {\n" +
			"\treturn \"INSERT INTO takeoffqs.pages (\" + cols + \") VALUES ($1)\"\n}\n",
		"routes/r.go": "package routes\n\nimport \"example.com/m/store\"\n\n" +
			"func Q() string { return store.InsertPages(\"status\") }\n",
	}
	ms = check(t, c, walked(t, "store/ins.go", files))
	if len(ms) != 1 || strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("the template instance must be judged at the definition: %+v", ms)
	}
	ms = check(t, c, walked(t, "routes/r.go", files))
	if len(ms) != 1 || strings.Contains(ms[0].Message, "could not be read") {
		t.Fatalf("the template call must be judged at the caller: %+v", ms)
	}

	// A file that cannot name the table is not parsed, so a syntax error there
	// is not this rule's. The same error beside the literal still is.
	if _, err := c.CheckFile(file("bad.go", "package db\n\nfunc broken( {\n")); err != nil {
		t.Fatalf("a file that cannot name the table must not be parsed: %v", err)
	}
	if _, err := c.CheckFile(file("bad.go", "package db\n\nconst q = \"takeoffqs.pages\"\nfunc broken( {\n")); err == nil {
		t.Fatal("a broken file that names the table must still be an error")
	}
}
