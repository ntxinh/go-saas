package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"

	"github.com/exodia/go-saas/internal/shared/errs"
)

// RateLimit denies requests over two GCRA windows — PerSecond(10) burst and
// PerMinute(120) sustained — keyed rl:u:{userID} when a user is in ctx
// (mounted after Authn) else rl:ip:{remoteIP}. Denials answer 429
// problem+json with Retry-After.
//
// ponytail: two sequential AllowN round trips per request; pipeline if
// latency matters.
func RateLimit(limiter *redis_rate.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "rl:ip:" + remoteIP(r)
			if uid, ok := UserID(r.Context()); ok {
				key = "rl:u:" + uid.String()
			}
			retry, err := denied(r, limiter, key)
			if err != nil {
				Logger(r.Context()).Error("rate limit check failed", "err", err)
				errs.Write(w, err)
				return
			}
			if retry > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
				errs.Write(w, errs.ErrTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// denied returns the longest Retry-After when either window denies, else 0.
func denied(r *http.Request, l *redis_rate.Limiter, key string) (time.Duration, error) {
	for i, limit := range []redis_rate.Limit{redis_rate.PerMinute(120), redis_rate.PerSecond(10)} {
		k := key
		if i == 1 {
			k += ":b" // separate counter so the burst check can't consume the minute budget
		}
		res, err := l.AllowN(r.Context(), k, limit, 1)
		if err != nil {
			return 0, err
		}
		if res.Allowed == 0 {
			return res.RetryAfter, nil
		}
	}
	return 0, nil
}

func remoteIP(r *http.Request) string {
	// ponytail: XFF is client-spoofable unless a trusted LB overwrites it
	// — fine for v1; verify at the LB when one fronts the API.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(ip)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
