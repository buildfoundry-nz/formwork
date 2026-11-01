package sqlextract_test

import "testing"

func localsFile(body string) map[string]string {
	return map[string]string{"q/q.go": "package q\n\nimport \"strings\"\n\nvar _ = strings.Join\n\nfunc use(*string) {}\n\nfunc f(s string, xs []string) string {\n" + body + "\n}\n"}
}

// A local written once, from something that folds, can only hold that value.
func TestAConstantLocalReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"define", "t := \"t1\"\n\treturn \"SELECT 1 FROM \" + t", "SELECT 1 FROM t1"},
		{"var with a value", "var t = \"t1\"\n\treturn \"SELECT 1 FROM \" + t", "SELECT 1 FROM t1"},
		{"typed var", "var t string = \"t1\"\n\treturn \"SELECT 1 FROM \" + t", "SELECT 1 FROM t1"},
		{"through a local", "a := \"t\"\n\tb := a + \"1\"\n\treturn \"SELECT 1 FROM \" + b", "SELECT 1 FROM t1"},
		{"read in a closure", "t := \"t1\"\n\tg := func() string { return \"SELECT 1 FROM \" + t }\n\treturn g()", "SELECT 1 FROM t1"},
		{"join of literals", "return \"SELECT \" + strings.Join([]string{\"a\", \"b\"}, \", \") + \" FROM t1\"", "SELECT a, b FROM t1"},
		{"join of a constant local", "cols := []string{\"a\", \"b\"}\n\treturn \"SELECT \" + strings.Join(cols, \", \") + \" FROM t1\"", "SELECT a, b FROM t1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cands, _, sites := extract(t, walkTree(t, localsFile(tc.body)), "q/q.go")
			if !hasCandidate(cands, tc.want) {
				t.Fatalf("want candidate %q, got %+v", tc.want, cands)
			}
			if len(sites) != 0 {
				t.Fatalf("a constant local is read, so it is no site: %+v", sites)
			}
		})
	}
}

// Anything that could change the value stays unread.
func TestALocalThatCanChangeStaysUnread(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"reassigned":                    "t := \"t1\"\n\tt = s\n\treturn \"SELECT 1 FROM \" + t",
		"branch reassigned":             "t := \"t1\"\n\tif s != \"\" {\n\t\tt = \"t2\"\n\t}\n\treturn \"SELECT 1 FROM \" + t",
		"appended":                      "t := \"t1\"\n\tt += s\n\treturn \"SELECT 1 FROM \" + t",
		"address taken":                 "t := \"t1\"\n\tuse(&t)\n\treturn \"SELECT 1 FROM \" + t",
		"closure write":                 "t := \"t1\"\n\tg := func() { t = s }\n\tg()\n\treturn \"SELECT 1 FROM \" + t",
		"range value":                   "t := \"\"\n\tfor _, t = range xs {\n\t}\n\treturn \"SELECT 1 FROM \" + t",
		"declared no value":             "var t string\n\tt = s\n\treturn \"SELECT 1 FROM \" + t",
		"non-constant value":            "t := s\n\treturn \"SELECT 1 FROM \" + t",
		"multi assign":                  "t, u := \"t1\", s\n\t_ = u\n\treturn \"SELECT 1 FROM \" + t + u",
		"join a non-constant":           "return \"SELECT \" + strings.Join([]string{\"a\", s}, \", \") + \" FROM t1\"",
		"join a parameter":              "return \"SELECT \" + strings.Join(xs, \", \") + \" FROM t1\"",
		"join a changed local":          "cols := []string{\"a\"}\n\tcols = append(cols, s)\n\treturn \"SELECT \" + strings.Join(cols, \", \") + \" FROM t1\"",
		"join an indexed write":         "cols := []string{\"a\"}\n\tcols[0] = s\n\treturn \"SELECT \" + strings.Join(cols, \", \") + \" FROM t1\"",
		"join a non-constant separator": "return \"SELECT \" + strings.Join([]string{\"a\"}, s) + \" FROM t1\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cands, _, sites := extract(t, walkTree(t, localsFile(body)), "q/q.go")
			if len(sites) == 0 {
				t.Fatalf("a value that can change must stay a site, candidates %+v", cands)
			}
		})
	}
}
