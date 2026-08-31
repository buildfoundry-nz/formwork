// sourcelist.go — the class-1 arm that pins a rule's DECLARED source list to
// the sources it claims to cover (#13517).
//
// THE DEFECT. formwork-rules-not-vacuous enumerates, file by file, the Go
// sources the vacuity census is built from. Nothing checked that enumeration
// against the package on disk, and it drifted by three files — scopeglobs.go
// (#10777), scopeindex.go and wallbudget.go (#12419) — across three separate
// changes over months, with every gate green throughout. The control is the
// identical classify.go split that produced relation.go (#12178): same
// operation, list updated, no gate between them either way. The rule that
// proves no other rule has quietly stopped protecting anything was itself
// maintained by a comment asking the next person to remember, and that comment
// had already failed three times running.
//
// WHY THE OTHER ARMS CANNOT SEE IT. Every instrument here measures a DECLARED
// glob against the tree; this defect is a source with no glob to measure. A
// file present in the directory and absent from the list matches nothing, is
// counted nowhere, and is invisible to EMPTY-SCOPE, EMPTY-GLOB and
// GLOB-UNTRACKED alike. Only the reverse direction — a listed path that is
// gone — was ever observable, and EMPTY-GLOB already gates it. The arm below is
// deliberately one-directional for that reason: adding it to the deletion side
// would double-count the same fact as two offenders.
//
// OPT-IN, BY MARKER. The census cannot guess which include lists are meant to
// be exhaustive — most are not, and a rule watching six unrelated trees would
// fire constantly. A rule subscribes by declaring it, in the comment plane the
// per-glob arm already reads:
//
//	scope:
//	  # source-list-exhaustive: tools/formwork-vacuity-census
//	  include:
//	    - "tools/formwork-vacuity-census/main.go"
//
// Non-recursive, and non-test only: a Go package IS one directory, and a test
// file is not a source the rule is built from. Coverage is decided with
// formwork's own matcher, so a list that names its directory with a glob is
// covered rather than flagged — the claim is that nothing is MISSING, not that
// the list is spelled one particular way.
package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

// sourcesIn returns the non-test Go sources sitting DIRECTLY in dir, sorted.
//
// Non-recursive by path.Dir rather than by prefix: a Go package is one
// directory, so tools/census/internal/helper.go belongs to another package and
// owes this list nothing. A prefix test would drag every subpackage in and make
// the marker fire the day a tool grows one.
//
// _test.go is excluded because the list declares what the rule is BUILT from.
// A rule's tests are not its sources, and the census's own package gains them
// constantly — obliging a scope.include row for each would turn the marker into
// a tax on writing tests.
func sourcesIn(dir string, paths []string) []string {
	var out []string
	for _, p := range paths {
		if path.Dir(p) != dir {
			continue
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// sourceListVerdicts judges every `# source-list-exhaustive:` declaration in the
// corpus against the scanned tree, returning verdicts keyed by rule id in the
// shape attachInventoryVerdicts folds.
//
// SOURCE-UNLISTED — a non-test .go file directly in the declared directory that
// no include glob covers. This is the #13517 defect itself. One verdict per
// rule, carrying every missing source as evidence: the cure is one edit to one
// list, so splitting it per file would report one problem as several.
//
// SOURCE-LIST-VACUOUS — the declared directory yields no non-test .go file at
// all. A marker over an empty set gates nothing, which is precisely the failure
// this census exists to catch, one level up; the likeliest causes are a typo and
// a path under .formwork/, which scan.Walk drops before any rule runs.
//
// Coverage goes through formwork's own matcher, never a re-implementation — the
// same choice countGlobMatches makes, and for the same reason (#10083: the shell
// cannot expand `**`, and 133 false zeros came of assuming it could). Matching
// the include globs ALONE is deliberate: the marker is a claim about the include
// list, so a rule's exclude/except entries must not be able to retire a source
// from it silently.
func sourceListVerdicts(gm *globMeasure, paths []string) map[string][]verdict {
	out := map[string][]verdict{}
	for _, d := range gm.sourceLists {
		dir := strings.TrimSuffix(d.dir, "/")
		sources := sourcesIn(dir, paths)
		if len(sources) == 0 {
			out[d.ruleID] = append(out[d.ruleID], verdict{
				class: class1Glob, code: "SOURCE-LIST-VACUOUS", gating: true,
				why: fmt.Sprintf("`# source-list-exhaustive: %s` declares a scope.include list exhaustive "+
					"over a directory holding no non-test Go source — the declaration reads as a guard "+
					"while gating nothing. Point it at the real package directory, or drop the marker. "+
					"A path under .formwork/ is always this finding: scan.Walk drops that directory "+
					"before any rule runs, so nothing there can ever be matched (#13517)", d.dir),
				evidence: []string{d.dir},
			})
			continue
		}

		// An include list cannot be empty on a rule the engine loaded, so this is
		// the marker sitting on a list config.Load would already have rejected.
		// Reported rather than skipped: every source is uncovered by definition.
		missing := sources
		if len(d.globs) > 0 {
			r, err := config.New("sourcelist", "forbidden-pattern", finding.SeverityError, "",
				d.globs, nil, nil, nil)
			if err != nil {
				// Unreachable through the real loader — config.Load builds every rule
				// through this same constructor, so an invalid glob fails the census
				// long before here. Fail closed rather than silently drop the arm.
				out[d.ruleID] = append(out[d.ruleID], verdict{
					class: class1Glob, code: "SOURCE-LIST-UNREADABLE", gating: true,
					why: fmt.Sprintf("the scope.include list carrying `# source-list-exhaustive: %s` "+
						"could not be compiled into a matcher, so its coverage cannot be judged: %v", d.dir, err),
					evidence: []string{d.dir},
				})
				continue
			}
			missing = nil
			for _, s := range sources {
				if !r.Applies(s) {
					missing = append(missing, s)
				}
			}
		}
		if len(missing) == 0 {
			continue
		}
		out[d.ruleID] = append(out[d.ruleID], verdict{
			class: class1Glob, code: "SOURCE-UNLISTED", gating: true,
			why: fmt.Sprintf("scope.include declares itself exhaustive over %s (`# source-list-exhaustive:`) "+
				"but %d of its %d non-test Go source(s) are not covered by any entry. A source absent from "+
				"the list matches no glob, so no other arm here can see it — EMPTY-GLOB and GLOB-UNTRACKED "+
				"both judge globs that ARE declared. Add the missing entries, and regenerate the rule's "+
				"live-include registry in the same change (#13517)", dir, len(missing), len(sources)),
			evidence: missing,
		})
	}
	return out
}
