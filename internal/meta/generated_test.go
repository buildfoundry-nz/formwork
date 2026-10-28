package meta_test

import (
	"strings"
	"testing"
)

const genLintRules = "rules:\n" +
	"  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n    tags: [txt]\n" +
	"  - id: codegen-fresh\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt'], generated: true}\n    params: {pattern: NEVER}\n    tags: [gen]\n"

func genLintRepo(lanes string) map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n" + lanes +
			"generated:\n  - {marker: 'GENERATED-BY-TOOL', proven_by: [codegen-fresh]}\n  - {marker: 'NO-TOOL-WRITES-THIS', proven_by: [codegen-fresh]}\n",
		".formwork/rules/r.yaml":                        genLintRules,
		".formwork/fixtures/no-banana/fire-1/f.txt":     "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt":     "clean\n",
		".formwork/fixtures/codegen-fresh/fire-1/f.txt": "NEVER want: codegen-fresh\n",
		".formwork/fixtures/codegen-fresh/pass-1/f.txt": "clean\n",
		"gen.txt":  "# GENERATED-BY-TOOL\nbanana\n",
		"hand.txt": "clean\n",
	}
}

// The census counts each header's files beside the rule that proves them.
func TestCensusCountsGeneratedFilesPerMarker(t *testing.T) {
	_, out := lint(t, genLintRepo(""))
	want := `  generated: "GENERATED-BY-TOOL": 1 file(s), judged only by rules declaring scope.generated; proven to reproduce by codegen-fresh`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

// A marker no file carries declares nothing, and a proving rule no CI lane
// selects proves nothing: both are problems.
func TestLintGeneratedDefinitionProblems(t *testing.T) {
	_, out := lint(t, genLintRepo("lanes:\n  ci: {tags: [txt], ci: true}\n"))
	for _, want := range []string{
		`generated: marker "NO-TOOL-WRITES-THIS" is carried by no scanned file's header`,
		`generated: marker "GENERATED-BY-TOOL": proving rule codegen-fresh runs in no CI lane`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
