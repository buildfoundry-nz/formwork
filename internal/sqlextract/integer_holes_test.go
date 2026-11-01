package sqlextract_test

import (
	"strings"
	"testing"
)

// goFile wraps one function body in a compilable file.
func goFile(body string) map[string]string {
	return map[string]string{"q/q.go": "package q\n\nimport (\n\t\"fmt\"\n\t\"strconv\"\n)\n\nvar _ = fmt.Sprint\nvar _ = strconv.Itoa\n\nfunc f(n int, s string) string {\n" + body + "\n}\n"}
}

// An integer cannot carry statement text, so a hole that can only hold one is
// read as a number: the query around it parses, and no predicate can be hiding
// a table in it.
func TestAnIntegerHoleReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"Itoa operand", `return "SELECT 1 FROM t LIMIT " + strconv.Itoa(n)`, "SELECT 1 FROM t LIMIT 1"},
		{"FormatInt operand", `return "SELECT 1 FROM t LIMIT " + strconv.FormatInt(int64(n), 10)`, "SELECT 1 FROM t LIMIT 1"},
		{"FormatUint operand", `return "SELECT 1 FROM t LIMIT " + strconv.FormatUint(uint64(n), 10)`, "SELECT 1 FROM t LIMIT 1"},
		{"a positional parameter", `return "SELECT 1 FROM t WHERE a = $" + strconv.Itoa(n)`, "SELECT 1 FROM t WHERE a = $1"},
		{"d verb", `return fmt.Sprintf("SELECT 1 FROM t LIMIT %d", n)`, "SELECT 1 FROM t LIMIT 1"},
		{"d verb with width", `return fmt.Sprintf("SELECT 1 FROM t LIMIT %05d", n)`, "SELECT 1 FROM t LIMIT 1"},
		{"d verb beside a constant", `return fmt.Sprintf("SELECT %s FROM t LIMIT %d", "a", n)`, "SELECT a FROM t LIMIT 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cands, _, sites := extract(t, walkTree(t, goFile(tc.body)), "q/q.go")
			if !hasCandidate(cands, tc.want) {
				t.Fatalf("want candidate %q, got %+v", tc.want, cands)
			}
			if len(sites) != 0 {
				t.Fatalf("an integer hole is read, so it is no site: %+v", sites)
			}
		})
	}
}

// Everything that could carry text stays unread: reading any of these would let
// a table hide in a hole a predicate believes it judged.
func TestAHoleThatCouldCarryTextStaysUnread(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"s verb":                `return fmt.Sprintf("SELECT 1 FROM %s", s)`,
		"v verb":                `return fmt.Sprintf("SELECT 1 FROM %v", s)`,
		"d beside s":            `return fmt.Sprintf("SELECT 1 FROM %s LIMIT %d", s, n)`,
		"Quote":                 `return "SELECT 1 FROM t WHERE a = " + strconv.Quote(s)`,
		"AppendInt":             `return "SELECT 1 FROM t LIMIT " + string(strconv.AppendInt(nil, 1, 10))`,
		"a local named strconv": "type box struct{}\n\tstrconv := box{}\n\t_ = strconv\n\treturn \"SELECT 1 FROM t WHERE a = \" + s",
		"a bare operand":        `return "SELECT 1 FROM t WHERE a = " + s`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, sites := extract(t, walkTree(t, goFile(body)), "q/q.go")
			if len(sites) == 0 {
				t.Fatal("a hole that could carry text must stay a site")
			}
		})
	}
}

// A local shadowing the package is not the package, so its method is not an
// integer format.
func TestAShadowedStrconvIsNoIntegerHole(t *testing.T) {
	t.Parallel()
	body := "strconv := struct{ Itoa func(int) string }{}\n\treturn \"SELECT 1 FROM t WHERE a = \" + strconv.Itoa(n)"
	cands, _, sites := extract(t, walkTree(t, goFile(body)), "q/q.go")
	for _, c := range cands {
		if strings.HasSuffix(c.Text, "a = 1") && !c.Partial {
			t.Fatalf("a shadowed strconv read as an integer: %+v", c)
		}
	}
	if len(sites) == 0 {
		t.Fatal("a shadowed strconv's call must stay a site")
	}
}
