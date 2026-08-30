// census_proof_test.go — #230's second half.
//
// The escape-hatch census is this repo's authoritative disclosure surface, and
// on external-tool rules it disclosed nothing about proof. Every command and
// git-diff rule got one unconditional line:
//
//	no-ghost: command rule (external tool, heavy — fixture-exempt)
//
// That is not false — "fixture-exempt" names the lint POLICY, which does exempt
// them — but it was emitted identically for a rule carrying a complete,
// passing fire/pass proof and for a rule carrying nothing at all. On the rule
// type #230 identifies as the least proven and the most exercised, the census
// could not tell the two apart.
//
// That undercuts #53's claim to have converted the gap "from an accident into a
// decision": a reader could see that a decision existed and not which one was
// made. The line now says.
package meta_test

import (
	"strings"
	"testing"
)

func censusProofRepo(extra map[string]string) map[string]string {
	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: command\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [bash, -c, \"! grep -rq GHOST .\"]}\n" +
			"    cure: \"drop it\"\n",
		"src.txt": "clean\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

func TestCensusSaysAHeavyRuleWithACompletePairIsProven(t *testing.T) {
	_, out := lint(t, censusProofRepo(map[string]string{
		".formwork/fixtures/no-ghost/fire-1/a.txt": "GHOST\n",
		".formwork/fixtures/no-ghost/fire-1.want":  "-\n",
		".formwork/fixtures/no-ghost/pass-1/b.txt": "clean\n",
	}))
	if !strings.Contains(out, "no-ghost: command rule (external tool, heavy — proved by fire+pass fixtures)") {
		t.Fatalf("a heavy rule with a complete pair must be disclosed as PROVEN, not as a "+
			"bare exemption:\n%s", out)
	}
}

func TestCensusSaysAHeavyRuleWithNoFixturesHasNoProof(t *testing.T) {
	_, out := lint(t, censusProofRepo(map[string]string{
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: command\n" +
			"    fixture_exempt: \"needs a live cluster\"\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [bash, -c, \"! grep -rq GHOST .\"]}\n" +
			"    cure: \"drop it\"\n",
	}))
	if !strings.Contains(out, "no-ghost: command rule (external tool, heavy — NO firing proof: no fixtures)") {
		t.Fatalf("a heavy rule with no fixtures must be disclosed as unproven:\n%s", out)
	}
}

// The narrowing. If every heavy rule read the same way the line would carry no
// information, which is the state this change exists to leave — so the two
// verdicts must be distinguishable in one run.
func TestCensusDistinguishesProvenFromUnprovenInOneRun(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: proven\n" +
			"    type: command\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [bash, -c, \"! grep -rq GHOST .\"]}\n" +
			"    cure: \"drop it\"\n" +
			"  - id: unproven\n" +
			"    type: command\n" +
			"    fixture_exempt: \"needs a live cluster\"\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [bash, -c, \"true\"]}\n" +
			"    cure: \"drop it\"\n",
		"src.txt":                                "clean\n",
		".formwork/fixtures/proven/fire-1/a.txt": "GHOST\n",
		".formwork/fixtures/proven/fire-1.want":  "-\n",
		".formwork/fixtures/proven/pass-1/b.txt": "clean\n",
	})
	if !strings.Contains(out, "proven: command rule (external tool, heavy — proved by fire+pass fixtures)") ||
		!strings.Contains(out, "unproven: command rule (external tool, heavy — NO firing proof: no fixtures)") {
		t.Fatalf("the census must tell a proven heavy rule from an unproven one in the "+
			"same run:\n%s", out)
	}
}

// exemptRule declares fixture_exempt so these cases exercise the CENSUS verdict
// without also tripping fixture-coverage's pair requirement (#240) — the two
// checks are independent and each is tested where it lives.
func exemptRule() string {
	return "rules:\n" +
		"  - id: no-ghost\n" +
		"    type: command\n" +
		"    fixture_exempt: \"needs a live cluster\"\n" +
		"    scope: {include: ['**/*.txt']}\n" +
		"    params: {cmd: [bash, -c, \"! grep -rq GHOST .\"]}\n" +
		"    cure: \"drop it\"\n"
}

// The two PARTIAL arms, which mutation caught as untested: each half of the pair
// alone is not a proof, and the census must say which half is missing rather
// than rounding either to "proven".
func TestCensusSaysWhichHalfOfTheProofIsMissing(t *testing.T) {
	_, fireOnly := lint(t, censusProofRepo(map[string]string{
		".formwork/rules/r.yaml":                   exemptRule(),
		".formwork/fixtures/no-ghost/fire-1/a.txt": "GHOST\n",
		".formwork/fixtures/no-ghost/fire-1.want":  "-\n",
	}))
	if !strings.Contains(fireOnly, "NO firing proof: fire fixture only, nothing shows the detector ran") {
		t.Errorf("a fire fixture alone is satisfied by a detector that cannot run; the "+
			"census must say so:\n%s", fireOnly)
	}
	_, passOnly := lint(t, censusProofRepo(map[string]string{
		".formwork/rules/r.yaml":                   exemptRule(),
		".formwork/fixtures/no-ghost/pass-1/b.txt": "clean\n",
	}))
	if !strings.Contains(passOnly, "NO firing proof: pass fixture only, nothing shows it can fire") {
		t.Errorf("a pass fixture alone shows only silence on clean input; the census must "+
			"say so:\n%s", passOnly)
	}
}

// Unreadable is not the same as absent. Reporting a filesystem error as "no
// fixtures" would state a verdict about the RULE that the run never established
// — the same substitution of an unanswerable question for a negative answer
// that lint's unreadable-file refusal (#30) exists to prevent.
//
// Driven by making the rule's fixture directory a FILE: os.ReadDir then fails
// with ENOTDIR, which is not os.IsNotExist, so it reaches the error arm. A
// chmod-based version would not survive a run as root.
func TestCensusDoesNotReportAnUnreadableFixtureDirAsNoFixtures(t *testing.T) {
	_, out := lint(t, censusProofRepo(map[string]string{
		".formwork/rules/r.yaml":      exemptRule(),
		".formwork/fixtures/no-ghost": "not a directory\n",
	}))
	if !strings.Contains(out, "firing proof UNKNOWN") {
		t.Fatalf("an unreadable fixture directory must be reported as UNKNOWN, never as a "+
			"verdict that the rule has no fixtures:\n%s", out)
	}
}
