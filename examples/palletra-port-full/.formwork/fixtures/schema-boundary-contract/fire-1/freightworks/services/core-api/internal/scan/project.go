//go:build ignore

package scan

import "palletra.example/freightworks/internal/dto"

// Reverted to a hand-coded DTO: the canonical generated proto import is gone.
func Project(row Row) dto.Page {
	return dto.Page{ID: row.ID}
}
