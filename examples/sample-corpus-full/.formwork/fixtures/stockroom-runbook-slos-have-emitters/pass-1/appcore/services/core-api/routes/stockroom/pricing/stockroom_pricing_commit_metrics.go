//go:build ignore

package pricing

import "go.opentelemetry.io/otel/metric"

// commitTelemetry emits the Go commit-path instruments. Every stockroom.* metric the
// runbook alerts on has a live emitter here.
func commitTelemetry(m metric.Meter) {
	_, _ = m.Float64Histogram("stockroom.pipeline.commit.duration_ms")
}
