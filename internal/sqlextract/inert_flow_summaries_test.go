package sqlextract_test

import (
	"strings"
	"testing"
)

const summaryDB = "type DB struct{}\n\nfunc (DB) Exec(string) error { return nil }\n\nvar conn DB\n\n"

// A helper whose parameters reach only its return is judged at each call: text given
// to it at one call does not come back at another.
func TestAHelperIsJudgedAtEachCall(t *testing.T) {
	const trim = "func flat(s string) string {\n\ts = strings.TrimSpace(s)\n\treturn s\n}\n\n"
	for name, body := range map[string]string{
		"another call is run":     summaryDB + trim + "func main() {\n\tfmt.Print(flat(\"DROP TABLE \" + os.Args[1]))\n\tconn.Exec(flat(\"SELECT 1\"))\n}\n",
		"through a second helper": summaryDB + trim + "func two(s string) string {\n\ts = flat(s)\n\treturn s\n}\n\nfunc main() {\n\tfmt.Print(two(\"DROP TABLE \" + os.Args[1]))\n\tconn.Exec(two(\"SELECT 1\"))\n}\n",
		"a recursive helper":      summaryDB + "func strip(s string, n int) string {\n\tif n == 0 {\n\t\treturn s\n\t}\n\treturn strip(strings.TrimSpace(s), n-1)\n}\n\nfunc main() {\n\tfmt.Print(strip(\"DROP TABLE \"+os.Args[1], 2))\n\tconn.Exec(strip(\"SELECT 1\", 2))\n}\n",
		"a test that ends it":     summaryDB + "func isDrop(s string) bool { return strings.HasPrefix(s, \"DROP\") }\n\nfunc main() {\n\tif isDrop(\"DROP TABLE \" + os.Args[1]) {\n\t\tconn.Exec(\"SELECT 1\")\n\t}\n}\n",
		"strings.Map":             "func main() {\n\tfmt.Print(strings.Map(func(r rune) rune { return r }, \"DROP TABLE \"+os.Args[1]))\n}\n",
		"a value not returned":    summaryDB + "func width(s string) int { return len(s) }\n\nfunc main() { fmt.Print(width(\"DROP TABLE \" + os.Args[1])) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": flowHead + body}), "gen/g.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// The negative controls: any other thing a helper does with its text leaves it
// unsummarised, and a summarised helper's result is still followed.
func TestAHelperThatDoesMoreStaysFollowed(t *testing.T) {
	const trim = "func flat(s string) string { return strings.TrimSpace(s) }\n\n"
	for name, body := range map[string]string{
		"its result is run":              summaryDB + trim + "func main() { conn.Exec(flat(\"DROP TABLE \" + os.Args[1])) }\n",
		"its helper's result run":        summaryDB + trim + "func two(s string) string { return flat(s) }\n\nfunc main() { conn.Exec(two(\"DROP TABLE \" + os.Args[1])) }\n",
		"it runs it":                     summaryDB + "func run(s string) string { conn.Exec(s); return s }\n\nfunc main() { fmt.Print(run(\"DROP TABLE \" + os.Args[1])) }\n",
		"it runs it through a helper":    summaryDB + "func run(s string) { conn.Exec(s) }\n\nfunc also(s string) string { run(s); return s }\n\nfunc main() { fmt.Print(also(\"DROP TABLE \" + os.Args[1])) }\n",
		"it keeps it in a variable":      summaryDB + "var held string\n\nfunc keep(s string) string { held = s; return s }\n\nfunc main() {\n\tfmt.Print(keep(\"DROP TABLE \" + os.Args[1]))\n\tconn.Exec(held)\n}\n",
		"it keeps it in a field":         summaryDB + "type box struct{ q string }\n\nvar b box\n\nfunc keep(s string) string { b.q = s; return s }\n\nfunc main() {\n\tfmt.Print(keep(\"DROP TABLE \" + os.Args[1]))\n\tconn.Exec(b.q)\n}\n",
		"it hands it to a closure value": summaryDB + "func keep(s string, f func(string)) string { f(s); return s }\n\nfunc main() { fmt.Print(keep(\"DROP TABLE \"+os.Args[1], func(q string) { conn.Exec(q) })) }\n",
		"a method of its name runs it":   summaryDB + "type M struct{}\n\nfunc (M) flat(s string) string { conn.Exec(s); return s }\n\nfunc main() { fmt.Print(M{}.flat(\"DROP TABLE \" + os.Args[1])) }\n",
		"strings.Map's result run":       summaryDB + "func main() {\n\tconn.Exec(strings.Map(func(r rune) rune { return r }, \"DROP TABLE \"+os.Args[1]))\n}\n",
		"strconv.Quote's result run":     summaryDB + "var _ = strconv.Quote\n\nfunc main() { conn.Exec(strconv.Quote(\"DROP TABLE \" + os.Args[1])) }\n",
		"it writes a slice parameter":    summaryDB + "func put(out []string, s string) { out[0] = s }\n\nfunc main() {\n\tq := make([]string, 1)\n\tput(q, \"DROP TABLE \"+os.Args[1])\n\tconn.Exec(q[0])\n}\n",
		"it writes a map parameter":      summaryDB + "func put(m map[string]string, s string) { m[\"k\"] = s }\n\nfunc main() {\n\tm := map[string]string{}\n\tput(m, \"DROP TABLE \"+os.Args[1])\n\tconn.Exec(m[\"k\"])\n}\n",
		"its own text is run":            summaryDB + "func ddl(t string) string { return \"DROP TABLE \" + t }\n\nfunc main() { conn.Exec(ddl(os.Args[1])) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			src := flowHead + body
			if strings.Contains(body, "strconv.") {
				src = strings.Replace(src, "\"strings\"\n", "\"strconv\"\n\t\"strings\"\n", 1)
			}
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "gen/g.go": src}), "gen/g.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}
