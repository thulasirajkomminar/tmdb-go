# tmdb-go

[![CI](https://github.com/thulasirajkomminar/tmdb-go/actions/workflows/ci.yml/badge.svg)](https://github.com/thulasirajkomminar/tmdb-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thulasirajkomminar/tmdb-go.svg)](https://pkg.go.dev/github.com/thulasirajkomminar/tmdb-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/thulasirajkomminar/tmdb-go)](https://goreportcard.com/report/github.com/thulasirajkomminar/tmdb-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

An unofficial Go SDK for [The Movie Database (TMDB) API](https://developer.themoviedb.org/).
The HTTP surface is generated from the official OpenAPI spec via
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen), split into
one sub-package per resource family (`movie`, `tv`, `discover`, …), and
fronted by a thin aggregator facade that handles base URL, bearer-token
auth, and HTTP timeout.

> **Status:** early / experimental. All 152 TMDB v3 endpoints are covered,
> but the upstream spec is OpenAPI 3.1 and many response schemas are
> inline / loosely typed.

## Install

```sh
go get github.com/thulasirajkomminar/tmdb-go
```

Importing the SDK has no special Go-version requirement; regenerating it
needs Go 1.24+ for the `tool` directive.

## Quickstart

Grab your **API Read Access Token** (v4 bearer token) from your TMDB
account's [API settings page](https://www.themoviedb.org/settings/api).

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/thulasirajkomminar/tmdb-go"
)

func main() {
    client, err := tmdb.New(os.Getenv("TMDB_TOKEN"))
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Movie.DetailsWithResponse(context.Background(), 550, nil)
    if err != nil {
        log.Fatal(err)
    }
    // Typed error: AsAPIError surfaces TMDB's status envelope when the
    // request was carried out but the API itself returned >= 400.
    if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
        log.Fatal(apiErr)
    }
    fmt.Println("title:", *resp.JSON200.Title)
}
```

`tmdb.New` configures the aggregated client with:

- the canonical base URL `https://api.themoviedb.org`
- an `Authorization: Bearer <token>` header on every request
- an `*http.Client` with a 30-second timeout
- automatic retry (3 attempts, exponential backoff, honours `Retry-After`)
  for `429` and `5xx` responses on GET requests

## Reaching endpoints

Operations are grouped by their `/3/<segment>/...` path prefix. Reach them
through the matching field on the aggregator:

```go
client.Movie.DetailsWithResponse(ctx, 550, nil)
client.Tv.SeriesDetailsWithResponse(ctx, 1399, nil)
client.Discover.MovieWithResponse(ctx, params)
client.Search.MovieWithResponse(ctx, params)
```

Every sub-client exposes both flavours:

| Method | Returns | When to use |
| --- | --- | --- |
| `Foo` | `*http.Response` | You want to stream / inspect the raw body. |
| `FooWithResponse` | `*FooResponse` (with `JSON200`, `Body`, `HTTPResponse`, `Status()`) | You want a parsed payload. Recommended. |

## Customization

```go
client, err := tmdb.New(
    token,
    tmdb.WithHTTPClient(myHTTPClient),         // override the default 30s timeout
    tmdb.WithRequestEditor(addTracingHeader),  // attach extra request middleware
    tmdb.WithServer("https://mock.example"),   // point at a mock server in tests
    tmdb.WithRetryConfig(tmdb.RetryConfig{     // tune the retry policy
        MaxRetries: 5,
        MinBackoff: 250 * time.Millisecond,
        MaxBackoff: 5 * time.Second,
    }),
)
```

To disable retries entirely:

```go
tmdb.WithRetryConfig(tmdb.RetryConfig{})  // MaxRetries: 0 → no retry
```

## Error handling

A successful call returns `(resp, nil)` even when TMDB itself responded
with a non-2xx status. Use `tmdb.AsAPIError` to convert those into a
typed `*tmdb.APIError`:

```go
resp, err := client.Movie.DetailsWithResponse(ctx, 999_999, nil)
if err != nil {
    // network / transport failure
    return err
}
if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
    // TMDB returned >= 400. apiErr.StatusCode / StatusMessage are
    // decoded from the standard envelope; HTTPStatusCode is always set.
    return apiErr
}
// resp.JSON200 is safe to use
```

`errors.As(err, &apiErr)` works as you'd expect when the error is wrapped
further up the stack, or use the `tmdb.IsAPIError(err)` shortcut.

## Examples

Runnable examples live in [`examples/`](./examples/):

| Example | What it shows |
| --- | --- |
| [`movie-by-id`](./examples/movie-by-id/) | Fetch a single movie's details by ID. Smallest possible call. |
| [`search-movie`](./examples/search-movie/) | Search movies by title, paginate / inspect rankings. |
| [`movies-by-month`](./examples/movies-by-month/) | `client.Discover.MovieWithResponse` with `primary_release_date` filters across multiple pages. |

```sh
export TMDB_TOKEN=<your token>
go run ./examples/movie-by-id -id 27205
go run ./examples/search-movie -q "blade runner"
go run ./examples/movies-by-month -month 2024-12
```

## Regenerating the client

`task generate` invokes [`cmd/codegen`](./cmd/codegen/), which:

1. Reads [`tmdb-api.json`](./tmdb-api.json) (the upstream spec — stays
   untouched).
2. Derives a tag for each operation from its URL path prefix
   (`/3/movie/...` → `movie`), writing the tagged copy to
   `.codegen/tmdb-api.tagged.json`.
3. Strips the `"<tag>-"` prefix from each operationId so the generated
   methods don't repeat the tag (e.g. `movie-details` becomes `Details`).
   Within-tag collisions are detected; if two ops would end up with the
   same name, both keep their original IDs and a warning is logged.
4. Invokes `oapi-codegen` once per tag, emitting `<tag>/<tag>.gen.go`.
5. Re-emits [`tmdb.gen.go`](./tmdb.gen.go) — the aggregator `Client`
   struct and its `fillSubClients` method. Adding a new sub-package never
   requires touching hand-written code.

The generator is pinned via the `tool` directive in [`go.mod`](./go.mod),
so contributors do not need to install `oapi-codegen` separately.

A weekly GitHub Action ([.github/workflows/spec-sync.yml](.github/workflows/spec-sync.yml))
fetches the upstream spec, runs the pipeline, and opens a PR if anything
changed — so updates land via review rather than silent drift.

### Known limitation: OpenAPI 3.1

The TMDB spec is OpenAPI 3.1, which `oapi-codegen` does not yet fully
support — see [oapi-codegen#373](https://github.com/oapi-codegen/oapi-codegen/issues/373).
Generation succeeds but emits a warning, and some response schemas are
flatter than they would be under a richer 3.0 definition.

## Development

Tasks are managed with [Task](https://taskfile.dev) (`brew install go-task`).

```sh
task              # list all tasks
task generate     # regenerate the client from tmdb-api.json
task build        # go build ./...
task test         # go test ./...
task tidy         # go mod tidy
task check        # CI gate: regenerate → drift check → vet → build → test
task spec:download # pull the latest upstream spec into tmdb-api.json
```

## Contributing & releases

- [CONTRIBUTING.md](./CONTRIBUTING.md) explains the codegen pipeline, how
  to add an example, and how to react to upstream spec changes.
- [CHANGELOG.md](./CHANGELOG.md) tracks notable changes per release.
- The project follows [Semantic Versioning](https://semver.org); until the
  API is declared stable, it ships under `v0.x`.
