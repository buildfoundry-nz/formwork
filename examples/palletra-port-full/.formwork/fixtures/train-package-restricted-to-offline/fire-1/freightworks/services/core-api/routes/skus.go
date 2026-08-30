//go:build ignore

package routes

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/parsecompose/classifier/local/train" // want: train-package-restricted-to-offline
)

// A request-path handler dragging the offline trainer + its RNG splitters into
// the request path — forbidden.
func (h *Handler) classifyFrame(ctx context.Context, feats []float64) int {
	model := train.Fit(nil, train.Options{})
	return model.Classify(feats)
}
