//go:build ignore

package page_gauges

import (
	"context"
	"net/http"

	"github.com/palletra/freightworks/services/core-api/internal/idempotency"
)

// handle folds the resolved request path into the fingerprint, so two metric
// URLs reusing one origin_request_id produce two fingerprints (never a replay).
func handle(ctx context.Context, r *http.Request, tx idempotency.Tx) error {
	return idempotency.Execute(ctx, tx, idempotency.Params{
		OriginRequestID: r.Header.Get("Idempotency-Key"),
		Route:           "PATCH /api/page-metrics/:metricId/label",
		RequestPath:     r.URL.Path,
	}, mutate)
}
