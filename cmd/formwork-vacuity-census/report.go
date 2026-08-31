// report.go — how the census SAYS what it found.
//
// Split out of main.go so that emission order lives in one file and nothing
// else does. The order is load-bearing rather than cosmetic: product
// enforcement runs this census as a formwork type:command rule, and such a
// finding keeps only a head and a tail of the detector's output — snippet() in
// formwork's internal/rules/command/command.go elides the middle, before
// the finding is constructed, so the CI annotation, the job log and
// formwork-findings.json all carry the same clipped string (#16031).

package main

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func report(rows []row, cfg *config.Config, fset *scan.FileSet, gm *globMeasure, meta map[string]ruleMeta, added map[string]bool, calibrated []byte, inCheckout, full bool, stdout, stderr io.Writer) int {
	byClass := map[string][]row{}
	population := map[string]int{}
	for _, rw := range rows {
		if rw.existenceObligation {
			population["class2"]++
		}
		if rw.hasFixtures {
			population["class3"]++
		}
		// One entry per row per class, not per verdict: a row carrying two
		// class-2 verdicts must not be listed (and counted) twice.
		seen := map[string]bool{}
		for _, v := range rw.verdicts {
			if seen[v.class] {
				continue
			}
			seen[v.class] = true
			byClass[v.class] = append(byClass[v.class], rw)
		}
	}

	// Provenance, verdicts and the closing banner are three separate emissions
	// so report() owns the ORDER they appear in and nothing else does. The
	// order is not cosmetic: a type:command finding keeps only a head and a
	// tail of the detector's output (snippet() in
	// formwork's internal/rules/command/command.go), so whatever prints
	// first is what a CI reader is left with.
	reporting, quiet, offenders := renderClassBlocks(byClass)
	if offenders > 0 {
		// Verdicts first. A type:command finding keeps only a head and a tail
		// of this output, so the first bytes are the only ones a CI reader is
		// certain to see, and the offending rule ids are what they came for.
		//
		// The classes that reported NOTHING are held back to the quiet block
		// below rather than dropped. They still print — a class reporting zero
		// must stay distinguishable from a class whose arm did not run — but
		// four "…: 0" headlines ahead of the first real verdict would spend
		// more than half the retained head saying nothing happened.
		stdout.Write([]byte(rollup(byClass, offenders)))
		stdout.Write([]byte(reporting))
		stdout.Write([]byte(quiet))
		stdout.Write(calibrated)
		printProvenance(rows, cfg, fset, gm, meta, population, added, inCheckout, full, stdout)
		return printVerdict(offenders, stdout, stderr)
	}
	// Nothing failed, so nothing competes for the head: keep the passing run's
	// order byte for byte, opening with the evidence that the instrument could
	// see. Every class is quiet here, so the two blocks below are one block.
	stdout.Write(calibrated)
	printProvenance(rows, cfg, fset, gm, meta, population, added, inCheckout, full, stdout)
	stdout.Write([]byte(reporting))
	stdout.Write([]byte(quiet))
	return printVerdict(offenders, stdout, stderr)
}

// printProvenance writes the corpus summary, the counts this instrument cannot
// decide, the optional full tables and the added-rules arm — every line that
// describes the RUN rather than a verdict.
func printProvenance(rows []row, cfg *config.Config, fset *scan.FileSet, gm *globMeasure, meta map[string]ruleMeta, population map[string]int, added map[string]bool, inCheckout, full bool, stdout io.Writer) {
	fmt.Fprintf(stdout, "\n%s corpus: %d rules, %d files scanned\n", tag, len(cfg.Rules), len(fset.Files))
	fmt.Fprintf(stdout, "  scope.include globs checked per-glob:        %d\n", gm.totalInclude)
	fmt.Fprintf(stdout, "  class-2 population (existence-obligation rules): %d\n", population["class2"])
	fmt.Fprintf(stdout, "  class-3 population (fixture-carrying rules):     %d\n", population["class3"])
	if n := undecidableRelations(cfg, meta); n > 0 {
		// Said out loud rather than left to be inferred from a zero. A verdict
		// this instrument cannot decide is a hole in its coverage, and a hole
		// that goes unnamed reads as coverage — which is the defect the census
		// exists to catch, one level up (#12178).
		fmt.Fprintf(stdout, "  set-relation rules NOT decided here:            %d\n", n)
		fmt.Fprintf(stdout, "    (relation: disjoint, or the two sides drawn from overlapping globs. Blanking\n")
		fmt.Fprintf(stdout, "     the b side cannot falsify either shape, so no verdict is claimed — #12180.)\n")
	}
	// Same obligation, one arm over: INERT-EXCEPT judges a literal except.paths
	// entry by re-running the rule over the one file it names, and that probe is
	// invalid for a heavy rule and misreads a relation. Those entries get no
	// verdict, so the count is said out loud rather than folded into the arm's
	// zero (#10777).
	if n := undecidedExceptEntries(cfg, gm); n > 0 {
		fmt.Fprintf(stdout, "  except.paths entries NOT decided here:         %d\n", n)
		fmt.Fprintf(stdout, "    (heavy or relation rules: a one-file probe would shell out once per entry, or\n")
		fmt.Fprintf(stdout, "     misread a relation an empty tree satisfies. No verdict is claimed — #10777.)\n")
	}
	// Same obligation again, for the name-anchored go types. DEAD-SYMBOL and
	// DEAD-SINK read an absent call site, which only condemns an anchor whose
	// subject is declared nowhere; a package-qualified pattern names another
	// package and the census has no type information to resolve it. Those rules
	// get no verdict either way, so the count is said out loud rather than folded
	// into the class-2 zero (#12494).
	if n := undecidedSymbolAnchors(cfg); n > 0 {
		fmt.Fprintf(stdout, "  symbol-anchored rules NOT decided here:        %d\n", n)
		fmt.Fprintf(stdout, "    (the anchor names another package, so an absent call site cannot prove the\n")
		fmt.Fprintf(stdout, "     symbol unreachable. No verdict is claimed — #12494.)\n")
	}

	if full {
		fmt.Fprintf(stdout, "\n%s classification table\n", tag)
		fmt.Fprintf(stdout, "  %-56s %-18s %6s %6s %-9s %s\n", "RULE", "TYPE", "SCOPE", "WITN", "FIXTURES", "VERDICT")
		for _, rw := range rows {
			verdict := "live"
			if len(rw.verdicts) > 0 {
				var vs []string
				for _, v := range rw.verdicts {
					vs = append(vs, v.code)
				}
				verdict = strings.Join(vs, ",")
			}
			w := "-"
			if rw.existenceObligation {
				w = fmt.Sprint(len(rw.witnesses))
			}
			fx := "-"
			if rw.hasFixtures {
				fx = fmt.Sprintf("%df/%dp", rw.fireCount, rw.passCount)
			}
			fmt.Fprintf(stdout, "  %-56s %-18s %6d %6s %-9s %s\n", rw.id, rw.typ, rw.scopeN, w, fx, verdict)
		}

		// The per-glob table is what makes a "N dead globs" claim reproducible
		// by anyone with a checkout (#10626): every include glob with its own
		// match count, dead ones marked, declared ones shown with their reason.
		fmt.Fprintf(stdout, "\n%s per-glob scope.include counts\n", tag)
		for _, rw := range rows {
			for _, g := range gm.include[rw.id] {
				mark := ""
				switch {
				case g.n == 0 && g.deadOK:
					mark = "  DECLARED-DEAD: " + g.reason
				case g.n == 0:
					mark = "  DEAD"
				}
				fmt.Fprintf(stdout, "  %-56s %8d %s%s\n", rw.id, g.n, g.glob, mark)
			}
		}
	}

	// Said out loud for the same reason the three counts above are: the arm that
	// keeps those holes from growing is diff-scoped, so a reader must be able to
	// tell "this change adds no rule" from "the arm did not run". Inert is the
	// stated base case — a proof scratch or a synthetic corpus, neither of which
	// has a change to judge — never a verdict (#15837).
	switch {
	case !inCheckout:
		fmt.Fprintf(stdout, "  rules ADDED by this change:                    (not a git checkout — arm inert)\n")
	default:
		fmt.Fprintf(stdout, "  rules ADDED by this change:                    %d\n", len(added))
		if ids := newRuleIDs(added); len(ids) > 0 {
			fmt.Fprintf(stdout, "    %s\n", strings.Join(ids, ", "))
		}
	}

}

// printClassBlocks writes every class headline and its gating detail, and
// returns how many gating verdicts it wrote. The headline prints at zero too:
// a class reporting nothing must stay visibly distinguishable from a class
// whose arm did not run.
func renderClassBlocks(byClass map[string][]row) (reporting, quiet string, offenders int) {
	var reportingBuf, quietBuf bytes.Buffer
	offenders = 0
	for _, class := range []string{classNew, class1, class1Glob, class2, class3} {
		list := byClass[class]
		sort.Slice(list, func(i, j int) bool { return list[i].id < list[j].id })
		n := 0
		for _, rw := range list {
			for _, v := range rw.verdicts {
				if v.class == class && v.gating {
					n++
				}
			}
		}
		// A class with nothing to report goes to the quiet buffer; one with a
		// verdict takes its headline AND its detail to the reporting buffer,
		// so the two never separate.
		w := &quietBuf
		if n > 0 {
			w = &reportingBuf
		}
		fmt.Fprintf(w, "\n%s %s: %d\n", tag, classTitle[class], n)
		for _, rw := range list {
			for _, v := range rw.verdicts {
				if v.class != class || !v.gating {
					continue
				}
				offenders++
				fmt.Fprintf(w, "  %s [%s] %s\n", rw.id, v.code, v.why)
				for _, e := range v.evidence {
					fmt.Fprintf(w, "      %s\n", e)
				}
			}
		}
	}

	return reportingBuf.String(), quietBuf.String(), offenders
}

// rollupNames caps how many rule ids the roll-up spells out. A line that grows
// with the corpus is not a line: this census's own mutation proof blanks the
// fixture-directory prefix, which makes all ~2198 fixture-carrying rules report
// at once, and an uncapped roll-up would be a single ~115 KB line. Capping is a
// readability rule first, but it has a second effect that matters more — the
// per-class detail below then starts at a bounded offset instead of a
// corpus-sized one, so the first COMPLETE finding (its code, its subject, its
// cure) stays reachable however many rules failed. Seven names and a count tell
// a reader the scale and give them a foothold; two thousand names tell them
// neither.
const rollupNames = 7

// rollup is the line that must survive truncation: the offender count, the
// first rollupNames rule ids with their verdict codes, and how many more went
// unnamed. A repeated code is counted rather than repeated. It is emitted
// before the per-class detail so a reader gets scale and names before anything
// else competes for the retained head.
func rollup(byClass map[string][]row, offenders int) string {
	type entry struct {
		id    string
		codes []string
	}
	seen := map[string]int{}
	var order []*entry
	for _, class := range []string{classNew, class1, class1Glob, class2, class3} {
		list := byClass[class]
		sort.Slice(list, func(i, j int) bool { return list[i].id < list[j].id })
		for _, rw := range list {
			for _, v := range rw.verdicts {
				if v.class != class || !v.gating {
					continue
				}
				i, ok := seen[rw.id]
				if !ok {
					i = len(order)
					seen[rw.id] = i
					order = append(order, &entry{id: rw.id})
				}
				order[i].codes = append(order[i].codes, v.code)
			}
		}
	}
	var parts []string
	for i, e := range order {
		if i == rollupNames {
			break
		}
		counts := map[string]int{}
		var codeOrder []string
		for _, c := range e.codes {
			if counts[c] == 0 {
				codeOrder = append(codeOrder, c)
			}
			counts[c]++
		}
		var cs []string
		for _, c := range codeOrder {
			if counts[c] > 1 {
				cs = append(cs, fmt.Sprintf("%s x%d", c, counts[c]))
				continue
			}
			cs = append(cs, c)
		}
		parts = append(parts, fmt.Sprintf("%s [%s]", e.id, strings.Join(cs, ", ")))
	}
	more := ""
	if n := len(order) - len(parts); n > 0 {
		more = fmt.Sprintf(", and %d more", n)
	}
	return fmt.Sprintf("%s FAIL: %d rule(s) cannot fail — %s%s\n", tag, len(order), strings.Join(parts, ", "), more)
}

// printVerdict writes the closing line and returns the process exit code.
func printVerdict(offenders int, stdout, stderr io.Writer) int {
	if offenders > 0 {
		fmt.Fprintf(stderr, "\n%s FAIL: %d rule(s) that cannot fail. Make the rule express its real invariant "+
			"(narrow the scope, move off mode:exists, declare the preprocess it actually reads), or delete it. "+
			"A dead scope.include glob is deleted when a sibling covers the subject, repointed when the code "+
			"moved, or declared dead in place with `# glob-dead: <reason>` when it is genuinely aspirational. "+
			"A GLOB-REMOVED finding means a live include glob was deleted from a rule while still listed in "+
			"%s — restore the glob or drop that row in the same change so the coverage loss is reviewable (#10876). "+
			"A FIXTURE-REMOVED finding is the same move one level over: a fixture directory was deleted while still listed in "+
			"%s/<rule>/%s, and the fixtures left behind cannot report the loss — restore the fixture or drop the row in the same change (#10838).\n",
			tag, offenders, liveIncludeInventoryRel, fixturesRootRel, fixtureManifestName)
		return 1
	}
	fmt.Fprintf(stdout, "\n%s OK: every rule can fail.\n", tag)
	return 0
}
