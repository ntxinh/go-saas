package errs_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMapsSentinels(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{fmt.Errorf("get org: %w", errs.ErrNotFound), 404},
		{errs.ErrConflict, 409},
		{errs.ErrUnauthorized, 401},
		{errs.ErrForbidden, 403},
		{errors.New("boom"), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		errs.Write(rec, c.err)
		assert.Equal(t, c.status, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
	}
}

func TestValidationDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	errs.Write(rec, errs.Validation(map[string]string{"name": "required"}))
	assert.Equal(t, 422, rec.Code)
	var p errs.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "name", p.InvalidParams[0].Field)
}
