// credentials.go — dead-entry hygiene for require_absent credentials. Split
// from lint.go, which the 750-line vendor cap bounds; same package.
//
// A credential no in-scope line holds sanctions nothing. The check used to
// report that as a holder count of zero, which also fired on every pruned or
// synthetic tree that carries one rule beside only the files its own proof
// needs: such a tree holds none of the repository's homes, and a check cannot
// tell that from a home that is gone. The zero case is judged here instead,
// over the whole tree, as notDeclaringProblems judges a marker no file
// declares, so a dead credential still fails `formwork lint` while a scratch
// tree still runs `formwork check`.
package meta

import (
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// credentialProblems reports each of the rule's require_absent credentials
// that no line of the files it reads holds. The files are the ones the engine
// would hand the rule: in scope, generated output dropped unless the rule
// judges it, each in the variant the rule's preprocess produces.
func credentialProblems(r *config.Rule, fset *scan.FileSet) []string {
	d, ok := r.Checker.(interface {
		DeadCredentials([]*scan.File) ([]string, error)
	})
	if !ok {
		return nil
	}
	gen := r.Generation()
	var files []*scan.File
	for _, f := range fset.Files {
		if !r.Applies(f.Path()) {
			continue
		}
		if gen != nil && !r.JudgesGenerated() {
			content, err := f.Content()
			if err != nil {
				return []string{fmt.Sprintf("%s: reading %s to test its generator header: %v", r.ID, f.Path(), err)}
			}
			if _, isGen := gen.Declares(content); isGen {
				continue
			}
		}
		v, err := f.Variant(r.Preprocess)
		if err != nil {
			return []string{fmt.Sprintf("%s: reading %s as the rule does: %v", r.ID, f.Path(), err)}
		}
		files = append(files, v)
	}
	dead, err := d.DeadCredentials(files)
	if err != nil {
		return []string{fmt.Sprintf("%s: matching credentials: %v", r.ID, err)}
	}
	var problems []string
	for _, src := range dead {
		problems = append(problems, fmt.Sprintf("%s: credential %q is held by no line this rule covers — it sanctions nothing", r.ID, src))
	}
	return problems
}
