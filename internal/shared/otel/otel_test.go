package otel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/ntxinh/go-saas/internal/shared/config"
	"github.com/ntxinh/go-saas/internal/shared/otel"
)

func testSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
}

func TestSetupNoEndpointInstallsNoop(t *testing.T) {
	shutdown, err := otel.Setup(context.Background(), &config.Config{Env: "test"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// The noop provider produces non-recording spans; shutdown is a no-op.
	_, span := trace.SpanFromContext(context.Background()).TracerProvider().
		Tracer("t").Start(context.Background(), "s")
	assert.False(t, span.IsRecording())
	assert.NoError(t, shutdown(context.Background()))
}

func TestLogHandlerInjectsTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(otel.LogHandler(slog.NewJSONHandler(&buf, nil)))

	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext())
	log.With("request_id", "r1").InfoContext(ctx, "hi")

	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", m["trace_id"])
	assert.Equal(t, "0102030405060708", m["span_id"])
	assert.Equal(t, "r1", m["request_id"], "With attrs must pass through the wrapper")
}

func TestLogHandlerWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(otel.LogHandler(slog.NewJSONHandler(&buf, nil)))

	log.InfoContext(context.Background(), "hi")

	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	_, hasTrace := m["trace_id"]
	assert.False(t, hasTrace, "no span in ctx → no trace_id key")
}
