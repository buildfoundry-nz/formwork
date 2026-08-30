//go:build ignore

package scan

// JobRow mirrors a palletra.projects row.
type JobRow struct {
	ProjectID int32 // want: no-int32-typed-id-fields
	Name      string
}
