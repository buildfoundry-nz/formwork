//go:build ignore

package admin

// The retention-sweep handler was removed; only an unrelated job remains.
func (h *AdminTasksHandler) SomeOtherJob(w http.ResponseWriter, r *http.Request) {}
