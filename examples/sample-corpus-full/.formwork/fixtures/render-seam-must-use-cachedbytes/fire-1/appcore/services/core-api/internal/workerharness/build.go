//go:build ignore

package workerharness

import "context"

func stagePrepareSource(ctx context.Context, key string) ([]byte, error) {
	raw, err := pageFeedCache.Bytes(ctx, key) // want: render-seam-must-use-cachedbytes
	return raw, err
}
