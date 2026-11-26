//go:build ignore

package gaugebook

import "github.com/sample/appcore/services/core-api/internal/pagetabid"

func CategoryFromPageType(pt string) string {
	return string(pagetabid.For(pt))
}
