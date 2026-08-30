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

func TestFacetsReturnsSortedFacetValuesAndUnknowns(t *testing.T) {
	// Fixture with 5 values in one facet, deliberately in non-sorted order
	fixture := `{
		"version": 1,
		"slugs": {
			"zebra-loc": {"facet": "location", "value": "zebra"},
			"apple-loc": {"facet": "location", "value": "apple"},
			"monkey-loc": {"facet": "location", "value": "monkey"},
			"banana-loc": {"facet": "location", "value": "banana"},
			"cherry-loc": {"facet": "location", "value": "cherry"}
		}
	}`

	tax, err := positions.LoadTaxonomy(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	// Pass slugs and unknowns in deliberately non-sorted order
	facets, unknown := tax.Facets([]string{
		"zebra-loc", "apple-loc", "monkey-loc", "banana-loc", "cherry-loc",
		"zulu", "alpha", "november", "bravo", "charlie",
	})

	// Assert facet values come back sorted
	if got := strings.Join(facets["location"], ","); got != "apple,banana,cherry,monkey,zebra" {
		t.Fatalf("location facet = %q, want apple,banana,cherry,monkey,zebra (sorted)", got)
	}

	// Assert unknown slugs come back sorted
	if got := strings.Join(unknown, ","); got != "alpha,bravo,charlie,november,zulu" {
		t.Fatalf("unknown = %q, want alpha,bravo,charlie,november,zulu (sorted)", got)
	}
}

func TestLoadTaxonomyRejectsNonPositiveVersion(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
	}{
		{
			name: "version zero",
			fixture: `{
				"version": 0,
				"slugs": {"some-slug": {"facet": "facet", "value": "val"}}
			}`,
		},
		{
			name: "version negative",
			fixture: `{
				"version": -1,
				"slugs": {"some-slug": {"facet": "facet", "value": "val"}}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := positions.LoadTaxonomy(strings.NewReader(tt.fixture))
			if err == nil {
				t.Fatalf("LoadTaxonomy: want error, got nil")
			}
			if !strings.Contains(err.Error(), "positive") {
				t.Fatalf("LoadTaxonomy error = %q, want substring 'positive'", err.Error())
			}
		})
	}
}

func TestLoadTaxonomyRejectsEmptySlugs(t *testing.T) {
	fixture := `{
		"version": 1,
		"slugs": {}
	}`

	_, err := positions.LoadTaxonomy(strings.NewReader(fixture))
	if err == nil {
		t.Fatalf("LoadTaxonomy: want error, got nil")
	}
	if !strings.Contains(err.Error(), "slugs") {
		t.Fatalf("LoadTaxonomy error = %q, want substring 'slugs'", err.Error())
	}
}
