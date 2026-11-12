package sqlextract_test

import "testing"

// A method call reaches the method its receiver's type declares. A method of the same
// name on another type, in this package or another, is a different function: what it
// does with its parameters says nothing about this call's text.

// The generator renders a row read into a builder through its own helper method and
// writes the result to a file. Elsewhere in the tree another type has a method of the
// same name and a different arity. The generator's text still only lands in a file.
const calleePlan = "package main\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\n" +
	"type plan struct{ table string }\n\n" +
	"func (p plan) readRows(b *strings.Builder, sql string, args string) {\n\tb.WriteString(\"rows, err := q.Query(ctx, \" + sql + \", \" + args + \")\\n\")\n}\n\n" +
	"func (p plan) render() string {\n\tvar b strings.Builder\n\tpage := \"SELECT * FROM \" + p.table + \" WHERE id > $1\"\n\tp.readRows(&b, page, \"after\")\n\treturn b.String()\n}\n\n" +
	"func main() {\n\tp := plan{table: os.Args[1]}\n\tos.WriteFile(\"x_gen.go\", []byte(p.render()), 0o644)\n}\n"

const calleeDB = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"

func TestAMethodCallReachesItsReceiversMethodOnly(t *testing.T) {
	for name, other := range map[string]string{
		"another package's method of another arity": "package svc\n\n" +
			"type converge struct{ n int }\n\n" +
			"func (c *converge) readRows(n int) (map[string]bool, error) { return nil, nil }\n\n" +
			"func (c *converge) Apply() { c.readRows(c.n) }\n",
		"another package's method of the same arity that runs its text": "package svc\n\nimport \"strings\"\n\n" + calleeDB +
			"type runner struct{}\n\n" +
			"func (r runner) readRows(b *strings.Builder, sql string, args string) { conn.Exec(sql + args) }\n\n" +
			"func Apply(b *strings.Builder) { var r runner; r.readRows(b, \"SELECT 1\", \"\") }\n",
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"go.mod": closedMod, "gen/g.go": calleePlan, "svc/s.go": other}
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
				t.Errorf("text that only lands in a file was reported unread, because a method of another type shares readRows's name: %+v", sites)
			}
		})
	}
}

// A call whose receiver's type the walk cannot name reaches the methods of its name
// that could take its arguments, and through them only.
func TestAMethodCallOfAnUnnamedReceiverReachesWhatFitsIt(t *testing.T) {
	const head = "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nvar _ = strings.Join\n\n" + calleeDB
	for name, body := range map[string]string{
		"an interface call, and a method of another arity": "type plan struct{}\n\n" +
			"func (plan) readRows(b *strings.Builder, sql string, args string) { b.WriteString(sql + args) }\n\n" +
			"type converge struct{}\n\nfunc (c *converge) readRows(n int) { conn.Exec(fmt.Sprint(n)) }\n\n" +
			"type reader interface{ readRows(b *strings.Builder, sql string, args string) }\n\n" +
			"func use(r reader) {\n\tvar b strings.Builder\n\tr.readRows(&b, \"SELECT * FROM \"+os.Args[1], \"x\")\n\tos.Stdout.WriteString(b.String())\n}\n\n" +
			"func init() { use(plan{}); new(converge).readRows(1) }\n",
		"an interface call reaching two methods that return it": "type G struct{}\n\nfunc (G) ddl(t string) string { return \"DROP TABLE \" + t }\n\n" +
			"type H struct{}\n\nfunc (H) ddl(t string) string { return \"DROP VIEW \" + t }\n\n" +
			"type ddler interface{ ddl(t string) string }\n\n" +
			"func main() {\n\tvar d ddler = G{}\n\tfmt.Print(d.ddl(os.Args[1]))\n\td = H{}\n\tfmt.Print(d.ddl(os.Args[1]))\n}\n",
		"a call elsewhere that cannot be placed does not join two types' methods": "type plan struct{}\n\nfunc (plan) emit(sql string) { fmt.Print(sql) }\n\n" +
			"type runner struct{}\n\nfunc (runner) emit(sql string) { conn.Exec(sql) }\n\n" +
			"type emitter interface{ emit(sql string) }\n\n" +
			"func any(e emitter) { e.emit(\"SELECT 1\") }\n\n" +
			"func main() {\n\tvar p plan\n\tp.emit(\"SELECT * FROM \" + os.Args[1])\n\tany(runner{})\n}\n",
		"a lone call argument of two results": "type plan struct{}\n\nfunc (plan) emit(a, b string) { fmt.Print(a, b) }\n\n" +
			"type emitter interface{ emit(a, b string) }\n\n" +
			"func pair() (string, string) { return \"SELECT * FROM \" + os.Args[1], \"x\" }\n\n" +
			"func use(e emitter) { e.emit(pair()) }\n\nfunc main() { use(plan{}) }\n",
		"a spread argument fits only a variadic method": "type plan struct{}\n\nfunc (plan) emit(parts ...string) { fmt.Print(parts) }\n\n" +
			"type runner struct{}\n\nfunc (runner) emit(parts []string) { conn.Exec(parts[0]) }\n\n" +
			"type emitter interface{ emit(parts ...string) }\n\n" +
			"func use(e emitter) { e.emit([]string{\"SELECT * FROM \" + os.Args[1]}...) }\n\n" +
			"func main() { use(plan{}); runner{}.emit(nil) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"go.mod": closedMod, "gen/g.go": head + body}
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
				t.Errorf("text that only lands in a file was reported unread: %+v", sites)
			}
		})
	}
}

// Text handed to a method that runs it stays a site, however many other methods of
// its name land their text in files; and a call whose receiver's type the walk cannot
// name reaches every method of its name that could take its arguments.
func TestAMethodOfTheSameNameThatRunsTextStaysASite(t *testing.T) {
	for name, tc := range map[string]struct{ path, body string }{
		"its receiver's method runs it": {"svc/s.go", "package svc\n\nimport \"os\"\n\n" + calleeDB +
			"type runner struct{}\n\n" +
			"func (r runner) readRows(q string) { conn.Exec(q) }\n\n" +
			"func Apply() { var r runner; r.readRows(\"DROP TABLE \" + os.Args[1]) }\n"},
		"through an interface, one implementation runs it": {"gen/i.go", "package main\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\n" + calleeDB +
			"type runner struct{}\n\n" +
			"func (r runner) readRows(b *strings.Builder, sql string, args string) { conn.Exec(sql) }\n\n" +
			"type reader interface{ readRows(b *strings.Builder, sql string, args string) }\n\n" +
			"func use(r reader) {\n\tvar b strings.Builder\n\tr.readRows(&b, \"DROP TABLE \"+os.Args[1], \"x\")\n\tos.Stdout.WriteString(b.String())\n}\n\n" +
			"func init() { use(runner{}) }\n"},
		"a call that cannot be placed, into a builder a method writes": {"gen/b.go", "package main\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\n" + calleeDB +
			"type writer struct{}\n\nfunc (writer) emit(b *strings.Builder, t string) { b.WriteString(\"DROP TABLE \" + t) }\n\n" +
			"type quiet struct{}\n\nfunc (quiet) emit(b *strings.Builder, t string) {}\n\n" +
			"func pick() writer { return writer{} }\n\n" +
			"func init() {\n\tvar b strings.Builder\n\tpick().emit(&b, os.Args[1])\n\tquiet{}.emit(&b, \"\")\n\tconn.Exec(b.String())\n}\n"},
		"a call that cannot be placed, into methods taking a builder and a writer": {"gen/w.go", "package main\n\nimport (\n\t\"io\"\n\t\"os\"\n\t\"strings\"\n)\n\n" + calleeDB +
			"type writer struct{}\n\nfunc (writer) emit(b *strings.Builder, t string) { b.WriteString(\"DROP TABLE \" + t) }\n\n" +
			"type sink struct{}\n\nfunc (sink) emit(w io.Writer, t string) { io.WriteString(w, t) }\n\n" +
			"func pick() writer { return writer{} }\n\n" +
			"func init() {\n\tvar b strings.Builder\n\tpick().emit(&b, os.Args[1])\n\tsink{}.emit(os.Stdout, \"\")\n\tconn.Exec(b.String())\n}\n"},
		"a call that cannot be placed, into a method's unnamed parameter": {"gen/u.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
			"type T struct{}\n\nfunc (T) emit(s string) { fmt.Print(s) }\n\n" +
			"type U struct{}\n\nfunc (U) emit(string) {}\n\n" +
			"type emitter interface{ emit(string) }\n\n" +
			"func use(e emitter) { e.emit(\"DROP TABLE \" + os.Args[1]) }\n\n" +
			"func init() { use(T{}); use(U{}) }\n"},
		"a call that cannot be placed, returning what one method composes": {"gen/r.go", "package main\n\nimport \"os\"\n\n" + calleeDB +
			"type G struct{}\n\nfunc (G) ddl(t string) string { return \"DROP TABLE \" + t }\n\n" +
			"type H struct{}\n\nfunc (H) ddl(t string) string { return t }\n\n" +
			"type ddler interface{ ddl(t string) string }\n\n" +
			"func use(d ddler) { conn.Exec(d.ddl(os.Args[1])) }\n\n" +
			"func init() { use(G{}); use(H{}) }\n"},
		"a call that cannot be placed, into a writer one method writes": {"gen/v.go", "package main\n\nimport (\n\t\"io\"\n\t\"os\"\n\t\"strings\"\n)\n\n" + calleeDB +
			"type A struct{}\n\nfunc (A) emit(w io.Writer, t string) { io.WriteString(w, \"DROP TABLE \"+t) }\n\n" +
			"type B struct{}\n\nfunc (B) emit(w io.Writer, t string) {}\n\n" +
			"type emitter interface{ emit(w io.Writer, t string) }\n\n" +
			"func use(e emitter) {\n\tvar b strings.Builder\n\te.emit(&b, os.Args[1])\n\tconn.Exec(b.String())\n}\n\n" +
			"func init() { use(A{}); use(B{}) }\n"},
		"through a type parameter named like a package type": {"gen/p.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" + calleeDB +
			"type T struct{}\n\nfunc (T) emit(s string) { fmt.Print(s) }\n\n" +
			"type runner struct{}\n\nfunc (runner) emit(s string) { conn.Exec(s) }\n\n" +
			"func run[T interface{ emit(string) }](x T) { x.emit(\"DROP TABLE \" + os.Args[1]) }\n\n" +
			"func init() { run(runner{}) }\n"},
	} {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{"go.mod": closedMod, "gen/g.go": calleePlan, tc.path: tc.body}
			if _, _, sites := extract(t, walkTree(t, files), tc.path); len(sites) == 0 {
				t.Error("text that reaches a driver through a method was not reported unread")
			}
		})
	}
}
