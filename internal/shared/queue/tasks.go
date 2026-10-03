package queue

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Task type identifiers shared by producers (API) and consumers (the
// Task-6 worker).
const (
	TaskEmailInvite       = "email:invite"
	TaskInviteExpirySweep = "org:invite-expiry"
)

// Tracing is asynq middleware: it extracts the `traceparent` Enqueue
// injected into the payload and wraps the handler's ctx in a consumer
// span, so worker work continues the API's trace.
func Tracing() asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			var p struct {
				Traceparent string `json:"traceparent"`
			}
			if err := json.Unmarshal(t.Payload(), &p); err == nil && p.Traceparent != "" {
				ctx = otel.GetTextMapPropagator().Extract(ctx,
					propagation.MapCarrier{"traceparent": p.Traceparent})
			}
			ctx, span := otel.Tracer("go-saas/worker").Start(ctx, t.Type(),
				trace.WithSpanKind(trace.SpanKindConsumer))
			defer span.End()
			return next.ProcessTask(ctx, t)
		})
	}
}
