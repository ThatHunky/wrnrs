package positions_test

import (
	"strings"
	"testing"

	"wrnrs/internal/positions"
)

const taxonomyFixture = `{
	"version": 1,
	"slugs": {
		"easy-level": {"facet": "level", "value": "easy"},
		"medium-level": {"facet": "level", "value": "medium"},
		"bed": {"facet": "location", "value": "bed"},
		"sofa": {"facet": "location", "value": "sofa"},
		"cowgirl": {"facet": "type", "value": "cowgirl"},
		"we-support-ukraine": {"facet": "", "value": ""}
	}
}`

func TestFacetsGroupsSlugsByFacet(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, unknown := tax.Facets([]string{"medium-level", "sofa", "bed", "cowgirl"})
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
	if got := strings.Join(facets["location"], ","); got != "bed,sofa" {
		t.Fatalf("location facet = %q, want bed,sofa (sorted)", got)
	}
	if got := strings.Join(facets["level"], ","); got != "medium" {
		t.Fatalf("level facet = %q, want medium", got)
	}
	if got := strings.Join(facets["type"], ","); got != "cowgirl" {
		t.Fatalf("type facet = %q, want cowgirl", got)
	}
}

func TestFacetsReportsUnknownSlugs(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	_, unknown := tax.Facets([]string{"bed", "brand-new-tag", "another-one"})
	if strings.Join(unknown, ",") != "another-one,brand-new-tag" {
		t.Fatalf("unknown = %v, want both new slugs sorted", unknown)
	}
}

func TestFacetsDropsSlugsMappedToAnEmptyFacet(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, unknown := tax.Facets([]string{"we-support-ukraine", "bed"})
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none — the slug is known and deliberately ignored", unknown)
	}
	if len(facets) != 1 || strings.Join(facets["location"], ",") != "bed" {
		t.Fatalf("facets = %v, want only location=bed", facets)
	}
}

func TestFacetsDeduplicatesRepeatedValues(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, _ := tax.Facets([]string{"bed", "bed", "sofa"})
	if got := strings.Join(facets["location"], ","); got != "bed,sofa" {
		t.Fatalf("location facet = %q, want bed,sofa without duplicates", got)
	}
}
