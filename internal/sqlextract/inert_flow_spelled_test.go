package sqlextract_test

import (
	"strings"
	"testing"
)

const twoPrograms = "module example.com/m\n\ngo 1.22\n"

const programDB = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"

// A file outside any module can import only the standard library, so a field of a
// name in some module's package is not one it can reach. One that spells an import
// this walk cannot place could be holding that package's type, so it can.
func TestAFileOutsideAnyModuleReachesWhatItCanImport(t *testing.T) {
	const mod = "module example.com/m\n\ngo 1.22\n"
	far := "package far\n\n" + programDB + "type Row struct{ Note string }\n\nfunc Run(r Row) { conn.Exec(r.Note) }\n"
	const printer = "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\ntype row struct{ Note string }\n\nfunc main() {\n\tr := row{Note: \"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(r.Note)\n}\n"
	files := map[string]string{"far/go.mod": mod, "far/far.go": far, "tools/print.go": printer}
	if _, _, sites := extract(t, walkTree(t, files), "tools/print.go"); len(sites) != 0 {
		t.Errorf("a stranger's field reached a file outside any module: %+v", sites)
	}
	foreign := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"example.net/lib\"\n)\n\nfunc main() {\n\tr := lib.New()\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n"
	files["tools/print.go"] = foreign
	if _, _, sites := extract(t, walkTree(t, files), "tools/print.go"); len(sites) == 0 {
		t.Error("a file with an unplaced import did not reach the module's field of that name")
	}
}

// A write through a spelled type reaches that type's field, and the controls: a
// promoted field and an unspelled receiver reach every field of the name in reach.
func TestAWriteThroughASpelledType(t *testing.T) {
	const pb = "package pb\n\ntype Msg struct{ Note string }\n"
	const head = "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"example.com/m/pb\"\n)\n\nvar _ = pb.Msg{}\n\n"
	const row = "type row struct{ Note string }\n\n"
	exact := map[string]string{
		"a keyed literal":   head + row + "func main() {\n\tr := row{Note: \"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(r.Note)\n}\n",
		"a typed variable":  head + row + "func main() {\n\tvar r row\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n",
		"a typed parameter": head + row + "func set(r *row, t string) { r.Note = \"DROP TABLE \" + t }\n\nfunc main() {\n\tvar r row\n\tset(&r, os.Args[1])\n\tfmt.Print(r.Note)\n}\n",
		"a literal's local": head + row + "func main() {\n\tr := &row{}\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n",
	}
	for name, body := range exact {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"go.mod": twoPrograms, "gen/g.go": body, "pb/m.pb.go": pb}
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
	exactFromCall := map[string]string{
		"a function's result":     head + row + "func mk() row { return row{} }\n\nfunc main() {\n\tr := mk()\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n",
		"a pair's first result":   head + row + "func mk() (*row, error) { return &row{}, nil }\n\nfunc main() {\n\tr, _ := mk()\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n",
		"regexp.QuoteMeta's text": head + row + "func main() { fmt.Print(strings.ToUpper(regexp.QuoteMeta(\"DROP TABLE \" + os.Args[1]))) }\n",
	}
	for name, body := range exactFromCall {
		t.Run(name, func(t *testing.T) {
			body = strings.Replace(body, "\"os\"\n", "\"os\"\n\t\"regexp\"\n\t\"strings\"\n", 1)
			files := map[string]string{"go.mod": twoPrograms, "gen/g.go": body, "pb/m.pb.go": pb}
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
	unread := map[string]string{
		"a result of the generated type": head + row + "func mk() *pb.Msg { return nil }\n\nfunc main() {\n\tm := mk()\n\tm.Note = \"DROP TABLE \" + os.Args[1]\n}\n",
		"QuoteMeta's text run":           head + programDB + "func main() { conn.Exec(regexp.QuoteMeta(\"DROP TABLE \" + os.Args[1])) }\n",
		"a promoted field":               head + "type base struct{ Other string }\n\ntype row struct {\n\tpb.Msg\n\tbase\n}\n\nfunc main() {\n\tvar r row\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n}\n",
		"an unspelled receiver":          head + row + "func get() *pb.Msg { return nil }\n\nfunc main() {\n\tm := get()\n\tm.Note = \"DROP TABLE \" + os.Args[1]\n}\n",
		"the generated message itself":   head + row + "func main() {\n\tm := pb.Msg{}\n\tm.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(m.Note)\n}\n",
	}
	for name, body := range unread {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(body, "regexp.") {
				body = strings.Replace(body, "\"os\"\n", "\"os\"\n\t\"regexp\"\n", 1)
			}
			files := map[string]string{"go.mod": twoPrograms, "gen/g.go": body, "pb/m.pb.go": pb}
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// A recursive local closure is followed like any closure only ever called. The controls:
// one assigned twice, and one that runs its text.
func TestARecursiveClosure(t *testing.T) {
	const head = "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n"
	const rec = "func main() {\n\tvar walk func(s string, d int)\n\twalk = func(s string, d int) {\n\t\tif d > 0 {\n\t\t\t%s\n\t\t\twalk(s, d-1)\n\t\t}\n\t}\n\twalk(\"DROP TABLE \"+os.Args[1], 2)\n}\n"
	files := func(body string) map[string]string {
		return map[string]string{"go.mod": twoPrograms, "gen/g.go": head + programDB + "var _ = fmt.Sprint\n\n" + body}
	}
	if _, _, sites := extract(t, walkTree(t, files(strings.Replace(rec, "%s", "fmt.Print(s)", 1))), "gen/g.go"); len(sites) != 0 {
		t.Errorf("reported unread: %+v", sites)
	}
	if _, _, sites := extract(t, walkTree(t, files(strings.Replace(rec, "%s", "conn.Exec(s)", 1))), "gen/g.go"); len(sites) == 0 {
		t.Error("a recursive closure that runs its text was not reported")
	}
	twice := "func main() {\n\tvar f func(s string)\n\tf = func(s string) { fmt.Print(s) }\n\tf = func(s string) { conn.Exec(s) }\n\tf(\"DROP TABLE \" + os.Args[1])\n}\n"
	if _, _, sites := extract(t, walkTree(t, files(twice)), "gen/g.go"); len(sites) == 0 {
		t.Error("a closure assigned twice was not reported")
	}
}

// A read through a spelled type reads that type's field. The controls: another type's
// field of the name, read and run, is that field's; and a type's own field read and run
// stays reported.
func TestAReadThroughASpelledType(t *testing.T) {
	const head = "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" + programDB
	const types = "type row struct{ Note string }\n\ntype hit struct{ Note string }\n\n"
	printed := head + types + "func (h hit) run() { conn.Exec(h.Note) }\n\nfunc main() {\n\tvar r row\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(r.Note)\n\thit{}.run()\n}\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "gen/g.go": printed}), "gen/g.go"); len(sites) != 0 {
		t.Errorf("another type's field of the name marked the text: %+v", sites)
	}
	for name, body := range map[string]string{
		"its own field run": head + types + "func (r row) run() { conn.Exec(r.Note) }\n\nfunc main() {\n\tvar r row\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tr.run()\n\tfmt.Print(1)\n}\n",
		"unspelled, run":    head + types + "func (r row) self() row { return r }\n\nfunc main() {\n\tvar r row\n\tr.Note = \"DROP TABLE \" + os.Args[1]\n\tx := r.self()\n\tconn.Exec(x.Note)\n\tfmt.Print(1)\n}\n",
		"a promoted field":  head + "type base struct{ Note string }\n\ntype top struct{ base }\n\nfunc main() {\n\tvar b base\n\tb.Note = \"DROP TABLE \" + os.Args[1]\n\tt := top{b}\n\tconn.Exec(t.Note)\n\tfmt.Print(1)\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "gen/g.go": body}), "gen/g.go"); len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// A *os.File parameter holds what its callers hand it, as an io.Writer one does.
func TestAFileParameterGivenAStream(t *testing.T) {
	const head = "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n"
	body := head + "func say(w *os.File, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() { say(os.Stderr, os.Args[1]) }\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "dev/a.go": body}), "dev/a.go"); len(sites) != 0 {
		t.Errorf("reported unread: %+v", sites)
	}
	opened := head + "func say(w *os.File, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc dial() *os.File { return nil }\n\nfunc main() { say(dial(), os.Args[1]) }\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "dev/a.go": opened}), "dev/a.go"); len(sites) == 0 {
		t.Error("a file parameter given a value the walk cannot place was not reported")
	}
}

// A range variable's type is spelled by what it ranges over.
func TestARangeVariableSpelledByItsSlice(t *testing.T) {
	const head = "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" + programDB
	const types = "type row struct{ Note string }\n\ntype hit struct{ Note string }\n\n"
	const hitRun = "func runAll(hits []hit) {\n\tfor _, h := range hits {\n\t\tconn.Exec(h.Note)\n\t}\n}\n\n"
	printed := head + types + hitRun + "func main() {\n\tr := row{Note: \"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(r.Note)\n\trunAll(nil)\n}\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "gen/g.go": printed}), "gen/g.go"); len(sites) != 0 {
		t.Errorf("another type's field read in a range marked the text: %+v", sites)
	}
	run := head + types + "func runAll(rows []row) {\n\tfor _, r := range rows {\n\t\tconn.Exec(r.Note)\n\t}\n}\n\nfunc main() {\n\trunAll([]row{{Note: \"DROP TABLE \" + os.Args[1]}})\n\tfmt.Print(1)\n}\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "gen/g.go": run}), "gen/g.go"); len(sites) == 0 {
		t.Error("a row ranged over and run was not reported")
	}
	keyed := head + types + "func main() {\n\tm := map[string]row{\"k\": {Note: \"DROP TABLE \" + os.Args[1]}}\n\tfor _, r := range m {\n\t\tconn.Exec(r.Note)\n\t}\n\tfmt.Print(1)\n}\n"
	if _, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": twoPrograms, "gen/g.go": keyed}), "gen/g.go"); len(sites) == 0 {
		t.Error("a map value ranged over and run was not reported")
	}
}

// pkg.Name is a name of that package, so a parameter of type schema.Note reads no
// field Note. The control: a variable named like nothing imported is still a value.
func TestAPackageNameIsNotAFieldRead(t *testing.T) {
	schema := "package schema\n\ntype Note struct{ Note string }\n"
	body := "package gen\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"example.com/m/schema\"\n)\n\nfunc use(n schema.Note) {}\n\nfunc main() {\n\tn := schema.Note{Note: \"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(n.Note)\n\tuse(n)\n}\n"
	files := map[string]string{"go.mod": twoPrograms, "schema/s.go": schema, "gen/g.go": body}
	if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
		t.Errorf("a type name read as a field: %+v", sites)
	}
	run := "package gen\n\nimport \"os\"\n\n" + programDB + "type box struct{ Note string }\n\nvar held box\n\nfunc main() {\n\theld.Note = \"DROP TABLE \" + os.Args[1]\n\tgo2()\n}\n"
	other := "package gen\n\nfunc go2() { conn.Exec(held.Note) }\n"
	files = map[string]string{"go.mod": twoPrograms, "gen/g.go": run, "gen/h.go": other}
	if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) == 0 {
		t.Error("a package variable's field read in another file was not followed")
	}
}
