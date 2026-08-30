// commandpin_corpus_test.go — a command rule's fire manifest must pin a
// substring the DETECTOR printed, not one the rule's own argv already carries
// (#262 finding 4).
//
// WHAT IS BROKEN WITHOUT THIS. internal/rules/command renders every finding as
//
//	command <argv> exited <n>, want <m>[: <tool output>]
//
// so the argv is present in the message whatever the tool did. expect.go
// satisfies a MessagePin with strings.Contains over that WHOLE message, which
// makes a pin lifted from the command's own text — the `echo "…"` inside a
// `bash -c` body is the common spelling — true of every finding the rule can
// produce. Such a pin is a bare `-` wearing a message: it holds that SOMETHING
// fired and nothing about what.
//
// Measured on this corpus at 90e01d7c, not predicted: three of the eight
// command fire manifests that carried a pin were satisfiable by the argv alone.
// Driven on ci-go-changed-langflag-must-cover-replace-targets/fire-2, whose pin
// was "expected exactly one live go_touched LANGFLAG marker" — deleting
// scripts/ci/lang-flags.go from the fire tree makes the detector take a wholly
// different branch and print "missing scripts/ci/lang-flags.go", and
// `formwork test --rule ci-go-changed-langflag-must-cover-replace-targets`
// still reported `OK — 4 fixture(s)` at exit 0.
//
// WHAT THIS ASSERTS, AND THE HALF IT DOES NOT. It asserts that every pin a
// command fire manifest DOES carry is drawn from the tool output. It does not
// assert that every command fire fixture carries one, and that second claim is
// false of this corpus for a reason outside the manifests: ten of its command
// rules exit non-zero on their fire path while printing nothing at all, so
// their finding message is the frame alone and no substring of it can name a
// verdict. Making those ten discriminate is an edit to their params.cmd, in
// .formwork/rules/, not to a .want file.
package fixturetest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filesize"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
	"gopkg.in/yaml.v3"
)

// pinnedCorpus is the corpus this runs over: the ported tree where the command
// escape hatch is actually used. examples/quickstart carries no command rule.
const pinnedCorpus = "examples/palletra-port-full"

// corpusRoot walks up to the directory holding go.mod and joins the corpus.
// Failing closed rather than guessing: a proof that cannot find the tree it is
// judging must not report a pass.
func corpusRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, pinnedCorpus)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory — cannot locate the repo root")
		}
		dir = parent
	}
}

// commandArgvs reads every `type: command` rule's params.cmd straight from the
// rule YAML. config.Rule keeps params unexported, and the argv is the exact
// text this test must prove the pin is NOT drawn from, so it is read rather
// than reconstructed.
func commandArgvs(t *testing.T, root string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no rule files under %s: %v", root, err)
	}
	var doc struct {
		Rules []struct {
			ID     string `yaml:"id"`
			Type   string `yaml:"type"`
			Params struct {
				Cmd []string `yaml:"cmd"`
			} `yaml:"params"`
		} `yaml:"rules"`
	}
	out := map[string][]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		doc.Rules = nil
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, r := range doc.Rules {
			if r.Type == "command" {
				out[r.ID] = r.Params.Cmd
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no command rule — this proof would pass over nothing", root)
	}
	return out
}

// manifestPins returns the message pins in one fire manifest: the `- <text>`
// lines collectExpectations turns into expectation.MessagePin. A bare `-`
// contributes nothing, which is the state this file exists to make visible.
func manifestPins(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var pins []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if pin := strings.TrimSpace(line[2:]); pin != "" {
			pins = append(pins, pin)
		}
	}
	return pins
}

// commandFrame returns the INVARIANT head of a command finding's message: the
// literal "command ", the argv, and the exit/expect clause up to and including
// the ": " that introduces tool output. Every finding this rule can emit —
// whatever the tool did, whatever it printed, whatever it exited — carries this
// head, so a pin contained in it is satisfied unconditionally.
//
// A message with no output at all has no cut, and the whole message is frame:
// that rule can carry no discriminating pin, which is the answer this returns.
//
// It fails the test rather than guessing when the message does not carry the
// shape internal/rules/command documents.
func commandFrame(t *testing.T, ruleID string, argv []string, msg string) string {
	t.Helper()
	prefix := "command " + fmt.Sprintf("%v", argv) + " "
	if !strings.HasPrefix(msg, prefix) {
		t.Fatalf("%s: finding message does not open with its own argv, so the frame cannot be located:\n  msg=%q\n  want prefix=%q", ruleID, msg, prefix)
	}
	i := strings.Index(msg[len(prefix):], ": ")
	if i < 0 {
		return msg
	}
	return msg[:len(prefix)+i+len(": ")]
}

func TestCommandFirePinsAreDrawnFromDetectorOutput(t *testing.T) {
	root := corpusRoot(t)
	argvs := commandArgvs(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("loading %s: %v", root, err)
	}
	checked := 0
	for _, r := range cfg.Rules {
		argv, isCommand := argvs[r.ID]
		if !isCommand {
			continue
		}
		ruleDir := filepath.Join(root, ".formwork", "fixtures", r.ID)
		entries, err := os.ReadDir(ruleDir)
		if err != nil {
			continue // no fixture tree; `formwork lint` reports coverage
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "fire-") {
				continue
			}
			pins := manifestPins(t, ruleDir+string(filepath.Separator)+e.Name()+".want")
			if len(pins) == 0 {
				continue
			}
			fresh, err := r.Fresh()
			if err != nil {
				t.Fatalf("%s: %v", r.ID, err)
			}
			findings, _, err := fixturetest.EvalIn(fresh, filepath.Join(ruleDir, e.Name()), 0)
			if err != nil {
				t.Fatalf("%s/%s: %v", r.ID, e.Name(), err)
			}
			findings = finding.Unsuppressed(findings)
			if len(findings) == 0 {
				t.Errorf("%s/%s: pinned manifest but the fixture produced no finding", r.ID, e.Name())
				continue
			}
			for _, pin := range pins {
				matched := false
				for _, f := range findings {
					frame := commandFrame(t, r.ID, argv, f.Message)
					if !strings.Contains(f.Message, pin) {
						continue
					}
					matched = true
					// Containment in the frame is the whole test. A pin that
					// matches the message and is NOT in the frame necessarily
					// reaches past it into the output, which is the property
					// being asserted — including the case where it STRADDLES
					// the cut. That spelling is not a curiosity: where a
					// detector's entire verdict is an `echo` literal inside its
					// own `bash -c` body, every sentence it can print is already
					// in the argv, and anchoring the pin at the frame's tail is
					// the only way left to hold that the tool printed it.
					if strings.Contains(frame, pin) {
						t.Errorf("%s/%s.want: pin %q is inside the command frame, so it is true of EVERY finding this rule can emit — it holds no more than a bare `-`.\n  frame=%q", r.ID, e.Name(), pin, frame)
					}
					break
				}
				if !matched {
					t.Errorf("%s/%s.want: pin %q matches no finding the fixture produced", r.ID, e.Name(), pin)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no command fire manifest carried a message pin — this proof judged nothing")
	}
	t.Logf("judged %d message pin(s) across the corpus's command fire manifests", checked)
}
