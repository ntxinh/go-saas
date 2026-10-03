package events_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/shared/events"
)

func TestPublishSubscribeEcho(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := events.NewRouter(log)
	require.NoError(t, err)

	got := make(chan []byte, 1)
	events.Subscribe(router, "test.echo", func(_ context.Context, payload []byte) error {
		got <- payload
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = router.Run(ctx) }()
	t.Cleanup(func() { _ = router.Close() })
	<-router.Running()

	require.NoError(t, router.Publisher().Publish(ctx, "test.echo", map[string]string{"hello": "world"}))

	select {
	case b := <-got:
		assert.JSONEq(t, `{"hello":"world"}`, string(b))
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive the published message within 2s")
	}
}
