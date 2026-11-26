//go:build ignore

package db

// The definition site is EXCLUDED from the adoption check: defining the seam
// here must not by itself satisfy the "has a production call site" requirement.
func AsTenantOrg(ctx context.Context, fn func() error) error {
	return fn()
}
