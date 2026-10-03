package queue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/exodia/go-saas/internal/shared/queue"
	"github.com/exodia/go-saas/internal/testutil"
)

func inspector(rdbAddr string) *asynq.Inspector {
	return asynq.NewInspector(asynq.RedisClientOpt{Addr: rdbAddr})
}

func pending(t *testing.T, addr string) []*asynq.TaskInfo {
	t.Helper()
	insp := inspector(addr)
	defer insp.Close()
	tasks, err := insp.ListPendingTasks("default")
	require.NoError(t, err)
	return tasks
}

func TestEnqueueMarshalsPayload(t *testing.T) {
	rdb := testutil.Redis(t)
	client := queue.NewClient(rdb)
	defer client.Close()

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
	rdb := testutil.Redis(t)
	client := queue.NewClient(rdb)
	defer client.Close()

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
