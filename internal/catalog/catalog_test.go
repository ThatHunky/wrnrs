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

// TestItemAndFilteredAliasCatalogOwnedData pins the documented contract on
// Catalog, Item and Filtered: the returned Item is a shallow copy whose
// Facets map, Tags slice, Text map and Media pointer still alias
// catalog-owned data. This is deliberate (a deep copy is too costly at
// hundreds of items per draw), but it means callers must treat every
// returned Item as read-only. This test exists so a future change to that
// contract — in either direction — has to touch this test on purpose
// instead of drifting silently.
func TestItemAndFilteredAliasCatalogOwnedData(t *testing.T) {
	media := catalog.MediaRef{Key: "positions/1.jpg"}
	c := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "1",
				Facets: map[string][]string{"level": {"easy"}},
				Tags:   []string{"starter_100"},
				Text:   map[string]catalog.ItemText{"uk": {Title: "перша"}},
				Media:  &media,
			},
		},
	}

	item, ok := c.Item("1")
	if !ok {
		t.Fatal("Item(1) not found")
	}

	// Mutate every reference-typed field through the value Item() handed
	// back.
	item.Facets["level"][0] = "mutated-facet"
	item.Tags[0] = "mutated-tag"
	item.Text["uk"] = catalog.ItemText{Title: "mutated-title"}
	item.Media.Key = "mutated-key"

	// A fresh Item() lookup must see every mutation: the fields alias the
	// catalog's own data rather than being copied.
	again, _ := c.Item("1")
	if again.Facets["level"][0] != "mutated-facet" {
		t.Fatalf("Facets = %v, want the mutation visible through aliasing", again.Facets)
	}
	if again.Tags[0] != "mutated-tag" {
		t.Fatalf("Tags = %v, want the mutation visible through aliasing", again.Tags)
	}
	if again.Text["uk"].Title != "mutated-title" {
		t.Fatalf("Text[uk] = %+v, want the mutation visible through aliasing", again.Text["uk"])
	}
	if again.Media.Key != "mutated-key" {
		t.Fatalf("Media.Key = %q, want the mutation visible through aliasing", again.Media.Key)
	}

	// Filtered must alias the same underlying data, not a copy of its own.
	filtered := c.Filtered(catalog.Filter{})
	if len(filtered) != 1 {
		t.Fatalf("Filtered = %d items, want 1", len(filtered))
	}
	if filtered[0].Tags[0] != "mutated-tag" {
		t.Fatalf("Filtered()[0].Tags = %v, want the same mutation visible through Filtered", filtered[0].Tags)
	}
}

func testFilterCatalog() catalog.Catalog {
	return catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "3",
				Facets: map[string][]string{"level": {"easy"}, "location": {"shower"}},
				Tags:   []string{"starter_100"},
				Text:   map[string]catalog.ItemText{"uk": {Title: "третя"}},
			},
			{
				ID:     "1",
				Facets: map[string][]string{"level": {"easy"}, "location": {"bed"}},
				Tags:   []string{"starter_100", "favourite"},
				Text:   map[string]catalog.ItemText{"uk": {Title: "перша"}},
			},
			{
				ID:     "2",
				Facets: map[string][]string{"level": {"hard"}, "location": {"bed", "sofa"}},
				Text:   map[string]catalog.ItemText{"uk": {Title: "друга"}},
			},
		},
	}
}

func filteredIDs(items []catalog.Item) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return strings.Join(ids, ",")
}

func TestFilteredWithoutCriteriaReturnsEverythingSortedByID(t *testing.T) {
	c := testFilterCatalog()
	if got := filteredIDs(c.Filtered(catalog.Filter{})); got != "1,2,3" {
		t.Fatalf("Filtered(empty) = %q, want 1,2,3", got)
	}
}

func TestFilteredOrsWithinFacetAndAndsAcrossFacets(t *testing.T) {
	c := testFilterCatalog()

	orWithin := c.Filtered(catalog.Filter{Include: map[string][]string{"location": {"sofa", "shower"}}})
	if got := filteredIDs(orWithin); got != "2,3" {
		t.Fatalf("Filtered(location in sofa|shower) = %q, want 2,3", got)
	}

	andAcross := c.Filtered(catalog.Filter{Include: map[string][]string{
		"level":    {"easy"},
		"location": {"bed"},
	}})
	if got := filteredIDs(andAcross); got != "1" {
		t.Fatalf("Filtered(level=easy AND location=bed) = %q, want 1", got)
	}
}

func TestFilteredExcludeAndTags(t *testing.T) {
	c := testFilterCatalog()

	excluded := c.Filtered(catalog.Filter{Exclude: map[string][]string{"level": {"hard"}}})
	if got := filteredIDs(excluded); got != "1,3" {
		t.Fatalf("Filtered(exclude level=hard) = %q, want 1,3", got)
	}

	tagged := c.Filtered(catalog.Filter{Tags: []string{"starter_100"}})
	if got := filteredIDs(tagged); got != "1,3" {
		t.Fatalf("Filtered(tag starter_100) = %q, want 1,3", got)
	}
}

func TestFilteredEmptyIncludeListIsIgnored(t *testing.T) {
	c := testFilterCatalog()
	got := c.Filtered(catalog.Filter{Include: map[string][]string{"level": {}}})
	if filteredIDs(got) != "1,2,3" {
		t.Fatalf("Filtered(level in {}) = %q, want all items", filteredIDs(got))
	}
}

func TestLoadDecodesFacetsTagsAndMedia(t *testing.T) {
	raw := `{
		"kind": "positions",
		"version": 1,
		"items": [
			{
				"id": "42",
				"facets": {"level": ["easy", "medium"]},
				"tags": ["starter_100"],
				"text": {"uk": {"title": "назва"}},
				"media": {"key": "positions/42.jpg", "width": 800, "height": 600}
			}
		]
	}`

	c, err := catalog.Load(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	item, ok := c.Item("42")
	if !ok {
		t.Fatal("Item(42) not found")
	}
	if got := item.Facets["level"]; len(got) != 2 || got[0] != "easy" || got[1] != "medium" {
		t.Fatalf("Facets[level] = %v, want [easy medium]", got)
	}
	if len(item.Tags) != 1 || item.Tags[0] != "starter_100" {
		t.Fatalf("Tags = %v, want [starter_100]", item.Tags)
	}
	if item.Media == nil || item.Media.Key != "positions/42.jpg" {
		t.Fatalf("Media = %+v, want key positions/42.jpg", item.Media)
	}
}

func TestFilteredTagsRequireEveryListedTag(t *testing.T) {
	c := testFilterCatalog()

	got := c.Filtered(catalog.Filter{Tags: []string{"starter_100", "favourite"}})
	if want := "1"; filteredIDs(got) != want {
		t.Fatalf("Filtered(tags starter_100+favourite) = %q, want %q", filteredIDs(got), want)
	}
}

func TestSelectNextAvoidsSeenItemsUntilExhaustedThenStartsNextCycle(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	first, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 42, Bucket: "positions", Cycle: 0, Items: items,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if cycle != 0 || exhausted {
		t.Fatalf("first draw cycle=%d exhausted=%v, want 0 and false", cycle, exhausted)
	}

	seen := map[string]bool{"a": true, "b": true, "c": true}
	next, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 42, Bucket: "positions", Cycle: 0, Items: items, Seen: seen,
	})
	if err != nil {
		t.Fatalf("SelectNext after exhaustion returned error: %v", err)
	}
	if cycle != 1 || !exhausted {
		t.Fatalf("exhausted draw cycle=%d exhausted=%v, want 1 and true", cycle, exhausted)
	}
	if next.ID == "" {
		t.Fatal("exhausted draw returned an empty item")
	}
	_ = first
}

// TestSelectNextExhaustedRefillReusesTheDeterministicShuffle pins the
// exhausted-refill branch to the same shuffle a fresh draw would use, not
// to the raw item order. Without this, an implementation that refills with
// `in.Items[0]` unshuffled on exhaustion passes every other SelectNext test
// — but a pair who has seen everything would then get the same item first,
// forever, instead of a reshuffled deck.
func TestSelectNextExhaustedRefillReusesTheDeterministicShuffle(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}

	seen := map[string]bool{"a": true, "b": true, "c": true, "d": true}
	exhausted, cycle, wasExhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 1, Bucket: "positions", Cycle: 0, Items: items, Seen: seen,
	})
	if err != nil {
		t.Fatalf("SelectNext (exhausted) returned error: %v", err)
	}
	if cycle != 1 || !wasExhausted {
		t.Fatalf("exhausted draw cycle=%d exhausted=%v, want 1 and true", cycle, wasExhausted)
	}

	fresh, freshCycle, freshExhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 1, Bucket: "positions", Cycle: 1, Items: items,
	})
	if err != nil {
		t.Fatalf("SelectNext (fresh cycle 1) returned error: %v", err)
	}
	if freshCycle != 1 || freshExhausted {
		t.Fatalf("fresh cycle-1 draw cycle=%d exhausted=%v, want 1 and false", freshCycle, freshExhausted)
	}

	if exhausted.ID != fresh.ID {
		t.Fatalf("exhausted refill picked %q but a fresh Cycle:1 draw with the same SeedID and Bucket picks %q; "+
			"the refill must reuse the same deterministic shuffle seed, not the raw item order", exhausted.ID, fresh.ID)
	}
}

func TestSelectNextSkipsSeenItems(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	seen := map[string]bool{"a": true, "b": true}

	got, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 7, Bucket: "positions", Cycle: 0, Items: items, Seen: seen,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if got.ID != "c" || cycle != 0 || exhausted {
		t.Fatalf("SelectNext = %s cycle=%d exhausted=%v, want c 0 false", got.ID, cycle, exhausted)
	}
}

func TestSelectNextIsDeterministicPerSeedAndBucket(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	in := catalog.SelectionInput{SeedID: 99, Bucket: "positions", Cycle: 3, Items: items}

	first, _, _, err := catalog.SelectNext(in)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	again, _, _, err := catalog.SelectNext(in)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if first.ID != again.ID {
		t.Fatalf("same input gave %s then %s, want a stable pick", first.ID, again.ID)
	}

	otherBucket := in
	otherBucket.Bucket = "dares"
	changed, _, _, err := catalog.SelectNext(otherBucket)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	otherSeed := in
	otherSeed.SeedID = 100
	changedSeed, _, _, err := catalog.SelectNext(otherSeed)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if changed.ID == first.ID {
		t.Fatal("changing only Bucket left the pick identical; Bucket is not being mixed into the shuffle seed")
	}
	if changedSeed.ID == first.ID {
		t.Fatal("changing only SeedID left the pick identical; SeedID is not being mixed into the shuffle seed")
	}
}

func TestSelectNextRejectsEmptyInput(t *testing.T) {
	_, _, _, err := catalog.SelectNext(catalog.SelectionInput{SeedID: 1, Bucket: "positions"})
	if err == nil {
		t.Fatal("SelectNext on empty items succeeded, want an error")
	}
}

func TestSelectNextCycleAffectsTheShuffle(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}

	cycleZero, _, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 99, Bucket: "positions", Cycle: 0, Items: items,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	cycleOne, _, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 99, Bucket: "positions", Cycle: 1, Items: items,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if cycleZero.ID == cycleOne.ID {
		t.Fatal("changing only Cycle left the pick identical; Cycle is not being mixed into the shuffle seed")
	}
}
