//go:build ignore

package pricing

import "context"

// rateHook is an ungated module-global test seam: an integration test swaps it
// to drive a deterministic race, but with no //go:build tag it ships in the
// production binary (sweep-16 #5 / sweep-17 #14).
var rateHook func(ctx context.Context, id string) error // want: no-production-test-seams

func commitPrice(ctx context.Context, id string) error {
	if rateHook != nil {
		return rateHook(ctx, id)
	}
	return nil
}
