package auth

import (
	"net/http"

	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/server"
)

type meResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// Orgs is never null — empty memberships marshal as [].
	Orgs []Org `json:"orgs"`
}

func (f *Feature) me(w http.ResponseWriter, r *http.Request) {
	id, ok := middleware.UserID(r.Context())
	if !ok {
		errs.Write(w, errs.ErrUnauthorized)
		return
	}
	email, err := f.profile(r.Context(), id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	orgs, err := f.orgs(r.Context(), id)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if orgs == nil {
		orgs = []Org{}
	}
	server.WriteJSON(w, http.StatusOK, meResponse{
		ID:    id.String(),
		Email: email,
		Orgs:  orgs,
	})
}
