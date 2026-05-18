// Command codegen regenerates the TMDB Go SDK from the upstream OpenAPI
// spec. It is the single entry point used by `task generate` and by the
// weekly spec-sync GitHub Action.
//
// The pipeline is:
//
//  1. Read tmdb-api.json (committed upstream copy).
//  2. Inject `tags` onto each operation, derived from the operation's URL
//     path prefix (/3/<segment>/...). The committed spec stays untouched;
//     the tagged copy is written to .codegen/tmdb-api.tagged.json.
//  3. For each operationId that starts with "<tag>-", strip that prefix
//     so generated method names are not redundantly prefixed (e.g.
//     `movie-details` -> `details` -> client.Movie.DetailsWithResponse).
//     Collisions are detected; offending ops keep their original names
//     and a warning is logged.
//  4. Invoke `go tool oapi-codegen` once per tag, filtering operations
//     and emitting <tag>/<tag>.gen.go.
//  5. Re-emit tmdb.gen.go (the aggregator Client + fillSubClients).
//
// Run via `task generate` or directly: `go run ./cmd/codegen`.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

const (
	specPath       = "tmdb-api.json"
	taggedDir      = ".codegen"
	taggedSpecPath = ".codegen/tmdb-api.tagged.json"
	modulePath     = "github.com/thulasirajkomminar/tmdb-go"
	facadePath     = "tmdb.gen.go"
)

// tagRenames maps a raw path-segment to a Go-friendly package name.
// Add entries here when TMDB ships a path segment that isn't a valid Go
// package name (only ASCII letters, no underscores).
var tagRenames = map[string]string{
	"guest_session": "guest",
}

type stripStats struct {
	stripped   int // operationId had "<tag>-" prefix; strip applied
	unchanged  int // operationId didn't start with "<tag>-"; left alone
	collisions int // strip would have caused a name clash; left as-is + warning logged
}

// tagCollision describes a group of operations within a tag that would
// share the same name after prefix-stripping, blocking the strip.
type tagCollision struct {
	Tag         string
	FinalName   string
	OriginalIDs []string
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("codegen: ")

	spec, err := readSpec()
	if err != nil {
		log.Fatalf("read spec: %v", err)
	}

	tags := tagOperations(spec)
	if len(tags) == 0 {
		log.Fatal("no tags derived from spec — refuse to wipe sub-packages")
	}

	stats, collisions := stripOperationIDPrefixes(spec, tags)
	for _, c := range collisions {
		log.Printf("warn: tag %q: %d operations collide on %q after prefix-strip — keeping originals %v",
			c.Tag, len(c.OriginalIDs), c.FinalName, c.OriginalIDs)
	}

	if err := writeTaggedSpec(spec); err != nil {
		log.Fatalf("write tagged spec: %v", err)
	}
	if err := generateSubPackages(tags); err != nil {
		log.Fatalf("per-tag generation: %v", err)
	}
	if err := writeFacade(tags); err != nil {
		log.Fatalf("facade: %v", err)
	}

	fmt.Printf("codegen: regenerated %d sub-packages and %s (%d ops stripped, %d collisions, %d unchanged)\n",
		len(tags), facadePath, stats.stripped, stats.collisions, stats.unchanged)
}

func readSpec() (map[string]any, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", specPath, err)
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}
	return spec, nil
}

// tagOperations injects a tag onto every operation. The tag is derived
// from the second path segment of the operation's URL (with optional
// renames from tagRenames). Returns the sorted unique tag list.
func tagOperations(spec map[string]any) []string {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return nil
	}
	tagSet := map[string]struct{}{}
	for pathKey, pathVal := range paths {
		pathItem, ok := pathVal.(map[string]any)
		if !ok {
			continue
		}
		tag := tagFromPath(pathKey)
		if tag == "" {
			continue
		}
		for _, methodVal := range pathItem {
			op, ok := methodVal.(map[string]any)
			if !ok {
				continue
			}
			if _, hasOpID := op["operationId"]; !hasOpID {
				continue
			}
			op["tags"] = []any{tag}
			tagSet[tag] = struct{}{}
		}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// tagFromPath derives a tag from a TMDB path like `/3/movie/{movie_id}`.
// Returns "" if the path can't be parsed.
func tagFromPath(p string) string {
	rest := strings.TrimPrefix(p, "/3/")
	if rest == p {
		return ""
	}
	seg, _, _ := strings.Cut(rest, "/")
	if seg == "" {
		return ""
	}
	if renamed, ok := tagRenames[seg]; ok {
		return renamed
	}
	return seg
}

// stripOperationIDPrefixes removes the leading "<tag>-" from each
// operationId in the given tag. Operations whose ID doesn't start with
// the tag prefix are left alone. Within a single tag, if two operations
// would end up with the same stripped name (a collision), neither is
// stripped and the conflict is returned for the caller to surface.
func stripOperationIDPrefixes(spec map[string]any, tags []string) (stripStats, []tagCollision) {
	paths, _ := spec["paths"].(map[string]any)
	if paths == nil {
		return stripStats{}, nil
	}

	type opRef struct {
		op       map[string]any
		origID   string
		proposed string // empty when not eligible for strip
	}
	perTag := map[string][]*opRef{}

	for pathKey, pathVal := range paths {
		tag := tagFromPath(pathKey)
		if tag == "" {
			continue
		}
		pathItem, ok := pathVal.(map[string]any)
		if !ok {
			continue
		}
		for _, methodVal := range pathItem {
			op, ok := methodVal.(map[string]any)
			if !ok {
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" {
				continue
			}
			ref := &opRef{op: op, origID: id}
			prefix := tag + "-"
			if strings.HasPrefix(id, prefix) {
				ref.proposed = strings.TrimPrefix(id, prefix)
			}
			perTag[tag] = append(perTag[tag], ref)
		}
	}

	var (
		stats      stripStats
		collisions []tagCollision
	)

	// Per tag, group by the final name each op would carry, and look for
	// duplicates. A duplicate means we cannot strip safely.
	for _, tag := range tags {
		ops := perTag[tag]
		if len(ops) == 0 {
			continue
		}
		groups := map[string][]*opRef{}
		for _, ref := range ops {
			finalName := ref.origID
			if ref.proposed != "" {
				finalName = ref.proposed
			}
			groups[finalName] = append(groups[finalName], ref)
		}

		// Deterministic ordering so callers (and tests) see collisions
		// in a stable sequence.
		finalNames := make([]string, 0, len(groups))
		for n := range groups {
			finalNames = append(finalNames, n)
		}
		sort.Strings(finalNames)

		for _, finalName := range finalNames {
			group := groups[finalName]
			if len(group) <= 1 {
				continue
			}
			origs := make([]string, len(group))
			for i, ref := range group {
				origs[i] = ref.origID
			}
			sort.Strings(origs)
			collisions = append(collisions, tagCollision{
				Tag:         tag,
				FinalName:   finalName,
				OriginalIDs: origs,
			})
			for _, ref := range group {
				if ref.proposed != "" {
					ref.proposed = ""
					stats.collisions++
				}
			}
		}

		for _, ref := range ops {
			if ref.proposed != "" {
				ref.op["operationId"] = ref.proposed
				stats.stripped++
			} else if !strings.HasPrefix(ref.origID, tag+"-") {
				stats.unchanged++
			}
			// ops where proposed was reset due to collision are already
			// counted under stats.collisions.
		}
	}

	return stats, collisions
}

func writeTaggedSpec(spec map[string]any) error {
	if err := os.MkdirAll(taggedDir, 0o755); err != nil {
		return err
	}
	out, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(taggedSpecPath, out, 0o644)
}

func generateSubPackages(tags []string) error {
	for _, tag := range tags {
		if err := os.MkdirAll(tag, 0o755); err != nil {
			return err
		}
		outFile := filepath.Join(tag, tag+".gen.go")
		cmd := exec.Command("go", "tool", "oapi-codegen",
			"-package", tag,
			"-include-tags", tag,
			"-generate", "types,client",
			"-o", outFile,
			taggedSpecPath,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("tag %q: %w", tag, err)
		}
	}
	return nil
}

func writeFacade(tags []string) error {
	out, err := renderFacade(tags)
	if err != nil {
		return err
	}
	return os.WriteFile(facadePath, out, 0o644)
}

// renderFacade produces the gofmt-formatted source for tmdb.gen.go for
// the given sub-package tags. It returns an error if the template or the
// formatter fail.
func renderFacade(tags []string) ([]byte, error) {
	var buf bytes.Buffer
	if err := facadeTmpl.Execute(&buf, struct {
		Module string
		Tags   []string
	}{Module: modulePath, Tags: tags}); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("gofmt: %w", err)
	}
	return out, nil
}

var facadeTmpl = template.Must(template.New("facade").Funcs(template.FuncMap{
	"title": func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
}).Parse(`// Code generated by cmd/codegen; DO NOT EDIT.

package tmdb

import (
{{- range .Tags }}
	"{{ $.Module }}/{{ . }}"
{{- end }}
)

// Client aggregates one generated sub-client per TMDB resource family.
// Each field is a ClientWithResponses, exposing both the raw ` + "`Foo`" + ` methods
// (returning *http.Response) and the typed ` + "`FooWithResponse`" + ` methods
// (returning *FooResponse with a parsed JSON200 payload).
type Client struct {
{{- range .Tags }}
	{{ title . }} *{{ . }}.ClientWithResponses
{{- end }}
}

// fillSubClients populates each sub-client field with the shared config.
func (c *Client) fillSubClients(server string, doer HTTPClient, eds []RequestEditor) {
{{- range .Tags }}
	c.{{ title . }} = &{{ . }}.ClientWithResponses{ClientInterface: &{{ . }}.Client{
		Server:         server,
		Client:         doer,
		RequestEditors: editors[{{ . }}.RequestEditorFn](eds),
	}}
{{- end }}
}
`))
