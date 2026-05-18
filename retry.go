package tmdb

import (
	"net/http"
	"strconv"
	"time"
)

// RetryConfig controls automatic retries for transient TMDB failures.
// New applies DefaultRetryConfig unless the caller passes WithRetryConfig
// (use WithRetryConfig(RetryConfig{}) to disable retry entirely).
//
// MinBackoff and MaxBackoff are resolved at construction time: a zero (or
// negative) value is replaced by the corresponding field of
// DefaultRetryConfig. Set them to a deliberately small duration (e.g.
// 1*time.Nanosecond) if you really want effectively no wait.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts after the first
	// request. Zero or negative disables retry entirely.
	MaxRetries int
	// MinBackoff is the wait before the first retry; the wait doubles
	// each attempt up to MaxBackoff. Defaults to 500ms when left zero.
	MinBackoff time.Duration
	// MaxBackoff caps the per-attempt wait. Defaults to 30s when left zero.
	MaxBackoff time.Duration
}

// DefaultRetryConfig is applied by New when the caller does not pass
// WithRetryConfig, and supplies fallback values for zero-valued fields
// of any RetryConfig passed to WithRetryConfig.
var DefaultRetryConfig = RetryConfig{
	MaxRetries: 3,
	MinBackoff: 500 * time.Millisecond,
	MaxBackoff: 30 * time.Second,
}

// WithRetryConfig sets the retry policy. Zero-valued MinBackoff and
// MaxBackoff inherit from DefaultRetryConfig. Pass RetryConfig{} (zero
// MaxRetries) to disable retry entirely.
func WithRetryConfig(cfg RetryConfig) Option {
	return func(c *config) { c.retry = cfg.resolved() }
}

// resolved fills in zero-valued backoff fields from DefaultRetryConfig.
// When retries are disabled (MaxRetries <= 0), the zero value is returned
// so the rest of the struct is irrelevant.
func (cfg RetryConfig) resolved() RetryConfig {
	if cfg.MaxRetries <= 0 {
		return RetryConfig{}
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = DefaultRetryConfig.MinBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultRetryConfig.MaxBackoff
	}
	return cfg
}

// retryClient retries 429/5xx GET requests. Non-idempotent methods are
// passed through unchanged.
type retryClient struct {
	inner HTTPClient
	cfg   RetryConfig
}

func (r *retryClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || r.cfg.MaxRetries <= 0 {
		return r.inner.Do(req)
	}

	var (
		lastResp *http.Response
		lastErr  error
	)
	backoff := r.cfg.MinBackoff
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		if lastResp != nil {
			_ = lastResp.Body.Close()
			lastResp = nil
		}
		resp, err := r.inner.Do(req)
		if err == nil && !shouldRetry(resp.StatusCode) {
			return resp, nil
		}
		lastResp = resp
		lastErr = err

		if attempt == r.cfg.MaxRetries {
			break
		}

		wait := backoff
		if resp != nil {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, perr := strconv.Atoi(ra); perr == nil && secs > 0 {
					wait = time.Duration(secs) * time.Second
				}
			}
		}
		if wait > r.cfg.MaxBackoff {
			wait = r.cfg.MaxBackoff
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-req.Context().Done():
			timer.Stop()
			if lastResp != nil {
				_ = lastResp.Body.Close()
			}
			return nil, req.Context().Err()
		}
		backoff *= 2
	}

	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
