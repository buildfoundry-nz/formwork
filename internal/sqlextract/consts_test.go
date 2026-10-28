package sqlextract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// walkTree writes files under a temp root and walks it, so the extractor reads
// constants through the same scan.File.Tree a rule does.
func walkTree(t *testing.T, files map[string]string) *scan.FileSet {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// extract runs FromGo and FromGoReassembled over rel through the walk.
func extract(t *testing.T, fs *scan.FileSet, rel string) (cands, recands []sqlextract.Candidate, sites []sqlextract.Site) {
	t.Helper()
	f := fs.File(rel)
	if f == nil {
		t.Fatalf("%s is not in the walk", rel)
	}
	src, err := f.Content()
	if err != nil {
		t.Fatal(err)
	}
	cands, sites, err = sqlextract.FromGoIn(f.Tree(), rel, src)
	if err != nil {
		t.Fatal(err)
	}
	recands, _, err = sqlextract.FromGoReassembledIn(f.Tree(), rel, src)
	if err != nil {
		t.Fatal(err)
	}
	return cands, recands, sites
}

func hasCandidate(cands []sqlextract.Candidate, text string) bool {
	for _, c := range cands {
		if c.Text == text && !c.Partial {
			return true
		}
	}
	return false
}

const query = "SELECT id, name FROM t WHERE org_id = $1"

// A composition made of literals and named constants is a Go constant
// expression, and both extractors read it whole — wherever the constant is
// declared. Each case composes the same query; what varies is where its parts
// live.
func TestCompositionOfConstantsIsRead(t *testing.T) {
	mod := "module example.com/m\n\ngo 1.22\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"same-file", map[string]string{
			"go.mod": mod,
			"db/q.go": "package db\n\nconst cols = \"id, name\"\n\n" +
				"func Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"function-local", map[string]string{
			"go.mod": mod,
			"db/q.go": "package db\n\nfunc Q() string {\n\tconst cols = \"id, name\"\n" +
				"\treturn \"SELECT \" + cols + \" FROM t WHERE org_id = $1\"\n}\n",
		}},
		{"sibling-file", map[string]string{
			"go.mod":     mod,
			"db/cols.go": "package db\n\nconst cols = \"id, name\"\n",
			"db/q.go":    "package db\n\nfunc Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"constant-of-constants", map[string]string{
			"go.mod":     mod,
			"db/cols.go": "package db\n\nconst (\n\tid = \"id\"\n\tcols = id + \", name\"\n)\n",
			"db/q.go":    "package db\n\nfunc Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"imported-package", map[string]string{
			"go.mod":          mod,
			"scan/vault/c.go": "package scanvault\n\nconst Cols = \"id, name\"\n",
			"db/q.go": "package db\n\nimport \"example.com/m/scan/vault\"\n\n" +
				"func Q() string { return \"SELECT \" + scanvault.Cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"aliased-import", map[string]string{
			"go.mod":     mod,
			"shape/c.go": "package shape\n\nconst Cols = \"id, name\"\n",
			"db/q.go": "package db\n\nimport sh \"example.com/m/shape\"\n\n" +
				"func Q() string { return \"SELECT \" + sh.Cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"replaced-module", map[string]string{
			"app/go.mod":       "module example.com/app\n\ngo 1.22\n\nreplace example.com/shared => ../shared\n",
			"shared/go.mod":    "module example.com/shared\n\ngo 1.22\n",
			"shared/sqlc/c.go": "package sqlc\n\nconst Cols = \"id, name\"\n",
			"app/db/q.go": "package db\n\nimport \"example.com/shared/sqlc\"\n\n" +
				"func Q() string { return \"SELECT \" + sqlc.Cols + \" FROM t WHERE org_id = $1\" }\n",
		}},
		{"sprintf-of-constants", map[string]string{
			"go.mod":     mod,
			"db/cols.go": "package db\n\nconst cols = \"id, name\"\n",
			"db/q.go": "package db\n\nimport \"fmt\"\n\n" +
				"func Q() string { return fmt.Sprintf(\"SELECT %s FROM t WHERE org_id = $1\", cols) }\n",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := walkTree(t, tc.files)
			rel := "db/q.go"
			if _, ok := tc.files["app/db/q.go"]; ok {
				rel = "app/db/q.go"
			}
			cands, recands, sites := extract(t, fs, rel)
			if !hasCandidate(cands, query) {
				t.Errorf("FromGo did not read the composition whole: %+v", cands)
			}
			if !hasCandidate(recands, query) {
				t.Errorf("FromGoReassembled did not read the composition whole: %+v", recands)
			}
			if len(sites) != 0 {
				t.Errorf("a composition FromGo read is not one it could not read: %+v", sites)
			}
		})
	}
}

// What the Go compiler would not fold, the extractor does not either. Each case
// holds a name that is not a constant at the composition, or one whose value
// the extractor cannot know, and the composition stays one it could not read.
func TestNonConstantOperandsStayUnread(t *testing.T) {
	mod := "module example.com/m\n\ngo 1.22\n"
	compose := "func Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"variable", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nvar cols = \"id, name\"\n\n" + compose}},
		{"local-shadows-the-constant", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nconst cols = \"id, name\"\n\nfunc Q(cols string) string {\n" +
				"\treturn \"SELECT \" + cols + \" FROM t WHERE org_id = $1\"\n}\n"}},
		{"declared-in-two-files", map[string]string{"go.mod": mod,
			"db/c_linux.go":  "package db\n\nconst cols = \"id, name\"\n",
			"db/c_darwin.go": "package db\n\nconst cols = \"id\"\n",
			"db/q.go":        "package db\n\n" + compose}},
		{"declared-only-in-a-test-file", map[string]string{"go.mod": mod,
			"db/c_test.go": "package db\n\nconst cols = \"id, name\"\n",
			"db/q.go":      "package db\n\n" + compose}},
		{"unexported-in-another-package", map[string]string{"go.mod": mod,
			"shape/c.go": "package shape\n\nconst cols = \"id, name\"\n",
			"db/q.go": "package db\n\nimport \"example.com/m/shape\"\n\n" +
				"func Q() string { return \"SELECT \" + shape.cols + \" FROM t WHERE org_id = $1\" }\n"}},
		{"module-outside-the-tree", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nimport \"example.org/elsewhere/shape\"\n\n" +
				"func Q() string { return \"SELECT \" + shape.Cols + \" FROM t WHERE org_id = $1\" }\n"}},
		{"sprintf-of-a-variable", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nimport \"fmt\"\n\nvar cols = \"id, name\"\n\n" +
				"func Q() string { return fmt.Sprintf(\"SELECT %s FROM t WHERE org_id = $1\", cols) }\n"}},
		{"sprintf-verb-that-is-not-plain", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nimport \"fmt\"\n\nconst cols = \"id, name\"\n\n" +
				"func Q() string { return fmt.Sprintf(\"SELECT %q FROM t WHERE org_id = $1\", cols) }\n"}},
		{"sprintf-argument-index", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nimport \"fmt\"\n\nconst cols = \"id, name\"\n\n" +
				"func Q() string { return fmt.Sprintf(\"SELECT %[1]s FROM t WHERE org_id = $1\", cols) }\n"}},
		{"call-of-a-function", map[string]string{"go.mod": mod,
			"db/q.go": "package db\n\nfunc colsOf() string { return \"id, name\" }\n\n" +
				"func Q() string { return \"SELECT \" + colsOf() + \" FROM t WHERE org_id = $1\" }\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := walkTree(t, tc.files)
			cands, _, sites := extract(t, fs, "db/q.go")
			if hasCandidate(cands, query) {
				t.Errorf("read a composition whose operand is not a constant: %+v", cands)
			}
			if len(sites) != 1 {
				t.Errorf("want the composition reported as unread, got %+v", sites)
			}
		})
	}
}

// A conversion to a string type is part of a constant expression; a call that
// merely looks like one is not.
func TestConversionToAStringTypeIsRead(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":   "module example.com/m\n\ngo 1.22\n",
		"st/st.go": "package st\n\ntype Status string\n\nconst Active Status = \"active\"\n",
		"db/q.go": "package db\n\nimport \"example.com/m/st\"\n\n" +
			"func Q() string { return \"SELECT id FROM t WHERE status = '\" + string(st.Active) + \"'\" }\n",
	})
	cands, _, sites := extract(t, fs, "db/q.go")
	if !hasCandidate(cands, "SELECT id FROM t WHERE status = 'active'") || len(sites) != 0 {
		t.Fatalf("string(st.Active) is a constant: candidates %+v, sites %+v", cands, sites)
	}
}

// A constant's value is its declaration's candidate. Read at a bare use it
// would be judged twice, at two lines; read as a fragment inside a composition
// the extractor still cannot read, it would be a second fragment of the same
// text.
func TestAConstantsValueIsNotACandidateAtItsUse(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.22\n",
		"db/cols.go": "package db\n\nconst cols = \"id, name\"\nconst listQ = \"SELECT id FROM t\"\n",
		"db/q.go": "package db\n\nimport \"fmt\"\n\nfunc run(string) {}\n\nfunc Q(v string, n int) string {\n\trun(listQ)\n" +
			"\trun(cols + v)\n\trun(fmt.Sprintf(\"%s LIMIT %d\", listQ, n))\n" +
			"\treturn \"SELECT \" + cols + \" FROM t WHERE x = \" + v\n}\n",
	})
	cands, recands, _ := extract(t, fs, "db/q.go")
	for _, set := range [][]sqlextract.Candidate{cands, recands} {
		for _, c := range set {
			if c.Text == "id, name" || c.Text == "SELECT id FROM t" {
				t.Errorf("a constant's value became a candidate at its use (line %d): %+v", c.Line, set)
			}
		}
	}
}

// Without a walk the file's own constants are still read; nothing else is.
func TestWithoutATreeOnlyTheFilesOwnConstantsAreRead(t *testing.T) {
	own := "package db\n\nconst cols = \"id, name\"\n\nfunc Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n"
	cands, sites, err := sqlextract.FromGo("db/q.go", []byte(own))
	if err != nil || !hasCandidate(cands, query) || len(sites) != 0 {
		t.Fatalf("the file's own constant: err %v, candidates %+v, sites %+v", err, cands, sites)
	}
	sibling := "package db\n\nfunc Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n"
	cands, sites, err = sqlextract.FromGo("db/q.go", []byte(sibling))
	if err != nil || hasCandidate(cands, query) || len(sites) != 1 {
		t.Fatalf("a sibling's constant with no tree to read it from: err %v, candidates %+v, sites %+v", err, cands, sites)
	}
}

// The walk a range scan restricts is still the walk a constant is read from: a
// change to the composing file alone must read the sibling it did not touch.
func TestARestrictedRunReadsConstantsFromTheWholeWalk(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.22\n",
		"db/cols.go": "package db\n\nconst cols = \"id, name\"\n",
		"db/q.go":    "package db\n\nfunc Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n",
	})
	only := fs.Restrict(map[string]bool{"db/q.go": true})
	if len(only.Files) != 1 {
		t.Fatalf("premise: the restricted run holds the composing file alone: %d", len(only.Files))
	}
	f := only.Files[0]
	src, _ := f.Content()
	cands, _, err := sqlextract.FromGoIn(f.Tree(), f.Path(), src)
	if err != nil || !hasCandidate(cands, query) {
		t.Fatalf("err %v, candidates %+v", err, cands)
	}
	if strings.Contains(f.Tree().Root, "") && f.Tree() != fs {
		t.Fatalf("Tree is the walk, not the restriction")
	}
}

// Rules check files from a worker pool, and every one of them reads the walk's
// constant tables: concurrent readers build each package once and agree.
func TestConstantTablesAreSafeForConcurrentReaders(t *testing.T) {
	fs := walkTree(t, map[string]string{
		"go.mod":          "module example.com/m\n\ngo 1.22\n",
		"scan/vault/c.go": "package scanvault\n\nconst Cols = \"id, name\"\n",
		"db/cols.go":      "package db\n\nconst cols = \"id, name\"\n",
		"db/q.go": "package db\n\nimport \"example.com/m/scan/vault\"\n\n" +
			"func Q() string { return \"SELECT \" + cols + \" FROM t WHERE org_id = $1\" }\n\n" +
			"func R() string { return \"SELECT \" + scanvault.Cols + \" FROM t WHERE org_id = $1\" }\n",
	})
	f := fs.File("db/q.go")
	src, _ := f.Content()
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cands, _, err := sqlextract.FromGoIn(f.Tree(), f.Path(), src)
			n := 0
			for _, c := range cands {
				if c.Text == query {
					n++
				}
			}
			if err != nil || n != 2 {
				errs <- fmt.Sprintf("err %v, %d whole reads of 2", err, n)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
