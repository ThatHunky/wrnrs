package positions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// SlugMapping maps one source tag slug onto a catalog facet. An empty Facet
// means the slug is known and deliberately ignored (site chrome, campaigns).
type SlugMapping struct {
	Facet string `json:"facet"`
	Value string `json:"value"`
}

type Taxonomy struct {
	Version int                    `json:"version"`
	Slugs   map[string]SlugMapping `json:"slugs"`
}

func LoadTaxonomy(r io.Reader) (*Taxonomy, error) {
	var t Taxonomy
	if err := json.NewDecoder(r).Decode(&t); err != nil {
		return nil, fmt.Errorf("decode taxonomy: %w", err)
	}
	if t.Version <= 0 {
		return nil, errors.New("taxonomy version must be positive")
	}
	if len(t.Slugs) == 0 {
		return nil, errors.New("taxonomy has no slugs")
	}
	return &t, nil
}

// Facets converts source slugs into catalog facets. Unknown slugs are returned
// separately so the caller can fail loudly instead of silently losing filters.
func (t *Taxonomy) Facets(slugs []string) (map[string][]string, []string) {
	facets := map[string]map[string]bool{}
	unknownSet := map[string]bool{}

	for _, slug := range slugs {
		mapping, ok := t.Slugs[slug]
		if !ok {
			unknownSet[slug] = true
			continue
		}
		if mapping.Facet == "" {
			continue
		}
		if facets[mapping.Facet] == nil {
			facets[mapping.Facet] = map[string]bool{}
		}
		facets[mapping.Facet][mapping.Value] = true
	}

	out := make(map[string][]string, len(facets))
	for facet, values := range facets {
		list := make([]string, 0, len(values))
		for value := range values {
			list = append(list, value)
		}
		sort.Strings(list)
		out[facet] = list
	}

	unknown := make([]string, 0, len(unknownSet))
	for slug := range unknownSet {
		unknown = append(unknown, slug)
	}
	sort.Strings(unknown)

	return out, unknown
}
