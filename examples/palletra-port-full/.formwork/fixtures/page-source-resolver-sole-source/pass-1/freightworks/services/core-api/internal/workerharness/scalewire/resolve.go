//go:build ignore

package scalewire

func resolve(ctx context.Context, doc []byte) {
	bytes, _ := pagefeedresolve.FetchSinglePageBytes(ctx, doc, pagefeedresolve.SoloPageIndex)
	_ = bytes
}
