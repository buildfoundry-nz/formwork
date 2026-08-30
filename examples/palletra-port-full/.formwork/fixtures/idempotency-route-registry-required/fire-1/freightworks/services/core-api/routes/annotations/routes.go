//go:build ignore

package annotations

var routeDef = idempotency.Route("POST /api/annotations") // want: idempotency-route-registry-required
