package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ItemText is the localized payload of a catalog item.
type ItemText struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// MediaRef points at an object in the object store rather than embedding bytes.
type MediaRef struct {
	Key    string `json:"key"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Item is one selectable entry: a position, a dare, a date idea.
type Item struct {
	ID     string              `json:"id"`
	Facets map[string][]string `json:"facets,omitempty"`
	Tags   []string            `json:"tags,omitempty"`
	Text   map[string]ItemText `json:"text"`
	Media  *MediaRef           `json:"media,omitempty"`
}

// Catalog is one content collection loaded at boot. Item and Filtered do not
// deep-copy: the returned Item's Facets map, Tags slice, Text map and Media
// pointer alias catalog-owned data, so callers must treat every returned
// Item as read-only. Filtered orders results lexicographically by ID, so
// numeric ids MUST be zero-padded to a fixed width (e.g. "001".."519") —
// otherwise they page out of numeric order.
type Catalog struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

func Load(r io.Reader) (*Catalog, error) {
	var c Catalog
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return &c, nil
}

func (c *Catalog) Validate(languages []string) error {
	if strings.TrimSpace(c.Kind) == "" {
		return errors.New("catalog kind must not be empty")
	}
	if c.Version <= 0 {
		return errors.New("catalog version must be positive")
	}
	seen := make(map[string]bool, len(c.Items))
	for i, item := range c.Items {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("item at index %d has empty id", i)
		}
		if seen[item.ID] {
			return fmt.Errorf("item %s is duplicated", item.ID)
		}
		seen[item.ID] = true
		for _, lang := range languages {
			if strings.TrimSpace(item.Text[lang].Title) == "" {
				return fmt.Errorf("item %s missing %s title", item.ID, lang)
			}
		}
	}
	return nil
}

// Item returns the item with the given id. The returned Item aliases the
// catalog's own Facets map, Tags slice, Text map and Media pointer — it is
// not a deep copy. Treat it as read-only: mutating any of those fields
// corrupts the catalog for every other reader for the process lifetime.
func (c *Catalog) Item(id string) (Item, bool) {
	for _, item := range c.Items {
		if item.ID == id {
			return item, true
		}
	}
	return Item{}, false
}
