package sqlextract_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

func TestRepeatedWalkExtractionDoesNotRebuildSyntax(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*scan.FileSet, string, []byte) ([]sqlextract.Candidate, []sqlextract.Site, error)
	}{
		{"constant", sqlextract.FromGoIn},
		{"reassembled", sqlextract.FromGoReassembledIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := walkTree(t, map[string]string{"q.go": "package db\nfunc Q() string { return \"SELECT id FROM things\" }\n"})
			f := fs.File("q.go")
			src, err := f.Content()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := tc.run(fs, f.Path(), src); err != nil {
				t.Fatal(err)
			}
			// A repeated request needs detached result slices, not another AST,
			// object-resolution table, constant fold, and expression traversal.
			allocs := testing.AllocsPerRun(10, func() {
				cands, _, err := tc.run(fs, f.Path(), src)
				if err != nil || !hasCandidate(cands, "SELECT id FROM things") {
					t.Fatalf("candidates=%+v err=%v", cands, err)
				}
			})
			if allocs > 16 {
				t.Fatalf("warm extraction allocated %.0f times; cached results need at most 16 allocations", allocs)
			}
		})
	}
}

func TestWalkExtractionKeepsSourcesPathsAndWalksDistinct(t *testing.T) {
	source := "package db\nfunc Q() string { return \"SELECT id FROM \" + table }\n"
	fs := walkTree(t, map[string]string{
		"one/q.go": source, "one/table.go": "package db\nconst table = \"one\"\n",
		"two/q.go": source, "two/table.go": "package db\nconst table = \"two\"\n",
	})
	for _, rel := range []string{"one/q.go", "two/q.go"} {
		cands, _, err := sqlextract.FromGoIn(fs, rel, []byte(source))
		want := "SELECT id FROM one"
		if rel == "two/q.go" {
			want = "SELECT id FROM two"
		}
		if err != nil || !hasCandidate(cands, want) {
			t.Fatalf("%s: candidates=%+v err=%v", rel, cands, err)
		}
	}
	other := walkTree(t, map[string]string{"one/q.go": source, "one/table.go": "package db\nconst table = \"other\"\n"})
	cands, _, err := sqlextract.FromGoIn(other, "one/q.go", []byte(source))
	if err != nil || !hasCandidate(cands, "SELECT id FROM other") {
		t.Fatalf("other walk: candidates=%+v err=%v", cands, err)
	}
	changed := []byte("package db\nfunc Q() string { return \"SELECT changed FROM things\" }\n")
	cands, _, err = sqlextract.FromGoIn(fs, "one/q.go", changed)
	if err != nil || !hasCandidate(cands, "SELECT changed FROM things") {
		t.Fatalf("changed source: candidates=%+v err=%v", cands, err)
	}
	if _, _, err := sqlextract.FromGoIn(fs, "one/q.go", []byte("package db\nfunc Q(")); err == nil {
		t.Fatal("changed invalid source reused a successful extraction")
	}
}

func TestWalkExtractionReturnsDetachedCandidatesAndSites(t *testing.T) {
	fs := walkTree(t, map[string]string{"q.go": "package db\nfunc Q(s string) string { return \"SELECT \" + s + \" FROM things\" }\n"})
	f := fs.File("q.go")
	src, err := f.Content()
	if err != nil {
		t.Fatal(err)
	}
	wantCands, wantSites, err := sqlextract.FromGoIn(fs, f.Path(), src)
	if err != nil || len(wantCands) == 0 || len(wantSites) == 0 {
		t.Fatalf("premise: candidates=%+v sites=%+v err=%v", wantCands, wantSites, err)
	}
	wantCands, wantSites = slices.Clone(wantCands), slices.Clone(wantSites)
	cands, sites, err := sqlextract.FromGoIn(fs, f.Path(), src)
	if err != nil {
		t.Fatal(err)
	}
	cands[0].Text = "caller changed candidate"
	sites[0].Key = "caller changed site"
	gotCands, gotSites, err := sqlextract.FromGoIn(fs, f.Path(), src)
	if err != nil || !reflect.DeepEqual(gotCands, wantCands) || !reflect.DeepEqual(gotSites, wantSites) {
		t.Fatalf("caller mutation leaked: candidates=%+v sites=%+v err=%v", gotCands, gotSites, err)
	}
}
