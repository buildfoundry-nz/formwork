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
