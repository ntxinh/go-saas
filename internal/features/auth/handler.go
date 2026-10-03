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
	// Orgs stays [] until the orgs feature fills it (Task 5); never null.
	Orgs []any `json:"orgs"`
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
	server.WriteJSON(w, http.StatusOK, meResponse{
		ID:    id.String(),
		Email: email,
		Orgs:  []any{},
	})
}
