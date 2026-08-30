//go:build ignore

package primaryaction

// The decider mints two footer kinds, but the proto doc-comment below documents
// only one — upcoming_section drifted out of the on-the-wire docs (the W7 regression).
const (
	KindConfirm        = "approve"
	KindAdvanceSection = "upcoming_section"
)
