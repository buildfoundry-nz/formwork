//go:build ignore

package primaryaction

// labelProceedToSkus has drifted from the frozen "Continue to Skus"
// literal (#4224) — the footer terminal button copy no longer matches (W-LABELS).
const (
	labelScaleConfirm         = "Confirm Scale"
	labelAccept               = "Approve"
	labelApproveRackingInputs = "Approve inputs"
	labelSkipAction           = "Skip"
	labelSectionDone          = "Section Complete"
	labelProceedToSkus        = "Proceed to Skus"
	labelReviewPendingPrefix  = "Review Pending in "
)

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// labelProceedToSkus        = "Continue to Skus"
