package astroway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Sentinel kinds; test with errors.Is(err, astroway.ErrRateLimit).
var (
	ErrBadRequest          = errors.New("astroway: bad request")
	ErrAuthentication      = errors.New("astroway: authentication failed")
	ErrPermissionDenied    = errors.New("astroway: permission denied")
	ErrNotFound            = errors.New("astroway: not found")
	ErrUnprocessableEntity = errors.New("astroway: unprocessable entity")
	ErrRateLimit           = errors.New("astroway: rate limited")
	// ErrQuotaExceeded means the account is out of credits: waiting a few seconds will not help.
	ErrQuotaExceeded   = errors.New("astroway: credits exhausted")
	ErrCalculation     = errors.New("astroway: calculation failed")
	ErrInternalServer  = errors.New("astroway: server error")
	ErrUnexpectedReply = errors.New("astroway: unexpected status")
)

// APIError is a non-2xx answer. Kind is one of the sentinels above.
type APIError struct {
	StatusCode       int
	Code             string
	Message          string
	RequestID        string
	RetryAfter       *time.Duration
	CreditsRemaining *int
	Body             []byte
	Kind             error
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("astroway: %d %s", e.StatusCode, e.Message)
	if e.Code != "" {
		s += " (" + e.Code + ")"
	}
	if e.RequestID != "" {
		s += " request_id=" + e.RequestID
	}
	return s
}

func (e *APIError) Unwrap() error { return e.Kind }

// ConnectionError is a network failure after all retries.
type ConnectionError struct {
	Path string
	Err  error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("astroway: network error calling %s: %v", e.Path, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

var (
	quotaCodes       = map[string]bool{"OUT_OF_CREDITS": true, "QUOTA_EXCEEDED": true, "CREDIT_LIMIT_REACHED": true}
	calculationCodes = map[string]bool{"CALCULATION_ERROR": true, "EPHEMERIS_ERROR": true}
)

func newAPIError(resp *http.Response, raw []byte) *APIError {
	e := &APIError{
		StatusCode:       resp.StatusCode,
		Message:          http.StatusText(resp.StatusCode),
		RequestID:        resp.Header.Get("X-Request-Id"),
		RetryAfter:       retryAfter(resp.Header.Get("Retry-After")),
		CreditsRemaining: intHeader(resp.Header.Get("X-Credits-Remaining")),
		Body:             raw,
	}
	var env struct {
		Error *struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error != nil {
		if env.Error.Code != nil {
			e.Code = fmt.Sprint(env.Error.Code)
		}
		if env.Error.Message != "" {
			e.Message = env.Error.Message
		}
	}
	e.Kind = classify(e.StatusCode, e.Code)
	return e
}

// Code first: quota and calculation failures ride on more than one status.
func classify(status int, code string) error {
	switch {
	case quotaCodes[code]:
		return ErrQuotaExceeded
	case calculationCodes[code]:
		return ErrCalculation
	case status == 400:
		return ErrBadRequest
	case status == 401:
		return ErrAuthentication
	case status == 402:
		return ErrQuotaExceeded
	case status == 403:
		return ErrPermissionDenied
	case status == 404:
		return ErrNotFound
	case status == 422:
		return ErrUnprocessableEntity
	case status == 429:
		return ErrRateLimit
	case status >= 500:
		return ErrInternalServer
	}
	return ErrUnexpectedReply
}

func retryAfter(v string) *time.Duration {
	if v == "" {
		return nil
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
		d := time.Duration(s * float64(time.Second))
		return &d
	}
	if t, err := http.ParseTime(v); err == nil {
		d := max(time.Until(t), 0)
		return &d
	}
	return nil
}
