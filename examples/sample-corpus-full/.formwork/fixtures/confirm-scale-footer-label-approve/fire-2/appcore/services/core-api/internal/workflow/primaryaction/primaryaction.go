//go:build ignore

package primaryaction

// labelAccept has drifted from the frozen "Approve" literal (W-LABELS).
const (
	labelScaleConfirm         = "Confirm Scale"
	labelAccept               = "Approved"
	labelApproveRackingInputs = "Approve inputs"
	labelSkipAction           = "Skip"
	labelSectionDone          = "Section Complete"
	labelProceedToSkus        = "Continue to Skus"
	labelReviewPendingPrefix  = "Review Pending in "
)

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// labelAccept               = "Approve"
