package sqlextract_test

import "testing"

const flowHead = "package gen\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n\t\"strings\"\n)\n\n" +
	"var (\n\t_ = fmt.Sprint\n\t_ = io.WriteString\n\t_ = os.Stdout\n\t_ = strings.Join\n)\n\n"

// Text a generator builds for a file, followed through its helpers to where it lands
// on disk or stdout, is judged in the file it lands in. No driver can run it from Go.
func TestGeneratedTextThatOnlyLandsInAFileIsNotASite(t *testing.T) {
	for name, body := range map[string]string{
		"returned then written": "func ddl(t string) string { return \"DROP TABLE \" + t }\n\n" +
			"func main() { os.WriteFile(\"x.sql\", []byte(ddl(os.Args[1])), 0o644) }\n",
		"through two helpers": "func inner(t string) string { return \"DROP TABLE \" + t }\n\nfunc outer(t string) string { return inner(t) + \";\" }\n\n" +
			"func main() { fmt.Print(outer(os.Args[1])) }\n",
		"into a builder": "func render(ts []string) string {\n\tvar b strings.Builder\n\tfor _, t := range ts {\n\t\tb.WriteString(\"DROP TABLE \" + t + \";\")\n\t}\n\treturn b.String()\n}\n\n" +
			"func main() { fmt.Fprint(os.Stdout, render(os.Args)) }\n",
		"a joined slice": "func render(ts []string) string {\n\tvar parts []string\n\tfor _, t := range ts {\n\t\tparts = append(parts, \"DROP TABLE \"+t)\n\t}\n\treturn strings.Join(parts, \";\")\n}\n\n" +
			"func main() { io.WriteString(os.Stdout, render(os.Args)) }\n",
		"passed to a writing helper": "func save(body string) { os.WriteFile(\"x.sql\", []byte(body), 0o644) }\n\n" +
			"func main() { save(\"DROP TABLE \" + os.Args[1]) }\n",
		"compared with the committed file": "func ddl(t string) string { return \"DROP TABLE \" + t }\n\n" +
			"func check(want string) bool { return ddl(os.Args[1]) == want }\n\nfunc main() { check(\"x\") }\n",
		"a method's result":       "type G struct{}\n\nfunc (G) ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() { fmt.Print(G{}.ddl(os.Args[1])) }\n",
		"a struct field":          "type S struct{ Q string }\n\nfunc main() {\n\ts := S{Q: \"DROP TABLE \" + os.Args[1]}\n\tfmt.Print(s.Q)\n}\n",
		"a field assigned":        "type S struct{ Q string }\n\nfunc main() {\n\tvar s S\n\ts.Q = \"DROP TABLE \" + os.Args[1]\n\tfmt.Print(s.Q)\n}\n",
		"a stream's write method": "func main() { os.Stdout.WriteString(\"DROP TABLE \" + os.Args[1]) }\n",
		"an opened file's write":  "func main() {\n\tf, _ := os.Create(\"x.sql\")\n\tf.WriteString(\"DROP TABLE \" + os.Args[1])\n}\n",
		"a map value written":     "func plant(files map[string]string) {\n\tfor name, body := range files {\n\t\tos.WriteFile(name, []byte(body), 0o644)\n\t}\n}\n\nfunc main() { plant(map[string]string{\"x.sql\": \"DROP TABLE \" + os.Args[1]}) }\n",
		"a map read with ok":      "func use(m map[string]string) {\n\tv, ok := m[\"k\"]\n\tif ok {\n\t\tfmt.Print(v)\n\t}\n}\n\nfunc main() { use(map[string]string{\"k\": \"DROP TABLE \" + os.Args[1]}) }\n",
		"a value with an error": "func ddl(t string) (string, error) { return \"DROP TABLE \" + t, nil }\n\n" +
			"func main() {\n\tbody, err := ddl(os.Args[1])\n\tif err != nil {\n\t\tpanic(err)\n\t}\n\tfmt.Print(body)\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + body}), "gen/g.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// The same flow across a package boundary.
func TestGeneratedTextIsFollowedAcrossPackages(t *testing.T) {
	_, _, sites := extract(t, walkTree(t, map[string]string{
		"go.mod":   closedMod,
		"lib/l.go": "package lib\n\nfunc DDL(t string) string { return \"DROP TABLE \" + t }\n",
		"cmd/m.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/m/lib\"\n)\n\nfunc main() { fmt.Print(lib.DDL(\"x\" + y())) }\n\nfunc y() string { return \"y\" }\n",
	}), "lib/l.go")
	if len(sites) != 0 {
		t.Errorf("reported unread: %+v", sites)
	}
}

// One path to a driver, or one thing the walk cannot follow, keeps the text a site.
func TestTextThatMayReachADriverStaysASite(t *testing.T) {
	const db = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"
	for name, body := range map[string]string{
		"run directly":                           db + "func main() { conn.Exec(\"DROP TABLE \" + os.Args[1]) }\n",
		"through a helper":                       db + "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() { conn.Exec(ddl(os.Args[1])) }\n",
		"written and run":                        db + "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() {\n\tbody := ddl(os.Args[1])\n\tfmt.Print(body)\n\tconn.Exec(body)\n}\n",
		"from a builder":                         db + "func main() {\n\tvar b strings.Builder\n\tb.WriteString(\"DROP TABLE \" + os.Args[1])\n\tconn.Exec(b.String())\n}\n",
		"a wrapper named like the driver":        "func Exec(q string) { run(q) }\n\nfunc run(string) {}\n\nvar _ = run\n\nfunc main() { Exec(\"DROP TABLE \" + os.Args[1]) }\n",
		"a function value":                       "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nvar f = ddl\n\nfunc main() { fmt.Print(ddl(os.Args[1])) }\n",
		"never called":                           "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() {}\n",
		"a method":                               db + "type G struct{}\n\nfunc (G) ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() { conn.Exec(G{}.ddl(os.Args[1])) }\n",
		"into a struct":                          db + "type S struct{ Q string }\n\nfunc main() {\n\ts := S{Q: \"DROP TABLE \" + os.Args[1]}\n\tconn.Exec(s.Q)\n}\n",
		"a map value run":                        db + "func main() {\n\tm := map[string]string{\"k\": \"DROP TABLE \" + os.Args[1]}\n\tconn.Exec(m[\"k\"])\n}\n",
		"a ranged map run":                       db + "func main() {\n\tfor _, q := range map[string]string{\"k\": \"DROP TABLE \" + os.Args[1]} {\n\t\tconn.Exec(q)\n\t}\n}\n",
		"an unexported field run in its package": db + "type S struct{ q string }\n\nfunc main() {\n\ts := S{q: \"DROP TABLE \" + os.Args[1]}\n\tconn.Exec(s.q)\n}\n",
		"into a package var":                     "var q string\n\nfunc main() { q = \"DROP TABLE \" + os.Args[1] }\n",
		"an unknown writer":                      "func render(w io.Writer, t string) { fmt.Fprint(w, \"DROP TABLE \"+t) }\n\nfunc main() { render(os.Stdout, os.Args[1]) }\n",
		"a closure":                              "func main() {\n\tf := func(t string) string { return \"DROP TABLE \" + t }\n\tfmt.Print(f(os.Args[1]))\n}\n",
		"its address taken":                      "func keep(*strings.Builder) {}\n\nfunc main() {\n\tvar b strings.Builder\n\tb.WriteString(\"DROP TABLE \" + os.Args[1])\n\tkeep(&b)\n}\n",
		"helpers in a cycle that run it":         db + "func a(q string) { b(q) }\n\nfunc b(q string) {\n\ta(q)\n\tconn.Exec(q)\n}\n\nfunc main() { a(\"DROP TABLE \" + os.Args[1]) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + body}), "gen/g.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}

// A program file's text is judged on the program: a test keeping or running it does
// not stop it being inert. A test file's own text is judged with the tests.
func TestProgramTextIsJudgedWithoutItsTests(t *testing.T) {
	const db = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"
	files := map[string]string{
		"go.mod":        closedMod,
		"gen/g.go":      flowHead + "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() { fmt.Print(ddl(os.Args[1])) }\n",
		"gen/g_test.go": "package gen\n\n" + db + "func use(t string) { conn.Exec(ddl(t)) }\n\nfunc own(t string) { conn.Exec(\"DROP TABLE \" + t) }\n",
	}
	if _, _, sites := extract(t, walkTree(t, files), "gen/g.go"); len(sites) != 0 {
		t.Errorf("program text reported unread: %+v", sites)
	}
	if _, _, sites := extract(t, walkTree(t, files), "gen/g_test.go"); len(sites) == 0 {
		t.Error("a test's own text that reaches a driver was not reported unread")
	}
}
