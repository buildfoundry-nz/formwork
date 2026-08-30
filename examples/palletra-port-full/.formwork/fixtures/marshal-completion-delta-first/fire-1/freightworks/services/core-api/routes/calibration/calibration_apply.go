//go:build ignore

package calibration

import "net/http"

// The delta is assigned BEFORE the marshal, so the cached body carries the
// stale rail projection — a replay would rewind the rail permanently.
func apply(w http.ResponseWriter, r *http.Request, cached bool, affected, delta any) {
	resp := &AdjustResponse{ImpactedPage: affected}
	resp.CompletionDiff = delta // want: marshal-completion-delta-first
	body, _ := shared.EncodeOpts.Marshal(resp)
	_ = body
	shared.WriteLiveOrCachedResponse(w, r, cached, resp)
}
