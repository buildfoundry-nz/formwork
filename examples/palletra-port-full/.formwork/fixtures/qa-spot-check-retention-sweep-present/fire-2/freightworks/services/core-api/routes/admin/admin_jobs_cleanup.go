//go:build ignore

package admin

// The retention-sweep handler was removed; only an unrelated job remains.
func (h *AdminTasksHandler) SomeOtherJob(w http.ResponseWriter, r *http.Request) {}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func (h *AdminTasksHandler) PurgeConfirmedQASpotChecks(w http.ResponseWriter, r *http.Request) {
