//go:build ignore

package pricing

import "context"

// priceHook is an ungated module-global test seam: an integration test swaps it
// to drive a deterministic race, but with no //go:build tag it ships in the
// production binary (audit-16 #5 / audit-17 #14).
var priceHook func(ctx context.Context, id string) error // want: no-test-seams-in-production

func applyPrice(ctx context.Context, id string) error {
	if priceHook != nil {
		return priceHook(ctx, id)
	}
	return nil
}
