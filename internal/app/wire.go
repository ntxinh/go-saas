// Package app is the single composition root: config → pool → keyfunc →
// cipher → services → features → router. Flat function calls, no DI
// framework.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/exodia/go-saas/internal/features/auth"
	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/features/users"
	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/events"
	"github.com/exodia/go-saas/internal/shared/mail"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/queue"
	"github.com/exodia/go-saas/internal/shared/security"
	"github.com/exodia/go-saas/internal/shared/server"
)

// Wire builds the application router. ctx bounds the JWKS refresh
// goroutine's lifetime.
func Wire(ctx context.Context, cfg *config.Config, log *slog.Logger) (*chi.Mux, error) {
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	keys, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKsURL()})
	if err != nil {
		return nil, fmt.Errorf("app: jwks %s: %w", cfg.JWKsURL(), err)
	}

	// PII_KEY is optional in dev: without it, PII fields decrypt to "".
	var cipher *security.Cipher
	if cfg.PIIKey != "" {
		cipher, err = security.New(cfg.PIIKey)
		if err != nil {
			return nil, fmt.Errorf("app: %w", err)
		}
	}

	// Redis + asynq: the API enqueues; the Task-6 worker consumes.
	rdb, err := queue.Redis(ctx, cfg)
	if err != nil {
		return nil, err
	}
	asynqClient := queue.NewClient(rdb)

	// Events bus: MemberInvited → durable email task (the only wired
	// subscriber; other events are publish-side contracts for now).
	router, err := events.NewRouter(log)
	if err != nil {
		return nil, fmt.Errorf("app: events: %w", err)
	}
	events.Subscribe(router, events.TopicMemberInvited, func(ctx context.Context, payload []byte) error {
		var p events.MemberInvited
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return queue.Enqueue(ctx, asynqClient, queue.TaskEmailInvite, p)
	})

	// Scheduled maintenance: hourly invite-expiry sweep. The handler
	// (worker-side, orgs.Service.ExpireStale) lands with Task 6.
	sched := asynq.NewScheduler(queue.RedisOpt(rdb), nil)
	if _, err := sched.Register("@hourly", asynq.NewTask(queue.TaskInviteExpirySweep, nil)); err != nil {
		return nil, fmt.Errorf("app: scheduler: %w", err)
	}

	userSvc := users.NewService(users.NewRepo(pool), cipher)
	orgSvc := orgs.NewService(pool, router.Publisher())
	authFeat := auth.New(
		func(ctx context.Context, id uuid.UUID) (string, error) {
			p, err := userSvc.Profile(ctx, id)
			return p.Email, err
		},
		func(ctx context.Context, id uuid.UUID) ([]auth.Org, error) {
			refs, err := orgSvc.OrgsOf(ctx, id)
			if err != nil {
				return nil, err
			}
			out := make([]auth.Org, len(refs))
			for i, o := range refs {
				out[i] = auth.Org{TenantID: o.TenantID, Name: o.Name, Role: o.Role}
			}
			return out, nil
		})
	orgsFeat := orgs.NewFeature(orgSvc)
	r := server.New(cfg, log)

	// The router and scheduler run until ctx is done; Close runs their
	// shutdown when the process is exiting anyway.
	go func() {
		<-ctx.Done()
		sched.Shutdown()
		_ = router.Close()
	}()
	go func() {
		if err := router.Run(ctx); err != nil {
			log.Error("events router stopped", "err", err)
		}
	}()
	go func() {
		if err := sched.Run(); err != nil {
			log.Error("scheduler stopped", "err", err)
		}
	}()

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.Authn(keys, cfg.Issuer(), "authenticated"))
		r.Use(middleware.UpsertUser(userSvc.Sync))
		authFeat.RegisterRoutes(r)
		orgsFeat.RegisterRoutes(r)
	})
	return r, nil
}

func mailerFor(cfg *config.Config) mail.Sender {
	if cfg.Env == "dev" && cfg.SMTPAddr != "" {
		return mail.NewSMTP(cfg.SMTPAddr, cfg.MailFrom)
	}
	if cfg.ResendAPIKey != "" {
		return mail.NewResend(cfg.ResendAPIKey, cfg.MailFrom)
	}
	return nil
}
