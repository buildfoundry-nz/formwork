package sqlextract_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// parserFile is package gen importing fmt, os and the given parser import lines.
func parserFile(imports, body string) string {
	return "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n" + imports + ")\n\n" +
		"var (\n\t_ = fmt.Sprint\n\t_ = os.Args\n)\n\n" + body
}

const (
	plainPgQuery   = "\t\"github.com/pganalyze/pg_query_go/v6\"\n"
	aliasedPgQuery = "\tpg \"github.com/pganalyze/pg_query_go/v6\"\n"
	plainWasi      = "\t\"github.com/wasilibs/go-pgquery\"\n"
	aliasedWasi    = "\tpgquery \"github.com/wasilibs/go-pgquery\"\n"
	parserDrivers  = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nfunc (DB) Query(string) error { return nil }\n\n" +
		"func (DB) QueryRow(string) error { return nil }\n\nvar pool DB\n\nvar tx DB\n\nvar db DB\n\n"
	// plExprShape is TakeoffQS's find-zero-reader-columns PL/pgSQL read: a local
	// written on several branches and handed only to the parser.
	plExprShape = "type plExpr struct {\n\tmode  int\n\tquery string\n}\n\n" +
		"func read(e plExpr) error {\n\tvar sql string\n\tswitch {\n\tcase e.mode == 0:\n\t\tsql = e.query\n" +
		"\tcase e.mode == 2:\n\t\tsql = \"SELECT \" + e.query\n\tdefault:\n\t\treturn nil\n\t}\n" +
		"\ttree, err := PKG.Parse(sql)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"PL/pgSQL %q: %w\", e.query, err)\n\t}\n" +
		"\tfmt.Print(tree)\n\treturn nil\n}\n\nfunc main() { read(plExpr{mode: 2, query: os.Args[1]}) }\n"
)

func withPkg(shape, pkg string) string { return strings.ReplaceAll(shape, "PKG", pkg) }

// A parse, a scan or a fingerprint returns a tree, tokens or a hash: text handed only
// to the PostgreSQL parser reaches no driver. The parser is known by its import path,
// so it is the parser under its plain name and under any alias.
func TestTextOnlyTheSQLParserReadsIsNotASite(t *testing.T) {
	for name, src := range map[string]string{
		"pg_query_go under its plain name": parserFile(plainPgQuery, withPkg(plExprShape, "pg_query")),
		"pg_query_go under an alias":       parserFile(aliasedPgQuery, withPkg(plExprShape, "pg")),
		"go-pgquery under its plain name":  parserFile(plainWasi, withPkg(plExprShape, "pg_query")),
		"go-pgquery under an alias":        parserFile(aliasedWasi, withPkg(plExprShape, "pgquery")),
		"parsed where it is built": parserFile(aliasedWasi,
			"func main() { pgquery.Parse(\"SELECT \" + os.Args[1]) }\n"),
		"scanned": parserFile(aliasedWasi,
			"func main() {\n\tscan, err := pgquery.Scan(\"SELECT \" + os.Args[1])\n\tfmt.Print(scan, err)\n}\n"),
		"fingerprinted": parserFile(plainPgQuery,
			"func main() {\n\tfp, _ := pg_query.Fingerprint(\"SELECT \" + os.Args[1])\n\tfmt.Print(fp)\n}\n"),
		"a PL/pgSQL body to JSON": parserFile(aliasedWasi,
			"func main() {\n\tjs, _ := pgquery.ParsePlPgSqlToJSON(\"CREATE FUNCTION f() RETURNS void AS $$ BEGIN \" + os.Args[1] + \" END $$ LANGUAGE plpgsql\")\n\tfmt.Print(js)\n}\n"),
		"normalized then printed": parserFile(aliasedPgQuery,
			"func main() {\n\tn, err := pg.Normalize(\"SELECT \" + os.Args[1])\n\tif err != nil {\n\t\tpanic(err)\n\t}\n\tfmt.Print(n)\n}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": src}), "gen/g.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// The parser ends only what it alone reads. Text a driver also reads, text a parser
// hands back as statements, and a call that merely spells the parser's name all keep
// it a site.
func TestTextTheSQLParserAndADriverReadStaysASite(t *testing.T) {
	for name, src := range map[string]string{
		"run by pool.Exec":   parserFile(aliasedWasi, parserDrivers+"func main() { pool.Exec(\"DELETE FROM \" + os.Args[1]) }\n"),
		"run by tx.Query":    parserFile(aliasedWasi, parserDrivers+"func main() { tx.Query(\"DELETE FROM \" + os.Args[1]) }\n"),
		"run by db.QueryRow": parserFile(aliasedWasi, parserDrivers+"func main() { db.QueryRow(\"DELETE FROM \" + os.Args[1]) }\n"),
		"parsed and run": parserFile(aliasedWasi, parserDrivers+
			"func main() {\n\tq := \"DELETE FROM \" + os.Args[1]\n\tif _, err := pgquery.Parse(q); err != nil {\n\t\tpanic(err)\n\t}\n\tpool.Exec(q)\n}\n"),
		"parsed under its plain name and run": parserFile(plainPgQuery, parserDrivers+
			"func main() {\n\tq := \"DELETE FROM \" + os.Args[1]\n\tpg_query.Parse(q)\n\ttx.Query(q)\n}\n"),
		"parsed deparsed and run": parserFile(aliasedWasi, parserDrivers+
			"func main() {\n\ttree, _ := pgquery.Parse(\"DELETE FROM \" + os.Args[1])\n\ttext, _ := pgquery.Deparse(tree)\n\tpool.Exec(text)\n}\n"),
		"parsed returned deparsed and run": parserFile(aliasedPgQuery, parserDrivers+
			"func parse(q string) *pg.ParseResult {\n\ttree, _ := pg.Parse(q)\n\treturn tree\n}\n\n"+
			"func main() {\n\ttext, _ := pg.Deparse(parse(\"DELETE FROM \" + os.Args[1]))\n\tpool.Exec(text)\n}\n"),
		"normalized then run": parserFile(aliasedPgQuery, parserDrivers+
			"func main() {\n\tn, _ := pg.Normalize(\"DELETE FROM \" + os.Args[1])\n\tdb.QueryRow(n)\n}\n"),
		"split then run": parserFile(plainWasi, parserDrivers+
			"func main() {\n\tstmts, _ := pg_query.SplitWithParser(\"DELETE FROM \"+os.Args[1], true)\n\tfor _, s := range stmts {\n\t\tpool.Exec(s)\n\t}\n}\n"),
		"another package under the parser's alias": parserFile("\tpgquery \"example.net/runner\"\n",
			"func main() { pgquery.Parse(\"DELETE FROM \" + os.Args[1]) }\n"),
		"another package under the parser's plain name": parserFile("\tpg_query \"example.net/runner\"\n",
			"func main() { pg_query.Parse(\"DELETE FROM \" + os.Args[1]) }\n"),
		"a local shadowing the parser": parserFile(aliasedWasi, parserDrivers+
			"type runner struct{}\n\nfunc (runner) Parse(q string) { pool.Exec(q) }\n\n"+
			"func main() {\n\tpgquery := runner{}\n\tpgquery.Parse(\"DELETE FROM \" + os.Args[1])\n}\n"),
		"a parser function that is not an entry point": parserFile(aliasedWasi,
			"func main() {\n\ts, _ := pgquery.Summary(\"DELETE FROM \"+os.Args[1], 0)\n\tfmt.Print(s)\n}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": src}), "gen/g.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// Read without a tree, as a single file's content is, the flow is not built: text
// parsed where it is built ends there on the file alone, and the parser is still
// known by its import path rather than by the name it is called.
func TestTextParsedWhereItIsBuiltIsNotASiteWithoutATree(t *testing.T) {
	for name, tc := range map[string]struct {
		imports string
		pkg     string
		site    bool
	}{
		"the parser under an alias":                {imports: aliasedWasi, pkg: "pgquery"},
		"the parser under its plain name":          {imports: plainPgQuery, pkg: "pg_query"},
		"another package under the parser's alias": {imports: "\tpgquery \"example.net/runner\"\n", pkg: "pgquery", site: true},
	} {
		t.Run(name, func(t *testing.T) {
			src := parserFile(tc.imports, "func main() { "+tc.pkg+".Parse(\"DELETE FROM \" + os.Args[1]) }\n")
			_, sites, err := sqlextract.FromGo("gen/g.go", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(sites) != 0; got != tc.site {
				t.Errorf("reported unread = %v, want %v: %+v", got, tc.site, sites)
			}
		})
	}
}

func TestTextParseResultIsConsumedBeforeItsInputIsInertWithoutATree(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		site bool
	}{
		"the tree and error are printed": {
			body: "func main() {\n\tq := \"DELETE FROM \" + os.Args[1]\n\ttree, err := pgquery.Parse(q)\n\tfmt.Print(tree, err)\n}\n",
		},
		"the tree is deparsed and run": {
			body: parserDrivers + "func main() {\n\tq := \"DELETE FROM \" + os.Args[1]\n\ttree, _ := pgquery.Parse(q)\n\ttext, _ := pgquery.Deparse(tree)\n\tpool.Exec(text)\n}\n",
			site: true,
		},
		"the tree is returned": {
			body: "func parse() *pgquery.ParseResult {\n\tq := \"DELETE FROM \" + os.Args[1]\n\ttree, _ := pgquery.Parse(q)\n\treturn tree\n}\n",
			site: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			src := parserFile(aliasedWasi, tc.body)
			_, sites, err := sqlextract.FromGo("gen/g.go", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(sites) != 0; got != tc.site {
				t.Errorf("reported unread = %v, want %v: %+v", got, tc.site, sites)
			}
		})
	}
}
