# Examples

Runnable examples for the `tmdb-go` SDK. Each example is a self-contained
`main` package in its own directory.

| Example | What it shows |
| --- | --- |
| [movie-by-id](./movie-by-id/) | Fetch a single movie by TMDB ID and print its details. Smallest end-to-end call. |
| [search-movie](./search-movie/) | Search the catalogue by title, optionally filtered by year. |
| [movies-by-month](./movies-by-month/) | Discover movies released in a given calendar month using `primary_release_date` filters, sorted by popularity. Demonstrates pagination. |

## Running an example

All examples expect a TMDB v4 read-access token in `TMDB_TOKEN`:

```sh
export TMDB_TOKEN=<your token>
go run ./examples/movie-by-id
go run ./examples/search-movie -q "the matrix"
go run ./examples/movies-by-month -month 2024-12
```

## Patterns to copy

- **Typed responses.** Each example calls `*WithResponse` methods and
  reads `resp.JSON200`. The `*Foo` (non-`WithResponse`) variants return a
  raw `*http.Response` if you need to stream the body instead.
- **TMDB-level errors.** Use `tmdb.AsAPIError(resp, resp.Body)` to detect
  non-2xx responses — see the
  [error handling section in the main README](../README.md#error-handling).
- **`*T` dereferencing.** The generated structs use pointer fields
  (because every JSON property is technically optional in the spec). Each
  example carries a tiny `deref[T](*T) T` helper — copy it rather than
  importing one.
