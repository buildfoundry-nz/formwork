//go:build ignore

package scan

// JobRow mirrors a sample.projects row. IDs are UUIDs — strings end-to-end.
type JobRow struct {
	ProjectID string
	Name      string
}
