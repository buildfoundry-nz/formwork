//go:build ignore

package pricing

import "go.opentelemetry.io/otel/metric"

// commitTelemetry emits the Go commit-path instruments. Only duration_ms is wired;
// stockroom.pipeline.commit.error_rate is named in the runbook but has no emitter.
func commitTelemetry(m metric.Meter) {
	_, _ = m.Float64Histogram("stockroom.pipeline.commit.duration_ms")
}
