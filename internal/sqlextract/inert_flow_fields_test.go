package sqlextract_test

import "testing"

const fieldScopeMod = "module example.com/m\n\ngo 1.22\n"

// Text kept in an exported field goes only where a package that can hold that struct
// sends it. A driver call on a field of the same name elsewhere is not a path to it.
func TestFieldTextIsJudgedByThePackagesThatCanReachIt(t *testing.T) {
	const gen = "package gen\n\nimport \"os\"\n\ntype Row struct{ Note string }\n\nfunc Build(t string) Row { return Row{Note: \"DROP TABLE \" + t} }\n\nfunc main() { os.Stdout.WriteString(Build(os.Args[1]).Note) }\n"
	const stranger = "package other\n\ntype DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\ntype Msg struct{ Note string }\n\nfunc Send(db DB, m Msg) { db.Exec(m.Note) }\n"
	files := map[string]string{"go.mod": fieldScopeMod, "gen/g.go": gen, "other/o.go": stranger}
	if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
		t.Errorf("a stranger's field of the same name marked the text bad: %+v", sites)
	}
}

// The negative controls: every way the text can still reach a driver or leave the
// module stays reported, so the scope above is not a hole.
func TestFieldTextThatCanEscapeStaysUnread(t *testing.T) {
	const db = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\n"
	const gen = "package gen\n\ntype Row struct{ Note string }\n\nfunc Build(t string) Row { return Row{Note: \"DROP TABLE \" + t} }\n"
	cases := map[string]map[string]string{
		"a direct importer sends it": {
			"gen/g.go": gen,
			"use/u.go": "package use\n\nimport \"example.com/m/gen\"\n\n" + db + "func Run(d DB, t string) { d.Exec(gen.Build(t).Note) }\n",
		},
		"a transitive importer sends it": {
			"gen/g.go": gen,
			"mid/m.go": "package mid\n\nimport \"example.com/m/gen\"\n\nfunc Row(t string) gen.Row { return gen.Build(t) }\n",
			"use/u.go": "package use\n\nimport \"example.com/m/mid\"\n\n" + db + "func Run(d DB, t string) { d.Exec(mid.Row(t).Note) }\n",
		},
		"a third-party struct literal takes it": {
			"gen/g.go": "package gen\n\nimport \"example.net/lib\"\n\ntype Row struct{ Note string }\n\nfunc Build(t string) lib.Q { return lib.Q{Note: \"DROP TABLE \" + t} }\n",
		},
		"a third-party receiver takes it": {
			"gen/g.go": "package gen\n\nimport \"example.net/lib\"\n\ntype Row struct{ Note string }\n\nfunc Build(q *lib.Q, t string) { q.Note = \"DROP TABLE \" + t }\n",
		},
		"a generated proto field takes it": {
			"gen/g.go":    "package gen\n\nimport \"example.com/m/pb\"\n\ntype Row struct{ Note string }\n\nfunc Build(m *pb.Msg, t string) { m.Note = \"DROP TABLE \" + t }\n",
			"pb/m.pb.go":  "package pb\n\ntype Msg struct{ Note string }\n",
			"pb/m_use.go": "package pb\n",
		},
		"no struct in reach declares the field": {
			"gen/g.go": "package gen\n\nfunc Build(t string) { held().Note = \"DROP TABLE \" + t }\n\nfunc held() *int { return nil }\n",
		},
		"another package's exported variable takes it": {
			"gen/g.go":  "package gen\n\nimport \"example.com/m/conf\"\n\ntype Row struct{ Note string }\n\nfunc Build(t string) { conf.Note = \"DROP TABLE \" + t }\n",
			"conf/c.go": "package conf\n\nvar Note string\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			files["go.mod"] = fieldScopeMod
			if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// What Go's types rule out needs no following: only + yields text, a string has no
// fields, and !x, -x and +x take no text. A pure standard-library call hands it on.
func TestTextGoesOnlyWhereGoLetsIt(t *testing.T) {
	for name, body := range map[string]string{
		"a bool operand":           "func ok(s string) bool { return s == \"x\" }\n\nfunc main() { fmt.Print(len(os.Args) > 1 && ok(\"DROP TABLE \"+os.Args[1])) }\n",
		"a negated call":           "func ok(s string) bool { return s == \"x\" }\n\nfunc main() { fmt.Print(!ok(\"DROP TABLE \" + os.Args[1])) }\n",
		"a field of a kept struct": "type V struct{ Legal bool; Why string }\n\nfunc mk(s string) V { return V{Why: s} }\n\nfunc main() {\n\tv := mk(\"DROP TABLE \" + os.Args[1])\n\tfmt.Print(v.Legal, v.Why)\n}\n",
		"strings.Cut":              "func main() {\n\ta, _, _ := strings.Cut(\"DROP TABLE \"+os.Args[1], \" \")\n\tfmt.Print(a)\n}\n",
		"a direct replacer":        "func main() { fmt.Print(strings.NewReplacer(\"(\", \" \").Replace(\"DROP TABLE \" + os.Args[1])) }\n",
		"a replacer variable":      "func main() {\n\tr := strings.NewReplacer(\"(\", \" \")\n\tfmt.Print(r.Replace(\"DROP TABLE \" + os.Args[1]))\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + body}), "gen/g.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// The negative controls for that: the same shapes stay reported where text can leave.
func TestTextThatStaysVisibleToADriverStaysUnread(t *testing.T) {
	const db = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"
	for name, body := range map[string]string{
		"a field read into a driver":     "type V struct{ Why string }\n\nfunc mk(s string) V { return V{Why: s} }\n\nfunc main() { conn.Exec(mk(\"DROP TABLE \" + os.Args[1]).Why) }\n",
		"a method on a named string":     "type S string\n\nfunc (s S) Go() { conn.Exec(string(s)) }\n\nfunc main() { S(\"DROP TABLE \" + os.Args[1]).Go() }\n",
		"a cut half into a driver":       "func main() {\n\ta, _, _ := strings.Cut(\"DROP TABLE \"+os.Args[1], \";\")\n\tconn.Exec(a)\n}\n",
		"a replaced text into a driver":  "func main() { conn.Exec(strings.NewReplacer(\"(\", \" \").Replace(\"DROP TABLE \" + os.Args[1])) }\n",
		"a Replace that is not strings'": "type R struct{}\n\nfunc (R) Replace(s string) string { conn.Exec(s); return s }\n\nfunc main() { fmt.Print(R{}.Replace(\"DROP TABLE \" + os.Args[1])) }\n",
		"text in the replacer itself":    "func main() { fmt.Print(strings.NewReplacer(\"(\", \"DROP TABLE \"+os.Args[1]).Replace(\"x\")) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + db + body}), "gen/g.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// A closure only ever called, a table row and a writer parameter are followed exactly.
func TestShapesTheWalkFollowsExactly(t *testing.T) {
	for name, body := range map[string]string{
		"a closure's parameter":     "func main() {\n\tsay := func(s string) { fmt.Print(s) }\n\tsay(\"DROP TABLE \" + os.Args[1])\n}\n",
		"a closure's result":        "func main() {\n\tmk := func(t string) string { return \"DROP TABLE \" + t }\n\tfmt.Print(mk(os.Args[1]))\n}\n",
		"a closure called in place": "func main() {\n\ts := func(t string) string { return \"DROP TABLE \" + t }(os.Args[1])\n\tfmt.Print(s)\n}\n",
		"a positional row":          "func main() {\n\tfor _, tc := range []struct{ name, body string }{{\"a\", \"DROP TABLE \" + os.Args[1]}} {\n\t\tfmt.Print(tc.body)\n\t}\n}\n",
		"a row of a named struct":   "type row struct{ name, body string }\n\nfunc main() {\n\tfor _, tc := range []row{{\"a\", \"DROP TABLE \" + os.Args[1]}} {\n\t\tfmt.Print(tc.body)\n\t}\n}\n",
		"nested text":               "func main() {\n\tfor _, r := range [][]string{{\"DROP TABLE \" + os.Args[1]}} {\n\t\tfmt.Print(r)\n\t}\n}\n",
		"a writer given a stream":   "func emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() { emit(os.Stdout, os.Args[1]) }\n",
		"a writer handed on":        "func in(w io.Writer, t string) { io.WriteString(w, \"DROP TABLE \"+t) }\n\nfunc out(w io.Writer, t string) { in(w, t) }\n\nfunc main() { out(os.Stderr, os.Args[1]) }\n",
		"a writer given a builder":  "func emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() {\n\tvar b strings.Builder\n\temit(&b, os.Args[1])\n\tfmt.Print(b.String())\n}\n",
		"a writer's own write":      "func emit(w io.Writer, t string) { w.Write([]byte(\"DROP TABLE \" + t)) }\n\nfunc main() { emit(os.Stdout, os.Args[1]) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + body}), "gen/g.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// The negative controls for those shapes.
func TestShapesThatReachADriverStayUnread(t *testing.T) {
	const db = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nfunc (DB) Write(b []byte) (int, error) { return 0, nil }\n\nvar conn DB\n\n"
	for name, body := range map[string]string{
		"a closure that runs it":       "func main() {\n\trun := func(q string) { conn.Exec(q) }\n\trun(\"DROP TABLE \" + os.Args[1])\n}\n",
		"a closure's result run":       "func main() {\n\tmk := func(t string) string { return \"DROP TABLE \" + t }\n\tconn.Exec(mk(os.Args[1]))\n}\n",
		"a closure passed on":          "func use(f func(string) string) { conn.Exec(f(\"x\")) }\n\nfunc main() {\n\tmk := func(t string) string { return \"DROP TABLE \" + t }\n\tuse(mk)\n\tfmt.Print(mk(\"y\"))\n}\n",
		"a closure returned":           "func mk() func(string) string { return func(t string) string { return \"DROP TABLE \" + t } }\n\nfunc main() { fmt.Print(mk()(os.Args[1])) }\n",
		"a closure reassigned":         "func main() {\n\tmk := func(t string) string { return \"DROP TABLE \" + t }\n\tmk = func(t string) string { conn.Exec(t); return t }\n\tfmt.Print(mk(os.Args[1]))\n}\n",
		"a row's column run":           "func main() {\n\tfor _, tc := range []struct{ name, body string }{{\"a\", \"DROP TABLE \" + os.Args[1]}} {\n\t\tconn.Exec(tc.body)\n\t}\n}\n",
		"a row of another package":     "func main() {\n\tr := strings.Reader{\"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(&r)\n}\n",
		"nested text run":              "func main() {\n\tfor _, r := range [][]string{{\"DROP TABLE \" + os.Args[1]}} {\n\t\tconn.Exec(r[0])\n\t}\n}\n",
		"a writer given a connection":  "func emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() { emit(conn, os.Args[1]) }\n",
		"a writer's builder then run":  "func emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() {\n\tvar b strings.Builder\n\temit(&b, os.Args[1])\n\tconn.Exec(b.String())\n}\n",
		"a writer nothing here calls":  "func Emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n",
		"a writer's function as value": "func emit(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nvar hook = emit\n\nfunc main() { emit(os.Stdout, os.Args[1]) }\n",
		"a map key that is run":        "func main() {\n\tfor q := range map[string]bool{\"DROP TABLE \" + os.Args[1]: true} {\n\t\tconn.Exec(q)\n\t}\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + db + body}), "gen/g.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}
