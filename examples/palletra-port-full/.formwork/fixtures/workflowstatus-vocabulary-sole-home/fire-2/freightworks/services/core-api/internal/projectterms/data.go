//go:build ignore

package projectterms

// Anti-vacuity failure: the taxonomy token has been renamed/moved out of this
// file, so the deleted-registry ban would be guarding a ghost. The required
// anchor is deliberately absent here (the vocabulary type was renamed away).
type JobStatus struct {
	Value string
	Rank  int
}

var Statuses = []JobStatus{
	{Value: "uploaded", Rank: 0},
	{Value: "priced", Rank: 1},
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// // The vocabulary still lives here: the WorkflowPhase taxonomy (ordered set +
