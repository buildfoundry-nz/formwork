//go:build ignore

package projectterms

// The vocabulary still lives here: the WorkflowPhase taxonomy (ordered set +
// each state's badge label, tone and icon) is the single Go source of truth.
type WorkflowPhase struct {
	Value string
	Rank  int
	Label string
	Tone  string
	Icon  string
}

func (WorkflowPhase) Values() []WorkflowPhase {
	return []WorkflowPhase{
		{Value: "uploaded", Rank: 0, Label: "Uploaded"},
		{Value: "priced", Rank: 1, Label: "Priced"},
	}
}
