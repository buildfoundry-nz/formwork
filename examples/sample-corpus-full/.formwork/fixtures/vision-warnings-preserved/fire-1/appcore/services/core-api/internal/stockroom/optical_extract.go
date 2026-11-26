//go:build ignore

package stockroom

func OpticalExtract(ctx context.Context, rows []Row) (*ParsedSheet, error) {
	table := assembleDetectionTable(rows)
	if len(table.Rows) == 0 {
		return nil, nil // want: vision-warnings-preserved
	}
	return table, nil
}
