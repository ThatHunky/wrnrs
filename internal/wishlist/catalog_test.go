package wishlist_test

import (
	"os"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func loadWishes(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/wishes.v1.json")
	if err != nil {
		t.Fatalf("open wishes catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load wishes catalog: %v", err)
	}
	return c
}

func TestWishesCatalogValidatesForBothLanguages(t *testing.T) {
	if err := loadWishes(t).Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestWishesCatalogHasExactlySixtyPaddedIDs(t *testing.T) {
	c := loadWishes(t)
	if len(c.Items) != 60 {
		t.Fatalf("catalog has %d items, want exactly 60", len(c.Items))
	}
	for i, item := range c.Items {
		if len(item.ID) != 4 || !strings.HasPrefix(item.ID, "w") {
			t.Fatalf("item %d id = %q, want the zero-padded form wNNN", i, item.ID)
		}
	}
	for i := 1; i < len(c.Items); i++ {
		if c.Items[i-1].ID >= c.Items[i].ID {
			t.Fatalf("ids are not strictly ascending at %d: %q then %q", i, c.Items[i-1].ID, c.Items[i].ID)
		}
	}
}

func TestWishesCatalogFacetsAreWithinTheAllowedVocabulary(t *testing.T) {
	kinds := map[string]int{"place": 0, "role": 0, "pace": 0, "mood": 0, "toys": 0, "scenario": 0}
	intensities := map[string]int{"gentle": 0, "medium": 0, "bold": 0}

	for _, item := range loadWishes(t).Items {
		ks := item.Facets["kind"]
		if len(ks) != 1 {
			t.Fatalf("item %s has %d kind values, want exactly one", item.ID, len(ks))
		}
		if _, ok := kinds[ks[0]]; !ok {
			t.Fatalf("item %s has unknown kind %q", item.ID, ks[0])
		}
		kinds[ks[0]]++

		is := item.Facets["intensity"]
		if len(is) != 1 {
			t.Fatalf("item %s has %d intensity values, want exactly one", item.ID, len(is))
		}
		if _, ok := intensities[is[0]]; !ok {
			t.Fatalf("item %s has unknown intensity %q", item.ID, is[0])
		}
		intensities[is[0]]++
	}

	for kind, n := range kinds {
		if n < 5 {
			t.Fatalf("kind %q has only %d items, want at least 5 so filtering by it is useful", kind, n)
		}
	}
	if intensities["gentle"] < 15 {
		t.Fatalf("only %d gentle items; the first screens must be soft", intensities["gentle"])
	}
}

func TestWishesCatalogBodiesAreOneSentenceEach(t *testing.T) {
	for _, item := range loadWishes(t).Items {
		for _, lang := range []string{"uk", "en"} {
			body := item.Text[lang].Body
			if n := len([]rune(body)); n < 40 || n > 200 {
				t.Fatalf("item %s %s body is %d runes, want 40-200", item.ID, lang, n)
			}
			if n := len([]rune(item.Text[lang].Title)); n > 40 {
				t.Fatalf("item %s %s title is %d runes, want at most 40", item.ID, lang, n)
			}
		}
	}
}
