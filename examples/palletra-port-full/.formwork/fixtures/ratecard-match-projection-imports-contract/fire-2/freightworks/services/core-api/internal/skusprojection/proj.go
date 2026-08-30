//go:build ignore

package skusprojection

func project() {}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// import "github.com/palletra/freightworks/services/core-api/internal/ratecardmatch"
