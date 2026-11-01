package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/declspan"
)

// scope.not_declaring is the declaration-keyed half of scope: the one scope
// test a path cannot answer. These pin the LOAD half — what the key decodes
// to, what it refuses, and that a marker names the declaration it opens, not
// the file that holds it.

func declRepo(t *testing.T, scope string) string {
	t.Helper()
	return writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": "rules:\n  - id: no-widget\n    type: forbidden-pattern\n" +
			"    scope: " + scope + "\n    params: {pattern: WIDGET}\n",
	})
}

func TestScopeNotDeclaringDefaultsToNone(t *testing.T) {
	cfg, err := config.Load(declRepo(t, "{include: ['**/*.go']}"))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]
	if got := r.NotDeclaring(); len(got) != 0 {
		t.Fatalf("NotDeclaring() = %v, want empty — an undeclared key must leave the rule exactly as it was", got)
	}
	if r.WholeTreeInvariant() {
		t.Fatal("a plain forbidden-pattern became a whole-tree invariant without declaring a marker")
	}
}

func TestScopeNotDeclaringIsCarriedWithItsHolders(t *testing.T) {
	cfg, err := config.Load(declRepo(t, "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner(', holders: 2}]}"))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]
	got := r.NotDeclaringEntries()
	if len(got) != 1 || got[0].Marker != "func Owner(" || got[0].Holders != 2 {
		t.Fatalf("NotDeclaringEntries() = %+v, want the one entry verbatim", got)
	}
	// The holder count is a fact about the whole scope, which a changeset scan
	// cannot see, so the rule runs over the tracked tree under --staged.
	if !r.WholeTreeInvariant() {
		t.Fatal("a rule pinning not_declaring holders must be a whole-tree invariant")
	}
}

// The whole point: a marker names the DECLARATION its line opens. The home is
// that declaration alone; the sibling in the same file is not a home.
func TestHomesAreTheDeclarationsTheMarkerOpens(t *testing.T) {
	cfg, err := config.Load(declRepo(t, "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner(', holders: 1}]}"))
	if err != nil {
		t.Fatal(err)
	}
	src := []byte("package p\n\nfunc Owner() {\n\tWIDGET()\n}\n\nfunc Sibling() {\n\tWIDGET()\n}\n")
	hs := cfg.Rules[0].Homes("p.go", src)
	if len(hs) != 1 || hs[0].Entry != 0 || hs[0].Span != (declspan.Span{Start: 3, End: 5}) {
		t.Fatalf("Homes = %+v, want the one Owner declaration, lines 3-5", hs)
	}
	if got := cfg.Rules[0].Homes("q.go", []byte("package q\n\nfunc Other() {}\n")); len(got) != 0 {
		t.Fatalf("Homes(no marker) = %+v, want none", got)
	}
}

// A path that Applies() covers must stay covered: not_declaring subtracts on
// content alone and must not quietly change the path test.
func TestNotDeclaringDoesNotAlterPathScope(t *testing.T) {
	cfg, err := config.Load(declRepo(t, "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner(', holders: 1}]}"))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]
	if !r.Applies("api/thing.go") {
		t.Fatal("Applies(api/thing.go) = false, want true — not_declaring must not narrow the path scope")
	}
	if r.Applies("api/thing.dart") {
		t.Fatal("Applies(api/thing.dart) = true, want false — include still governs")
	}
}

// Every refusal below is an edit that did not finish. The bare-string case is
// the pre-0.14 shape: an unpinned marker lets any file that copies it remove a
// declaration of its own.
func TestLoadRejectsBadScopeNotDeclaring(t *testing.T) {
	for _, tc := range []struct{ name, scope, want string }{
		{"bare string instead of a sequence", "{include: ['**/*.go'], not_declaring: 'DO NOT EDIT.'}", "want a sequence"},
		{"bare-string entry", "{include: ['**/*.go'], not_declaring: ['func Owner(']}", "must be {marker, holders}"},
		{"empty marker matches every file", "{include: ['**/*.go'], not_declaring: [{marker: '', holders: 1}]}", "matches every file"},
		{"whitespace-only marker", "{include: ['**/*.go'], not_declaring: [{marker: '   ', holders: 1}]}", "matches every file"},
		{"missing holders", "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner('}]}", "holders is required"},
		{"zero holders", "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner(', holders: 0}]}", "holders must be >= 1"},
		{"unknown key", "{include: ['**/*.go'], not_declaring: [{marker: 'func Owner(', holders: 1, why: x}]}", "unknown key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(declRepo(t, tc.scope))
			if err == nil {
				t.Fatalf("Load succeeded; want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %q, want it to mention %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "scope.not_declaring") {
				t.Fatalf("Load error = %q, want it to name the key so the operator knows which line to fix", err)
			}
		})
	}
}
