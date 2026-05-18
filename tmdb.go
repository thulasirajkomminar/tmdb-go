// Package tmdb is a Go SDK for The Movie Database (TMDB) HTTP API.
//
// The 152 operations in the TMDB v3 spec are split across sub-packages by
// their URL prefix — Movies under [github.com/thulasirajkomminar/tmdb-go/movie],
// TV under [github.com/thulasirajkomminar/tmdb-go/tv], and so on. The Client
// returned by [New] aggregates one client per sub-package so callers can
// reach any endpoint from a single entry point:
//
//	c, _ := tmdb.New("YOUR_TMDB_V4_READ_ACCESS_TOKEN")
//	resp, _ := c.Movie.DetailsWithResponse(ctx, 550, nil)
//	resp, _ := c.Tv.SeriesDetailsWithResponse(ctx, 1399, nil)
//
// The list of sub-packages and the [Client] struct itself are emitted by
// `task generate` from the upstream spec — see tmdb.gen.go.
package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultServer is the canonical TMDB API base URL.
const DefaultServer = "https://api.themoviedb.org"

// DefaultTimeout is applied to the HTTP client when callers do not supply
// their own via WithHTTPClient.
const DefaultTimeout = 30 * time.Second

// Date is the calendar-date type used by generated request params for fields
// declared as OpenAPI `format: date` (e.g. PrimaryReleaseDateGte). Re-exported
// so callers don't have to import oapi-codegen's runtime/types package.
type Date = openapi_types.Date

// HTTPClient is the small interface every sub-package expects for performing
// requests. *http.Client satisfies it.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// RequestEditor mirrors each sub-package's RequestEditorFn. The same value
// can be passed to every sub-client.
type RequestEditor func(ctx context.Context, req *http.Request) error

// Option configures the aggregated Client at construction time.
type Option func(*config)

type config struct {
	server     string
	httpClient HTTPClient
	editors    []RequestEditor
	retry      RetryConfig
}

// WithHTTPClient overrides the default *http.Client (which has DefaultTimeout).
func WithHTTPClient(c HTTPClient) Option {
	return func(cfg *config) { cfg.httpClient = c }
}

// WithBearerToken attaches an "Authorization: Bearer <token>" header to every
// outgoing request — the security scheme declared in the TMDB spec.
func WithBearerToken(token string) Option {
	return WithRequestEditor(bearerEditor(token))
}

// WithRequestEditor appends a callback that may mutate every outgoing request
// before it is sent. Editors run in the order they are registered.
func WithRequestEditor(fn RequestEditor) Option {
	return func(cfg *config) { cfg.editors = append(cfg.editors, fn) }
}

// WithServer overrides DefaultServer (useful for mock servers in tests).
func WithServer(server string) Option {
	return func(cfg *config) { cfg.server = server }
}

// New returns an aggregated Client configured for the TMDB API: it sets
// DefaultServer as the base URL, attaches a bearer-token request editor,
// and uses an *http.Client with DefaultTimeout. Each sub-package field
// receives the same configuration.
//
// The token must be a TMDB v4 "API Read Access Token" (from your account's
// API settings page), not the legacy v3 API key.
//
// opts are applied after the defaults, so callers can override any of them.
func New(token string, opts ...Option) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("tmdb: bearer token must not be empty")
	}
	cfg := &config{
		server:     DefaultServer,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		retry:      DefaultRetryConfig.resolved(),
	}
	cfg.editors = append(cfg.editors, bearerEditor(token))
	for _, o := range opts {
		o(cfg)
	}

	// Generated NewClient appends a trailing slash to the server; do the
	// same here so direct-struct construction matches that contract.
	server := cfg.server
	if len(server) == 0 || server[len(server)-1] != '/' {
		server += "/"
	}

	doer := cfg.httpClient
	if cfg.retry.MaxRetries > 0 {
		doer = &retryClient{inner: doer, cfg: cfg.retry}
	}

	c := &Client{}
	c.fillSubClients(server, doer, cfg.editors)
	return c, nil
}

// editors converts the facade's []RequestEditor into a sub-package's
// []RequestEditorFn slice. The underlying function signature is identical,
// so each element is a no-op conversion.
func editors[T ~func(context.Context, *http.Request) error](in []RequestEditor) []T {
	out := make([]T, len(in))
	for i, e := range in {
		out[i] = T(e)
	}
	return out
}

func bearerEditor(token string) RequestEditor {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}
