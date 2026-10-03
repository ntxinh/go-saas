package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ntxinh/go-saas/internal/shared/errs"
)

const idemTTL = 24 * time.Hour

// storedResponse is what a completed idempotent request replays.
type storedResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// Idempotency honors `Idempotency-Key` on POST/PATCH inside the authed
// group (needs UserID). Redis key `idem:{user}:{method}:{path}:{key}`,
// SET NX EX 24h marker "processing"; a second request while the first
// runs answers 409, after it completes the stored {status, body} is
// replayed with `Idempotent-Replay: true`.
//
// ponytail: SET NX then GET on conflict is two calls, not atomic; a
// SET-NX→done flip between them replays "processing" as 409 — same
// answer the client should retry anyway.
func Idempotency(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || (r.Method != http.MethodPost && r.Method != http.MethodPatch) {
				next.ServeHTTP(w, r)
				return
			}
			uid, ok := UserID(r.Context())
			if !ok {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			rkey := fmt.Sprintf("idem:%s:%s:%s:%s", uid, r.Method, r.URL.Path, key)

			claimed, err := rdb.SetNX(r.Context(), rkey, "processing", idemTTL).Result()
			if err != nil {
				Logger(r.Context()).Error("idempotency claim failed", "err", err)
				errs.Write(w, err)
				return
			}
			if !claimed {
				replay(w, r, rdb, rkey)
				return
			}
			// A handler panic must not strand the "processing" marker for
			// the full TTL: delete it and re-panic so the outer Recover
			// middleware turns it into a 500.
			defer func() {
				if p := recover(); p != nil {
					// Background ctx — r.Context() may be cancelled during unwind.
					_ = rdb.Del(context.Background(), rkey).Err()
					panic(p)
				}
			}()

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}

			raw, _ := json.Marshal(storedResponse{
				Status:      rec.status,
				ContentType: rec.Header().Get("Content-Type"),
				Body:        rec.body.String(),
			})
			if err := rdb.Set(r.Context(), rkey, raw, idemTTL).Err(); err != nil {
				Logger(r.Context()).Error("idempotency store failed", "err", err)
			}
		})
	}
}

// replay answers from the stored response, or 409 while still processing.
func replay(w http.ResponseWriter, r *http.Request, rdb *redis.Client, rkey string) {
	raw, err := rdb.Get(r.Context(), rkey).Result()
	if err != nil || raw == "processing" {
		errs.Write(w, errs.ErrConflict)
		return
	}
	var stored storedResponse
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		errs.Write(w, errs.ErrConflict)
		return
	}
	if stored.ContentType != "" {
		w.Header().Set("Content-Type", stored.ContentType)
	}
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(stored.Status)
	_, _ = w.Write([]byte(stored.Body))
}

// statusRecorder captures status + body while passing them through.
type statusRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (s *statusRecorder) WriteHeader(status int) {
	if s.status != 0 {
		return // first WriteHeader wins
	}
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.WriteHeader(http.StatusOK)
	}
	s.body.Write(b)
	return s.ResponseWriter.Write(b)
}
