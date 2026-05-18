package tmdb

import (
	"encoding/json"
	"errors"
	"fmt"
)

// APIError is returned when TMDB responds with a non-2xx status. The
// HTTPStatusCode and HTTPStatus fields are always populated; StatusCode,
// StatusMessage, and Success are decoded from TMDB's error envelope when
// the body matches the expected shape.
//
//	if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
//	    log.Printf("tmdb: %s (%d)", apiErr.StatusMessage, apiErr.StatusCode)
//	}
type APIError struct {
	// HTTPStatusCode is the HTTP status code (e.g. 401).
	HTTPStatusCode int `json:"-"`
	// HTTPStatus is the full HTTP status line (e.g. "401 Unauthorized").
	HTTPStatus string `json:"-"`
	// StatusCode is TMDB's internal status code (see
	// https://developer.themoviedb.org/docs/errors).
	StatusCode int `json:"status_code"`
	// StatusMessage is TMDB's human-readable error message.
	StatusMessage string `json:"status_message"`
	// Success is always false for errors. TMDB includes it in the envelope
	// for consistency.
	Success bool `json:"success"`
	// Body is the raw response body. Useful when TMDB returns a non-standard
	// error shape.
	Body []byte `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.StatusMessage != "" {
		return fmt.Sprintf("tmdb: HTTP %d (status %d): %s", e.HTTPStatusCode, e.StatusCode, e.StatusMessage)
	}
	return fmt.Sprintf("tmdb: HTTP %d", e.HTTPStatusCode)
}

// AsAPIError inspects an oapi-codegen *WithResponse value and returns a
// typed *APIError when the HTTP status is >= 400. It returns nil for
// success responses. The TMDB error envelope is parsed best-effort from
// body; fields that cannot be decoded remain at their zero value.
//
// The first argument satisfies the small interface implemented by every
// generated FooResponse (Status() / StatusCode() methods).
func AsAPIError(r interface {
	StatusCode() int
	Status() string
}, body []byte) *APIError {
	if r == nil || r.StatusCode() < 400 {
		return nil
	}
	e := &APIError{
		HTTPStatusCode: r.StatusCode(),
		HTTPStatus:     r.Status(),
		Body:           body,
	}
	// Best-effort decode: TMDB usually returns the envelope, but some 5xx
	// responses come back as plain text from an upstream proxy.
	_ = json.Unmarshal(body, e)
	return e
}

// IsAPIError reports whether err is, or wraps, an *APIError.
func IsAPIError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr)
}
