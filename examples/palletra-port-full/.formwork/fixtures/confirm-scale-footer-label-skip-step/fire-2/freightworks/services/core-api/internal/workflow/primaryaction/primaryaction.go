//go:build ignore

package primaryaction

// The labelSkipAction const drifted away from its frozen literal.
const (
	labelScaleConfirm = "Confirm Scale"
	labelAccept       = "Approve"
	labelSkipAction   = "Skip Step"
)

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// labelSkipAction   = "Skip"
