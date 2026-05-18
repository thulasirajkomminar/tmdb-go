package tmdb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thulasirajkomminar/tmdb-go"
)

// retryConfigFast is a low-latency retry policy shared across the table
// cases so the suite stays quick.
var retryConfigFast = tmdb.RetryConfig{
	MaxRetries: 3,
	MinBackoff: 1 * time.Millisecond,
	MaxBackoff: 10 * time.Millisecond,
}

// retryServer counts calls and returns the supplied responses in order.
// If more requests come in than `codes` entries, it serves 200.
func retryServer(t *testing.T, codes ...int) (url string, calls *atomic.Int32) {
	t.Helper()
	calls = new(atomic.Int32)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := calls.Add(1)
		code := http.StatusOK
		if int(i)-1 < len(codes) {
			code = codes[int(i)-1]
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(ts.Close)
	return ts.URL, calls
}

func TestRetry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		codes      []int
		cfg        tmdb.RetryConfig
		wantCalls  int32
		wantStatus int
	}{
		{
			name:       "retries 429 then succeeds",
			codes:      []int{http.StatusTooManyRequests, http.StatusOK},
			cfg:        retryConfigFast,
			wantCalls:  2,
			wantStatus: http.StatusOK,
		},
		{
			name:       "retries through 5xx then succeeds",
			codes:      []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK},
			cfg:        retryConfigFast,
			wantCalls:  3,
			wantStatus: http.StatusOK,
		},
		{
			name:       "zero MaxRetries disables retry",
			codes:      []int{http.StatusTooManyRequests},
			cfg:        tmdb.RetryConfig{},
			wantCalls:  1,
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name:       "exhausts retries and returns the last response",
			codes:      []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable},
			cfg:        tmdb.RetryConfig{MaxRetries: 2, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
			wantCalls:  3, // 1 initial + 2 retries
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "succeeds on first attempt without retrying",
			codes:      nil, // server always serves 200
			cfg:        retryConfigFast,
			wantCalls:  1,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			url, calls := retryServer(t, tc.codes...)

			c, err := tmdb.New("tok",
				tmdb.WithServer(url),
				tmdb.WithRetryConfig(tc.cfg),
			)
			require.NoError(t, err)

			resp, err := c.Authentication.ValidateKey(context.Background())
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.wantCalls, calls.Load())
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
		})
	}
}

// Context cancellation is its own test because the assertion shape
// differs (error path, bounded call count rather than exact match).
func TestRetry_contextCancellationStopsRetries(t *testing.T) {
	t.Parallel()
	url, calls := retryServer(t,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
	)

	c, err := tmdb.New("tok",
		tmdb.WithServer(url),
		tmdb.WithRetryConfig(tmdb.RetryConfig{
			MaxRetries: 5,
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 50 * time.Millisecond,
		}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err = c.Authentication.ValidateKey(ctx)
	assert.Error(t, err, "expected context error")
	assert.Less(t, calls.Load(), int32(5), "context should cancel before retries exhaust")
}
