//go:build ignore

package handler

import "net/http"

// Orders serves the order list.
//
// This comment is the point of the pass fixture. It says panic( — the exact
// text the rule forbids — and it must NOT fire, because decomment-go blanks
// comments before the matcher runs. Delete `preprocess: decomment-go` from the
// rule and this fixture fails, which is how you know the preprocessor is doing
// real work rather than sitting there decoratively.
func Orders(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
