//go:build ignore

package gaugebook

import "github.com/palletra/freightworks/services/core-api/internal/pagetabid"

func CategoryFromPageType(pt string) string {
	return string(pagetabid.For(pt))
}
