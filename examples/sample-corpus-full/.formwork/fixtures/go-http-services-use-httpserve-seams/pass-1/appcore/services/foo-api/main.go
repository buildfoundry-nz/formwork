//go:build ignore

package main

import (
	"net/http"

	"github.com/sample/appcore/internal/httpserve"
	"github.com/sample/appcore/internal/liveprobe"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", liveprobe.Probe(nil))
	srv := httpserve.NewServer(":8080", mux)
	httpserve.ListenAndServe(srv)
}
