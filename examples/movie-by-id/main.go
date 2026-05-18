// Fetches a movie by its TMDB ID and prints the highlights.
//
// Usage:
//
//	export TMDB_TOKEN=<your v4 read access token>
//	go run ./examples/movie-by-id           # defaults to Fight Club (550)
//	go run ./examples/movie-by-id -id 27205 # Inception
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/thulasirajkomminar/tmdb-go"
)

func main() {
	id := flag.Int("id", 550, "TMDB movie ID")
	flag.Parse()

	token := os.Getenv("TMDB_TOKEN")
	if token == "" {
		log.Fatal("TMDB_TOKEN environment variable is required")
	}

	client, err := tmdb.New(token)
	if err != nil {
		log.Fatalf("tmdb client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Movie.DetailsWithResponse(ctx, int32(*id), nil)
	if err != nil {
		log.Fatalf("movie-details: %v", err)
	}
	if apiErr := tmdb.AsAPIError(resp, resp.Body); apiErr != nil {
		log.Fatal(apiErr)
	}
	m := resp.JSON200
	if m == nil {
		log.Fatalf("unexpected empty body: %s", resp.Status())
	}

	fmt.Printf("Title:    %s\n", deref(m.Title))
	fmt.Printf("Tagline:  %s\n", deref(m.Tagline))
	fmt.Printf("Year:     %s\n", deref(m.ReleaseDate))
	fmt.Printf("Runtime:  %d min\n", deref(m.Runtime))
	fmt.Printf("Rating:   %.1f (%d votes)\n", deref(m.VoteAverage), deref(m.VoteCount))
	fmt.Printf("Overview: %s\n", deref(m.Overview))
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
