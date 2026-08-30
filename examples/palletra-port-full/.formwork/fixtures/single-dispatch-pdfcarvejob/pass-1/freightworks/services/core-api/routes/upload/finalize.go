//go:build ignore

package upload

func finalize(ctx context.Context, pid string) {
	if err := parseruns.RoutePdfSplit(ctx, pid); err != nil {
		return
	}
}
