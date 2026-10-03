// Package queue owns the Redis connection and the asynq enqueue helper
// every producer uses (the API enqueues; the Task-6 worker consumes).
package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/exodia/go-saas/internal/shared/config"
)

// Redis connects to cfg.RedisURL. The same client backs asynq (Task 8
// shares this helper).
func Redis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("queue: redis url: %w", err)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("queue: redis ping: %w", err)
	}
	return rdb, nil
}

// RedisOpt adapts a go-redis client to asynq's connection option —
// including TLS and ACL username (rediss:// managed Redis like Upstash).
func RedisOpt(rdb *redis.Client) asynq.RedisClientOpt {
	o := rdb.Options()
	return asynq.RedisClientOpt{
		Addr:      o.Addr,
		Username:  o.Username,
		Password:  o.Password,
		DB:        o.DB,
		TLSConfig: o.TLSConfig,
	}
}

// NewClient builds an asynq client on the given Redis connection.
func NewClient(rdb *redis.Client) *asynq.Client {
	return asynq.NewClient(RedisOpt(rdb))
}

// Enqueue marshals payload to JSON and enqueues it as taskType. When ctx
// carries a valid otel span context, a W3C `traceparent` key is injected
// into the payload so the worker can continue the trace.
func Enqueue[T any](ctx context.Context, client *asynq.Client, taskType string, payload T, opts ...asynq.Option) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue: marshal %s: %w", taskType, err)
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("queue: remarshal %s: %w", taskType, err)
		}
		m["traceparent"] = fmt.Sprintf("00-%s-%s-%s", sc.TraceID(), sc.SpanID(), sc.TraceFlags())
		raw, err = json.Marshal(m)
		if err != nil {
			return fmt.Errorf("queue: remarshal %s: %w", taskType, err)
		}
	}
	if _, err := client.EnqueueContext(ctx, asynq.NewTask(taskType, raw), opts...); err != nil {
		return fmt.Errorf("queue: enqueue %s: %w", taskType, err)
	}
	return nil
}
