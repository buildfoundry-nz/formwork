package sqlextract_test

import "testing"

const uuidImport = "package db\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/google/uuid\"\n)\n\n"

// A value uuid.Parse returned renders as hex and dashes only, so it cannot carry
// statement text, any more than an integer can. Where it stands in a query, the query
// parses as it will at run time and no predicate can have a table hidden in it.
func TestParsedUUIDIsReadAsAUUID(t *testing.T) {
	const want = "SELECT id FROM t WHERE org_id = '00000000-0000-0000-0000-000000000000'::uuid LIMIT 5"
	for name, body := range map[string]string{
		"concatenated": "func Q(s string) (string, error) {\n\tp, err := uuid.Parse(s)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n" +
			"\treturn \"SELECT id FROM t WHERE org_id = '\" + p.String() + \"'::uuid LIMIT 5\", nil\n}\n",
		"a Sprintf argument": "const tmpl = \"SELECT id FROM t WHERE org_id = %s LIMIT %d\"\n\nconst top = 5\n\n" +
			"func Q(s string) (string, error) {\n\tp, err := uuid.Parse(s)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n" +
			"\treturn fmt.Sprintf(tmpl, \"'\"+p.String()+\"'::uuid\", top), nil\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			src := uuidImport + "var _ = fmt.Sprint\n\n" + body
			cands, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": src}), "db/q.go")
			if len(sites) != 0 {
				t.Errorf("reported unread: %+v", sites)
			}
			if name == "concatenated" && !hasCandidate(cands, want) {
				t.Errorf("did not read %q: %+v", want, cands)
			}
		})
	}
}

// Only a value that can be nothing but a parsed uuid reads this way.
func TestUUIDLookalikesStayUnread(t *testing.T) {
	const use = "\treturn \"SELECT id FROM t WHERE org_id = '\" + p.String() + \"'::uuid\", nil\n}\n"
	for name, src := range map[string]string{
		"reassigned":              uuidImport + "var _ = fmt.Sprint\n\nfunc Q(s string, o uuid.UUID) (string, error) {\n\tp, err := uuid.Parse(s)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\tp = o\n" + use,
		"address taken":           uuidImport + "var _ = fmt.Sprint\n\nfunc use(*uuid.UUID) {}\n\nfunc Q(s string) (string, error) {\n\tp, err := uuid.Parse(s)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\tuse(&p)\n" + use,
		"another package's Parse": "package db\n\nimport uuid \"example.com/m/notuuid\"\n\nfunc Q(s string) (string, error) {\n\tp, err := uuid.Parse(s)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n" + use,
		"a parameter":             uuidImport + "var _ = fmt.Sprint\n\nfunc Q(p uuid.UUID) (string, error) {\n" + use,
		"the raw string":          uuidImport + "var _ = fmt.Sprint\n\nfunc Q(s string) (string, error) {\n\tif _, err := uuid.Parse(s); err != nil {\n\t\treturn \"\", err\n\t}\n\treturn \"SELECT id FROM t WHERE org_id = '\" + s + \"'::uuid\", nil\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, sites := extract(t, walkTree(t, map[string]string{"go.mod": closedMod, "db/q.go": src}), "db/q.go")
			if len(sites) == 0 {
				t.Error("the composition was read")
			}
		})
	}
}
