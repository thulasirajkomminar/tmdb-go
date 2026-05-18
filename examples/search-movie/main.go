// Searches movies by title and prints the top matches.
//
// Usage:
//
//	export TMDB_TOKEN=<your v4 read access token>
//	go run ./examples/search-movie -q "the matrix"
//	go run ./examples/search-movie -q "blade runner" -year 1982 -limit 3
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/thulasirajkomminar/tmdb-go"
	"github.com/thulasirajkomminar/tmdb-go/search"
)

func main() {
	query := flag.String("q", "", "title to search for (required)")
	year := flag.String("year", "", "filter by release year (optional)")
	limit := flag.Int("limit", 5, "max results to print")
	flag.Parse()

	if *query == "" {
		flag.Usage()
		os.Exit(2)
	}

	token := os.Getenv("TMDB_TOKEN")
	if token == "" {
		log.Fatal("TMDB_TOKEN environment variable is required")
	}

	client, err := tmdb.New(token)
	if err != nil {
		log.Fatalf("tmdb client: %v", err)
	}

	params := &search.MovieParams{Query: *query}
	if *year != "" {
		params.Year = year
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Search.MovieWithResponse(ctx, params)
	if err != nil {
		log.Fatalf("search-movie: %v", err)
	}
	if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
		log.Fatal(apiErr)
	}
	if resp.JSON200 == nil {
		log.Fatalf("unexpected empty body: %s", resp.Status())
	}

	results := deref(resp.JSON200.Results)
	if len(results) == 0 {
		fmt.Println("No matches.")
		return
	}

	fmt.Printf("Top %d of %d matches for %q:\n\n", min(*limit, len(results)), deref(resp.JSON200.TotalResults), *query)
	for i, m := range results {
		if i >= *limit {
			break
		}
		fmt.Printf("%d. %s (%s)  rating %.1f  id=%d\n",
			i+1,
			deref(m.Title),
			deref(m.ReleaseDate),
			deref(m.VoteAverage),
			deref(m.Id),
		)
	}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
