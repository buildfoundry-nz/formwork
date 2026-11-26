//go:build ignore

package sourcerouting

import "context"

// MatchOnlySeam fetches through the hit-only cache seam (miss -> caller fallback).
func MatchOnlySeam(ctx context.Context, key string) ([]byte, error) {
	return cache.StoredBytes(ctx, key)
}
