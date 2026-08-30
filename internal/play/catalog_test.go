package play_test

import (
	"os"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func loadPlayCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/play.v1.json")
	if err != nil {
		t.Fatalf("open play catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load play catalog: %v", err)
	}
	return c
}

func TestPlayCatalogValidatesForBothLanguages(t *testing.T) {
	if err := loadPlayCatalog(t).Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPlayCatalogHasExactlyEightyPaddedAscendingIDs(t *testing.T) {
	c := loadPlayCatalog(t)
	if len(c.Items) != 80 {
		t.Fatalf("catalog has %d items, want exactly 80", len(c.Items))
	}
	for i, item := range c.Items {
		if len(item.ID) != 4 || !strings.HasPrefix(item.ID, "p") {
			t.Fatalf("item %d id = %q, want the zero-padded form pNNN", i, item.ID)
		}
		if i > 0 && c.Items[i-1].ID >= item.ID {
			t.Fatalf("ids are not strictly ascending at %d: %q then %q", i, c.Items[i-1].ID, item.ID)
		}
	}
}

func TestPlayCatalogFacetDistributionIsExact(t *testing.T) {
	kinds := map[string]int{"truth": 0, "dare": 0}
	intensities := map[string]int{"gentle": 0, "medium": 0, "bold": 0}
	perKind := map[string]map[string]int{
		"truth": {"gentle": 0, "medium": 0, "bold": 0},
		"dare":  {"gentle": 0, "medium": 0, "bold": 0},
	}

	for _, item := range loadPlayCatalog(t).Items {
		ks := item.Facets["kind"]
		if len(ks) != 1 {
			t.Fatalf("item %s has %d kind values, want exactly one", item.ID, len(ks))
		}
		if _, ok := kinds[ks[0]]; !ok {
			t.Fatalf("item %s has unknown kind %q", item.ID, ks[0])
		}
		is := item.Facets["intensity"]
		if len(is) != 1 {
			t.Fatalf("item %s has %d intensity values, want exactly one", item.ID, len(is))
		}
		if _, ok := intensities[is[0]]; !ok {
			t.Fatalf("item %s has unknown intensity %q", item.ID, is[0])
		}
		kinds[ks[0]]++
		intensities[is[0]]++
		perKind[ks[0]][is[0]]++
	}

	if kinds["truth"] != 40 || kinds["dare"] != 40 {
		t.Fatalf("kind split = truth %d / dare %d, want 40/40", kinds["truth"], kinds["dare"])
	}
	if intensities["gentle"] != 30 || intensities["medium"] != 30 || intensities["bold"] != 20 {
		t.Fatalf("intensity split = %v, want gentle 30 / medium 30 / bold 20", intensities)
	}
	for kind, want := range map[string]map[string]int{
		"truth": {"gentle": 15, "medium": 15, "bold": 10},
		"dare":  {"gentle": 15, "medium": 15, "bold": 10},
	} {
		for intensity, n := range want {
			if perKind[kind][intensity] != n {
				t.Fatalf("%s/%s = %d, want %d", kind, intensity, perKind[kind][intensity], n)
			}
		}
	}
}

func TestPlayCatalogTextLengthsAreWithinBounds(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		for _, lang := range []string{"uk", "en"} {
			title := []rune(item.Text[lang].Title)
			body := []rune(item.Text[lang].Body)
			if len(title) == 0 || len(title) > 40 {
				t.Fatalf("item %s %s title is %d runes, want 1-40", item.ID, lang, len(title))
			}
			if len(body) < 30 || len(body) > 160 {
				t.Fatalf("item %s %s body is %d runes, want 30-160", item.ID, lang, len(body))
			}
		}
	}
}

func TestPlayCatalogTruthsAskAndDaresInstruct(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		body := item.Text["uk"].Body
		isTruth := item.Facets["kind"][0] == "truth"
		endsWithQuestion := strings.HasSuffix(strings.TrimSpace(body), "?")
		if isTruth && !endsWithQuestion {
			t.Fatalf("truth %s does not end in a question mark: %q", item.ID, body)
		}
		if !isTruth && endsWithQuestion {
			t.Fatalf("dare %s reads as a question: %q", item.ID, body)
		}
	}
}
