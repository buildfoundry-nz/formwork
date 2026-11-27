//go:build ignore

package stockroom

func OpticalExtract(ctx context.Context, rows []Row) (*ParsedSheet, error) {
	table := assembleDetectionTable(rows)
	// Always return the assembled table so merged ParseNotices reach the caller.
	return table, nil
}
