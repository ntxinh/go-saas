package orgs

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/server"
)

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		errs.Write(w, errs.Validation(map[string]string{"body": "invalid JSON"}))
		return false
	}
	return true
}

func orgID(r *http.Request) uuid.UUID {
	id, _ := middleware.TenantID(r.Context())
	return id
}

func (f *Feature) create(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserID(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	org, err := f.svc.Create(r.Context(), userID, req.Name)
	if err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusCreated, org)
}

func (f *Feature) list(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserID(r.Context())
	orgs, err := f.svc.OrgsOf(r.Context(), userID)
	if err != nil {
		errs.Write(w, err)
		return
	}
	if orgs == nil {
		orgs = []OrgRef{}
	}
	server.WriteJSON(w, http.StatusOK, orgs)
}

func (f *Feature) get(w http.ResponseWriter, r *http.Request) {
	org, err := f.svc.Get(r.Context(), orgID(r))
	if err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusOK, org)
}

func (f *Feature) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	org, err := f.svc.Update(r.Context(), orgID(r), req.Name)
	if err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusOK, org)
}

func (f *Feature) members(w http.ResponseWriter, r *http.Request) {
	ms, err := f.svc.Members(r.Context(), orgID(r))
	if err != nil {
		errs.Write(w, err)
		return
	}
	if ms == nil {
		ms = []Member{}
	}
	server.WriteJSON(w, http.StatusOK, ms)
}

func (f *Feature) addMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID uuid.UUID `json:"user_id"`
		Role   string    `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := f.svc.AddMember(r.Context(), orgID(r), req.UserID, req.Role); err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusCreated, Member{UserID: req.UserID, Role: req.Role})
}

func (f *Feature) removeMember(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		errs.Write(w, errs.ErrNotFound)
		return
	}
	if err := f.svc.RemoveMember(r.Context(), orgID(r), userID); err != nil {
		errs.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *Feature) changeRole(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		errs.Write(w, errs.ErrNotFound)
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := f.svc.ChangeRole(r.Context(), orgID(r), userID, req.Role); err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusOK, Member{UserID: userID, Role: req.Role})
}

func (f *Feature) invite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerRole, _ := middleware.Role(ctx)
	inviterEmail, _ := middleware.Email(ctx)
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	inv, err := f.svc.Invite(ctx, orgID(r), inviterEmail, callerRole, req.Email, req.Role)
	if err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusCreated, inv)
}

func (f *Feature) acceptInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := middleware.UserID(ctx)
	email, _ := middleware.Email(ctx)
	if err := f.svc.Accept(ctx, chi.URLParam(r, "token"), userID, email); err != nil {
		errs.Write(w, err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (f *Feature) revokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID, err := uuid.Parse(chi.URLParam(r, "inviteID"))
	if err != nil {
		errs.Write(w, errs.ErrNotFound)
		return
	}
	if err := f.svc.RevokeInvite(r.Context(), orgID(r), inviteID); err != nil {
		errs.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
