//go:build ignore

package detectiongates

import "github.com/palletra/freightworks/services/core-api/internal/pagekinds"

func tierBearing(spec pagekinds.Spec) bool {
	return spec.TierBearing
}
