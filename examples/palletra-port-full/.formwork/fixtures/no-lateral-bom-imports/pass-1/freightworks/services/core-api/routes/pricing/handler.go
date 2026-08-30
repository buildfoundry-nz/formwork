//go:build ignore

package pricing

import (
	"github.com/palletra/freightworks/services/core-api/internal/bomdoc"
	"github.com/palletra/freightworks/services/core-api/routes/bom_templates"
)

func Handle() {
	_ = bomdoc.Nil
	_ = bom_templates.Nil
}
