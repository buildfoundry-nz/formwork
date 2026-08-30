//go:build ignore

package barprop

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/typepropagate"
)

// Propagate is a thin wrapper: it supplies its literal AuditNote + a Stamp
// closure and delegates the shared envelope to the typepropagate core.
func Propagate(ctx context.Context) error {
	return typepropagate.Propagate(ctx, typepropagate.Opts{
		AuditNote: "beam_kind_propagation",
		Stamp:     markBar,
	})
}
