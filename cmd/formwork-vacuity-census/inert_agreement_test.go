package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vacuity"
)

// The census and `formwork test` must agree on a pass fixture standing at an
// except.paths entry. Each case builds one corpus, asks the census, then asks
// vacuity.Judge — the call `formwork test` makes — about the same fixture tree.
func TestCensusAndJudgeAgreeOnDeclaredInertExcept(t *testing.T) {
	cases := []struct {
		name, marker string
		vacuous      bool
	}{
		{"declared with a reason", "# except-declaration: generic over every kind; the pattern cannot appear", false},
		{"bare marker", "# except-declaration:", true},
		{"no marker", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			marker := ""
			if tc.marker != "" {
				marker = "        " + tc.marker + "\n"
			}
			root := writeCorpus(t, "rules:\n"+
				"  - id: decl-rule\n"+
				"    type: forbidden-pattern\n"+
				"    severity: error\n"+
				"    scope:\n"+
				"      include: ['src/**/*.go']\n"+
				"    except:\n"+
				"      paths:\n"+
				marker+
				"        - 'src/decl.go'\n"+
				"    params: { pattern: 'BANNED' }\n"+
				"    tags: [always]\n",
				map[string]string{"src/a.go": "package a\n"})
			writeFixture(t, root, "decl-rule", "fire-1", map[string]string{
				"src/a.go": "package a\n\nvar x = BANNED\n",
			})
			writeFixture(t, root, "decl-rule", "pass-1", map[string]string{
				"src/decl.go": "package a\n\nvar y = fine\n",
			})

			_, out := census(t, root)
			censusVacuous := strings.Contains(out, "PASS-FIXTURE-VACUOUS")

			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			inert, err := vacuity.ReadDeclaredInert(root)
			if err != nil {
				t.Fatal(err)
			}
			fs, err := scan.Walk(filepath.Join(root, ".formwork", "fixtures", "decl-rule", "pass-1"))
			if err != nil {
				t.Fatal(err)
			}
			v, err := vacuity.Judge(cfg.Rules[0], fs, inert)
			if err != nil {
				t.Fatal(err)
			}
			judgeVacuous := v.Kind == vacuity.KindVacuous

			if censusVacuous != tc.vacuous || judgeVacuous != tc.vacuous {
				t.Fatalf("census vacuous=%v, Judge vacuous=%v, want both %v\n%s", censusVacuous, judgeVacuous, tc.vacuous, out)
			}
		})
	}
}
