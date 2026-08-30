// census_pkgseed_test.go — #311, the second instance of the class its own fix
// closed.
//
// #311 half 2 names `strings.Builder` FIRST among the four compositions whose
// unordered locking SELECT the gate cannot see, and the fix taught the builder
// walk to read the text of a LOCAL written into it — `q := "SELECT …"` then
// `b.WriteString(q)` — because the reviewer's four-file repro used that
// spelling. Every other spelling of the same query stayed silent: a package
// scope is where Go code usually puts a query constant, and
//
//	const listQ = "SELECT id FROM t WHERE s = 'x'"
//	…
//	b.WriteString(listQ)
//	b.WriteString(" FOR UPDATE")
//
// left the builder holding " FOR UPDATE" alone, which is not SQL-shaped, so the
// site was dropped and the census said nothing — about a file the rule also
// says nothing about, because a builder is never tracked. That is #311 half 2
// verbatim, surviving inside #311's own fix for the shape #311 leads with.
//
// The narrowing below is the other half and it is not decoration: a package
// scope holds paths, log prefixes and format strings too, and resolving names
// against it must not put a census line on every builder in a repo that happens
// to declare a constant.
package sqlparse_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
)

const pkgSeedQuery = "SELECT id FROM t WHERE s = 'x'"

// pkgSeedCensus is what the census says about src for the locking rules, with
// the premise checked: the rule itself must report nothing, or the assertion is
// about a file the gate would have caught anyway.
func pkgSeedCensus(t *testing.T, src string) int {
	t.Helper()
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	if ms := matches(t, c, file("b.go", src)); len(ms) != 0 {
		t.Fatalf("premise: the rule is supposed to report nothing here: %+v", ms)
	}
	sites, ok, err := sqlparse.CensusSites("sql/locking-select-order", "b.go", []byte(src))
	if err != nil || !ok {
		t.Fatalf("CensusSites: ok=%v err=%v", ok, err)
	}
	return len(sites)
}

// A query declared where Go code usually declares one.
func TestBuilderWrittenFromAPackageLevelSeedIsNotSilent(t *testing.T) {
	for _, tc := range []struct {
		name string
		decl string
	}{
		// The idiomatic spelling: a package-level const.
		{"const-decl", "const listQ = \"" + pkgSeedQuery + "\"\n"},
		// A package-level var, which binds the same way.
		{"var-decl", "var listQ = \"" + pkgSeedQuery + "\"\n"},
		// Grouped, which is one GenDecl holding several specs.
		{"grouped-const", "const (\n\tother = \"x\"\n\tlistQ = \"" + pkgSeedQuery + "\"\n)\n"},
		// Grouped var, same shape.
		{"grouped-var", "var (\n\tother = \"x\"\n\tlistQ = \"" + pkgSeedQuery + "\"\n)\n"},
		// Two names in one spec, paired with two values by index.
		{"multi-name-spec", "const other, listQ = \"x\", \"" + pkgSeedQuery + "\"\n"},
		// The declaration is itself a composition the operand reader handles.
		{"concat-decl", "const listQ = \"SELECT id\" + \" FROM t WHERE s = 'x'\"\n"},
		// Declared BELOW the function that writes it, which is legal Go and
		// says nothing about whether the pass can read it.
		{"declared-after-use", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decl := tc.decl
			trailer := ""
			if tc.name == "declared-after-use" {
				trailer = "const listQ = \"" + pkgSeedQuery + "\"\n"
			}
			src := "package db\n\nimport \"strings\"\n\n" + decl +
				"\nfunc Q() string {\n\tvar b strings.Builder\n" +
				"\tb.WriteString(listQ)\n\tb.WriteString(\" FOR UPDATE\")\n" +
				"\treturn b.String()\n}\n" + trailer
			if n := pkgSeedCensus(t, src); n != 1 {
				t.Fatalf("this file hides an unordered locking SELECT in a builder "+
					"seeded from a package-level declaration, and the census reports "+
					"%d line(s) about it", n)
			}
		})
	}
}

// The narrowing. A package scope is mostly not queries, and a name the FUNCTION
// binds is the one that reaches the builder — reading the package declaration
// over the top of it would invent text for a builder assembling a path.
func TestBuilderWrittenFromANonSQLPackageLevelSeedStaysUnreported(t *testing.T) {
	for _, tc := range []struct {
		name, decl, body string
	}{
		// A package-level constant that is not SQL at all.
		{"path-const", "const logDir = \"/var/log/app\"\n",
			"\tvar b strings.Builder\n\tb.WriteString(logDir)\n" +
				"\tb.WriteString(\"/today.log\")\n\treturn b.String()\n"},
		// The local shadows the package declaration, and the local is what the
		// builder is written from. Concatenating the two would report a SQL
		// coverage gap in a function that composes a log path.
		{"local-shadows-package-seed", "const listQ = \"" + pkgSeedQuery + "\"\n",
			"\tlistQ := \"/var/log/app\"\n\tvar b strings.Builder\n" +
				"\tb.WriteString(listQ)\n\tb.WriteString(\"/today.log\")\n\treturn b.String()\n"},
		// The other direction of the same collision, and it takes its own case:
		// above, reading the package declaration OVER the local puts a query on
		// a log-path builder, while here reading it UNDER the local completes a
		// fragment the function never completes. `listQ := "SELECT id"` has a
		// leading statement keyword and no structural keyword after it, so it
		// is not SQL-shaped on its own and the builder really does hold only
		// that; joining the package constant's " FROM t …" onto it manufactures
		// a query no scope in this function produces.
		{"package-text-is-not-joined-onto-the-local",
			"const listQ = \" FROM t WHERE s = 'x' FOR UPDATE\"\n",
			"\tlistQ := \"SELECT id\"\n\tvar b strings.Builder\n" +
				"\tb.WriteString(listQ)\n\treturn b.String()\n"},
		// A package-level value that is not string text: nothing to read, and a
		// placeholder read as text would call the builder SQL-bearing.
		{"non-string-package-value", "var count = 5\n",
			"\tvar b strings.Builder\n\tb.WriteString(runtimeQ(count))\n" +
				"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// One right-hand expression for two names pairs by index with nothing.
		{"multi-value-package-binding", "var lhs, listQ = two()\n",
			"\tvar b strings.Builder\n\tb.WriteString(listQ)\n" +
				"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// A grouped const whose later spec repeats the previous expression
		// implicitly: that spec carries no value to read.
		{"implicit-repetition", "const (\n\tfirst = \"" + pkgSeedQuery + "\"\n\tlistQ\n)\n",
			"\tvar b strings.Builder\n\tb.WriteString(listQ)\n" +
				"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package db\n\nimport \"strings\"\n\n" +
				"func two() (string, string) { return \"a\", \"b\" }\n\n" +
				"func runtimeQ(int) string { return \"\" }\n\n" +
				tc.decl + "\nfunc Q() string {\n" + tc.body + "}\n"
			if n := pkgSeedCensus(t, src); n != 0 {
				t.Fatalf("nothing here is a SQL composition the rule declined to "+
					"read, and the census reports %d line(s) about it — the flood "+
					"builder.go's doc warns about", n)
			}
		})
	}
}
