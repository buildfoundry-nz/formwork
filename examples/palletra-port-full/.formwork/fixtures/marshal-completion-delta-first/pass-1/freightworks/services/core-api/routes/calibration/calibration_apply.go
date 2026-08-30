//go:build ignore

package calibration

import "net/http"

// Marshal the delta-free response first (that body is what gets cached), then
// assign the projection onto the fresh response only.
func apply(w http.ResponseWriter, r *http.Request, cached bool, affected, delta any) {
	resp := &AdjustResponse{ImpactedPage: affected}
	body, _ := shared.EncodeOpts.Marshal(resp)
	_ = body
	resp.CompletionDiff = delta
	shared.WriteLiveOrCachedResponse(w, r, cached, resp)
}
