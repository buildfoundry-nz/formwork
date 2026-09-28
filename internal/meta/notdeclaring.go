// notdeclaring.go — dead-entry hygiene for scope.not_declaring. Split from
// lint.go, which the 750-line vendor cap bounds; same package.
//
// A marker no file in the rule's scope declares subtracts nothing, and that is
// the failure mode which makes an exemption dangerous rather than merely
// useless: it reads as a narrow, reasoned carve-out while protecting nothing, so
// a reader stops looking for the real one.
//
// No comment escape here, unlike scope.exclude. A preventative glob for a
// directory the repo has not created yet is a coherent thing to write; a marker
// for a declaration no generator emits is a guess, and its cure is to name a
// marker some generator actually writes.
package meta

import (
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// notDeclaringProblems reports each of the rule's scope.not_declaring markers
// that no file it covers declares.
func notDeclaringProblems(r *config.Rule, fset *scan.FileSet) []string {
	var problems []string
	for _, m := range r.NotDeclaring() {
		if declaredByAny(m, r, fset) {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: scope.not_declaring %q is declared by no file this rule covers — it subtracts nothing", r.ID, m))
	}
	return problems
}

// declaredByAny reports whether any file in the rule's PATH scope declares the
// marker. Scoped to the rule's own include/exclude on purpose: a marker is dead
// relative to what the rule looks at, and asking the whole tree would call an
// entry live because some unrelated file elsewhere happens to carry it.
//
// A file whose content cannot be read counts as not declaring: the alternative
// is calling an entry live on a file nobody could read, which is the direction
// that hides a dead entry.
func declaredByAny(marker string, r *config.Rule, fset *scan.FileSet) bool {
	for _, f := range fset.Files {
		if !r.Applies(f.Path()) {
			continue
		}
		content, err := f.Content()
		if err != nil {
			continue
		}
		if _, ok := r.DeclinedBy(content); ok {
			return true
		}
	}
	return false
}
