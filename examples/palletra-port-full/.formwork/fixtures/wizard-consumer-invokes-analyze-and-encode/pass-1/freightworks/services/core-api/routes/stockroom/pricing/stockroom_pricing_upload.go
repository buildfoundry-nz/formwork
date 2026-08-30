//go:build ignore

package pricing

func handleImport(ctx context.Context, file []byte) error {
	payload, err := stockroomanalyze.ScanAndEncode(ctx, file)
	if err != nil {
		return err
	}
	return persist(ctx, payload)
}
