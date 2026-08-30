package wishlist_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/wishlist"
)

func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"wish.hub.progress": "Відмічено: %d з %d",
	}})
	return b
}

func TestSwipeKeyboardOffersThreeAnswersAndSkip(t *testing.T) {
	markup := wishlist.SwipeKeyboard("uk", "w007")

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{
		"wish:answer:wish:w007:want",
		"wish:answer:wish:w007:curious",
		"wish:answer:wish:w007:no",
		"wish:next",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
	for _, d := range data {
		if len(d) > 64 {
			t.Fatalf("callback data %q is %d bytes, over Telegram's 64-byte cap", d, len(d))
		}
	}
}

func TestHubKeyboardHidesMatchesCountWithoutAPair(t *testing.T) {
	withPair := wishlist.HubKeyboard("uk", true, 4)
	withoutPair := wishlist.HubKeyboard("uk", false, 0)

	var withText, withoutText string
	for _, row := range withPair.InlineKeyboard {
		for _, b := range row {
			withText += b.Text + " "
		}
	}
	for _, row := range withoutPair.InlineKeyboard {
		for _, b := range row {
			withoutText += b.Text + " "
		}
	}
	if !strings.Contains(withText, "4") {
		t.Fatalf("paired hub %q does not show the match count", withText)
	}
	if strings.Contains(withoutText, "(0)") {
		t.Fatalf("solo hub %q shows a zero match count; it should not promise matches without a pair", withoutText)
	}
}

func TestSwipeCaptionShowsTitleAndProgress(t *testing.T) {
	item := catalog.Item{
		ID:   "w007",
		Text: map[string]catalog.ItemText{"uk": {Title: "Свічки", Body: "Кімната, освітлена лише свічками."}},
	}
	caption := wishlist.SwipeCaption(testBundle(), "uk", item, 6, 60)

	if !strings.Contains(caption, "Свічки") {
		t.Fatalf("caption %q does not contain the title", caption)
	}
	if !strings.Contains(caption, "Кімната") {
		t.Fatalf("caption %q does not contain the body", caption)
	}
	if !strings.Contains(caption, "6") || !strings.Contains(caption, "60") {
		t.Fatalf("caption %q does not contain the 6/60 progress", caption)
	}
}
