package tmdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetryConfig_resolved(t *testing.T) {
	t.Parallel()

	defaultMin := DefaultRetryConfig.MinBackoff
	defaultMax := DefaultRetryConfig.MaxBackoff

	cases := []struct {
		name string
		in   RetryConfig
		want RetryConfig
	}{
		{
			name: "zero backoffs inherit defaults",
			in:   RetryConfig{MaxRetries: 5},
			want: RetryConfig{MaxRetries: 5, MinBackoff: defaultMin, MaxBackoff: defaultMax},
		},
		{
			name: "only MinBackoff inherits",
			in:   RetryConfig{MaxRetries: 2, MaxBackoff: 1 * time.Second},
			want: RetryConfig{MaxRetries: 2, MinBackoff: defaultMin, MaxBackoff: 1 * time.Second},
		},
		{
			name: "only MaxBackoff inherits",
			in:   RetryConfig{MaxRetries: 2, MinBackoff: 10 * time.Millisecond},
			want: RetryConfig{MaxRetries: 2, MinBackoff: 10 * time.Millisecond, MaxBackoff: defaultMax},
		},
		{
			name: "explicit values preserved verbatim",
			in:   RetryConfig{MaxRetries: 5, MinBackoff: 7 * time.Millisecond, MaxBackoff: 9 * time.Second},
			want: RetryConfig{MaxRetries: 5, MinBackoff: 7 * time.Millisecond, MaxBackoff: 9 * time.Second},
		},
		{
			name: "zero MaxRetries flattens entire config to zero",
			in:   RetryConfig{MinBackoff: time.Second, MaxBackoff: time.Minute},
			want: RetryConfig{},
		},
		{
			name: "negative MaxRetries also disables",
			in:   RetryConfig{MaxRetries: -1, MinBackoff: time.Second, MaxBackoff: time.Minute},
			want: RetryConfig{},
		},
		{
			name: "negative backoffs inherit defaults (treated like zero)",
			in:   RetryConfig{MaxRetries: 1, MinBackoff: -1, MaxBackoff: -1},
			want: RetryConfig{MaxRetries: 1, MinBackoff: defaultMin, MaxBackoff: defaultMax},
		},
		{
			name: "zero value (everything default) stays zero",
			in:   RetryConfig{},
			want: RetryConfig{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.in.resolved())
		})
	}
}
