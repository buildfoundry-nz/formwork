package sqlextract_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

const tmplMod = "module example.com/m\n\ngo 1.22\n"

// visible is the shape the templates exist for: one predicate, spliced with
// the alias of the query that needs it.
const visible = "package desk\n\n// VisibleWhere is the one home of the predicate.\n" +
	"func VisibleWhere(a string) string {\n\treturn a + \".deleted_at IS NULL AND \" + a + \".status <> 'draft'\"\n}\n"

func sitesAt(sites []sqlextract.Site, line int) int {
	n := 0
	for _, s := range sites {
		if s.Line == line {
			n++
		}
	}
	return n
}

// Called with constants, a template reads as the instance it builds — at the
// call, and once per distinct set of arguments at the definition, whose body
// is then not a composition the extractor could not read.
func TestATemplateCalledWithConstantsIsReadAtBothEnds(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":       tmplMod,
		"desk/vis.go":  visible,
		"desk/read.go": "package desk\n\nfunc Q() string { return \"SELECT id FROM p WHERE \" + VisibleWhere(\"p\") }\n",
		"jobs/read.go": "package jobs\n\nimport \"example.com/m/desk\"\n\n" +
			"func R() string { return \"SELECT id FROM projects hp WHERE \" + desk.VisibleWhere(\"hp\") }\n\n" +
			"func S() string { return \"SELECT 1 FROM p WHERE \" + desk.VisibleWhere(\"p\") }\n",
	})
	cands, _, sites := extract(t, fs, "jobs/read.go")
	if !hasCandidate(cands, "SELECT id FROM projects hp WHERE hp.deleted_at IS NULL AND hp.status <> 'draft'") || len(sites) != 0 {
		t.Fatalf("the call reads whole: candidates %+v, sites %+v", cands, sites)
	}
	cands, _, sites = extract(t, fs, "desk/vis.go")
	var instances []string
	for _, c := range cands {
		if c.Line == 5 {
			instances = append(instances, c.Text)
		}
	}
	if len(instances) != 2 || len(sites) != 0 {
		t.Fatalf("the definition is judged once per argument set (p, hp) and is no site: "+
			"instances %q, sites %+v", instances, sites)
	}
}

// One caller passing a non-constant, and the definition cannot be judged
// whole: its body stays a site, and so does that call.
func TestATemplateSomeCallerFeedsANonConstantStaysUnread(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":      tmplMod,
		"desk/vis.go": visible,
		"desk/read.go": "package desk\n\nfunc run(string) {}\n\nfunc Q(alias string) {\n" +
			"\trun(VisibleWhere(\"p\"))\n\trun(VisibleWhere(alias))\n}\n",
	})
	_, _, sites := extract(t, fs, "desk/vis.go")
	if len(sites) != 1 || sites[0].Line != 5 {
		t.Fatalf("the body is a site when a caller feeds it a non-constant: %+v", sites)
	}
	cands, _, sites := extract(t, fs, "desk/read.go")
	if sitesAt(sites, 6) != 0 || sitesAt(sites, 7) != 1 {
		t.Fatalf("the constant call reads, the other is a site: %+v", sites)
	}
	if !strings.Contains(sites[len(sites)-1].Text, sqlextract.Placeholder+".deleted_at IS NULL") {
		t.Fatalf("the unread call's text is the template with the operand it could not read: %q", sites[len(sites)-1].Text)
	}
	if !hasCandidate(cands, "p.deleted_at IS NULL AND p.status <> 'draft'") {
		t.Fatalf("a bare constant call is a candidate of its own: %+v", cands)
	}
}

// A template nothing in the walk calls says nothing about what it builds.
func TestATemplateNothingCallsStaysUnread(t *testing.T) {
	fs := walkTree(t, map[string]string{"go.mod": tmplMod, "desk/vis.go": visible})
	if _, _, sites := extract(t, fs, "desk/vis.go"); len(sites) != 1 {
		t.Fatalf("an uncalled template's body is a site: %+v", sites)
	}
}

// A template calling a template passes its own parameters, so the inner one is
// instantiated from the outer one's instances.
func TestATemplateCalledFromATemplateIsInstantiatedThroughIt(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":      tmplMod,
		"desk/vis.go": visible,
		"desk/backlog.go": "package desk\n\nfunc Backlog(a string) string {\n" +
			"\treturn VisibleWhere(a) + \" AND \" + a + \".assignee IS NULL\"\n}\n",
		"desk/read.go": "package desk\n\nfunc Q() string { return \"SELECT id FROM p WHERE \" + Backlog(\"p\") }\n",
	})
	cands, _, sites := extract(t, fs, "desk/vis.go")
	if len(sites) != 0 || !hasCandidate(cands, "p.deleted_at IS NULL AND p.status <> 'draft'") {
		t.Fatalf("the inner template is judged with the outer call's argument: candidates %+v, sites %+v", cands, sites)
	}
	if _, _, sites := extract(t, fs, "desk/backlog.go"); len(sites) != 0 {
		t.Fatalf("the outer template's body, and its inner call, are judged: %+v", sites)
	}
}

// A '+' operand that is an unread template call is part of its composition's
// site; it is not a second one.
func TestAnUnreadTemplateOperandIsItsCompositionsSite(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":      tmplMod,
		"desk/vis.go": visible,
		"desk/read.go": "package desk\n\nfunc Q(alias string) string {\n" +
			"\treturn \"SELECT id FROM p WHERE \" + VisibleWhere(alias)\n}\n",
	})
	if _, _, sites := extract(t, fs, "desk/read.go"); len(sites) != 1 || sites[0].Line != 4 {
		t.Fatalf("want the one composition site: %+v", sites)
	}
}

// Without a walk no call is indexed: the body is a site as it was, and a
// constant call in the same file still reads.
func TestWithoutATreeATemplateBodyIsASite(t *testing.T) {
	src := visible + "\nfunc Q() string { return \"SELECT id FROM p WHERE \" + VisibleWhere(\"p\") }\n"
	cands, sites, err := sqlextract.FromGo("desk/vis.go", []byte(src))
	if err != nil || len(sites) != 1 || sites[0].Line != 5 {
		t.Fatalf("err %v, sites %+v", err, sites)
	}
	if !hasCandidate(cands, "SELECT id FROM p WHERE p.deleted_at IS NULL AND p.status <> 'draft'") {
		t.Fatalf("the call in the file reads: %+v", cands)
	}
}
