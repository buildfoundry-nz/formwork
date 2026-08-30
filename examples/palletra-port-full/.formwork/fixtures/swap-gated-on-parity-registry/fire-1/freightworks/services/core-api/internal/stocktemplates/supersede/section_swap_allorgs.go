//go:build ignore

package supersede

import (
	"context"
	"fmt"

	"github.com/palletra/freightworks/services/core-api/internal/parityregistry"
)

// SwapSectionAllOrgs supersedes the named section for every org under
// db.AsSuperuser. The native spec is resolved BEFORE the parity guard runs, so an
// un-parity-validated section is already resolved by the time the refusal fires
// — a gate that runs after the swap resolves is not a gate (#6783).
func SwapSectionAllOrgs(ctx context.Context, reg *parityregistry.Registry, jobType, segmentTitle string) error {
	spec := builtinSectionSpec(jobType, segmentTitle) // want: swap-gated-on-parity-registry
	if !reg.Has(segmentTitle) {
		return fmt.Errorf("section %q absent from parity registry", segmentTitle)
	}
	return applySpec(ctx, spec)
}
