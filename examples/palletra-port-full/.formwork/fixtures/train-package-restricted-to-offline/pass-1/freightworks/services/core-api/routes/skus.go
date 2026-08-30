//go:build ignore

package routes

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/parsecompose/classifier/local"
)

// The runtime only local.Load()s the trained artifact and calls Model.Classify
// — it never imports the offline trainer package.
func (h *Handler) classifyFrame(ctx context.Context, feats []float64) int {
	model := local.Load(h.weightsPath)
	return model.Classify(feats)
}
