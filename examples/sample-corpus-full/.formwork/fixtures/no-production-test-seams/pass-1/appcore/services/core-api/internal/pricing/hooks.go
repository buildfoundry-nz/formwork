//go:build ignore
//go:build !production

package pricing

import "context"

// rateHook is the dev-only test seam, gated behind //go:build !production. The
// production build compiles the //go:build production noop sibling instead, so
// no seam ships in the release binary.
var rateHook func(ctx context.Context, id string) error

func commitPrice(ctx context.Context, id string) error {
	if rateHook != nil {
		return rateHook(ctx, id)
	}
	return nil
}
