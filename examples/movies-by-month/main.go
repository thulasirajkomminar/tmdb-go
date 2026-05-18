// Lists movies released in a given month via TMDB's /discover/movie endpoint.
//
// Usage:
//
//	export TMDB_TOKEN=<your v4 read access token>
//	go run ./examples/movies-by-month                # current month
//	go run ./examples/movies-by-month -month 2024-12 # December 2024
//	go run ./examples/movies-by-month -month 2024-12 -region US -pages 2
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/thulasirajkomminar/tmdb-go"
	"github.com/thulasirajkomminar/tmdb-go/discover"
)

func main() {
	month := flag.String("month", time.Now().UTC().Format("2006-01"), "month to list, formatted YYYY-MM")
	region := flag.String("region", "", "ISO-3166-1 region code (optional, e.g. US, GB)")
	pages := flag.Int("pages", 1, "number of result pages to fetch (50 max per TMDB)")
	flag.Parse()

	token := os.Getenv("TMDB_TOKEN")
	if token == "" {
		log.Fatal("TMDB_TOKEN environment variable is required")
	}

	start, end, err := monthBounds(*month)
	if err != nil {
		log.Fatalf("invalid -month: %v", err)
	}

	client, err := tmdb.New(token)
	if err != nil {
		log.Fatalf("tmdb client: %v", err)
	}

	sortBy := discover.MovieParamsSortByPopularityDesc
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Movies released between %s and %s\n\n", start, end)

	var shown, total int
	for page := 1; page <= *pages; page++ {
		p := int32(page)
		params := &discover.MovieParams{
			PrimaryReleaseDateGte: &tmdb.Date{Time: start},
			PrimaryReleaseDateLte: &tmdb.Date{Time: end},
			SortBy:                &sortBy,
			Page:                  &p,
		}
		if *region != "" {
			params.Region = region
		}

		resp, err := client.Discover.MovieWithResponse(ctx, params)
		if err != nil {
			log.Fatalf("page %d: %v", page, err)
		}
		if resp.JSON200 == nil {
			log.Fatalf("page %d: tmdb returned %s\n%s", page, resp.Status(), string(resp.Body))
		}

		total = deref(resp.JSON200.TotalResults)
		for _, m := range deref(resp.JSON200.Results) {
			fmt.Printf("%-10s  %-5.1f  (%d votes)  %s\n",
				deref(m.ReleaseDate),
				deref(m.VoteAverage),
				deref(m.VoteCount),
				deref(m.Title),
			)
			shown++
		}

		if page >= deref(resp.JSON200.TotalPages) {
			break
		}
	}

	fmt.Printf("\nShown %d of %d results\n", shown, total)
}

func monthBounds(month string) (time.Time, time.Time, error) {
	first, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return first, first.AddDate(0, 1, -1), nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
