// reference_cli_test.go — the Introspection section's CLI promises, executed
// against the binary (#288, #330).
//
// WHY THIS EXISTS. reference_manual_test.go next door checks the manual against
// the rule-type and preprocessor REGISTRIES, which is why the vocabulary lists
// cannot rot. Nothing checked the manual against the COMMAND SURFACE, and both
// of that section's opening promises turned out to be false:
//
//	"Every one takes `-format human|json`"   — five of the eight commands
//	                                          listed beneath it did not; four
//	                                          exited 2 (#330)
//	"an out-of-root path is exit 2"          — `scope /etc/passwd` answered
//	                                          class=runtime at exit 0 (#288)
//
// Both were born false in the commit that wrote the section (7b8c1314), and
// both survived because the only executable check on this file reads registry
// names. An operator wiring `formwork lint -format json` into CI on the
// strength of the page gets exit 2, which this project's own exit-code contract
// defines as "the run did not reach a verdict".
//
// WHAT IT ASSERTS, AND IN WHICH DIRECTION. One direction only: every command
// the manual LISTS under those promises must keep them. It does not assert the
// reverse — a command that grows `-format` without being listed here is not a
// failure, so a later change adding the flag to `lint` cannot be failed by this
// guard until it also claims it. What it does catch is the shape that produced
// both issues: a claim written wider than the code.
//
// The invocations are read out of the manual's own code block rather than
// listed here, so adding a line to that block automatically puts the new
// command under the promises. A command in the block with no entry in
// exerciseAs is a failure with instructions, never a silent skip — that is the
// vacuity the count-substring assertions next door were criticised for.
package meta_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/cli"
	"github.com/buildfoundry-nz/formwork/internal/config"
)

// repoRoot is where this package sits relative to the tree the CLI is pointed
// at — the same hop referenceManual takes.
const repoRoot = "../.."

// exercise is how one documented command is turned into a runnable invocation.
// Flags must precede operands (Go's flag package stops parsing at the first
// positional), which is why the three parts are kept apart rather than written
// as one slice.
type exercise struct {
	sub      []string // subcommand tokens that come before any flag
	flags    []string // extra flags that keep the run cheap and READ-ONLY
	operands []string // positional arguments, appended last
	needsID  bool     // substitute a live rule id for the "" operand slot
}

// exerciseAs holds every command name the manual's Introspection block can
// name. Entries exist for commands NOT currently listed there
// (`test`, `lint`, `hooks`, `version`) on purpose: the block is the input, so an
// entry that is not exercised today is what lets a future addition be exercised
// the moment someone writes the line.
//
// `hooks` is deliberately pinned to `verify`, never `install`: this guard runs
// against the real repository root and must not write to it.
var exerciseAs = map[string]exercise{
	"list":      {operands: []string{"rules"}},
	"explain":   {operands: []string{""}, needsID: true},
	"rules-for": {operands: []string{"docs/reference.md"}},
	"scope":     {},
	"check":     {flags: []string{"--rule", ""}, needsID: true},
	"test":      {flags: []string{"--rule", ""}, needsID: true},
	"lint":      {flags: []string{"--rule", ""}, needsID: true},
	"hooks":     {sub: []string{"verify"}},
	"version":   {},
}

// introspectionSection returns the manual's "## Introspection" section — the
// preface that makes the promises, and the command names its code block lists.
func introspectionSection(t *testing.T) (preface string, commands []string, pathCommands map[string]bool) {
	t.Helper()
	manual := referenceManual(t)
	const heading = "\n## Introspection\n"
	i := strings.Index(manual, heading)
	if i < 0 {
		t.Fatal("docs/reference.md has no \"## Introspection\" section — if it was renamed, move this guard with it rather than deleting it")
	}
	section := manual[i+len(heading):]
	if j := strings.Index(section, "\n## "); j >= 0 {
		section = section[:j]
	}
	open := strings.Index(section, "```")
	if open < 0 {
		t.Fatal("the Introspection section has no command block; this guard reads the block to know what the manual claims")
	}
	preface = section[:open]
	body := section[open:]
	body = body[strings.Index(body, "\n")+1:]
	if end := strings.Index(body, "```"); end >= 0 {
		body = body[:end]
	}
	pathCommands = map[string]bool{}
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if k := strings.Index(line, "#"); k >= 0 {
			line = line[:k]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "formwork" {
			continue
		}
		name := fields[1]
		if !seen[name] {
			seen[name] = true
			commands = append(commands, name)
		}
		// The out-of-root promise applies to whatever the manual spells with a
		// path operand.
		for _, f := range fields[2:] {
			if strings.Contains(f, "<path>") {
				pathCommands[name] = true
			}
		}
	}
	// Floors, not counts: the exact contents are the manual's business, but a
	// parse that silently found nothing would make every assertion below pass
	// against an empty set — the tautological shape this repo names explicitly.
	if len(commands) < 4 {
		t.Fatalf("parsed only %d commands out of the Introspection block (%v) — the block's shape changed and this guard is now vacuous; fix the parse", len(commands), commands)
	}
	if len(pathCommands) == 0 {
		t.Fatal("no command in the Introspection block takes a <path> operand — the out-of-root assertion below would be vacuous")
	}
	return preface, commands, pathCommands
}

// invocationFor builds one runnable argv for a documented command, with the
// caller's flags placed where the flag package can still see them.
func invocationFor(t *testing.T, name string, ruleID string, flags, operands []string) []string {
	t.Helper()
	ex, ok := exerciseAs[name]
	if !ok {
		t.Fatalf("docs/reference.md lists `formwork %s` under the Introspection promises and this guard has no way to run it — "+
			"add an entry to exerciseAs (cheap and read-only), rather than dropping the command from the check", name)
	}
	argv := append([]string{name}, ex.sub...)
	argv = append(argv, "-C", repoRoot)
	for _, f := range ex.flags {
		if f == "" && ex.needsID {
			f = ruleID
		}
		argv = append(argv, f)
	}
	argv = append(argv, flags...)
	for _, o := range ex.operands {
		if o == "" && ex.needsID {
			o = ruleID
		}
		argv = append(argv, o)
	}
	return append(argv, operands...)
}

// aRuleID resolves a live rule id from this repository's own config, so the
// exercises run against something real instead of a hard-coded string that
// would quietly become an "unknown rule id" run.
func aRuleID(t *testing.T) string {
	t.Helper()
	cfg, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("loading this repository's own config: %v", err)
	}
	if len(cfg.Rules) == 0 {
		t.Fatal("this repository declares no rules — the per-rule exercises would be vacuous")
	}
	return cfg.Rules[0].ID
}

func runDocumented(t *testing.T, argv []string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := cli.Run(argv, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// The #330 promise, executed. Note what is NOT asserted: not the exit code, and
// not the output. A command may legitimately exit 1 or 2 for its own reasons
// here. The claim under test is only that the flag the manual promises EXISTS —
// "flag provided but not defined" is the exact failure an operator hits.
func TestReferenceManualIntrospectionCommandsTakeTheFormatFlag(t *testing.T) {
	preface, commands, _ := introspectionSection(t)
	if !strings.Contains(preface, "`-format human|json`") {
		t.Fatal("the Introspection preface no longer promises `-format human|json`. This guard exists because that promise was " +
			"false for five of the eight commands it covered (#330) — if the claim moved, move this guard with it; do not " +
			"delete the claim to make the page true.")
	}
	ruleID := aRuleID(t)
	for _, name := range commands {
		t.Run(name, func(t *testing.T) {
			argv := invocationFor(t, name, ruleID, []string{"-format", "json"}, nil)
			_, _, errOut := runDocumented(t, argv)
			if strings.Contains(errOut, "flag provided but not defined") {
				t.Errorf("docs/reference.md promises `formwork %s` takes -format human|json, and it does not:\n"+
					"  %v\n  %s\n"+
					"Add the flag, or move the command out from under the promise — the operator wiring this into CI gets exit 2, "+
					"which this project's exit-code contract defines as \"the run did not reach a verdict\".",
					name, argv, strings.SplitN(errOut, "\n", 2)[0])
			}
		})
	}
}

// The same preface promises "An unknown id, kind, format ... is exit 2 — an
// empty answer to a wrong question would be a guidance fail-open." A command
// that ACCEPTS the flag and silently ignores a value it does not understand
// keeps the letter of the promise above and breaks this one.
func TestReferenceManualIntrospectionRefusesAnUnknownFormat(t *testing.T) {
	preface, commands, _ := introspectionSection(t)
	if !strings.Contains(preface, "format") || !strings.Contains(preface, "exit 2") {
		t.Fatal("the Introspection preface no longer promises exit 2 for an unknown format — see the note on the sibling test")
	}
	ruleID := aRuleID(t)
	for _, name := range commands {
		t.Run(name, func(t *testing.T) {
			argv := invocationFor(t, name, ruleID, []string{"-format", "hologram"}, nil)
			code, out, errOut := runDocumented(t, argv)
			if code != 2 || !strings.Contains(errOut, "unknown format") {
				t.Errorf("`formwork %s -format hologram` must be exit 2 naming the format; got exit %d\n  %v\n  stdout: %q\n  stderr: %q",
					name, code, argv, firstLine(out), firstLine(errOut))
			}
		})
	}
}

// The #288 promise, executed. `rules-for /etc/passwd` always kept it; `scope
// /etc/passwd` answered class=runtime at exit 0 — a confident class for a path
// in a frame the classifier never uses, which is the guidance fail-open the
// preface names.
func TestReferenceManualIntrospectionRefusesAnOutOfRootPath(t *testing.T) {
	preface, _, pathCommands := introspectionSection(t)
	if !strings.Contains(preface, "out-of-root") {
		t.Fatal("the Introspection preface no longer promises exit 2 for an out-of-root path — see the note on the sibling test")
	}
	ruleID := aRuleID(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	for name := range pathCommands {
		t.Run(name, func(t *testing.T) {
			argv := invocationFor(t, name, ruleID, nil, []string{outside})
			code, out, errOut := runDocumented(t, argv)
			if code != 2 {
				t.Errorf("docs/reference.md documents `formwork %s <path>...` and promises exit 2 for an out-of-root path; got exit %d\n"+
					"  %v\n  stdout: %q\n  stderr: %q\n"+
					"An empty — or confident — answer to a wrong question is the guidance fail-open the same paragraph warns about.",
					name, code, argv, firstLine(out), firstLine(errOut))
			}
		})
	}
}

func firstLine(s string) string {
	return strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]
}
