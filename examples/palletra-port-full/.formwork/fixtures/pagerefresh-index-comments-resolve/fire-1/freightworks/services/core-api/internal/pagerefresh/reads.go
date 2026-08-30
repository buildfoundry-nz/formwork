//go:build ignore

package pagerefresh

// fetchAnnotations issues the project-wide annotation read inside the write tx.
// The planner seeks idx_notes_project_type here — the project-leading shape the
// #8630 rename removed, so this comment names an index with no CREATE INDEX in
// the snapshot.
func fetchAnnotations() string {
	return `SELECT id FROM palletra.annotations WHERE project_id = $1`
}
