//go:build ignore

package main

import (
	"net/http"

	"github.com/palletra/freightworks/internal/httpserve"
	"github.com/palletra/freightworks/internal/liveprobe"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", liveprobe.Probe(nil))
	srv := httpserve.NewServer(":8080", mux)
	httpserve.ListenAndServe(srv)
}
