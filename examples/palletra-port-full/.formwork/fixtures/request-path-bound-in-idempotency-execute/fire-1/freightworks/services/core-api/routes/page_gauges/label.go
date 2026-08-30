//go:build ignore

package page_gauges

import (
	"context"
	"net/http"

	"github.com/palletra/freightworks/services/core-api/internal/idempotency"
)

// handle fingerprints only the body — the Params literal binds no resolved
// RequestPath, so one origin_request_id reused across two metric URLs collapses.
func handle(ctx context.Context, r *http.Request, tx idempotency.Tx) error {
	return idempotency.Execute(ctx, tx, idempotency.Params{ // want: request-path-bound-in-idempotency-execute
		OriginRequestID: r.Header.Get("Idempotency-Key"),
		Route:           "PATCH /api/page-metrics/:metricId/label",
	}, mutate)
}
