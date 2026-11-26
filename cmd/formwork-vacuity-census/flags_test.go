package main

import (
	"bytes"
	"strings"
	"testing"
)

// withSynthDir points the heavy-rule exemption at rel for one test and puts
// the previous value back afterwards.
func withSynthDir(t *testing.T, rel string) {
	t.Helper()
	prev := synthDirRel
	SetSynthDir(rel)
	t.Cleanup(func() { synthDirRel = prev })
}

// withCanaries replaces the named canaries for one test.
func withCanaries(t *testing.T, cs ...canary) {
	t.Helper()
	prev := calibration
	calibration = cs
	t.Cleanup(func() { calibration = prev })
}

// The synth tree is the corpus's layout, not the census's: with no
// --synth-dir, a real synth naming the rule witnesses nothing, because the
// census was never told where to look.
func TestHeavyRuleSynthWitnessesNothingWithoutASynthDir(t *testing.T) {
	withSynthDir(t, "")
	root := writeCorpus(t, `rules:
  - id: witnessed-command
    type: command
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      cmd: ['true']
      expect: { exit: 0 }
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n",
		"svc/internal/synthtests/witnessed_command_synthetic_test.go": "//go:build lockdown\n\n" +
			"package synthtests\n\nimport \"testing\"\n\n" +
			"func TestWitnessedCommandFires(t *testing.T) {\n\t_ = \"witnessed-command\"\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "NO-FIRE-WITNESS") {
		t.Fatalf("with no --synth-dir a synth file still witnessed a heavy rule:\n%s", out)
	}
	if !strings.Contains(out, "none was given") {
		t.Fatalf("the verdict does not say no --synth-dir was given:\n%s", out)
	}
}

func TestParseCanary(t *testing.T) {
	c, err := parseCanary("no-new-js-ts=1000")
	if err != nil || c.id != "no-new-js-ts" || c.minHit != 1000 {
		t.Fatalf("parseCanary = %+v, %v", c, err)
	}
	for _, bad := range []string{"", "no-floor", "=3", "id=", "id=x", "id=0", "id=-2"} {
		if _, err := parseCanary(bad); err == nil {
			t.Errorf("parseCanary(%q) accepted a canary with no usable floor", bad)
		}
	}
}

// A named canary under its floor makes every zero unbelievable, so the census
// refuses with exit 2; at or over its floor the run goes on.
func TestCanaryUnderItsFloorFailsCalibration(t *testing.T) {
	rules := `rules:
  - id: live-rule
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { mode: exists, pattern: 'package' }
    tags: [always]
`
	files := map[string]string{"src/a.go": "package a\n", "src/b.go": "package b\n"}

	withCanaries(t, canary{id: "live-rule", minHit: 3})
	var out, errb bytes.Buffer
	if code := run(writeCorpus(t, rules, files), false, false, &out, &errb); code != 2 {
		t.Fatalf("a canary with 2 in-scope files under a floor of 3 exited %d, want 2:\n%s\n%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "NOT CALIBRATED") {
		t.Fatalf("the under-floor canary was not reported:\n%s", out.String())
	}

	withCanaries(t, canary{id: "live-rule", minHit: 2})
	out.Reset()
	errb.Reset()
	run(writeCorpus(t, rules, files), false, false, &out, &errb)
	if !strings.Contains(out.String(), "live-rule") || strings.Contains(out.String(), "NOT CALIBRATED") {
		t.Fatalf("a canary at its floor was not reported live:\n%s", out.String())
	}
}
