//go:build ignore

package supersede

import (
	"context"
	"fmt"

	"github.com/palletra/freightworks/services/core-api/internal/parityregistry"
)

// SwapSectionAllOrgs refuses a section absent from the parity registry
// BEFORE it resolves the native spec, so an un-parity-validated section never
// reaches the swap (#6783).
func SwapSectionAllOrgs(ctx context.Context, reg *parityregistry.Registry, jobType, segmentTitle string) error {
	if !reg.Has(segmentTitle) {
		return fmt.Errorf("section %q absent from parity registry", segmentTitle)
	}
	spec := builtinSectionSpec(jobType, segmentTitle)
	return applySpec(ctx, spec)
}
