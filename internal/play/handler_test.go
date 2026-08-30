package play_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/play"
)

func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"play.next":             "ДАЛІ-МАРКЕР",
		"play.skip":             "ПРОПУСК-МАРКЕР",
		"play.filters":          "ФІЛЬТРИ-МАРКЕР",
		"play.kind.truth":       "Правда",
		"play.kind.dare":        "Дія",
		"play.intensity.gentle": "Мʼяко",
		"play.intensity.bold":   "Сміливо",
	}})
	return b
}

func testCard() catalog.Item {
	return catalog.Item{
		ID:     "p007",
		Facets: map[string][]string{"kind": {"dare"}, "intensity": {"gentle"}},
		Text:   map[string]catalog.ItemText{"uk": {Title: "Обійми", Body: "Обійми партнера і не відпускай хвилину."}},
	}
}

func TestCardCaptionShowsActorTypeAndBody(t *testing.T) {
	caption := play.CardCaption(testBundle(), "uk", testCard(), "Оля")

	if !strings.Contains(caption, "Оля") {
		t.Fatalf("caption %q does not name the actor", caption)
	}
	if !strings.Contains(caption, "Обійми партнера") {
		t.Fatalf("caption %q does not contain the card body", caption)
	}
	if !strings.Contains(caption, "Дія") {
		t.Fatalf("caption %q does not say whether this is a truth or a dare", caption)
	}
}

func TestCardCaptionWithoutAnActorHasNoOrphanedSeparator(t *testing.T) {
	caption := play.CardCaption(testBundle(), "uk", testCard(), "")

	if strings.HasPrefix(strings.TrimSpace(caption), ":") {
		t.Fatalf("caption %q starts with an orphaned separator", caption)
	}
	if !strings.Contains(caption, "Обійми партнера") {
		t.Fatalf("caption %q lost the body when there was no actor", caption)
	}
}

// TestCardCaptionFallsBackToRawFacetValueOnMissingKey pins the guard the
// brief calls out by name: Bundle.Text returns the key itself on a miss
// (see internal/wishlist/keyboards.go's SwipeCaption, which feeds that miss
// straight into Sprintf and renders literal "wish.hub.progress%!(EXTRA...)"
// text on screen). CardCaption must not repeat that mistake for
// "play.kind.{value}" / "play.intensity.{value}": testBundle above defines
// no "play.intensity.medium" key, so a card with intensity=medium must
// degrade to the raw facet value "medium", never to the raw dotted key
// "play.intensity.medium".
func TestCardCaptionFallsBackToRawFacetValueOnMissingKey(t *testing.T) {
	card := catalog.Item{
		ID:     "p008",
		Facets: map[string][]string{"kind": {"dare"}, "intensity": {"medium"}},
		Text:   map[string]catalog.ItemText{"uk": {Title: "Т", Body: "Тіло картки"}},
	}

	caption := play.CardCaption(testBundle(), "uk", card, "")

	if strings.Contains(caption, "play.intensity.medium") {
		t.Fatalf("caption %q leaked the raw i18n key instead of falling back to the raw facet value", caption)
	}
	if !strings.Contains(caption, "medium") {
		t.Fatalf("caption %q did not fall back to the raw facet value %q", caption, "medium")
	}
}

func TestCardKeyboardOffersNextSkipAndFilters(t *testing.T) {
	markup := play.CardKeyboard(testBundle(), "uk")

	var data, texts []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
			texts = append(texts, button.Text)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{"play:next", "play:skip", "play:filters"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
	for _, d := range data {
		if len(d) > 64 {
			t.Fatalf("callback data %q is %d bytes, over Telegram's 64-byte cap", d, len(d))
		}
	}
	labels := strings.Join(texts, " ")
	if !strings.Contains(labels, "ДАЛІ-МАРКЕР") || !strings.Contains(labels, "ПРОПУСК-МАРКЕР") {
		t.Fatalf("button labels %q do not come from the bundle", labels)
	}
}

func TestHubKeyboardOffersOpenFiltersAndMenu(t *testing.T) {
	markup := play.HubKeyboard(testBundle(), "uk")

	var data, texts []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
			texts = append(texts, button.Text)
			if len(button.CallbackData) > 64 {
				t.Fatalf("callback data %q is %d bytes, over the cap", button.CallbackData, len(button.CallbackData))
			}
		}
	}
	joined := strings.Join(data, " ")
	for _, want := range []string{"play:open", "play:filters", "menu:main"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("hub keyboard callbacks %q are missing %q", joined, want)
		}
	}
	labels := strings.Join(texts, " ")
	if !strings.Contains(labels, "ФІЛЬТРИ-МАРКЕР") {
		t.Fatalf("hub keyboard labels %q do not come from the bundle", labels)
	}
}

func TestFiltersKeyboardMarksActiveValuesAndFitsTheCallbackCap(t *testing.T) {
	filter := catalog.Filter{Include: map[string][]string{"kind": {"dare"}}}
	markup := play.FiltersKeyboard(testBundle(), "uk", filter)

	var data []string
	activeMarked := false
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
			if button.CallbackData == "play:filter:kind:dare" && strings.Contains(button.Text, "✓") {
				activeMarked = true
			}
			if len(button.CallbackData) > 64 {
				t.Fatalf("callback data %q is %d bytes, over the cap", button.CallbackData, len(button.CallbackData))
			}
		}
	}
	if !activeMarked {
		t.Fatalf("the active kind=dare filter is not marked; callbacks were %v", data)
	}
	if !strings.Contains(strings.Join(data, " "), "play:filter:intensity:gentle") {
		t.Fatalf("callbacks %v do not offer the intensity facet", data)
	}
}

func TestToggleFilterValueDoesNotMutateItsArgument(t *testing.T) {
	original := catalog.Filter{Include: map[string][]string{"kind": {"dare"}}}

	added := play.ToggleFilterValue(original, "intensity", "bold")
	if len(original.Include["intensity"]) != 0 {
		t.Fatalf("ToggleFilterValue mutated the caller's filter: %+v", original)
	}
	if len(added.Include["intensity"]) != 1 || added.Include["intensity"][0] != "bold" {
		t.Fatalf("added filter = %+v, want intensity=bold", added.Include)
	}

	removed := play.ToggleFilterValue(added, "intensity", "bold")
	if len(removed.Include["intensity"]) != 0 {
		t.Fatalf("toggling twice left %v, want the value removed", removed.Include["intensity"])
	}
}
