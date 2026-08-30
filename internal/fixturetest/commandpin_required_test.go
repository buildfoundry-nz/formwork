// commandpin_required_test.go — a command rule's fire manifest may not declare
// a bare `-` when the tool PRINTED something (#262 finding 4, durable half).
//
// WHAT IS BROKEN WITHOUT THIS. A `-` line is an expectation with an empty
// MessagePin, and diff() ignores Message for those: any scope-level finding
// satisfies it. command.FinalizeErr emits exactly one scope-level finding
// whatever happened, so a bare `-` holds only "the tool disagreed with
// expect:" — never why. Reproduced on this tree at 1e81245e: with
// schema-fk-delete-path-indexed/fire-1.want reduced to `-` and `this is not go`
// appended to its checker, `formwork test --rule schema-fk-delete-path-indexed`
// reported `OK — 2 fixture(s)` at exit 0. The checker never compiled; the
// message was the go build error; the fixture called it a proof.
//
// That hazard is NEW to the ports and not hypothetical: the sh/py originals had
// no compile step, so "the detector is broken" could not previously wear the
// same exit code as "the detector fired".
//
// WHY THE REQUIREMENT IS OUTPUT-CONDITIONAL RATHER THAN ABSOLUTE. Ten command
// rules in examples/palletra-port-full exit 1 on their fire path while printing
// nothing at all — measured by evaluating each in its own fire-1 tree, and they
// are exactly the ten whose fire manifests are still bare: gate-scripts-are-
// wired, gate-synth-test-coverage, go-http-services-use-httpserve-seams,
// go-services-call-cgroup-configure, golangci-required-linters-stay-enabled,
// jsonb-key-naming-matches-manifest, local-accept-threshold-declared-once,
// migration-partman-parent-settings-cascade, migration-partman-retention-
// preserved-on-replace, shared-internal-tree-excludes-core-api-only-packages.
// Their message is the frame alone, so no substring of it can name a verdict
// and there is no pin available to write. An opt-out list would let exactly
// those ten keep accepting arbitrary compile-failure output forever — a checker
// that fails to build PRINTS, so conditioning on output closes the hazard for
// them too, while a standing exemption would not.
//
// THE GUARDS AGAINST OVER-REACH ARE ASSERTED, NOT ASSUMED. Six accept cases sit
// beside the two refusals, one per way of not being the defect: the silent
// command rule, the silent half of the output_forbid message shape, the pinned
// rule, the rule that produced no finding at all, the rule whose only unpinned
// expectation is line-anchored, and a non-command rule whose scope-level
// message is every bit as talkative.
package fixturetest_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
)

// commandPinConfig declares one rule per case the requirement has to get right.
// text-talks is the type-scoping guard: its scope-level finding reads "required
// pattern not found in any in-scope file" — as talkative as any command
// finding, and bare `-` manifests for that shape are the corpus norm — so a
// requirement that leaked past `type: command` would fail it.
//
// cmd-forbid-silent is the second message shape internal/rules/command emits,
// `output matched forbidden pattern %q` with no output tail: reachable because
// `^$` matches the empty output of a tool that printed nothing.
const commandPinConfig = `rules:
  - id: cmd-talks
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', 'echo detector-said-this; exit 1']
  - id: cmd-silent
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', 'exit 1']
  - id: cmd-pinned
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', 'echo detector-said-this; exit 1']
  - id: cmd-marker
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', 'echo detector-said-this; exit 1']
  - id: cmd-nofire
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', ':']
  - id: cmd-forbid-talks
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', 'echo boom']
      expect:
        output_forbid: 'boom'
  - id: cmd-forbid-silent
    type: command
    scope: {include: ['**']}
    params:
      cmd: ['sh', '-c', ':']
      expect:
        output_forbid: '^$'
  - id: text-talks
    type: required-pattern
    scope: {include: ['**/*.md']}
    params:
      pattern: 'anchor'
      mode: exists
`

// fixtureCase is one rule's fire fixture: the manifest, and the single file the
// tree contains. The file's contents are irrelevant to a command rule — it
// exists so scan.Walk has something to walk — except in cmd-marker, where it
// carries the inline `want:` marker that case is about, and in text-talks,
// where its lack of "anchor" is the violation.
type fixtureCase struct {
	want    string
	subject string
}

func runCommandPinCorpus(t *testing.T, cases map[string]fixtureCase) string {
	t.Helper()
	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  commandPinConfig,
	}
	for id, c := range cases {
		files[".formwork/fixtures/"+id+"/fire-1/subject.md"] = c.subject
		files[".formwork/fixtures/"+id+"/fire-1.want"] = c.want
	}
	root := writeRepo(t, files)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func TestCommandFireManifestMustPinToolOutput(t *testing.T) {
	const plain = "nothing here\n"
	out := runCommandPinCorpus(t, map[string]fixtureCase{
		"cmd-talks":         {want: "-\n", subject: plain},
		"cmd-silent":        {want: "-\n", subject: plain},
		"cmd-pinned":        {want: "- detector-said-this\n", subject: plain},
		"cmd-marker":        {want: "- detector-said-this\n", subject: "nothing here want: cmd-marker\n"},
		"cmd-nofire":        {want: "-\n", subject: plain},
		"cmd-forbid-talks":  {want: "-\n", subject: plain},
		"cmd-forbid-silent": {want: "-\n", subject: plain},
		"text-talks":        {want: "-\n", subject: plain},
	})

	// THE REFUSAL. Both talkative command rules are reported, and the report
	// carries the tool's own words: an author cannot write the pin the message
	// demands unless the message shows them what the tool said.
	for _, id := range []string{"cmd-talks", "cmd-forbid-talks"} {
		if !strings.Contains(out, "["+id+"] FAIL — 1 problem(s)") {
			t.Errorf("%s: a bare `-` was accepted for a command rule whose tool printed output — a checker that only fails to COMPILE satisfies this fixture\n%s", id, out)
		}
	}
	for _, want := range []string{
		"fire-1: fire fixture declares a bare '-'",
		"satisfied by ANY scope-level finding",
		"detector-said-this",
		"boom",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal does not say %q:\n%s", want, out)
		}
	}

	// THE ACCEPTANCES. Each is a distinct way of NOT being the defect, and each
	// goes red under a different over-reach.
	for _, id := range []string{"cmd-silent", "cmd-forbid-silent", "cmd-pinned", "text-talks"} {
		if !strings.Contains(out, "["+id+"] OK — 1 fixture(s)") {
			t.Errorf("%s: refused, but its manifest is the best one available for that rule\n%s", id, out)
		}
	}

	// The two acceptances that fail for a DIFFERENT reason, so the count is the
	// assertion: exactly the one problem diff already owns, and no second one
	// invented by the pin requirement on top of it.
	//
	//   cmd-nofire  the tool agreed with expect: and produced no finding. The
	//               manifest is wrong and diff says so; a pin requirement that
	//               did not first ask whether anything was printed would add a
	//               refusal quoting an empty string.
	//   cmd-marker  its only unpinned expectation is LINE-ANCHORED (an inline
	//               `want:` marker), which pins a location rather than leaning
	//               on message-blindness. diff reports it unmatched; counting
	//               it as a bare scope-level expectation would double-report.
	for _, c := range []struct{ id, problem string }{
		{"cmd-nofire", "fire-1: missing expected finding - (scope-level)"},
		{"cmd-marker", "fire-1: missing expected finding subject.md:1"},
	} {
		if !strings.Contains(out, "["+c.id+"] FAIL — 1 problem(s)") || !strings.Contains(out, c.problem) {
			t.Errorf("%s: want exactly the one problem %q that diff owns\n%s", c.id, c.problem, out)
		}
	}
}
