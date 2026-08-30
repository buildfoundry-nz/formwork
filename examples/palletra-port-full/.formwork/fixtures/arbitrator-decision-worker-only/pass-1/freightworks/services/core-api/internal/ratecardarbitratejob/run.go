//go:build ignore

package ratecardarbitratejob

import "context"

// The async worker is the only place the LLM decision runs (exempt location).
func run(ctx context.Context, prompt Prompt) Verdict {
	return skuarbitrate.Decide(ctx, prompt)
}
