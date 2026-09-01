//go:build ignore
//go:build !production

package pricing

import "context"

// priceHook is the dev-only test seam, gated behind //go:build !production. The
// production build compiles the //go:build production noop sibling instead, so
// no seam ships in the release binary.
var priceHook func(ctx context.Context, id string) error

func applyPrice(ctx context.Context, id string) error {
	if priceHook != nil {
		return priceHook(ctx, id)
	}
	return nil
}
