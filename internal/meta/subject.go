// subject.go — scope.subject: production in the escape-hatch census and its
// dead-entry hygiene. Split from census.go and lint.go, which the 750-line
// vendor cap bounds; same package.
//
// A production subject leaves the repository's test code out of the rule
// (config/testcode.go). That is a statement about what the rule is about, not
// a forgiven violation, but it still removes files from evaluation, so the
// census counts them on every run and lint refuses a statement that removes
// nothing.
package meta

import (
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// productionLeftOut counts the files the rule's include and exclude reach
// that the repository's tests: names: the files scope.subject: production
// leaves out.
func productionLeftOut(r *config.Rule, fset *scan.FileSet) int {
	if !r.SubjectIsProduction() {
		return 0
	}
	n := 0
	for _, f := range fset.Files {
		p := f.Path()
		if r.TestCode().Contains(p) && r.InPathScope(p) {
			n++
		}
	}
	return n
}

// subjectCensusLine is the census line for a production-subject rule.
func subjectCensusLine(r *config.Rule, fset *scan.FileSet) (string, bool) {
	if !r.SubjectIsProduction() {
		return "", false
	}
	return fmt.Sprintf("  %s: scope.subject: production: %d test file(s) left out of this rule's evaluation", r.ID, productionLeftOut(r, fset)), true
}

// subjectProblems reports a production subject that leaves out no file: a
// statement about tests on a rule that reaches none says nothing, and reads as
// a decision someone made.
func subjectProblems(r *config.Rule, fset *scan.FileSet) []string {
	if !r.SubjectIsProduction() || productionLeftOut(r, fset) > 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s: scope.subject: production leaves out no file this rule's scope reaches — it says nothing; drop it", r.ID)}
}
