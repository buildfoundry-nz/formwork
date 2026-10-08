package pattern_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

const credentialRule = "pattern: '\"auto_policy\"'\n" +
	"require_absent:\n  - {credential: '^const SourceAutoPolicy = \"auto_policy\"', holders: 1}\n"

func runAll(t *testing.T, c rules.Checker, files map[string]string) []rules.Match {
	t.Helper()
	var out []rules.Match
	for name, body := range files {
		ms, err := c.CheckFile(scan.NewMemFile(name, []byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ms...)
	}
	if f, ok := c.(rules.Finalizer); ok {
		out = append(out, f.Finalize()...)
	}
	return out
}

// The declaration excuses its own file, and the count matches.
func TestCredentialExcusesItsHolder(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", credentialRule)
	ms := runAll(t, c, map[string]string{
		"audit/mapping.go": "package audit\nconst SourceAutoPolicy = \"auto_policy\"\n",
		"other/use.go":     "package other\nvar s = audit.SourceAutoPolicy\n",
	})
	if len(ms) != 0 {
		t.Fatalf("want no findings, got %+v", ms)
	}
}

// The COPY forgery: the credential's text inside a string on a line that does
// not start with it. Before 0.9 this excused the file.
func TestCredentialCopiedMidLineDoesNotExcuse(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", credentialRule)
	ms := runAll(t, c, map[string]string{
		"audit/mapping.go": "package audit\nconst SourceAutoPolicy = \"auto_policy\"\n",
		"forge/forge.go": "package forge\nvar s = \"auto_policy\"\n" +
			"const decoy = `const SourceAutoPolicy = \"auto_policy\"`\n",
	})
	if len(ms) == 0 || ms[0].Line == 0 {
		t.Fatalf("want a line finding in the forging file, got %+v", ms)
	}
}

// The GROWTH forgery: a second file carrying the credential at the start of a
// line. It excuses itself, and the holder count catches it.
func TestCredentialSecondHolderIsCounted(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", credentialRule)
	ms := runAll(t, c, map[string]string{
		"audit/mapping.go": "package audit\nconst SourceAutoPolicy = \"auto_policy\"\n",
		"forge/forge.go":   "package forge\nconst SourceAutoPolicy = \"auto_policy\"\nvar s = \"auto_policy\"\n",
	})
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "held by 2 line(s)") {
		t.Fatalf("want one holder-count finding, got %+v", ms)
	}
}

// A holder in a file the prefilter skips is still counted.
func TestCredentialCountedThroughThePrefilter(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "pattern: 'rawQuery'\nprefilter: rawQuery\n"+
		"require_absent:\n  - {credential: '^// OWNER', holders: 1}\n")
	ms := runAll(t, c, map[string]string{
		"a.go": "// OWNER\nrawQuery()\n",
		"b.go": "// OWNER\nnothing here\n",
	})
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "held by 2 line(s)") {
		t.Fatalf("want the prefiltered holder counted, got %+v", ms)
	}
}

// Losing the holder is a finding too: a credential nothing holds is stale.
func TestCredentialMissingHolderIsAFinding(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", credentialRule)
	ms := runAll(t, c, map[string]string{"other/use.go": "package other\n"})
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "held by 0 line(s)") {
		t.Fatalf("want a holder-count finding, got %+v", ms)
	}
}

func TestCredentialRefusedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, params, want string }{
		{"bare string", "pattern: x\nrequire_absent: ['^OWNER']\n", "not a bare string"},
		{"bare none_of", "all_of: [x]\nnone_of: ['^OWNER']\n", "not a bare string"},
		{"both kinds", "pattern: x\nrequire_absent: [{credential: '^O', holders: 1, evidence: y, within: 1}]\n", "not both"},
		{"neither", "pattern: x\nrequire_absent: [{}]\n", "set credential+holders or evidence+within"},
		{"evidence without within", "pattern: x\nrequire_absent: [{evidence: y}]\n", "within is required"},
		{"evidence too far", "pattern: x\nrequire_absent: [{evidence: y, within: 101}]\n", "within must be between 0 and 100"},
		{"unanchored", "pattern: x\nrequire_absent: [{credential: 'OWNER', holders: 1}]\n", "must be anchored"},
		{"no holders", "pattern: x\nrequire_absent: [{credential: '^OWNER'}]\n", "holders is required"},
		{"zero holders", "pattern: x\nrequire_absent: [{credential: '^OWNER', holders: 0}]\n", "holders must be >= 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildChecker("forbidden-pattern", tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestCredentialFlagGroupAnchorAccepted(t *testing.T) {
	if _, err := buildChecker("forbidden-pattern", "pattern: x\nrequire_absent: [{credential: '(?i)^owner', holders: 1}]\n"); err != nil {
		t.Fatal(err)
	}
}

// A rule with credentials is a whole-tree invariant; one without stays
// range-scopeable.
func TestCredentialMakesTheRuleWholeTree(t *testing.T) {
	if !rules.IsWholeTreeInvariant(mustChecker(t, "forbidden-pattern", credentialRule)) {
		t.Fatal("a rule with credentials must be evaluated over the whole tree")
	}
	if rules.IsWholeTreeInvariant(mustChecker(t, "forbidden-pattern", "pattern: x\n")) {
		t.Fatal("a rule without credentials must stay range-scopeable")
	}
}

func buildChecker(typeName, params string) (rules.Checker, error) {
	factory, _ := rules.Lookup(typeName)
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		return nil, err
	}
	return factory(doc.Content[0])
}

const evidenceRule = "pattern: 'INSERT INTO'\nrequire_absent:\n  - {evidence: 'ephemeralDB\\(', within: 3}\n"

// Evidence beside the trigger discharges it.
func TestEvidenceNearTheTriggerExcuses(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", evidenceRule)
	ms := runAll(t, c, map[string]string{"a_test.go": "db := ephemeralDB(t)\n\nexec(\"INSERT INTO x\")\n"})
	if len(ms) != 0 {
		t.Fatalf("want no findings, got %+v", ms)
	}
}

// The audit's opt-out: evidence somewhere in the file no longer excuses a
// trigger it has nothing to do with.
func TestEvidenceFarFromTheTriggerDoesNotExcuse(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", evidenceRule)
	body := "db := ephemeralDB(t)\n" + strings.Repeat("// filler\n", 10) + "exec(\"INSERT INTO x\")\n"
	ms := runAll(t, c, map[string]string{"a_test.go": body})
	if len(ms) != 1 || ms[0].Line != 12 {
		t.Fatalf("want one finding on line 12, got %+v", ms)
	}
}

// Each trigger needs its own evidence: one discharged trigger does not
// excuse a second one elsewhere in the file.
func TestEvidenceIsPerTrigger(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", evidenceRule)
	body := "db := ephemeralDB(t)\nexec(\"INSERT INTO x\")\n" + strings.Repeat("// filler\n", 10) + "exec(\"INSERT INTO y\")\n"
	ms := runAll(t, c, map[string]string{"a_test.go": body})
	if len(ms) != 1 || ms[0].Line != 13 {
		t.Fatalf("want the undischarged trigger on line 13, got %+v", ms)
	}
}

// Multiline triggers are discharged per match too.
func TestEvidenceMultilinePerTrigger(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "pattern: '(?m)^UPDATE[\\s\\S]*?^CREATE UNIQUE INDEX'\nmultiline: true\n"+
		"require_absent:\n  - {evidence: '(?m)^DELETE FROM', within: 2}\n")
	ok := "UPDATE t SET a=1;\nDELETE FROM t WHERE dup;\nCREATE UNIQUE INDEX i ON t(a);\n"
	if ms := runAll(t, c, map[string]string{"1.up.sql": ok}); len(ms) != 0 {
		t.Fatalf("want no findings, got %+v", ms)
	}
	bad := ok + strings.Repeat("-- x\n", 10) + "UPDATE u SET a=1;\nCREATE UNIQUE INDEX j ON u(a);\n"
	if ms := runAll(t, c, map[string]string{"2.up.sql": bad}); len(ms) != 1 || ms[0].Line != 14 {
		t.Fatalf("want the second, undischarged trigger on line 14, got %+v", ms)
	}
}
