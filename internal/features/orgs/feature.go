package orgs

import (
	"github.com/go-chi/chi/v5"

	"github.com/exodia/go-saas/internal/shared/middleware"
)

// Feature is the orgs feature module.
type Feature struct {
	svc *Service
	ef  middleware.Enforcer
}

// NewFeature builds the feature. ef may be nil (tests without casbin);
// then the {orgID} subtree keeps only the Tenant membership gate.
func NewFeature(svc *Service, ef middleware.Enforcer) *Feature {
	return &Feature{svc: svc, ef: ef}
}

// RegisterRoutes mounts /orgs on r. Callers mount under /v1 behind
// Authn+UpsertUser. The {orgID} subtree runs Tenant (resolves membership
// + tenant/role into ctx) then RBAC (casbin checks role perms per route).
func (f *Feature) RegisterRoutes(r chi.Router) {
	r.Route("/orgs", func(r chi.Router) {
		r.Post("/", f.create)
		r.Get("/", f.list)
		r.Route("/{orgID}", func(r chi.Router) {
			r.Use(middleware.Tenant(f.svc, "orgID"))
			if f.ef != nil {
				r.Use(middleware.RBAC(f.ef))
			}
			r.Get("/", f.get)
			r.Patch("/", f.update)
			r.Get("/members", f.members)
			r.Post("/members", f.addMember)
			r.Delete("/members/{userID}", f.removeMember)
			r.Patch("/members/{userID}", f.changeRole)
			r.Post("/invites", f.invite)
			r.Delete("/invites/{inviteID}", f.revokeInvite)
		})
	})
	// Accept carries its own capability (the token resolves the org), so
	// it mounts outside the tenant-middleware subtree.
	r.Post("/invites/{token}/accept", f.acceptInvite)
}
