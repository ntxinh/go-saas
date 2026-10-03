// Package events is the in-process event bus: a watermill router over a
// GoChannel pub/sub. Publishing is at-most-once (a message is lost if the
// process dies before a handler acks); anything that must survive a
// crash is pushed to the durable asynq queue by a subscriber.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// Router is a watermill router plus the GoChannel bus it routes over.
type Router struct {
	*message.Router
	bus *gochannel.GoChannel
}

// NewRouter builds the router: Recoverer innermost (turns a handler
// panic into an error) inside Retry{MaxRetries:3}.
func NewRouter(log *slog.Logger) (*Router, error) {
	wlog := watermill.NewSlogLogger(log)
	r, err := message.NewRouter(message.RouterConfig{}, wlog)
	if err != nil {
		return nil, err
	}
	r.AddMiddleware(
		middleware.Retry{MaxRetries: 3, InitialInterval: 10 * time.Millisecond, MaxInterval: time.Second}.Middleware,
		middleware.Recoverer,
	)
	return &Router{Router: r, bus: gochannel.NewGoChannel(gochannel.Config{}, wlog)}, nil
}

// Publisher returns a JSON publisher for the router's bus; it satisfies
// features' Publisher seam (e.g. orgs.Publisher).
func (r *Router) Publisher() *Publisher { return &Publisher{pub: r.bus} }

// Close stops the router then closes the bus.
func (r *Router) Close() error {
	if err := r.Router.Close(); err != nil {
		return err
	}
	return r.bus.Close()
}

// HandlerFunc receives the raw JSON payload of one event message.
type HandlerFunc func(ctx context.Context, payload []byte) error

// Subscribe registers h for topic on this router's bus. Returning a
// non-nil error nacks the message and triggers the retry middleware.
func (r *Router) Subscribe(topic string, h HandlerFunc) {
	r.AddConsumerHandler("events."+topic, topic, r.bus,
		func(msg *message.Message) error { return h(msg.Context(), msg.Payload) })
}

// Subscribe registers h for topic on router.
func Subscribe(router *Router, topic string, h HandlerFunc) {
	router.Subscribe(topic, h)
}

// Publisher marshals payloads to JSON and publishes them on a topic.
type Publisher struct{ pub message.Publisher }

// Publish marshals payload and publishes it on topic.
func (p *Publisher) Publish(ctx context.Context, topic string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := message.NewMessage(watermill.NewUUID(), b)
	msg.SetContext(ctx)
	return p.pub.Publish(topic, msg)
}
