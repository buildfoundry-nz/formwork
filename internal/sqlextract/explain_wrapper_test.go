package sqlextract_test

import "testing"

const explainFile = "package db\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n\n"

// EXPLAIN around another statement adds no statement of its own. The text it wraps
// is judged where it is built, so the wrapper is not a composition to read.
func TestExplainWrapperIsNotASite(t *testing.T) {
	for name, body := range map[string]string{
		"concatenated":    "func Q(s string) string { return \"EXPLAIN \" + s }\n",
		"with options":    "func Q(s string) string { return \"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) \" + s }\n",
		"bare analyze":    "func Q(s string) string { return \"EXPLAIN ANALYZE \" + s }\n",
		"a constant":      "const prefix = \"EXPLAIN (FORMAT JSON) \"\n\nfunc Q(s string) string { return prefix + s }\n",
		"a Sprintf":       "func Q(s string) string { return fmt.Sprintf(\"EXPLAIN (FORMAT JSON) %s\", s) }\n",
		"a closed prefix": "func prefix(a bool) string {\n\tif a {\n\t\treturn \"EXPLAIN (ANALYZE) \"\n\t}\n\treturn \"EXPLAIN \"\n}\n\nfunc Q(a bool, s string) string { return prefix(a) + s }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": explainFile + body}), "db/q.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
		})
	}
}

// Anything more than the wrapper around one whole hole stays a composition.
func TestExplainWithMoreThanAWrapperStaysASite(t *testing.T) {
	for name, body := range map[string]string{
		"the statement started":  "func Q(s string) string { return \"EXPLAIN SELECT * FROM t WHERE a = \" + s }\n",
		"a second statement":     "func Q(s string) string { return \"EXPLAIN SELECT 1; DELETE FROM t WHERE a = \" + s }\n",
		"text after the hole":    "func Q(s string) string { return \"EXPLAIN \" + s + \" LIMIT 1\" }\n",
		"options after the hole": "func Q(s string) string { return fmt.Sprintf(\"EXPLAIN %s LIMIT 1\", s) }\n",
		"two holes":              "func Q(a, b string) string { return fmt.Sprintf(\"EXPLAIN %s %s\", a, b) }\n",
		"not a wrapper":          "func Q(s string) string { return \"EXPLAINED SELECT \" + s }\n",
		"an option hole":         "func Q(o, s string) string { return \"EXPLAIN (\" + o + \") \" + s }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": explainFile + body}), "db/q.go")
			if len(sites) == 0 {
				t.Error("the composition was not reported unread")
			}
		})
	}
}
