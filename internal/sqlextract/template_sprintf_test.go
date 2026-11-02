package sqlextract_test

import "testing"

const sprintfTemplate = "package db\n\nimport \"fmt\"\n\nconst cols = \"id, name\"\n\n" +
	"func list(table, order string) string {\n" +
	"\treturn fmt.Sprintf(`SELECT %s FROM %s WHERE org_id = $1 ORDER BY %s`, cols, table, order)\n}\n\n"

// A template's body may be a fmt.Sprintf of constants and its parameters: bound by
// a call whose arguments are constants, it builds one text like any other body.
func TestSprintfTemplateIsReadAtItsCall(t *testing.T) {
	files := map[string]string{
		"go.mod":  closedMod,
		"db/q.go": sprintfTemplate + "func Q() string { return list(\"t\", \"id\") }\n",
	}
	cands, _, sites := extract(t, walkTree(t, files), "db/q.go")
	if !hasCandidate(cands, "SELECT id, name FROM t WHERE org_id = $1 ORDER BY id") {
		t.Errorf("did not read the instance: %+v", cands)
	}
	if len(sites) != 0 {
		t.Errorf("reported unread: %+v", sites)
	}
}

func TestSprintfTemplateStaysUnreadWhenItCannotFold(t *testing.T) {
	for name, src := range map[string]string{
		"a caller passes a non-constant": sprintfTemplate + "func Q(order string) string { return list(\"t\", order) }\n",
		"an argument is not a parameter or a constant": "package db\n\nimport \"fmt\"\n\nvar mutable = \"t\"\n\nfunc set(s string) { mutable = s }\n\n" +
			"func list(order string) string {\n\treturn fmt.Sprintf(`SELECT id FROM %s ORDER BY %s`, mutable, order)\n}\n\n" +
			"func Q() string { return list(\"id\") }\n",
		"too few arguments": "package db\n\nimport \"fmt\"\n\n" +
			"func list(order string) string {\n\treturn fmt.Sprintf(`SELECT id FROM %s ORDER BY %s`, order)\n}\n\n" +
			"func Q() string { return list(\"id\") }\n",
		"a verb the reader does not model": "package db\n\nimport \"fmt\"\n\n" +
			"func list(order string) string {\n\treturn fmt.Sprintf(`SELECT id FROM %q ORDER BY id`, order)\n}\n\n" +
			"func Q() string { return list(\"id\") }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": src}), "db/q.go")
			if len(sites) == 0 {
				t.Error("the composition was read")
			}
		})
	}
}
