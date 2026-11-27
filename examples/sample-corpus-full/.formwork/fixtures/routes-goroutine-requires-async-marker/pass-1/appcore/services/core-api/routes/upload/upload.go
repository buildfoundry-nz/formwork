//go:build ignore

package upload

import "context"

// HandleUpload spawns a goroutine, but classifies its durability with an inline
// async-ok marker on the launch line — the positive-token allowlist.
func HandleUpload(ctx context.Context) error {
	go dispatchInboundEmail(ctx) // async-ok(best-effort): courtesy email, loss is a harmless no-op
	return nil
}
