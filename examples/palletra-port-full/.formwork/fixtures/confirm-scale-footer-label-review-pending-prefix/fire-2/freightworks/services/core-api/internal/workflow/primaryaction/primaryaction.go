//go:build ignore

package primaryaction

// labelReviewPendingPrefix lost its intentional trailing space, so the
// footer would compose "Review Unapproved in{label}" (W-LABELS).
const (
	labelScaleConfirm         = "Confirm Scale"
	labelAccept               = "Approve"
	labelApproveRackingInputs = "Approve inputs"
	labelSkipAction           = "Skip"
	labelSectionDone          = "Section Complete"
	labelProceedToSkus        = "Continue to Skus"
	labelReviewPendingPrefix  = "Review Unapproved in"
)

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// labelReviewPendingPrefix  = "Review Pending in "
