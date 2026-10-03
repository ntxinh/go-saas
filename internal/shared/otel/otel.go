// Package otel wires OpenTelemetry tracing: OTLP export when an endpoint
// is configured, a noop provider when it isn't, plus a slog.Handler
// wrapper that stamps trace_id/span_id on every record.
package otel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/exodia/go-saas/internal/shared/config"
)

// Setup installs the W3C TraceContext propagator and a global tracer
// provider: OTLP/HTTP batch export when cfg.OTLPEndpoint is set, noop
// otherwise. The returned shutdown flushes spans; it is always non-nil.
func Setup(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	res := resource.NewSchemaless(
		attribute.String("service.name", "go-saas-"+cfg.Env),
	)
	if cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint),
		otlptracehttp.WithHeaders(parseHeaders(cfg.OTLPHeaders)),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// parseHeaders turns "k=v,k2=v2" (OTEL_EXPORTER_OTLP_HEADERS format)
// into a header map; malformed pairs are skipped.
func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// LogHandler wraps base so every record handled with a ctx carrying a
// valid span context gains trace_id/span_id attributes.
func LogHandler(base slog.Handler) slog.Handler { return ctxHandler{base} }

type ctxHandler struct{ base slog.Handler }

func (h ctxHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.base.Enabled(ctx, l)
}

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := oteltrace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.base.Handle(ctx, r)
}

func (h ctxHandler) WithAttrs(a []slog.Attr) slog.Handler { return ctxHandler{h.base.WithAttrs(a)} }
func (h ctxHandler) WithGroup(g string) slog.Handler      { return ctxHandler{h.base.WithGroup(g)} }
