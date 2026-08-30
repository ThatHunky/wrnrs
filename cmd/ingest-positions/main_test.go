package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
)

const testTaxonomy = `{
	"version": 1,
	"slugs": {
		"medium-level": {"facet": "level", "value": "medium"},
		"sofa": {"facet": "location", "value": "sofa"}
	}
}`

func TestBuildItemMapsPageOntoCatalogItem(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	page := positions.ParsedPage{
		Number:      519,
		Name:        "Revelation",
		Description: "Sex position #519 - Revelation (on the couch).",
		ImageURL:    "https://example.test/uploads/18_55.png",
		TagSlugs:    []string{"medium-level", "sofa"},
	}

	item, unknown, err := buildItem(page, tax)
	if err != nil {
		t.Fatalf("buildItem: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
	if item.ID != "519" {
		t.Fatalf("ID = %q, want 519", item.ID)
	}
	if item.Text["en"].Title != "Revelation" {
		t.Fatalf("en title = %q, want Revelation", item.Text["en"].Title)
	}
	if item.Text["en"].Body != page.Description {
		t.Fatalf("en body = %q, want the description", item.Text["en"].Body)
	}
	if item.Media == nil || item.Media.Key != "positions/519.png" {
		t.Fatalf("Media = %+v, want key positions/519.png", item.Media)
	}
	if strings.Join(item.Facets["location"], ",") != "sofa" {
		t.Fatalf("location facet = %v, want sofa", item.Facets["location"])
	}
}

func TestBuildItemReportsUnknownSlugsInsteadOfDroppingThem(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	page := positions.ParsedPage{
		Number:   3,
		Name:     "Test",
		ImageURL: "https://example.test/a.png",
		TagSlugs: []string{"sofa", "totally-new-tag"},
	}

	_, unknown, err := buildItem(page, tax)
	if err != nil {
		t.Fatalf("buildItem: %v", err)
	}
	if strings.Join(unknown, ",") != "totally-new-tag" {
		t.Fatalf("unknown = %v, want [totally-new-tag]", unknown)
	}
}

func TestObjectKeyUsesTheSourceExtension(t *testing.T) {
	if got := objectKey(519, "https://example.test/uploads/18_55.png"); got != "positions/519.png" {
		t.Fatalf("objectKey = %q, want positions/519.png", got)
	}
	if got := objectKey(7, "https://example.test/uploads/a.jpg?v=2"); got != "positions/007.jpg" {
		t.Fatalf("objectKey with query = %q, want positions/007.jpg (zero-padded)", got)
	}
	if got := objectKey(9, "https://example.test/uploads/noext"); got != "positions/009.png" {
		t.Fatalf("objectKey without extension = %q, want the zero-padded png default", got)
	}
}

func TestProcessPageSkipsPagesWithUnknownSlugsWithoutAddingOrMarkingDone(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	resumePath := filepath.Join(dir, "progress.json")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	page := positions.ParsedPage{
		Number:   3,
		Name:     "Test",
		ImageURL: "https://example.test/a.png",
		TagSlugs: []string{"sofa", "totally-new-tag"},
	}

	items := map[string]catalog.Item{}
	state := progress{Done: map[string]bool{}}

	imageFetched := false
	unknown, err := processPage(3, page, tax, func() ([]byte, int, error) {
		imageFetched = true
		return nil, http.StatusOK, nil
	}, imagesDir, items, state, resumePath, catalogPath)
	if err != nil {
		t.Fatalf("processPage: %v", err)
	}
	if imageFetched {
		t.Fatal("processPage fetched the image for a page with unknown tag slugs; it must skip before fetching")
	}
	if strings.Join(unknown, ",") != "totally-new-tag" {
		t.Fatalf("unknown = %v, want [totally-new-tag]", unknown)
	}
	if len(items) != 0 {
		t.Fatalf("items = %v, want no item added for a page with unknown slugs", items)
	}
	if state.Done["3"] {
		t.Fatal("state.Done[3] = true, want the page left undone so a later run re-fetches it")
	}
	if _, err := os.Stat(catalogPath); !os.IsNotExist(err) {
		t.Fatalf("catalog file was written for a skipped page: stat err = %v", err)
	}
}

func TestProcessPagePersistsCatalogImmediatelyAfterASuccessfulPage(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	resumePath := filepath.Join(dir, "progress.json")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	page := positions.ParsedPage{
		Number:   519,
		Name:     "Revelation",
		ImageURL: "https://example.test/uploads/18_55.png",
		TagSlugs: []string{"medium-level", "sofa"},
	}

	items := map[string]catalog.Item{}
	state := progress{Done: map[string]bool{}}

	unknown, err := processPage(519, page, tax, func() ([]byte, int, error) {
		return []byte("fake-image-bytes"), http.StatusOK, nil
	}, imagesDir, items, state, resumePath, catalogPath)
	if err != nil {
		t.Fatalf("processPage: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
	if !state.Done["519"] {
		t.Fatal("state.Done[519] = false, want the page marked done after a successful commit")
	}

	// The whole point of incremental persistence: the catalog on disk must
	// already contain this item, without main ever reaching its final,
	// end-of-run writeCatalog call.
	onDisk := loadCatalog(catalogPath)
	if _, ok := onDisk.Item("519"); !ok {
		t.Fatalf("catalog file at %s does not yet contain item 519 immediately after processPage returned; catalog = %+v", catalogPath, onDisk)
	}
}

func TestProcessPageResumedRunPreservesEarlierItems(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	resumePath := filepath.Join(dir, "progress.json")
	imagesDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Simulate the artifacts an earlier, interrupted run left behind: a
	// catalog file with one item already written, and a progress file
	// marking that same page done.
	earlier := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:   "001",
				Text: map[string]catalog.ItemText{"en": {Title: "First"}},
			},
		},
	}
	earlierData, err := json.MarshalIndent(earlier, "", "  ")
	if err != nil {
		t.Fatalf("marshal earlier catalog: %v", err)
	}
	if err := os.WriteFile(catalogPath, earlierData, 0o644); err != nil {
		t.Fatalf("write earlier catalog: %v", err)
	}
	saveProgress(resumePath, progress{Done: map[string]bool{"1": true}})

	// Now mimic what main does at startup: load the progress and the
	// existing catalog into the same items map a real run would use.
	state := loadProgress(resumePath)
	existing := loadCatalog(catalogPath)
	items := map[string]catalog.Item{}
	for _, item := range existing.Items {
		items[item.ID] = item
	}
	if _, ok := items["001"]; !ok {
		t.Fatalf("test setup broken: loadCatalog(%s) does not contain the earlier item", catalogPath)
	}

	page := positions.ParsedPage{
		Number:   519,
		Name:     "Revelation",
		ImageURL: "https://example.test/uploads/18_55.png",
		TagSlugs: []string{"medium-level", "sofa"},
	}

	unknown, err := processPage(519, page, tax, func() ([]byte, int, error) {
		return []byte("fake-image-bytes"), http.StatusOK, nil
	}, imagesDir, items, state, resumePath, catalogPath)
	if err != nil {
		t.Fatalf("processPage: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}

	onDisk := loadCatalog(catalogPath)
	if _, ok := onDisk.Item("001"); !ok {
		t.Fatalf("catalog file at %s lost the earlier item 001 after processing page 519; catalog = %+v", catalogPath, onDisk)
	}
	if _, ok := onDisk.Item("519"); !ok {
		t.Fatalf("catalog file at %s does not contain the newly processed item 519; catalog = %+v", catalogPath, onDisk)
	}
}

func TestLoadCatalogOnMissingOrMalformedFileYieldsEmptyCatalog(t *testing.T) {
	dir := t.TempDir()

	missing := loadCatalog(filepath.Join(dir, "does-not-exist.json"))
	if missing.Kind != "positions" || missing.Version != 1 || len(missing.Items) != 0 {
		t.Fatalf("loadCatalog(missing) = %+v, want an empty positions/v1 catalog", missing)
	}

	malformedPath := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformedPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write malformed catalog: %v", err)
	}
	malformed := loadCatalog(malformedPath)
	if malformed.Kind != "positions" || malformed.Version != 1 || len(malformed.Items) != 0 {
		t.Fatalf("loadCatalog(malformed) = %+v, want an empty positions/v1 catalog", malformed)
	}
}
