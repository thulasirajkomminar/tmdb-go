package tmdb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thulasirajkomminar/tmdb-go"
)

func TestNew(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"empty token is rejected", "", true},
		{"non-empty token succeeds", "dummy", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := tmdb.New(tc.token)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, c)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, c)
		})
	}
}

// All sub-client fields on Client must be non-nil after New. Use
// reflection so adding a new sub-package automatically extends coverage.
func TestNew_allSubClientsPopulated(t *testing.T) {
	t.Parallel()

	c, err := tmdb.New("dummy")
	require.NoError(t, err)

	v := reflect.ValueOf(c).Elem()
	require.Greater(t, v.NumField(), 0, "Client struct has no fields")

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name
		require.Equal(t, reflect.Ptr, f.Kind(), "field %s is not a pointer", name)
		assert.False(t, f.IsNil(), "field %s should be non-nil after New", name)
	}
}

// captureServer spins up a mock server that records every request and
// serves an empty 200. The returned URL plugs into tmdb.WithServer.
func captureServer(t *testing.T) (url string, lastReq func() *http.Request) {
	t.Helper()
	var captured *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"status_code":1,"status_message":"OK"}`))
	}))
	t.Cleanup(ts.Close)
	return ts.URL, func() *http.Request { return captured }
}

func TestRequestPipeline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		token     string
		opts      func(serverURL string) []tmdb.Option
		assertReq func(t *testing.T, req *http.Request)
	}{
		{
			name:  "bearer token is attached",
			token: "test-token-123",
			opts: func(url string) []tmdb.Option {
				return []tmdb.Option{tmdb.WithServer(url)}
			},
			assertReq: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "Bearer test-token-123", req.Header.Get("Authorization"))
				assert.True(t, strings.HasPrefix(req.URL.Path, "/3/authentication"),
					"request path = %q, want /3/authentication...", req.URL.Path)
			},
		},
		{
			name:  "custom request editor runs alongside the bearer editor",
			token: "tok",
			opts: func(url string) []tmdb.Option {
				return []tmdb.Option{
					tmdb.WithServer(url),
					tmdb.WithRequestEditor(func(_ context.Context, req *http.Request) error {
						req.Header.Set("X-Test", "hello")
						return nil
					}),
				}
			},
			assertReq: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
				assert.Equal(t, "hello", req.Header.Get("X-Test"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			url, lastReq := captureServer(t)

			c, err := tmdb.New(tc.token, tc.opts(url)...)
			require.NoError(t, err)

			resp, err := c.Authentication.ValidateKey(context.Background())
			require.NoError(t, err)
			defer resp.Body.Close()

			req := lastReq()
			require.NotNil(t, req, "mock server received no request")
			tc.assertReq(t, req)
		})
	}
}

// WithHTTPClient takes precedence over the default. Detect it by routing
// through a custom RoundTripper that records the fact it was used.
func TestWithHTTPClient_isUsed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(ts.Close)

	rt := &recordingTransport{inner: http.DefaultTransport}
	custom := &http.Client{Transport: rt}

	c, err := tmdb.New("tok",
		tmdb.WithServer(ts.URL),
		tmdb.WithHTTPClient(custom),
	)
	require.NoError(t, err)

	resp, err := c.Authentication.ValidateKey(context.Background())
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Positive(t, rt.calls, "custom http.Client.Transport was never invoked")
}

type recordingTransport struct {
	inner http.RoundTripper
	calls int
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return r.inner.RoundTrip(req)
}
