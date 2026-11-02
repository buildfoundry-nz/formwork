package sqlextract_test

import (
	"strings"
	"testing"
)

const closedMod = "module example.com/m\n\ngo 1.22\n"

const closedFilter = `func filter(f Filters) string {
	switch {
	case f.Unit == "":
		return ""
	case f.Unit == "none":
		return " AND unit IS NULL"
	default:
		return " AND unit = @unit"
	}
}

type Filters struct{ Unit string }

`

const closedQuery = `func Q(f Filters) string { return "SELECT id FROM t WHERE org_id = $1" + filter(f) }
`

func texts(t *testing.T, files map[string]string, rel string) (full map[string]bool, sites int) {
	t.Helper()
	files["go.mod"] = closedMod
	cands, _, gotSites := extract(t, walkTree(t, files), rel)
	full = map[string]bool{}
	for _, c := range cands {
		if !c.Partial {
			full[c.Text] = true
		}
	}
	return full, len(gotSites)
}

// A function whose every return is a constant can only ever produce one of those
// constants, whatever it is passed and however it chooses. A composition calling
// it is judged once per value.
func TestClosedHelperIsReadOncePerReturn(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"same file": {"db/q.go": "package db\n\n" + closedFilter + closedQuery},
		"sibling file": {
			"db/f.go": "package db\n\n" + closedFilter,
			"db/q.go": "package db\n\n" + closedQuery,
		},
	} {
		t.Run(name, func(t *testing.T) {
			full, sites := texts(t, files, "db/q.go")
			if sites != 0 {
				t.Errorf("composition reported unread %d times", sites)
			}
			_, reassembled, _ := extract(t, walkTree(t, files), "db/q.go")
			if !hasCandidate(reassembled, "SELECT id FROM t WHERE org_id = $1 AND unit IS NULL") {
				t.Errorf("reassembly did not read the second value: %+v", reassembled)
			}
			for _, want := range []string{
				"SELECT id FROM t WHERE org_id = $1",
				"SELECT id FROM t WHERE org_id = $1 AND unit IS NULL",
				"SELECT id FROM t WHERE org_id = $1 AND unit = @unit",
			} {
				if !full[want] {
					t.Errorf("did not read %q: %v", want, full)
				}
			}
		})
	}
}

func TestClosedHelperInAnotherPackageIsRead(t *testing.T) {
	full, sites := texts(t, map[string]string{
		"filters/f.go": "package filters\n\ntype F struct{ On bool }\n\n" +
			"func Clause(f F) string {\n\tif f.On {\n\t\treturn \" AND on\"\n\t}\n\treturn \"\"\n}\n",
		"db/q.go": "package db\n\nimport \"example.com/m/filters\"\n\n" +
			"func Q(f filters.F) string { return \"SELECT id FROM t WHERE org_id = $1\" + filters.Clause(f) }\n",
	}, "db/q.go")
	if sites != 0 || !full["SELECT id FROM t WHERE org_id = $1 AND on"] || !full["SELECT id FROM t WHERE org_id = $1"] {
		t.Errorf("sites %d, read %v", sites, full)
	}
}

// A helper that returns another closed helper's result, a wrapper among them, can
// produce every value that helper can and nothing else.
func TestClosedHelperMayDelegate(t *testing.T) {
	full, sites := texts(t, map[string]string{
		"filters/f.go": "package filters\n\nfunc Clause(n int) string {\n\tif n > 0 {\n\t\treturn \" AND b\"\n\t}\n\treturn \"\"\n}\n",
		"db/q.go": "package db\n\nimport \"example.com/m/filters\"\n\n" +
			"func wrapper(n int) string { return filters.Clause(n) }\n\n" +
			"func outer(off bool, n int) string {\n\tif off {\n\t\treturn \" AND FALSE\"\n\t}\n\treturn wrapper(n)\n}\n\n" +
			"func Q(off bool, n int) string { return \"SELECT id FROM t WHERE org_id = $1\" + outer(off, n) }\n",
	}, "db/q.go")
	if sites != 0 {
		t.Errorf("composition reported unread %d times", sites)
	}
	const base = "SELECT id FROM t WHERE org_id = $1"
	for _, want := range []string{base, base + " AND b", base + " AND FALSE"} {
		if !full[want] {
			t.Errorf("did not read %q: %v", want, full)
		}
	}
}

// The worlds are linear, as branch worlds are: three helpers of two values read in
// five worlds, not eight, with every value judged and both all-on and all-off read.
func TestClosedHelpersAreNeverMultiplied(t *testing.T) {
	full, sites := texts(t, map[string]string{"db/q.go": `package db

func a(on bool) string {
	if on {
		return " AND a"
	}
	return ""
}

func b(on bool) string {
	if on {
		return " AND b"
	}
	return ""
}

func c(on bool) string {
	if on {
		return " AND c"
	}
	return ""
}

func Q(x, y, z bool) string { return "SELECT id FROM t WHERE org_id = $1" + a(x) + b(y) + c(z) }
`}, "db/q.go")
	if sites != 0 {
		t.Errorf("composition reported unread %d times", sites)
	}
	const base = "SELECT id FROM t WHERE org_id = $1"
	for _, want := range []string{
		base + " AND a AND b AND c", // all at their first value
		base,                        // all at their last
		base + " AND b AND c",       // each varied on its own
		base + " AND a AND c",
		base + " AND a AND b",
	} {
		if !full[want] {
			t.Errorf("did not read %q: %v", want, full)
		}
	}
	if full[base+" AND a"] || full[base+" AND b"] || full[base+" AND c"] {
		t.Errorf("formed a world past the linear bound: %v", full)
	}
}

// Anything that could make the result other than one of its constants keeps the
// composition unread.
func TestOpenHelperStaysUnread(t *testing.T) {
	for name, body := range map[string]string{
		"a return that is not constant": "func filter(s string, on bool) string {\n\tif on {\n\t\treturn \" AND a\"\n\t}\n\treturn s\n}\n",
		"a return of a variable":        "func filter(on bool) string {\n\tv := pick()\n\tif on {\n\t\treturn \" AND a\"\n\t}\n\treturn v\n}\n",
		"a call to something else":      "func filter(on bool) string {\n\tif on {\n\t\treturn pick()\n\t}\n\treturn \"\"\n}\n",
		"a named result":                "func filter(on bool) (out string) {\n\tif on {\n\t\treturn \" AND a\"\n\t}\n\tout = pick()\n\treturn\n}\n",
		"two results":                   "func filter(on bool) (string, error) {\n\treturn \" AND a\", nil\n}\n",
		"a concatenation with a helper": "func filter(on bool) string {\n\tif on {\n\t\treturn \" AND \" + inner(on)\n\t}\n\treturn \"\"\n}\n\nfunc inner(on bool) string {\n\tif on {\n\t\treturn \"a\"\n\t}\n\treturn \"b\"\n}\n",
		"a helper that calls itself":    "func filter(on bool) string {\n\tif on {\n\t\treturn filter(!on)\n\t}\n\treturn filter(on)\n}\n",
		"delegating to an open helper":  "func filter(on bool) string {\n\tif on {\n\t\treturn inner()\n\t}\n\treturn \"\"\n}\n\nfunc inner() string {\n\treturn pick()\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			call := "func Q(on bool) string { return \"SELECT id FROM t WHERE org_id = $1\" + filter(on) }\n"
			if name == "a return that is not constant" {
				call = "func Q(on bool) string { return \"SELECT id FROM t WHERE org_id = $1\" + filter(\"x\", on) }\n"
			}
			full, sites := texts(t, map[string]string{
				"db/q.go": "package db\n\nfunc pick() string { return lookup() }\n\n" + body + "\n" + call,
			}, "db/q.go")
			if sites == 0 {
				t.Errorf("composition was read: %v", full)
			}
		})
	}
}

// A method's result can depend on its receiver through more than its returns, and
// this table has no types to follow one, so it is not read.
func TestMethodIsNotAClosedHelper(t *testing.T) {
	_, sites := texts(t, map[string]string{"db/q.go": `package db

type F struct{ on bool }

func (f F) clause() string {
	if f.on {
		return " AND a"
	}
	return ""
}

func Q(f F) string { return "SELECT id FROM t WHERE org_id = $1" + f.clause() }
`}, "db/q.go")
	if sites == 0 {
		t.Error("a method call was read as a closed helper")
	}
}

// Past the cap the worlds are not truncated: dropping one would silence it, so the
// composition goes back to being one the rule cannot read.
func TestTooManyClosedValuesStayUnread(t *testing.T) {
	var b strings.Builder
	b.WriteString("package db\n\nfunc many(n int) string {\n\tswitch n {\n")
	for i := 0; i < 300; i++ {
		b.WriteString("\tcase ")
		b.WriteString(strings.Repeat("1", 1))
		b.WriteString(" + ")
		b.WriteString(itoa(i))
		b.WriteString(":\n\t\treturn \" AND c")
		b.WriteString(itoa(i))
		b.WriteString("\"\n")
	}
	b.WriteString("\t}\n\treturn \"\"\n}\n\nfunc Q(n int) string { return \"SELECT id FROM t WHERE org_id = $1\" + many(n) }\n")
	_, sites := texts(t, map[string]string{"db/q.go": b.String()}, "db/q.go")
	if sites == 0 {
		t.Error("a helper past the cap was read in part")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}
