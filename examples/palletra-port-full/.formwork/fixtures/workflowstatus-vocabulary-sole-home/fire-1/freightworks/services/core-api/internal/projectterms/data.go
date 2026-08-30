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
