//go:build ignore

package scalewire

import "context"

// stage downloads only the display PNG; the source PDF comes from pageFeed.
func stage(ctx context.Context, t task) ([]byte, error) {
	return download(ctx, t.PagePaintKey)
}
