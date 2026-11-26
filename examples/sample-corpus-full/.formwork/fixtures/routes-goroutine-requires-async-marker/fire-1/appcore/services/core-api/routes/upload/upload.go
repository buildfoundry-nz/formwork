//go:build ignore

package upload

import "context"

// HandleUpload spawns a bare, unmarked goroutine to do durable post-response
// work — the latent #4234 no-delivery-guarantee shape this gate rejects.
func HandleUpload(ctx context.Context) error {
	go dispatchInboundEmail(ctx) // want: routes-goroutine-requires-async-marker
	return nil
}
