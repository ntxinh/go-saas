package middleware

import (
	"errors"
	"net/http"

	"github.com/exodia/go-saas/internal/shared/errs"
)

// Recover catches handler panics, logs them on the request logger and
// answers 500 problem+json (chi's Recoverer only writes a bare 500).
// Mount right after RequestID.
func Recover() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					Logger(r.Context()).Error("panic recovered", "panic", p)
					errs.Write(w, errors.New("internal"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
