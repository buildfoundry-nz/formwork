//go:build ignore

package admin

// The #5976 retention sweep: deletes terminal 'verified' spot-check reviews.
func (h *AdminTasksHandler) PurgeConfirmedQASpotChecks(w http.ResponseWriter, r *http.Request) {
	_ = "DELETE FROM palletra.qa_sample_check_reviews WHERE status = 'verified'"
}
