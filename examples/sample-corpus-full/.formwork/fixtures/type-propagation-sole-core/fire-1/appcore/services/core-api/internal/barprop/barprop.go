//go:build ignore

package barprop

import "context"

// Propagate re-implements the validation + audit envelope inline instead of
// delegating to the shared typepropagate core — the SV-4 drifted-clone shape.
func Propagate(ctx context.Context) error {
	reason := "beam_kind_propagation" // want: type-propagation-sole-core
	if err := validateEmbedded(ctx); err != nil {
		return err
	}
	return markInlineEnvelope(ctx, reason)
}
