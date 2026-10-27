package vacuity_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vacuity"
)

// ruleWithExcept returns a rule file whose except.paths entry is preceded by
// marker (omitted when empty).
func ruleWithExcept(marker string) string {
	if marker != "" {
		marker = "        " + marker + "\n"
	}
	return "rules:\n" +
		"  - id: decl-rule\n" +
		"    type: forbidden-pattern\n" +
		"    severity: error\n" +
		"    scope:\n" +
		"      include: ['src/**/*.go']\n" +
		"    except:\n" +
		"      paths:\n" +
		marker +
		"        - 'src/decl.go'\n" +
		"    params: { pattern: 'BANNED' }\n"
}

// judgeAt loads ruleFile as a repo's rule file, reads its declarations the way
// `formwork test` does, and judges a pass fixture holding only the file at path.
func judgeAt(t *testing.T, ruleFile, path string) vacuity.Verdict {
	t.Helper()
	root := t.TempDir()
	write(t, root, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  ruleFile,
	})
	inert, err := vacuity.ReadDeclaredInert(root)
	if err != nil {
		t.Fatal(err)
	}
	r := loadRule(t, ruleFile[len("rules:\n"):])
	v, err := vacuity.Judge(r, walk(t, map[string]string{path: "package a\n\nvar y = fine\n"}), inert)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestJudgeExcusesAFixtureAtADeclaredInertExceptPath(t *testing.T) {
	v := judgeAt(t, ruleWithExcept("# except-declaration: generic over every kind; the pattern cannot appear"), "src/decl.go")
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestJudgeDoesNotExcuseABareExceptDeclaration(t *testing.T) {
	v := judgeAt(t, ruleWithExcept("# except-declaration:"), "src/decl.go")
	wantVerdict(t, v, vacuity.KindVacuous, vacuity.CodeVacuous)
}

func TestJudgeDoesNotExcuseAnUndeclaredExceptPath(t *testing.T) {
	v := judgeAt(t, ruleWithExcept(""), "src/decl.go")
	wantVerdict(t, v, vacuity.KindVacuous, vacuity.CodeVacuous)
}

// The declaration excuses the glob it sits on, not the rule: a fixture whose
// file is at some OTHER excepted path is still vacuous.
func TestJudgeDoesNotExcuseAFixtureAtADifferentExceptPath(t *testing.T) {
	file := "rules:\n" +
		"  - id: decl-rule\n" +
		"    type: forbidden-pattern\n" +
		"    severity: error\n" +
		"    scope:\n" +
		"      include: ['src/**/*.go']\n" +
		"    except:\n" +
		"      paths:\n" +
		"        # except-declaration: generic; the pattern cannot appear\n" +
		"        - 'src/decl.go'\n" +
		"        - 'src/other.go'\n" +
		"    params: { pattern: 'BANNED' }\n"
	v := judgeAt(t, file, "src/other.go")
	wantVerdict(t, v, vacuity.KindVacuous, vacuity.CodeVacuous)
}

func TestExceptEntriesReadsTheMarkerPerEntry(t *testing.T) {
	got, err := vacuity.ExceptEntries([]byte(ruleWithExcept("# except-declaration: because")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "decl-rule" || len(got[0].Entries) != 1 {
		t.Fatalf("ExceptEntries = %+v", got)
	}
	e := got[0].Entries[0]
	if e.Glob != "src/decl.go" || !e.Declared || e.Reason != "because" {
		t.Fatalf("entry = %+v, want declared glob src/decl.go with reason", e)
	}
}
