package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/queue"
)

// WireWorker builds the asynq server + mux: config → pool → redis →
// mail.Sender → handlers. Same flat style as Wire; a worker without a
// mailer is useless so it fails fast.
func WireWorker(ctx context.Context, cfg *config.Config, _ *slog.Logger) (*asynq.Server, *asynq.ServeMux, error) {
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	rdb, err := queue.Redis(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	mailer := mailerFor(cfg)
	if mailer == nil {
		return nil, nil, errors.New("app: no mail sender (set RESEND_API_KEY, or SMTP_ADDR in dev)")
	}

	orgSvc := orgs.NewService(pool, nil, nil) // worker never publishes; no casbin writes
	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TaskEmailInvite, orgs.HandleEmailInvite(mailer, cfg.AppURL))
	mux.HandleFunc(queue.TaskInviteExpirySweep, orgs.HandleInviteExpirySweep(orgSvc))

	srv := asynq.NewServer(queue.RedisOpt(rdb), asynq.Config{Queues: map[string]int{"default": 1}})
	return srv, mux, nil
}
