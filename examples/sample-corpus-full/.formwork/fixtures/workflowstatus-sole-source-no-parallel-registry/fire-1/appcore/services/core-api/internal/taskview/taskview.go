//go:build ignore

package taskview

// A reintroduced parallel workflow-status registry — exactly the fold that was
// deleted. Should be read from projectterms.WorkflowPhase instead.
var statusOrder = []string{"uploaded", "priced", "annotating"} // want: workflowstatus-sole-source-no-parallel-registry

func firstState() string {
	return statusOrder[0]
}
