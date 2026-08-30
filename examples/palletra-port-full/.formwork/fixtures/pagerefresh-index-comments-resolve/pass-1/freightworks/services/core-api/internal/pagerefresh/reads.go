//go:build ignore

package pagerefresh

// fetchAnnotations issues the project-wide annotation read inside the write tx.
// The planner seeks idx_notes_org_type here — the org-leading shape that leads
// under the RLS org_id filter and is live in the committed snapshot.
func fetchAnnotations() string {
	return `SELECT id FROM palletra.annotations WHERE org_id = $1 AND project_id = $2`
}
