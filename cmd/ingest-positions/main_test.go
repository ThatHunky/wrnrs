package main

import (
	"strings"
	"testing"

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
