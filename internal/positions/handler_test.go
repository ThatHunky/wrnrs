package positions_test

import (
	"context"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
	"wrnrs/internal/telegram"
)

type fakePhotoBot struct {
	sentRefs  []string
	editedIDs []string
	captions  []string
	markups   []telegram.InlineKeyboardMarkup
}

func (b *fakePhotoBot) SendPhotoRef(_ context.Context, _ int64, fileID, caption string, markup any) (telegram.SentPhoto, error) {
	b.sentRefs = append(b.sentRefs, fileID)
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return telegram.SentPhoto{MessageID: int64(len(b.sentRefs)), FileID: fileID}, nil
}

func (b *fakePhotoBot) SendPhotoBytes(_ context.Context, _ int64, _ []byte, caption string, markup any) (telegram.SentPhoto, error) {
	b.sentRefs = append(b.sentRefs, "uploaded")
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return telegram.SentPhoto{MessageID: int64(len(b.sentRefs)), FileID: "new-file-id"}, nil
}

func (b *fakePhotoBot) EditMessageMediaRef(_ context.Context, _, _ int64, fileID, caption string, markup any) error {
	b.editedIDs = append(b.editedIDs, fileID)
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return nil
}

func TestBrowseCaptionCarriesTitleFacetsAndPosition(t *testing.T) {
	item := catalog.Item{
		ID:     "519",
		Facets: map[string][]string{"level": {"medium"}, "location": {"sofa"}},
		Text:   map[string]catalog.ItemText{"uk": {Title: "Одкровення", Body: "Опис пози."}},
	}

	caption := positions.BrowseCaption("uk", item, 4, 100, true, false)

	if !strings.Contains(caption, "Одкровення") {
		t.Fatalf("caption %q does not contain the title", caption)
	}
	if !strings.Contains(caption, "Опис пози.") {
		t.Fatalf("caption %q does not contain the body", caption)
	}
	if !strings.Contains(caption, "5/100") {
		t.Fatalf("caption %q does not contain the 1-based position 5/100", caption)
	}
	if !strings.Contains(caption, "✅") {
		t.Fatalf("caption %q does not mark the position as tried", caption)
	}
}

func TestBrowseKeyboardExposesEveryControl(t *testing.T) {
	markup := positions.BrowseKeyboard("uk", "519", 4, false, false, false)

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{"pos:browse:3", "pos:browse:5", "pos:random", "pos:mark:tried:519", "pos:mark:favorited:519", "pos:filters"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
}

// TestBrowseKeyboardExposesHideControl pins the fix for the review finding
// that MarkHidden and VisibleWithMarks's hide-filtering were unreachable
// from the UI: no keyboard ever emitted a pos:mark:hidden: callback. This
// asserts the browse keyboard now wires one in, using the button's own
// existing parameters rather than a signature change.
func TestBrowseKeyboardExposesHideControl(t *testing.T) {
	markup := positions.BrowseKeyboard("uk", "519", 4, false, false, false)

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")
	if !strings.Contains(joined, "pos:mark:hidden:519") {
		t.Fatalf("keyboard callbacks %q are missing pos:mark:hidden:519", joined)
	}
}

func TestBrowseKeyboardDisablesSharedMarksWithoutAPair(t *testing.T) {
	withoutPair := positions.BrowseKeyboard("uk", "519", 0, false, false, true)

	var texts []string
	for _, row := range withoutPair.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "pos:mark:") {
				texts = append(texts, button.Text)
			}
		}
	}
	if len(texts) == 0 {
		t.Fatal("solo users lost the mark buttons entirely; they must stay visible with a hint")
	}
	for _, text := range texts {
		if !strings.Contains(text, "🔒") {
			t.Fatalf("solo mark button %q has no lock marker", text)
		}
	}
}

func TestBrowseCaptionMarksFavorited(t *testing.T) {
	item := catalog.Item{
		ID:   "1",
		Text: map[string]catalog.ItemText{"uk": {Title: "Т"}},
	}
	caption := positions.BrowseCaption("uk", item, 0, 1, false, true)
	if !strings.Contains(caption, "⭐") {
		t.Fatalf("caption %q does not mark the position as favorited", caption)
	}
	if strings.Contains(caption, "✅") {
		t.Fatalf("caption %q marks tried when it should not", caption)
	}
}

func TestBrowseCaptionFallsBackToEnglishWhenTranslationMissing(t *testing.T) {
	item := catalog.Item{
		ID:   "1",
		Text: map[string]catalog.ItemText{"en": {Title: "English Only", Body: "English body."}},
	}
	caption := positions.BrowseCaption("uk", item, 0, 1, false, false)
	if !strings.Contains(caption, "English Only") {
		t.Fatalf("caption %q did not fall back to english title", caption)
	}
}

func TestBrowseKeyboardWrapsNavigationAtZero(t *testing.T) {
	markup := positions.BrowseKeyboard("uk", "1", 0, false, false, false)
	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")
	if !strings.Contains(joined, "pos:browse:-1") {
		t.Fatalf("keyboard callbacks %q should carry the raw previous index -1 (the handler normalizes it), got none", joined)
	}
}

func TestHubKeyboardCarriesEveryEntryPoint(t *testing.T) {
	markup := positions.HubKeyboard("uk")
	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")
	for _, want := range []string{"pos:random", "pos:browse:0", "pos:filters", "pos:dump:confirm", "menu:main"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("hub keyboard callbacks %q are missing %q", joined, want)
		}
	}
}

func TestFiltersKeyboardTogglesMarkSelectedValues(t *testing.T) {
	facets := []positions.FacetOption{
		{Facet: "level", Values: []string{"easy", "medium", "hard"}},
	}
	filter := catalog.Filter{Include: map[string][]string{"level": {"medium"}}}

	markup := positions.FiltersKeyboard("uk", filter, facets)

	var selected, unselected string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "pos:filter:level:medium" {
				selected = button.Text
			}
			if button.CallbackData == "pos:filter:level:easy" {
				unselected = button.Text
			}
		}
	}
	if selected == "" || unselected == "" {
		t.Fatalf("filters keyboard missing expected callbacks; rows=%+v", markup.InlineKeyboard)
	}
	if !strings.Contains(selected, "✓") {
		t.Fatalf("selected facet value %q has no selection marker", selected)
	}
	if strings.Contains(unselected, "✓") {
		t.Fatalf("unselected facet value %q wrongly marked as selected", unselected)
	}
}

func TestFiltersKeyboardCarriesBrowseAndMenuEscape(t *testing.T) {
	markup := positions.FiltersKeyboard("uk", catalog.Filter{}, nil)
	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")
	for _, want := range []string{"pos:browse:0", "menu:main"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("filters keyboard callbacks %q are missing %q", joined, want)
		}
	}
}

func TestDumpConfirmKeyboardCarriesGoAndCancel(t *testing.T) {
	markup := positions.DumpConfirmKeyboard("uk")
	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")
	if !strings.Contains(joined, "pos:dump:go") {
		t.Fatalf("dump confirm keyboard %q is missing pos:dump:go", joined)
	}
	if !strings.Contains(joined, "pos:open") {
		t.Fatalf("dump confirm keyboard %q is missing an escape back to the hub", joined)
	}
}

func TestToggleFilterValueAddsAndRemoves(t *testing.T) {
	filter := catalog.Filter{}

	added := positions.ToggleFilterValue(filter, "level", "medium")
	if !containsValue(added.Include["level"], "medium") {
		t.Fatalf("ToggleFilterValue did not add the value: %+v", added)
	}

	removed := positions.ToggleFilterValue(added, "level", "medium")
	if containsValue(removed.Include["level"], "medium") {
		t.Fatalf("ToggleFilterValue did not remove the value on second toggle: %+v", removed)
	}

	// The original filter must not be mutated by either call.
	if containsValue(filter.Include["level"], "medium") {
		t.Fatalf("ToggleFilterValue mutated the original filter")
	}
}

func containsValue(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
