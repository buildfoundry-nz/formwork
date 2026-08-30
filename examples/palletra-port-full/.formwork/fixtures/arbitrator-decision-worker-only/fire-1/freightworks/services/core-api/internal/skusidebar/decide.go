//go:build ignore

package skusidebar

import "context"

// The sidebar read must NOT build a prompt or decide — this leaks Gemini onto a hot read.
func computeVerdict(ctx context.Context, prompt Prompt) Verdict {
	return skuarbitrate.Decide(ctx, prompt) // want: arbitrator-decision-worker-only
}
