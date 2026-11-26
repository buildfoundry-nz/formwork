//go:build ignore

package scan

import "sample.example/appcore/internal/dto"

// Reverted to a hand-coded DTO: the canonical generated proto import is gone.
func Project(row Row) dto.Page {
	return dto.Page{ID: row.ID}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// import pb "sample.example/schema/gen/go/sample/v1"
