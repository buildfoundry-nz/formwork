// syntax_multiline_test.go — #17979.
//
// pair-consistency compiled with regexp.Compile directly, so `syntax:` and
// `multiline:` were accepted by the pattern rule types and REFUSED here. The
// cost was not the refusal, it was what a repo did about it: converting a
// forbidden-pattern rule to a pair dropped those params, and dropping
// `multiline: true` leaves a rule that still loads, still reports OK, and no
// longer matches anything — a silently disabled guardrail. These arms pin both
// params and, in MissingMultiline, pin the silent-disable itself so nobody
// "simplifies" the flag away again.
package pairconsistency_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// A lookaround trigger: only a Getenv NOT prefixed by TQS_ obliges the
// companion. RE2 cannot express this at all, which is why the rule needs the
// regexp2 backend rather than a rewritten pattern.
const lookaroundParams = "syntax: regexp2\ntrigger: '(?<!TQS_)ENVIRONMENT'\nrequires: 'IsLocalDevelopment'\n"

func TestSyntaxRegexp2LookaroundFires(t *testing.T) {
	c := mustChecker(t, lookaroundParams)
	f := scan.NewMemFile("a.go", []byte("package p\nvar x = os.Getenv(\"ENVIRONMENT\")\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("a bare ENVIRONMENT read owes the companion: want 1 finding, got %d: %+v", len(ms), ms)
	}
}

// The lookaround's whole purpose: the TQS_-prefixed variable is a DIFFERENT
// variable and must not oblige anything. Under RE2 this would fire, which is
// exactly the false exemption #17979 removed downstream.
func TestSyntaxRegexp2LookaroundSkipsPrefixedName(t *testing.T) {
	c := mustChecker(t, lookaroundParams)
	f := scan.NewMemFile("a.go", []byte("package p\nvar x = os.Getenv(\"TQS_ENVIRONMENT\")\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("TQS_ENVIRONMENT is a different variable and owes nothing: want 0 findings, got %d: %+v", len(ms), ms)
	}
}

func TestUnknownSyntaxIsAConfigError(t *testing.T) {
	err := checkerErr(t, "syntax: pcre\ntrigger: 'A'\nrequires: 'B'\n")
	if err == nil {
		t.Fatal("an unknown syntax must be a config error, not a silent fallback to RE2")
	}
	if !strings.Contains(err.Error(), "syntax") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// The cross-line trigger: a Join naming the rules dir, then a ReadDir within a
// short window. same-file scans per LINE, so this can only match when the unit
// text is the whole file.
const crossLineParams = "multiline: true\ntrigger: 'filepath\\.Join\\([^;\\n]*\"rules\"\\s*\\)[\\s\\S]{0,80}?os\\.ReadDir\\('\nrequires: 'func ruleYAMLFiles\\('\n"

var crossLineSrc = []byte("package p\n\nfunc walk(root string) {\n\tdir := filepath.Join(root, \"rules\")\n\tentries, err := os.ReadDir(dir)\n\t_, _ = entries, err\n}\n")

func TestMultilineCrossLineTriggerFires(t *testing.T) {
	c := mustChecker(t, crossLineParams)
	f := scan.NewMemFile("a.go", crossLineSrc)
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("the cross-line walk owes ruleYAMLFiles: want 1 finding, got %d: %+v", len(ms), ms)
	}
	// The finding must anchor on the line the match STARTS on, not line 1 —
	// under multiline the unit is the whole file, so a naive implementation
	// reports the top of it and sends the reader to the wrong place.
	if ms[0].Line != 4 {
		t.Errorf("finding should anchor on the Join line (4), got %d", ms[0].Line)
	}
}

// THE SILENT-DISABLE ARM. Identical rule with multiline dropped: the trigger
// spans lines, same-file matches per line, so it matches nothing and the rule
// reports clean over a file that violates it. This is what made dropping the
// flag during a conversion dangerous, and it must stay demonstrable.
func TestMissingMultilineSilentlyDisablesACrossLineTrigger(t *testing.T) {
	c := mustChecker(t, strings.Replace(crossLineParams, "multiline: true\n", "", 1))
	f := scan.NewMemFile("a.go", crossLineSrc)
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("without multiline a per-line scan cannot see a cross-line trigger; got %d: %+v", len(ms), ms)
	}
}
