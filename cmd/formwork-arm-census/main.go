// formwork-arm-census — two ways a rule ARM can be written so that it cannot
// fail, both invisible to every instrument the repo already runs.
//
// Usage: go run -C tools/formwork-arm-census . --check=tautology|multi-witness <repo-root>
//
// Product enforcement is formwork type:command
// (.formwork/rules/arm-shape-ratchets.yaml) with origin on this file.
// Exit 0 = no offenders, 1 = offenders listed, 2 = usage/env error.
//
// NO ALLOWLIST, NO REGISTER, NO DECLARED EXCEPTION. Both checks fail on every
// violation in the corpus today, not just on the next one. A ceiling set at the
// current offender count, a file enumerating today's offenders, or a
// `# <rule>-ok:` comment would each be the same move: the class stays in the
// tree and the gate reports green. The cure is to fix the arm.
//
// # --check=tautology — the required pattern that requires nothing
//
// `required-pattern` with `pattern: '.'` is a gate on the file having a line.
// shell-unit-tests-exist was exactly that over
// scripts/lib/file-size-caps.test.sh: every one of its fifty assertions about
// the file-size cap resolver (the #8629/#8630 scar) could be deleted, the file
// replaced with `#!/usr/bin/env bash` + `exit 0`, and the arm stayed green — as
// did its sibling shell-unit-tests, which only required ci.yml to NAME the
// script. CI then ran the gutted script, got exit 0, and reported the lane
// green. The pair was jointly inert (#12182).
//
// No instrument saw it. `formwork lint` checks that the scope matches a file,
// and it did. The vacuity census's class-2 probes ask whether the evidence is a
// comment or the rule's own detector; `.` matches the code plane, so it is
// neither. `formwork test` replayed a fire fixture holding an EMPTY file, which
// the arm does fail — but emptying the file also empties the scope, so the only
// state the fixture proved was the one state that cannot occur in a tree where
// the file exists at all.
//
// The verdict is empirical, not a blocklist of spellings: a pattern is
// tautological when it matches every line of an arbitrary-line probe corpus
// (tautology.go). `.`, `.*`, `[\s\S]*`, `(?s).*`, `[^]`, `.{0,}` and `[a-z]*`
// are one rule written seven ways, and enumerating six of them just names the
// seventh.
//
// # --check=multi-witness — the last-witness gate
//
// An existence obligation — `required-pattern mode: exists`, or `pattern-count
// op: at-least` — passes as long as ONE witness survives. With one or two
// witnesses in the tree that is a real gate: delete the line and it fires. With
// dozens it is a gate against the class being deleted ENTIRELY, which is not a
// regression anyone was going to commit, while the regression its own cure
// names sails through. Three measured on develop@708fe7a0aa:
//
//	withtenantorg-adopted             74 witnesses. Cure: "convert a
//	                                  worker-pipeline writer to call
//	                                  db.WithTenantOrg (audit-2 BE-ARCH-1)".
//	                                  Revert 73 of the 74 production call sites
//	                                  to db.WithAdmin — the RLS-bypass
//	                                  regression the rule is NAMED for — and it
//	                                  passed.
//	schema-contract-boundary          54 witnesses, 18 of them in _test.go
//	                                  files. Cure asks for "at least one scan/
//	                                  helper" importing the generated protos;
//	                                  strip every non-test import and hand-roll
//	                                  DTOs, and it stayed green off a test file.
//	schema-snapshot-has-btree-indexes 517 witnesses, all CREATE INDEX lines.
//	                                  Cure: the snapshot "must remain the
//	                                  canonical index inventory"; drop 516 and
//	                                  one surviving index still read as
//	                                  canonical.
//
// The cure is a counted ratchet: `pattern-count op: at-least` with n at the
// measured count, so the FIRST witness lost is the one that fires. The detector
// prints the n to write.
//
// # Why an external tool
//
// The engine's scanner skips `.formwork/` outright
// (formwork's internal/scan/scan.go), so no declarative rule can read the
// rule corpus — a `required-pattern` scoped at `.formwork/rules/**` matches
// zero files and passes vacuously forever, which is the exact defect this file
// exists to catch. Same reason formwork-rules-not-vacuous,
// exists-rule-cure-not-universal and count-relation-arm-is-anchored all shell
// out to a census.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

// check names one of the two questions, its rule id, and the one-line reason
// printed beside each offender.
type check struct {
	name   string
	ruleID string
	why    string
}

var checks = map[string]check{
	"tautology": {
		name:   "tautology",
		ruleID: "no-tautological-required-pattern",
		why: "is a required-pattern whose regex matches an arbitrary line — it requires nothing of the file " +
			"beyond having a line at all, so every line the rule is about can be deleted and it stays green",
	},
	"narrowable": {
		name:   "narrowable",
		ruleID: "required-pattern-is-not-a-prefix-of-its-own-narrowing",
		why: "is a required-pattern whose match stops inside a traversal source expression — narrowing that " +
			"collection appends past the end of the match, so the walk the arm guards can be cut to its first " +
			"member with the arm still green",
	},
	"embedded": {
		name:   "embedded",
		ruleID: "required-pattern-is-not-a-substring-of-a-longer-token",
		why: "is a required-pattern that a LONGER identifier in its own scope keeps green — deleting every " +
			"place the pattern stands as its own token leaves the arm matching text that belongs to some " +
			"other construct, so the thing the rule is about can go and the arm never notices",
	},
	"multi-witness": {
		name:   "multi-witness",
		ruleID: "multi-witness-arm-is-a-ratchet",
		why: "is an existence obligation with far more witnesses than its own floor — no single regression " +
			"can trip it, only deletion of the whole class",
	},
}

// offender is one flagged arm plus the human-readable detail of its verdict.
type offender struct {
	file   string
	line   int
	arm    string
	detail string
}

func main() {
	which := flag.String("check", "", "which question to ask: tautology | multi-witness | narrowable | embedded")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: go run -C tools/formwork-arm-census . --check=tautology|multi-witness|narrowable|embedded <repo-root>")
	}
	flag.Parse()
	c, ok := checks[*which]
	if !ok || flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	os.Exit(run(c, flag.Arg(0), os.Stdout, os.Stderr))
}

func run(c check, root string, out, errOut io.Writer) int {
	// Calibrate the instrument before reporting on the corpus (calibrate.go).
	// A census whose verdict machinery is broken reports "0 flagged" and reads
	// exactly like a clean tree.
	if err := calibrate(c.name, out); err != nil {
		fmt.Fprintln(errOut, "arm census:", err)
		return 2
	}
	arms, err := loadCorpus(root)
	if err != nil {
		fmt.Fprintln(errOut, "arm census:", err)
		return 2
	}
	var (
		bad      []offender
		examined int
	)
	switch c.name {
	case "tautology":
		bad, examined, err = detectTautologies(arms)
	case "multi-witness":
		bad, examined, err = detectMultiWitness(root, arms)
	case "narrowable":
		bad, examined, err = detectNarrowable(root, arms)
	case "embedded":
		bad, examined, err = detectEmbedded(root, arms)
	}
	if err != nil {
		fmt.Fprintln(errOut, "arm census:", err)
		return 2
	}
	fmt.Fprintf(out, "arm census (%s): %d arm(s) in corpus, %d examined, %d flagged\n",
		c.name, len(arms), examined, len(bad))
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].file != bad[j].file {
			return bad[i].file < bad[j].file
		}
		return bad[i].line < bad[j].line
	})
	for _, b := range bad {
		fmt.Fprintf(out, "FAIL %s:%d: arm %q %s\n     %s\n", b.file, b.line, b.arm, c.why, b.detail)
	}
	if len(bad) > 0 {
		fmt.Fprintf(out, "%s: %d offending arm(s)\n", c.ruleID, len(bad))
		return 1
	}
	fmt.Fprintf(out, "%s: OK\n", c.ruleID)
	return 0
}

// detectTautologies flags every `required-pattern` arm whose pattern matches an
// arbitrary line. Only required-pattern: a FORBIDDEN pattern of `.` bans every
// non-empty file, which is a rule that always fails, not one that never does —
// a different, and self-announcing, defect.
func detectTautologies(arms []arm) ([]offender, int, error) {
	var bad []offender
	examined := 0
	for _, a := range arms {
		if a.Type != "required-pattern" || a.Pattern == "" {
			continue
		}
		examined++
		taut, err := tautological(a.Pattern, a.Syntax)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d (%s): %w", a.File, a.Line, a.ID, err)
		}
		if taut {
			bad = append(bad, offender{a.File, a.Line, a.ID, fmt.Sprintf(
				"pattern %q matches every probe line. Replace it with the token the rule is actually about, "+
					"or express the invariant as a pattern-count floor.", a.Pattern)})
		}
	}
	return bad, examined, nil
}
