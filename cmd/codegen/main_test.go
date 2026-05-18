package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagFromPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single-segment", "/3/movie", "movie"},
		{"nested", "/3/movie/{movie_id}", "movie"},
		{"deeply nested", "/3/tv/{series_id}/season/{n}/episode/{m}", "tv"},
		{"rename guest_session", "/3/guest_session/{id}/rated/movies", "guest"},
		{"empty after prefix", "/3/", ""},
		{"missing /3/ prefix", "/api/foo", ""},
		{"empty input", "", ""},
		{"unrelated absolute path", "/v1/movie", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tagFromPath(tc.in))
		})
	}
}

func TestTagOperations(t *testing.T) {
	t.Parallel()

	t.Run("injects tag onto every operation and returns sorted unique tags", func(t *testing.T) {
		t.Parallel()
		spec := map[string]any{
			"paths": map[string]any{
				"/3/movie/{id}": map[string]any{
					"get":  map[string]any{"operationId": "movie-details"},
					"post": map[string]any{"operationId": "movie-add"},
				},
				"/3/tv/{id}": map[string]any{
					"get": map[string]any{"operationId": "tv-series-details"},
				},
				"/3/guest_session/{id}/rated": map[string]any{
					"get": map[string]any{"operationId": "guest-rated"},
				},
			},
		}

		tags := tagOperations(spec)

		assert.Equal(t, []string{"guest", "movie", "tv"}, tags)
		movieGet := spec["paths"].(map[string]any)["/3/movie/{id}"].(map[string]any)["get"].(map[string]any)
		assert.Equal(t, []any{"movie"}, movieGet["tags"])
		guestGet := spec["paths"].(map[string]any)["/3/guest_session/{id}/rated"].(map[string]any)["get"].(map[string]any)
		assert.Equal(t, []any{"guest"}, guestGet["tags"], "guest_session segment should be renamed to guest")
	})

	t.Run("skips path items that aren't objects", func(t *testing.T) {
		t.Parallel()
		spec := map[string]any{
			"paths": map[string]any{
				"/3/movie/{id}": "not-an-object",
				"/3/tv/{id}": map[string]any{
					"get": map[string]any{"operationId": "tv-details"},
				},
			},
		}

		tags := tagOperations(spec)

		assert.Equal(t, []string{"tv"}, tags)
	})

	t.Run("skips method entries without an operationId", func(t *testing.T) {
		t.Parallel()
		spec := map[string]any{
			"paths": map[string]any{
				"/3/movie/{id}": map[string]any{
					"parameters": []any{}, // common PathItem-level field, not a method
					"get":        map[string]any{"operationId": "movie-details"},
					"post":       map[string]any{}, // no operationId
				},
			},
		}

		tags := tagOperations(spec)
		require.Equal(t, []string{"movie"}, tags)

		getOp := spec["paths"].(map[string]any)["/3/movie/{id}"].(map[string]any)["get"].(map[string]any)
		assert.Equal(t, []any{"movie"}, getOp["tags"])

		postOp := spec["paths"].(map[string]any)["/3/movie/{id}"].(map[string]any)["post"].(map[string]any)
		_, hasTags := postOp["tags"]
		assert.False(t, hasTags, "operations without operationId should not get tags")
	})

	t.Run("paths not under /3/ are ignored", func(t *testing.T) {
		t.Parallel()
		spec := map[string]any{
			"paths": map[string]any{
				"/v1/movie": map[string]any{
					"get": map[string]any{"operationId": "v1-movie"},
				},
			},
		}

		assert.Empty(t, tagOperations(spec))
	})

	t.Run("spec without paths returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, tagOperations(map[string]any{}))
	})
}

func TestStripOperationIDPrefixes(t *testing.T) {
	t.Parallel()

	// Build a fresh spec per case — strip mutates in place.
	makeSpec := func(ops map[string]string) map[string]any {
		// ops maps path -> operationId; method is always "get".
		paths := map[string]any{}
		for path, opID := range ops {
			paths[path] = map[string]any{
				"get": map[string]any{"operationId": opID},
			}
		}
		return map[string]any{"paths": paths}
	}

	opIDOf := func(spec map[string]any, path string) string {
		v, _ := spec["paths"].(map[string]any)[path].(map[string]any)["get"].(map[string]any)["operationId"].(string)
		return v
	}

	t.Run("strips matching prefix", func(t *testing.T) {
		t.Parallel()
		spec := makeSpec(map[string]string{
			"/3/movie/{id}":    "movie-details",
			"/3/movie/popular": "movie-popular-list",
		})
		tags := tagOperations(spec)
		stats, collisions := stripOperationIDPrefixes(spec, tags)

		assert.Empty(t, collisions)
		assert.Equal(t, 2, stats.stripped)
		assert.Equal(t, 0, stats.collisions)
		assert.Equal(t, 0, stats.unchanged)
		assert.Equal(t, "details", opIDOf(spec, "/3/movie/{id}"))
		assert.Equal(t, "popular-list", opIDOf(spec, "/3/movie/popular"))
	})

	t.Run("leaves operationIds without the tag prefix unchanged", func(t *testing.T) {
		t.Parallel()
		// changes-movie-list belongs to the movie tag by path, but its
		// operationId doesn't begin with "movie-".
		spec := makeSpec(map[string]string{
			"/3/movie/{id}":    "movie-details",
			"/3/movie/changes": "changes-movie-list",
		})
		tags := tagOperations(spec)
		stats, collisions := stripOperationIDPrefixes(spec, tags)

		assert.Empty(t, collisions)
		assert.Equal(t, 1, stats.stripped)
		assert.Equal(t, 1, stats.unchanged)
		assert.Equal(t, 0, stats.collisions)
		assert.Equal(t, "details", opIDOf(spec, "/3/movie/{id}"))
		assert.Equal(t, "changes-movie-list", opIDOf(spec, "/3/movie/changes"))
	})

	t.Run("two ops that would collide on the same stripped name keep originals", func(t *testing.T) {
		t.Parallel()
		// Both would become "details" after strip: movie-details strips,
		// movie-details-extra strips, they don't actually collide here;
		// pick a real clash with a non-stripping op.
		spec := makeSpec(map[string]string{
			"/3/movie/{id}":    "movie-details",
			"/3/movie/literal": "details", // no prefix; same final name as above
		})
		tags := tagOperations(spec)
		stats, collisions := stripOperationIDPrefixes(spec, tags)

		require.Len(t, collisions, 1)
		assert.Equal(t, "movie", collisions[0].Tag)
		assert.Equal(t, "details", collisions[0].FinalName)
		assert.Equal(t, []string{"details", "movie-details"}, collisions[0].OriginalIDs, "collision report should sort original IDs")

		assert.Equal(t, 1, stats.collisions, "the strip-able op should be counted as collided")
		assert.Equal(t, 0, stats.stripped)
		assert.Equal(t, 1, stats.unchanged, "the non-prefixed op is counted as unchanged")
		assert.Equal(t, "movie-details", opIDOf(spec, "/3/movie/{id}"), "collided op keeps original ID")
		assert.Equal(t, "details", opIDOf(spec, "/3/movie/literal"))
	})

	t.Run("two strip-eligible ops with the same final name collide too", func(t *testing.T) {
		t.Parallel()
		// Construct a deliberate clash: two operationIds in the same tag
		// that BOTH strip to the same name.
		spec := makeSpec(map[string]string{
			"/3/movie/{id}":  "movie-details",
			"/3/movie/other": "movie-details", // identical opIds in two paths
		})
		tags := tagOperations(spec)
		stats, collisions := stripOperationIDPrefixes(spec, tags)

		require.Len(t, collisions, 1)
		assert.Equal(t, "movie", collisions[0].Tag)
		assert.Equal(t, "details", collisions[0].FinalName)
		assert.Equal(t, 2, stats.collisions)
		assert.Equal(t, 0, stats.stripped)
		// Both keep their original (identical) IDs.
		assert.Equal(t, "movie-details", opIDOf(spec, "/3/movie/{id}"))
		assert.Equal(t, "movie-details", opIDOf(spec, "/3/movie/other"))
	})

	t.Run("collisions in one tag do not block strips in another", func(t *testing.T) {
		t.Parallel()
		spec := makeSpec(map[string]string{
			"/3/movie/{id}":    "movie-details",
			"/3/movie/literal": "details", // collides with the above
			"/3/tv/{id}":       "tv-series-details",
		})
		tags := tagOperations(spec)
		stats, collisions := stripOperationIDPrefixes(spec, tags)

		require.Len(t, collisions, 1)
		assert.Equal(t, "movie", collisions[0].Tag)

		// movie tag: 1 collision, 1 unchanged. tv tag: 1 stripped.
		assert.Equal(t, 1, stats.stripped)
		assert.Equal(t, 1, stats.collisions)
		assert.Equal(t, 1, stats.unchanged)
		assert.Equal(t, "series-details", opIDOf(spec, "/3/tv/{id}"))
	})

	t.Run("empty spec is a no-op", func(t *testing.T) {
		t.Parallel()
		spec := map[string]any{"paths": map[string]any{}}
		stats, collisions := stripOperationIDPrefixes(spec, nil)

		assert.Empty(t, collisions)
		assert.Equal(t, stripStats{}, stats)
	})

	t.Run("spec without paths returns zero stats", func(t *testing.T) {
		t.Parallel()
		stats, collisions := stripOperationIDPrefixes(map[string]any{}, nil)
		assert.Empty(t, collisions)
		assert.Equal(t, stripStats{}, stats)
	})
}

func TestRenderFacade(t *testing.T) {
	t.Parallel()

	t.Run("single tag", func(t *testing.T) {
		t.Parallel()
		out, err := renderFacade([]string{"movie"})
		require.NoError(t, err)

		s := string(out)
		assert.True(t, strings.HasPrefix(s, "// Code generated by cmd/codegen; DO NOT EDIT."))
		assert.Contains(t, s, `"github.com/thulasirajkomminar/tmdb-go/movie"`)
		assert.Contains(t, s, "Movie *movie.ClientWithResponses")
		assert.Contains(t, s, "c.Movie = &movie.ClientWithResponses{")
		assert.Contains(t, s, "editors[movie.RequestEditorFn](eds)")
	})

	t.Run("multiple tags preserve order and title-case fields", func(t *testing.T) {
		t.Parallel()
		out, err := renderFacade([]string{"account", "tv"})
		require.NoError(t, err)

		s := string(out)
		assert.Contains(t, s, `"github.com/thulasirajkomminar/tmdb-go/account"`)
		assert.Contains(t, s, `"github.com/thulasirajkomminar/tmdb-go/tv"`)
		// gofmt aligns struct fields (variable whitespace) and the
		// generic call site, so match against the constructor-call
		// substrings which gofmt leaves alone.
		assert.Contains(t, s, "c.Account = &account.ClientWithResponses{")
		assert.Contains(t, s, "c.Tv = &tv.ClientWithResponses{")
		// Caller passes tags sorted; the template preserves that order.
		assert.Less(t, strings.Index(s, "c.Account"), strings.Index(s, "c.Tv"))
	})

	t.Run("empty tag list produces a valid (empty) Client", func(t *testing.T) {
		t.Parallel()
		out, err := renderFacade(nil)
		require.NoError(t, err)

		s := string(out)
		assert.Contains(t, s, "type Client struct {")
		assert.Contains(t, s, "func (c *Client) fillSubClients(")
		// No tag means no imports beyond the bare ones (there are none).
		assert.NotContains(t, s, `"github.com/thulasirajkomminar/tmdb-go/`)
	})

	t.Run("output is gofmt clean (round-trips through format.Source)", func(t *testing.T) {
		t.Parallel()
		// renderFacade already invokes format.Source; if the template ever
		// emits something gofmt rejects, renderFacade returns an error.
		_, err := renderFacade([]string{"account", "movie", "tv"})
		require.NoError(t, err)
	})
}
