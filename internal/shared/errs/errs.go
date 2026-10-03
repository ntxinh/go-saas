// Package errs maps domain errors to RFC 9457 problem+json responses.
package errs

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
)

// Sentinel errors returned by services and mapped to HTTP statuses by Write.
var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrValidation      = errors.New("validation failed")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrTooManyRequests = errors.New("rate limit exceeded")
)

// Problem is an RFC 9457 problem details document.
type Problem struct {
	Type          string  `json:"type"`
	Title         string  `json:"title"`
	Status        int     `json:"status"`
	Detail        string  `json:"detail"`
	InvalidParams []Param `json:"invalid_params,omitempty"`
}

// Param describes one invalid request field.
type Param struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type validationErr struct{ fields map[string]string }

func (e *validationErr) Error() string { return ErrValidation.Error() }
func (e *validationErr) Unwrap() error { return ErrValidation }

// Validation returns an error that Write renders as a 422 with per-field reasons.
func Validation(fields map[string]string) error {
	return &validationErr{fields: fields}
}

// Write renders err as an RFC 9457 problem+json response.
func Write(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	p := Problem{Type: "about:blank", Status: status, Detail: "internal"}

	var ve *validationErr
	switch {
	case errors.As(err, &ve):
		status = http.StatusUnprocessableEntity
		params := make([]Param, 0, len(ve.fields))
		for f, r := range ve.fields {
			params = append(params, Param{Field: f, Reason: r})
		}
		sort.Slice(params, func(i, j int) bool { return params[i].Field < params[j].Field })
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error(), InvalidParams: params}
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	case errors.Is(err, ErrConflict):
		status = http.StatusConflict
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	case errors.Is(err, ErrValidation):
		status = http.StatusUnprocessableEntity
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	case errors.Is(err, ErrTooManyRequests):
		status = http.StatusTooManyRequests
		p = Problem{Type: "about:blank", Status: status, Detail: err.Error()}
	}
	p.Title = http.StatusText(status)

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
