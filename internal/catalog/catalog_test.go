package catalog_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func TestValidateRequiresTitleForEveryConfiguredLanguage(t *testing.T) {
	raw := `{
		"kind": "positions",
		"version": 1,
		"items": [
			{"id": "519", "text": {"uk": {"title": "Одкровення"}}}
		]
	}`

	c, err := catalog.Load(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	err = c.Validate([]string{"uk", "en"})
	if err == nil {
		t.Fatal("Validate succeeded, expected missing locale error")
	}
	if !strings.Contains(err.Error(), "519") || !strings.Contains(err.Error(), "en") {
		t.Fatalf("Validate error %q does not mention the item and the missing locale", err)
	}
}

func TestValidateRejectsDuplicateAndEmptyIDs(t *testing.T) {
	dup := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{ID: "1", Text: map[string]catalog.ItemText{"uk": {Title: "а"}}},
			{ID: "1", Text: map[string]catalog.ItemText{"uk": {Title: "б"}}},
		},
	}
	if err := dup.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Validate on duplicate id returned %v, want duplication error", err)
	}

	empty := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items:   []catalog.Item{{ID: "  ", Text: map[string]catalog.ItemText{"uk": {Title: "а"}}}},
	}
	if err := empty.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("Validate on empty id returned %v, want empty id error", err)
	}
}

func TestValidateRejectsMissingKindAndVersion(t *testing.T) {
	noKind := catalog.Catalog{Version: 1}
	if err := noKind.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("Validate without kind returned %v, want kind error", err)
	}

	noVersion := catalog.Catalog{Kind: "positions"}
	if err := noVersion.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Validate without version returned %v, want version error", err)
	}
}

func TestItemLookup(t *testing.T) {
	c := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{ID: "519", Text: map[string]catalog.ItemText{"uk": {Title: "Одкровення"}}},
		},
	}

	item, ok := c.Item("519")
	if !ok || item.Text["uk"].Title != "Одкровення" {
		t.Fatalf("Item(519) = %+v, %v; want the stored item", item, ok)
	}
	if _, ok := c.Item("missing"); ok {
		t.Fatal("Item(missing) reported ok, want not found")
	}
}
