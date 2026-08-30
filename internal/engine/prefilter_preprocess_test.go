package engine_test

// #8 — a rule that combines `prefilter:` with `preprocess:` must apply the
// prefilter to the PREPROCESSED bytes, never to the raw file.
//
// The hazard, as reported: a prefilter is a cheap literal gate that skips a
// file before the real matcher runs. If it were applied to raw bytes while the
// matcher ran on preprocessed bytes, the gate could discard a file whose
// preprocessed text WOULD have matched — the rule then reports clean because
// the file was never examined. That is a silent false negative, the one
// outcome the exit-code contract exists to make impossible.
//
// Each case below is built so the prefilter literal is absent from the raw
// bytes and present only after preprocessing. A prefilter applied to raw would
// drop the file and report nothing; the assertion is that the finding IS
// reported. This is a REGRESSION fixture: the engine is sound today (evalFile
// hands the checker the preprocess variant, so prefilter and matcher read the
// same bytes by construction), and these cases fail the moment a change
// reintroduces a raw-byte gate ahead of the variant.
//
// Verified to discriminate by mutation: passing the raw file to checkFile
// instead of the variant fails every case.

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern" // registers forbidden-pattern
	"gopkg.in/yaml.v3"
)

// checkerWithParams builds a registered checker from YAML params.
func checkerWithParams(t *testing.T, typeName, params string) rules.Checker {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	factory, ok := rules.Lookup(typeName)
	if !ok {
		t.Fatalf("rule type %q not registered", typeName)
	}
	c, err := factory(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPrefilterAppliesToPreprocessedBytesNotRaw(t *testing.T) {
	cases := []struct {
		name string
		// preprocess variant the rule declares
		preprocess string
		path       string
		// raw does NOT contain the prefilter literal; the variant does
		raw string
		// literal absent from raw, present after preprocessing
		literal string
	}{
		{
			// decomment-go blanks the comment to spaces: "ab/*z*/cd" -> "ab     cd",
			// so the spaced literal exists only in the variant.
			name:       "decomment-go",
			preprocess: "decomment-go",
			path:       "a.go",
			raw:        "x := ab/*z*/cd\n",
			literal:    "ab     cd",
		},
		{
			// strings-only-go keeps string interiors and blanks all code, bringing
			// two literals into a spaced adjacency that the raw bytes never had.
			name:       "strings-only-go",
			preprocess: "strings-only-go",
			path:       "b.go",
			raw:        "f(\"AA\")+g(\"BB\")\n",
			literal:    "AA      BB",
		},
		{
			// destring-sh blanks string interiors: "\"zz\"" -> "\"  \"".
			name:       "destring-sh",
			preprocess: "destring-sh",
			path:       "c.sh",
			raw:        "x=\"zz\"y\n",
			literal:    "\"  \"",
		},
		{
			// destring-decomment-sh additionally blanks the comment body, so
			// the code before it acquires a spaced tail the raw bytes lack:
			// "x=1 #zz" -> "x=1    ".
			name:       "destring-decomment-sh",
			preprocess: "destring-decomment-sh",
			path:       "d.sh",
			raw:        "x=1 #zz\ny=2\n",
			literal:    "x=1    ",
		},
		{
			// decomment-sh blanks ONLY the comment, so the string interior
			// survives and picks up a spaced tail: "x=\"zz\" #q" ->
			// "x=\"zz\"   ". Discriminating against destring-decomment-sh,
			// which would have blanked the "zz" too.
			name:       "decomment-sh",
			preprocess: "decomment-sh",
			path:       "e.sh",
			raw:        "x=\"zz\" #q\ny=2\n",
			literal:    "\"zz\"   ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Guard the fixture itself: if the literal ever appears in the raw
			// bytes, the case proves nothing — it would pass under a raw-byte
			// prefilter too.
			if contains(tc.raw, tc.literal) {
				t.Fatalf("fixture is not discriminating: raw %q already contains the prefilter literal %q", tc.raw, tc.literal)
			}

			params := "pattern: '" + yamlEscape(tc.literal) + "'\nprefilter: '" + yamlEscape(tc.literal) + "'\n"
			c := checkerWithParams(t, "forbidden-pattern", params)

			r, err := config.New("prefilter-probe", "forbidden-pattern", finding.SeverityError,
				"fix it", []string{"**/*"}, nil, nil, c)
			if err != nil {
				t.Fatal(err)
			}
			r.Preprocess = tc.preprocess

			fset := memFileSet(map[string]string{tc.path: tc.raw})
			got, err := engine.Run([]*config.Rule{r}, fset, 1)
			if err != nil {
				t.Fatalf("engine.Run: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("want 1 finding (the prefilter must see preprocessed bytes), got %d: %+v", len(got), got)
			}
			if got[0].Path != tc.path {
				t.Errorf("path: got %q want %q", got[0].Path, tc.path)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// yamlEscape doubles single quotes for a YAML single-quoted scalar.
func yamlEscape(s string) string {
	out := ""
	for _, r := range s {
		if r == '\'' {
			out += "''"
		}
		out += string(r)
	}
	return out
}
