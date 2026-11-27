//go:build ignore

package metricmenu

import "github.com/sample/appcore/services/core-api/internal/metricspage"

func InputTypes() map[string][]string {
	return metricspage.DefaultPageTallyRules().InputTypes()
}
