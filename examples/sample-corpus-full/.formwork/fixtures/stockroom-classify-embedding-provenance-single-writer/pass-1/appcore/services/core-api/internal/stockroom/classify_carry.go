//go:build ignore

package stockroom

import "context"

// commitRollover parameterises the provenance column instead of forking the
// literal; the /run auto-apply owns source='embedding'.
func commitRollover(ctx context.Context, tx Tx, id int64, source string) error {
	_, err := tx.Exec(ctx, "UPDATE priced_lines SET source = $2 WHERE id = $1", id, source)
	return err
}
