//go:build ignore

package skuextract

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/gemini"
)

// generate reaches the model through the shared gemini.Client, whose methods
// route through the capped builders — no genai config is hand-rolled here, so
// the MaxOutputTokens ceiling is inherited by construction.
func generate(ctx context.Context, client *gemini.Client, prompt string) (string, error) {
	return client.Extract(ctx, prompt)
}
