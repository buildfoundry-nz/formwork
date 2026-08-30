//go:build ignore

package handler

import "net/http"

func Orders(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		panic("missing id") // want: no-panic-in-request-path
	}
	w.WriteHeader(http.StatusOK)
}
