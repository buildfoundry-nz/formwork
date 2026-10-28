// scopedeclare_test.go — scope.not_declaring end to end: a rule loaded from
// YAML, a real walked tree, engine.Run, and the finding that must not be there.
//
// Its own file rather than engine_test.go's memFileSet: the whole point of this
// key is that the verdict comes from a file's CONTENT, so the test has to hand
// the engine real files whose bytes differ while their names do not carry the
// answer. Every file here is `**/*.go` under the same directory — if the skip
// were secretly keyed on path, all three would share one verdict.
package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// declTree writes a repo whose one rule bans WIDGET in every .go file, with
// the given scope.not_declaring line, then returns the loaded rules and the
// walked tree.
func declTree(t *testing.T, notDeclaring string, files map[string]string) ([]*config.Rule, *scan.FileSet) {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-widget\n    type: forbidden-pattern\n" +
			"    severity: error\n    scope:\n      include: ['**/*.go']\n" + notDeclaring +
			"    params: {pattern: WIDGET}\n    cure: do not\n",
	}
	for k, v := range files {
		all[k] = v
	}
	for rel, content := range all {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Rules, fset
}

const (
	ownerBody = "package p\n\nfunc Owner() {\n\tWIDGET()\n}\n\nfunc Sibling() {\n\tWIDGET()\n}\n"
	handBody  = "package p\n\nconst B = \"WIDGET\"\n"
)

// The load-bearing assertion, and it needs every part. The marker's
// declaration is the home and is masked; the sibling in the SAME file is
// judged, and so is the file with no marker at all. Before 0.14 the owner's
// whole file was skipped, so the sibling's WIDGET was invisible.
func TestRunNotDeclaringMasksOnlyTheDeclarationTheMarkerOpens(t *testing.T) {
	rs, fset := declTree(t, "      not_declaring: [{marker: 'func Owner(', holders: 1}]\n", map[string]string{
		"owner.go": ownerBody,
		"hand.go":  handBody,
	})
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"owner.go": 8, "hand.go": 3}
	if len(got) != len(want) {
		t.Fatalf("findings = %+v, want %v — the sibling and the unmarked file, never the owner", got, want)
	}
	for _, f := range got {
		if want[f.Path] != f.Line {
			t.Fatalf("finding %s:%d, want one of %v", f.Path, f.Line, want)
		}
	}
}

// Without the key, the same tree yields the same rule judging every use. This
// is the control: it proves the masking above is the key working rather than
// the rule being broken, misscoped, or the pattern never matching.
func TestRunWithoutNotDeclaringJudgesEveryUse(t *testing.T) {
	rs, fset := declTree(t, "", map[string]string{
		"owner.go": ownerBody,
		"hand.go":  handBody,
	})
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want 3 — with no declaration key the owner is not a home", got)
	}
}

// The holder count is pinned. A copy of the marker in another file is a
// second home, and the run says so; so is a marker nothing declares any more.
func TestRunNotDeclaringHoldersArePinned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a copied marker", map[string]string{"owner.go": ownerBody, "copy.go": "package p\n\nfunc Owner() {}\n"}, "declared by 2 place(s)"},
		{"a marker nothing declares", map[string]string{"hand.go": handBody}, "declared by 0 place(s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, fset := declTree(t, "      not_declaring: [{marker: 'func Owner(', holders: 1}]\n", tc.files)
			got, err := engine.Run(rs, fset, 1)
			if err != nil {
				t.Fatal(err)
			}
			var count bool
			for _, f := range got {
				count = count || (f.Path == "" && strings.Contains(f.Message, tc.want))
			}
			if !count {
				t.Fatalf("findings = %+v, want a scope-level %q", got, tc.want)
			}
		})
	}
}

// A fixture tree proves the marker and the home; it cannot hold the
// repository's count, so the count is not judged there.
func TestRunNotDeclaringHoldersAreNotJudgedOnAFixture(t *testing.T) {
	rs, fset := declTree(t, "      not_declaring: [{marker: 'func Owner(', holders: 5}]\n", map[string]string{"owner.go": ownerBody})
	fset.Fixture = true
	got, err := engine.Run(rs, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != 8 {
		t.Fatalf("findings = %+v, want only the sibling owner.go:8", got)
	}
}
