package tmdb_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thulasirajkomminar/tmdb-go"
)

// fakeResp lets the table cases construct a minimal value satisfying
// tmdb.AsAPIError's interface input without standing up a sub-package
// response type.
type fakeResp struct {
	code int
	text string
}

func (f fakeResp) StatusCode() int { return f.code }
func (f fakeResp) Status() string  { return f.text }

func TestAsAPIError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		resp           fakeResp
		body           []byte
		wantNil        bool
		wantHTTPStatus int
		wantStatusCode int
		wantMessage    string
		wantErrorText  string
	}{
		{
			name:    "success status returns nil",
			resp:    fakeResp{code: http.StatusOK, text: "200 OK"},
			body:    []byte(`{"id":1}`),
			wantNil: true,
		},
		{
			name:    "2xx (created) returns nil",
			resp:    fakeResp{code: http.StatusCreated, text: "201 Created"},
			body:    []byte(`{"id":99}`),
			wantNil: true,
		},
		{
			name:           "401 with TMDB envelope is decoded",
			resp:           fakeResp{code: http.StatusUnauthorized, text: "401 Unauthorized"},
			body:           []byte(`{"status_code":7,"status_message":"Invalid API key.","success":false}`),
			wantHTTPStatus: http.StatusUnauthorized,
			wantStatusCode: 7,
			wantMessage:    "Invalid API key.",
			wantErrorText:  "tmdb: HTTP 401 (status 7): Invalid API key.",
		},
		{
			name:           "502 with HTML body falls back to HTTP-only message",
			resp:           fakeResp{code: http.StatusBadGateway, text: "502 Bad Gateway"},
			body:           []byte("<html>upstream blew up</html>"),
			wantHTTPStatus: http.StatusBadGateway,
			wantStatusCode: 0,
			wantMessage:    "",
			wantErrorText:  "tmdb: HTTP 502",
		},
		{
			name:           "404 with empty body still produces an APIError",
			resp:           fakeResp{code: http.StatusNotFound, text: "404 Not Found"},
			body:           nil,
			wantHTTPStatus: http.StatusNotFound,
			wantErrorText:  "tmdb: HTTP 404",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tmdb.AsAPIError(tc.resp, tc.body)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantHTTPStatus, got.HTTPStatusCode)
			assert.Equal(t, tc.wantStatusCode, got.StatusCode)
			assert.Equal(t, tc.wantMessage, got.StatusMessage)
			assert.Equal(t, tc.wantErrorText, got.Error())
			assert.Equal(t, tc.body, got.Body)
		})
	}
}

func TestIsAPIError(t *testing.T) {
	t.Parallel()

	apiErr := &tmdb.APIError{HTTPStatusCode: 404}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain error", errors.New("plain"), false},
		{"raw APIError", apiErr, true},
		{"wrapped APIError via fmt.Errorf", fmt.Errorf("call failed: %w", apiErr), true},
		{"double-wrapped APIError", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", apiErr)), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tmdb.IsAPIError(tc.err))
		})
	}
}
