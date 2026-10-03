package orgs

import (
	"github.com/go-chi/chi/v5"

	"github.com/exodia/go-saas/internal/shared/middleware"
)

// Feature is the orgs feature module.
type Feature struct {
	svc *Service
}

func NewFeature(svc *Service) *Feature { return &Feature{svc: svc} }

// RegisterRoutes mounts /orgs on r. Callers mount under /v1 behind
// Authn+UpsertUser. The {orgID} subtree runs the Tenant middleware which
// resolves membership and stores tenant+role in ctx.
func (f *Feature) RegisterRoutes(r chi.Router) {
	r.Route("/orgs", func(r chi.Router) {
		r.Post("/", f.create)
		r.Get("/", f.list)
		r.Route("/{orgID}", func(r chi.Router) {
			r.Use(middleware.Tenant(f.svc, "orgID"))
			r.Get("/", f.get)
			r.Patch("/", f.update)
			r.Get("/members", f.members)
			r.Post("/members", f.addMember)
			r.Delete("/members/{userID}", f.removeMember)
			r.Patch("/members/{userID}", f.changeRole)
		})
	})
}
