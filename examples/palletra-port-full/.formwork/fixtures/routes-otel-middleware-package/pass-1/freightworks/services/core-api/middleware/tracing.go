//go:build ignore

package middleware

import "go.opentelemetry.io/otel"

var _ = otel.Tracer

func Tracing(name string) {}
