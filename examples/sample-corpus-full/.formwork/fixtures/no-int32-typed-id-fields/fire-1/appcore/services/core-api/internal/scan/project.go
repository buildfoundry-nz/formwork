//go:build ignore

package scan

// JobRow mirrors a sample.projects row.
type JobRow struct {
	ProjectID int32 // want: no-int32-typed-id-fields
	Name      string
}
