package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	otelnoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/exodia/go-saas/internal/shared/queue"
	"github.com/exodia/go-saas/internal/testutil"
)

func inspector(rdbAddr string) *asynq.Inspector {
	return asynq.NewInspector(asynq.RedisClientOpt{Addr: rdbAddr})
}

func pending(t *testing.T, addr string) []*asynq.TaskInfo {
	t.Helper()
	insp := inspector(addr)
	defer func() { _ = insp.Close() }()
	tasks, err := insp.ListPendingTasks("default")
	require.NoError(t, err)
	return tasks
}

func TestEnqueueMarshalsPayload(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	client := queue.NewClient(rdb)
	defer func() { _ = client.Close() }()

	err := queue.Enqueue(context.Background(), client, queue.TaskEmailInvite, struct {
		Name string `json:"name"`
	}{Name: "x"})
	require.NoError(t, err)

	tasks := pending(t, rdb.Options().Addr)
	require.Len(t, tasks, 1)
	assert.Equal(t, queue.TaskEmailInvite, tasks[0].Type)
	assert.JSONEq(t, `{"name":"x"}`, string(tasks[0].Payload))
}

func TestEnqueueInjectsTraceparent(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	client := queue.NewClient(rdb)
	defer func() { _ = client.Close() }()

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	require.NoError(t, queue.Enqueue(ctx, client, "x:y", struct {
		A string `json:"a"`
	}{A: "b"}))

	tasks := pending(t, rdb.Options().Addr)
	require.Len(t, tasks, 1)
	var m map[string]any
	require.NoError(t, json.Unmarshal(tasks[0].Payload, &m))
	assert.Equal(t, "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01", m["traceparent"])
}

func TestWorkerTracingExtractsTraceparent(t *testing.T) {
	// Real SDK provider so spans record and expose their context.
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		// Restore noop so other tests never inherit this SDK instance.
		otel.SetTracerProvider(otelnoop.NewTracerProvider())
	})
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		SpanID:     trace.SpanID{8, 8, 8, 8, 8, 8, 8, 8},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	payload := `{"name":"x","traceparent":"00-` + parent.TraceID().String() + `-` + parent.SpanID().String() + `-01"}`
	task := asynq.NewTask(queue.TaskEmailInvite, []byte(payload))

	var sawValid bool
	h := queue.Tracing()(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		sawValid = trace.SpanContextFromContext(ctx).IsValid()
		return nil
	}))
	require.NoError(t, h.ProcessTask(context.Background(), task))

	assert.True(t, sawValid, "handler ctx must carry the worker span")
	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, parent.TraceID(), ended[0].SpanContext().TraceID())
	assert.True(t, ended[0].Parent().IsRemote(), "extracted parent must be remote")
}

func TestWorkerTracingRecordsError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(otelnoop.NewTracerProvider())
	})
	otel.SetTracerProvider(tp)

	fail := errors.New("smtp refused")
	h := queue.Tracing()(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		return fail
	}))
	task := asynq.NewTask(queue.TaskEmailInvite, []byte(`{"name":"x"}`))

	err := h.ProcessTask(context.Background(), task)
	require.ErrorIs(t, err, fail)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, codes.Error, ended[0].Status().Code)
	assert.Equal(t, fail.Error(), ended[0].Status().Description)
	require.Len(t, ended[0].Events(), 1, "RecordError must emit an exception event")
}

func TestProviderRestoredAfterTracingTests(t *testing.T) {
	// If a sibling test leaked its SDK provider, this span would record.
	_, span := otel.Tracer("t").Start(context.Background(), "s")
	defer span.End()
	assert.False(t, span.IsRecording(), "global provider must be noop")
}

func TestRedisOptPropagatesTLSAndUsername(t *testing.T) {
	opt, err := redis.ParseURL("rediss://user:secret@upstash.example:6379/2")
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	defer func() { _ = rdb.Close() }()

	ro := queue.RedisOpt(rdb)
	assert.Equal(t, "user", ro.Username)
	assert.Equal(t, "upstash.example:6379", ro.Addr)
	assert.Equal(t, 2, ro.DB)
	require.NotNil(t, ro.TLSConfig, "rediss:// must carry TLS into asynq")
}
