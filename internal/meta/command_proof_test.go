// command_proof_test.go — #230.
//
// #230 asks whether `formwork test` can drive a `command` rule to a named fire,
// and how a run that COULD NOT EXECUTE is distinguished from one that fired.
// Measured against the engine as it stands, the answer to the first is yes with
// no engine change: a fire fixture, a `.want` manifest for the scope-level
// finding, and a pass fixture drive a command rule to `1/1 rules passed`.
//
// The second is where the hole was, and it is not where the issue expected. A
// detector that cannot run — `go run ./tools/detector/main.go` where that path
// does not exist — exits non-zero in EVERY fixture. So the fire fixture is
// satisfied for the wrong reason, and the only thing that catches it is the
// PASS fixture, which sees the same non-zero exit as an unexpected finding. The
// differential is the proof that the detector ran; neither fixture proves it
// alone.
//
// fixture-coverage did not require the pair. For a declarative rule it judges
// `fire == 0` and `pass == 0` independently, but the external-tool branch asked
// `fire == 0 && pass == 0` — so a command rule carrying a fire fixture and no
// pass fixture reported healthy at exit 0, which is the fig-leaf shape #230 was
// filed about, one level up from where it was looked for.
//
// (An exec failure proper — a binary not on PATH — is already exit 2 with the
// exec error named, so "could not run" is distinguished at the strongest level.
// This is about the weaker one: a detector that runs and fails for its own
// reasons.)
package meta_test

import (
	"strings"
	"testing"
)

func commandRuleRepo(fixtures map[string]string) map[string]string {
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
	for k, v := range fixtures {
		files[k] = v
	}
	return files
}

// The hole: a fire fixture with no pass sibling is not a proof, because a
// detector that cannot run satisfies it.
func TestCommandRuleWithAFireFixtureAndNoPassFixtureIsNotAProof(t *testing.T) {
	_, out := lint(t, commandRuleRepo(map[string]string{
		".formwork/fixtures/no-ghost/fire-1/a.txt": "GHOST\n",
		".formwork/fixtures/no-ghost/fire-1.want":  "-\n",
	}))
	if !strings.Contains(out, "no-ghost") || !strings.Contains(out, "pass fixture") {
		t.Fatalf("a heavy rule with a fire fixture and no pass fixture must be reported — "+
			"the pass fixture is what proves the detector RAN rather than merely exiting "+
			"non-zero:\n%s", out)
	}
}

// The narrowing, and it is the one that matters: a complete pair must stay
// silent. A check that reported every command rule would say nothing about
// which ones are proven, which is the state #230 is trying to leave.
func TestCommandRuleWithBothFixturesIsNotReported(t *testing.T) {
	_, out := lint(t, commandRuleRepo(map[string]string{
		".formwork/fixtures/no-ghost/fire-1/a.txt": "GHOST\n",
		".formwork/fixtures/no-ghost/fire-1.want":  "-\n",
		".formwork/fixtures/no-ghost/pass-1/b.txt": "clean\n",
	}))
	// Matched on the fixture-coverage MESSAGE, not on the words "pass fixture":
	// the escape-hatch census names both fixtures when it reports a rule as
	// proved (#230), so a bare substring test fails against correct output.
	if strings.Contains(out, "but no pass fixture") {
		t.Fatalf("a command rule with a complete fire/pass pair is proven and must not "+
			"be reported:\n%s", out)
	}
}

// The declared exemption still governs: a rule that says why it cannot be
// fixtured is a decision, and this check must not reopen it (#53).
func TestDeclaredFixtureExemptionStillSuppressesThePairRequirement(t *testing.T) {
	files := commandRuleRepo(nil)
	files[".formwork/rules/r.yaml"] = "rules:\n" +
		"  - id: no-ghost\n" +
		"    type: command\n" +
		"    fixture_exempt: \"needs a live cluster\"\n" +
		"    scope: {include: ['**/*.txt']}\n" +
		"    params: {cmd: [bash, -c, \"! grep -rq GHOST .\"]}\n" +
		"    cure: \"drop it\"\n"
	files[".formwork/fixtures/no-ghost/fire-1/a.txt"] = "GHOST\n"
	files[".formwork/fixtures/no-ghost/fire-1.want"] = "-\n"
	_, out := lint(t, files)
	if strings.Contains(out, "but no pass fixture") {
		t.Fatalf("a DECLARED exemption is a recorded decision and must still suppress "+
			"this check:\n%s", out)
	}
}

// The mirror arm, which the pair requirement created and which mutation caught
// as untested: a heavy rule carrying only a PASS fixture. It proves the detector
// stays quiet on clean input and nothing at all about whether it can fire —
// which is the weaker half, since a detector that never fires is a gate that
// never gates.
func TestCommandRuleWithAPassFixtureAndNoFireFixtureIsNotAProof(t *testing.T) {
	_, out := lint(t, commandRuleRepo(map[string]string{
		".formwork/fixtures/no-ghost/pass-1/b.txt": "clean\n",
	}))
	if !strings.Contains(out, "no-ghost") || !strings.Contains(out, "fire fixture") {
		t.Fatalf("a heavy rule with a pass fixture and no fire fixture must be reported — "+
			"nothing proves the detector can fire at all:\n%s", out)
	}
}
