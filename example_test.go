package tmdb_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/thulasirajkomminar/tmdb-go"
)

// Basic usage: construct an aggregated client with a v4 read-access token
// and reach an endpoint via the appropriate sub-client field.
func ExampleNew() {
	client, err := tmdb.New("YOUR_TMDB_V4_READ_ACCESS_TOKEN")
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Movie.DetailsWithResponse(context.Background(), 550, nil)
	if err != nil {
		log.Fatal(err)
	}
	if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
		log.Fatal(apiErr)
	}
	fmt.Println(*resp.JSON200.Title)
}

// Disabling retries: pass a zero-valued RetryConfig.
func ExampleWithRetryConfig_disabled() {
	client, _ := tmdb.New(
		"token",
		tmdb.WithRetryConfig(tmdb.RetryConfig{}),
	)
	_ = client
}

// Custom retry policy: bump MaxRetries and tighten backoff.
func ExampleWithRetryConfig_custom() {
	client, _ := tmdb.New(
		"token",
		tmdb.WithRetryConfig(tmdb.RetryConfig{
			MaxRetries: 5,
			MinBackoff: 250 * time.Millisecond,
			MaxBackoff: 5 * time.Second,
		}),
	)
	_ = client
}

// Pointing the client at a mock server in tests.
func ExampleWithServer() {
	client, _ := tmdb.New(
		"test-token",
		tmdb.WithServer("https://mock.example.com"),
	)
	_ = client
}

// Adding a custom request editor — e.g. for tracing headers.
func ExampleWithRequestEditor() {
	traceID := "abc-123"
	client, _ := tmdb.New(
		"token",
		tmdb.WithRequestEditor(func(_ context.Context, req *http.Request) error {
			req.Header.Set("X-Trace-ID", traceID)
			return nil
		}),
	)
	_ = client
}

// Typed error handling: AsAPIError converts a non-2xx response into a
// rich *APIError. Use errors.As to inspect it.
func ExampleAsAPIError() {
	client, _ := tmdb.New("token")

	resp, err := client.Movie.DetailsWithResponse(context.Background(), 999999, nil)
	if err != nil {
		log.Fatal(err)
	}
	if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
		fmt.Printf("tmdb error: code=%d msg=%q\n", apiErr.StatusCode, apiErr.StatusMessage)
		return
	}
	fmt.Println(*resp.JSON200.Title)
}

// IsAPIError unwraps wrapped errors and reports whether any layer is a
// TMDB *APIError. Useful when you've wrapped the SDK call inside your
// own error type.
func ExampleIsAPIError() {
	innerErr := &tmdb.APIError{
		HTTPStatusCode: 401,
		StatusCode:     7,
		StatusMessage:  "Invalid API key.",
	}
	wrapped := fmt.Errorf("auth check failed: %w", innerErr)

	if tmdb.IsAPIError(wrapped) {
		var apiErr *tmdb.APIError
		_ = errors.As(wrapped, &apiErr)
		fmt.Println(apiErr.StatusMessage)
	}
	// Output: Invalid API key.
}
