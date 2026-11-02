package sqlextract_test

import "testing"

const inertHead = "package db\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n\t\"os\"\n\t\"regexp\"\n\t\"strings\"\n)\n\n" +
	"var (\n\t_ = errors.New\n\t_ = fmt.Sprint\n\t_ = os.Stdout\n\t_ = regexp.MustCompile\n\t_ = strings.Contains\n)\n\n"

// Text consumed where it is built can never be a statement a driver runs: it is
// compared, matched, or reported, and the result is a bool, an int or a message.
func TestCompositionConsumedOnTheSpotIsNotASite(t *testing.T) {
	for name, body := range map[string]string{
		"an equality":             "func Q(l, t string) bool { return l == \"CREATE TABLE \" + t + \" (\" }\n",
		"an inequality":           "func Q(l, t string) bool { return \"INSERT INTO \" + t != l }\n",
		"a regexp":                "func Q(t string) *regexp.Regexp { return regexp.MustCompile(`DELETE\\s+FROM\\s+` + t) }\n",
		"a strings needle":        "func Q(s, n string) bool { return strings.Contains(s, \"CREATE TYPE \"+n) }\n",
		"a fold compare":          "func Q(s, c string) bool { return strings.EqualFold(s, c+\" IS NOT NULL\") }\n",
		"an error":                "func Q(t string) error { return errors.New(\"DELETE FROM \" + t + \" failed\") }\n",
		"a wrapped error":         "func Q(t string) error { return fmt.Errorf(\"SELECT * FROM %s failed\", t) }\n",
		"stderr":                  "func Q(t string) { fmt.Fprintf(os.Stderr, \"DELETE FROM %s failed\\n\", t) }\n",
		"stdout":                  "func Q(t string) { fmt.Println(\"DELETE FROM \" + t) }\n",
		"a local needle":          "func Q(s, n string) bool {\n\tneedle := \"CREATE TYPE \" + n\n\treturn strings.HasPrefix(s, needle) || strings.Contains(s, needle)\n}\n",
		"a message around a call": "func name(t string) string { return t }\n\nfunc Q(t string) error { return errors.New(\"DELETE FROM \" + name(t) + \" failed\") }\n",
		"a test message":          "type T struct{}\n\nfunc (T) Errorf(string, ...any) {}\n\nfunc Q(v T, n string) { v.Errorf(\"SELECT * FROM %s\", n) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": inertHead + body}), "db/q.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// Anywhere else the text might reach a driver, so it stays a site.
func TestCompositionThatMayReachADriverStaysASite(t *testing.T) {
	for name, body := range map[string]string{
		"returned":                   "func Q(t string) string { return \"DELETE FROM \" + t }\n",
		"an unknown call":            "func run(string) {}\n\nfunc Q(t string) { run(\"DELETE FROM \" + t) }\n",
		"a local also returned":      "func Q(s, n string) string {\n\tq := \"DELETE FROM \" + n\n\tif strings.Contains(s, q) {\n\t\treturn \"\"\n\t}\n\treturn q\n}\n",
		"a local reassigned":         "func run(string) {}\n\nfunc Q(s, n string) bool {\n\tq := \"DELETE FROM \" + n\n\trun(q)\n\treturn s == q\n}\n",
		"a shadowed strings":         "type sx struct{}\n\nfunc (sx) Contains(a, b string) bool { return Exec(b) }\n\nfunc Exec(string) bool { return true }\n\nfunc Q(n string) bool {\n\tstrings := sx{}\n\treturn strings.Contains(\"a\", \"DELETE FROM \"+n)\n}\n",
		"a driver inside a message":  "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nfunc Q(d DB, t string) { fmt.Println(d.Exec(\"DELETE FROM \" + t)) }\n",
		"a helper inside a compare":  "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar d DB\n\nfunc run(q string) string { d.Exec(q); return \"\" }\n\nfunc Q(l, t string) bool { return l == run(\"DELETE FROM \"+t) }\n",
		"a closure inside a message": "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nfunc Q(d DB, t string) {\n\tfmt.Println(func() string { d.Exec(\"DELETE FROM \" + t); return \"\" }())\n}\n",
		"a replacement result":       "func Q(s, n string) string { return strings.ReplaceAll(s, \"x\", \"DELETE FROM \"+n) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": inertHead + body}), "db/q.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}
