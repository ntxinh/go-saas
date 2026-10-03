// Package middleware holds shared chi middleware.
package middleware

import (
	"context"
	"log/slog"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

// Context keys shared by all middleware in this package. Keep them in one
// block so values never collide.
const (
	loggerKey ctxKey = iota
	ctxUser
	ctxTenant
	ctxRole
)

// RequestID assigns a request id (chi) and stores a logger carrying it in ctx.
func RequestID(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return chimw.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), loggerKey,
				log.With("request_id", chimw.GetReqID(r.Context())))
			next.ServeHTTP(w, r.WithContext(ctx))
		}))
	}
}

// Logger returns the request-scoped logger, or slog.Default().
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
