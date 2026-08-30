//go:build ignore

package metricmenu

import "github.com/palletra/freightworks/services/core-api/internal/metricspage"

func InputTypes() map[string][]string {
	return metricspage.DefaultPageTallyRules().InputTypes()
}
