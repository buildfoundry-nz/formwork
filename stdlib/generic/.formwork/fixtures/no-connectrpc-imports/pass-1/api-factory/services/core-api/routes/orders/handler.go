//go:build ignore

package orders

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/api/orders", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = protojson.Marshal(nil)
	})
	return r
}
