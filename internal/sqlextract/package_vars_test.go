package sqlextract_test

import "testing"

const useCols = "func Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n"

// An unexported package variable its package never writes after declaring it
// can only hold its initialiser, so it reads the way a constant does.
func TestUnwrittenPackageVarIsRead(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"same file": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\n" + useCols,
		},
		"sibling file": {
			"db/cols.go": "package db\n\nvar cols = \"id, name\"\n",
			"db/q.go":    "package db\n\n" + useCols,
		},
		"var of a var": {
			"db/q.go": "package db\n\nvar id = \"id\"\n\nvar cols = id + \", name\"\n\n" + useCols,
		},
		"typed string": {
			"db/q.go": "package db\n\nvar cols string = \"id, name\"\n\n" + useCols,
		},
		"template call": {
			"db/q.go": "package db\n\nfunc list(a string) string { return a + \", name\" }\n\n" +
				"var cols = list(\"id\")\n\n" + useCols,
		},
		"written only by a test": {
			"db/q.go":      "package db\n\nvar cols = \"id, name\"\n\n" + useCols,
			"db/q_test.go": "package db\n\nfunc set() { cols = \"x\" }\n",
		},
		"a local of the same name is a different variable": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\n" + useCols +
				"func other() string {\n\tcols := \"x\"\n\treturn cols\n}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			files["go.mod"] = "module example.com/m\n\ngo 1.22\n"
			cands, _, sites := extract(t, walkTree(t, files), "db/q.go")
			if !hasCandidate(cands, query) {
				t.Errorf("composition was not read: %+v", cands)
			}
			if len(sites) != 0 {
				t.Errorf("composition was reported unread: %+v", sites)
			}
		})
	}
}

// Anything that could leave the variable holding another value keeps the
// composition unread, in whichever file of the package the write is.
func TestWrittenPackageVarIsNotRead(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"assigned": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\nfunc set(s string) { cols = s }\n\n" + useCols,
		},
		"assigned in init": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\nfunc init() { cols = \"x\" }\n\n" + useCols,
		},
		"assigned in a sibling file": {
			"db/cols.go": "package db\n\nvar cols = \"id, name\"\n\nfunc set(s string) { cols = s }\n",
			"db/q.go":    "package db\n\n" + useCols,
		},
		"appended": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\nfunc set(s string) { cols += s }\n\n" + useCols,
		},
		"address taken": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\nvar p = &cols\n\n" + useCols,
		},
		"range target": {
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\nfunc set(xs []string) {\n\tfor _, cols = range xs {\n\t}\n}\n\n" + useCols,
		},
		"exported, so any package can write it": {
			"db/q.go": "package db\n\nvar Cols = \"id, name\"\n\n" +
				"func Q() string { return \"SELECT \" + Cols + \" FROM t WHERE org_id = $1\" }\n",
		},
		"declared twice across build files": {
			"db/a.go": "package db\n\nvar cols = \"id, name\"\n",
			"db/b.go": "package db\n\nvar cols = \"other\"\n",
			"db/q.go": "package db\n\n" + useCols,
		},
		"non-constant initialiser": {
			"db/q.go": "package db\n\nfunc load() string { return lookup() }\n\nvar cols = load()\n\n" + useCols,
		},
		"another type": {
			"db/q.go": "package db\n\ntype name struct{}\n\nvar cols name = \"id, name\"\n\n" + useCols,
		},
		"no value": {
			"db/q.go": "package db\n\nvar cols string\n\n" + useCols,
		},
		"pair from one call": {
			"db/q.go": "package db\n\nfunc two() (string, string) { return \"id\", \"name\" }\n\nvar cols, other = two()\n\n" + useCols,
		},
	} {
		t.Run(name, func(t *testing.T) {
			files["go.mod"] = "module example.com/m\n\ngo 1.22\n"
			cands, _, sites := extract(t, walkTree(t, files), "db/q.go")
			if hasCandidate(cands, query) {
				t.Errorf("composition read as if the variable held only its initialiser: %+v", cands)
			}
			if len(sites) == 0 {
				t.Error("composition was not reported unread")
			}
		})
	}
}
