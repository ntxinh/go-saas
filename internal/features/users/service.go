// Package users owns the users table: lazy sync on first authenticated
// request and PII-decrypting profile reads.
package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ntxinh/go-saas/internal/shared/errs"
	"github.com/ntxinh/go-saas/internal/shared/security"
)

// Profile is a decrypted users row. PII fields are "" when NULL in the db
// or when no cipher is configured.
type Profile struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Phone       string
}

// Service syncs and reads users.
type Service struct {
	repo   *Repo
	cipher *security.Cipher
}

// NewService builds the users service. cipher may be nil (PII_KEY unset) —
// PII fields then decrypt to "".
func NewService(repo *Repo, cipher *security.Cipher) *Service {
	return &Service{repo: repo, cipher: cipher}
}

// Sync upserts the user on every authenticated request (last_seen updates).
func (s *Service) Sync(ctx context.Context, id uuid.UUID, email string) error {
	if err := s.repo.Sync(ctx, id, email); err != nil {
		return fmt.Errorf("users: sync: %w", err)
	}
	return nil
}

// Profile loads and decrypts a user.
func (s *Service) Profile(ctx context.Context, id uuid.UUID) (Profile, error) {
	row, err := s.repo.Get(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, fmt.Errorf("users: %w", errs.ErrNotFound)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("users: get: %w", err)
	}
	p := Profile{ID: id, Email: row.Email}
	if p.DisplayName, err = s.cipher.MayDecrypt(row.DisplayName); err != nil {
		return Profile{}, err
	}
	if p.Phone, err = s.cipher.MayDecrypt(row.Phone); err != nil {
		return Profile{}, err
	}
	return p, nil
}
