// unreadable.go — lint's refusal to judge a tree it cannot read (#30). Split
// from lint.go, which the 750-line vendor cap bounds; same package.
package meta

import (
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// refuseUnreadableInScope returns an error naming the first file some rule's
// scope selects that cannot be read.
//
// "In scope for at least one rule" is the claim it makes and the claim it can
// support: a file no rule governs changes no verdict lint reports, so refusing
// over one would make lint reject repositories on account of files it never
// judges. Files are visited in the walk's order, which scan sorts, so a repo
// with several unreadable files always names the same one.
func refuseUnreadableInScope(rls []*config.Rule, files []*scan.File) error {
	for _, f := range files {
		governed := false
		for _, r := range rls {
			if r.Applies(f.Path()) {
				governed = true
				break
			}
		}
		if !governed {
			continue
		}
		if _, err := f.Content(); err != nil {
			return fmt.Errorf("lint cannot judge this repository: %s is in scope for at least one rule but could not be read (%w) — every verdict below draws on file content, and a governed file the engine cannot read is a rule that is not enforced", f.Path(), err)
		}
	}
	return nil
}
