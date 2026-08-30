package wishlist

import (
	"fmt"
	"strings"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/telegram"
)

// itemTitleAndBody resolves the localized title/body for an item, falling
// back to English and then to whatever language IS present, mirroring
// internal/positions' itemTitleAndBody: a caption should never come up empty
// just because neither preferred language has a translation yet.
func itemTitleAndBody(language string, item catalog.Item) (string, string) {
	if text, ok := item.Text[language]; ok && strings.TrimSpace(text.Title) != "" {
		return text.Title, text.Body
	}
	if text, ok := item.Text["en"]; ok && strings.TrimSpace(text.Title) != "" {
		return text.Title, text.Body
	}
	for _, text := range item.Text {
		if strings.TrimSpace(text.Title) != "" {
			return text.Title, text.Body
		}
	}
	return "", ""
}

// HubKeyboard is the module's entry screen: start swiping, see matches, see
// your own answers, back to the app's main menu.
//
// The matches button shows a count only when hasPair is true. Without a
// pair the button stays visible — the feature should be discoverable even
// solo — but never carries a number: there is no partner to match with yet,
// which is a different state from having matched with nobody, and printing
// "(0)" would misleadingly read as the latter.
func HubKeyboard(language string, hasPair bool, matches int) telegram.InlineKeyboardMarkup {
	swipeText := "💛 Відмічати"
	matchesText := "🔥 Збіги"
	if hasPair {
		matchesText = fmt.Sprintf("🔥 Збіги (%d)", matches)
	}
	mineText := "📊 Мої відповіді"
	menuText := "⌂ Меню"
	if language != "uk" {
		swipeText = "💛 Rate wishes"
		matchesText = "🔥 Matches"
		if hasPair {
			matchesText = fmt.Sprintf("🔥 Matches (%d)", matches)
		}
		mineText = "📊 My answers"
		menuText = "⌂ Menu"
	}

	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: swipeText, CallbackData: "wish:open"}},
		{{Text: matchesText, CallbackData: "wish:matches"}, {Text: mineText, CallbackData: "wish:mine"}},
		{{Text: menuText, CallbackData: "menu:main"}},
	}}
}

// SwipeKeyboard is shown under one wish card: the three private answers,
// plus skip-to-next. itemID is embedded raw in every callback, so the
// longest real id in the catalog ("519") plus "curious" (the longest answer
// word) sets the cap: "wish:answer:position:519:curious" is 30 bytes,
// comfortably inside Telegram's 64-byte callback_data limit.
func SwipeKeyboard(language, itemID string) telegram.InlineKeyboardMarkup {
	wantText, curiousText, noText, skipText := "💛 Хочу", "🤔 Цікаво", "🚫 Ні", "⏭ Пропустити"
	if language != "uk" {
		wantText, curiousText, noText, skipText = "💛 Want", "🤔 Curious", "🚫 No", "⏭ Skip"
	}

	prefix := "wish:answer:wish:" + itemID + ":"
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: wantText, CallbackData: prefix + "want"},
			{Text: curiousText, CallbackData: prefix + "curious"},
			{Text: noText, CallbackData: prefix + "no"},
		},
		{{Text: skipText, CallbackData: "wish:next"}},
	}}
}

// SwipeCaption renders one wish's title, body and answered/total progress.
// The progress line comes from the "wish.hub.progress" i18n key, which the
// caller (Task 4's handler, real content files) supplies with a %d-of-%d
// placeholder pair.
func SwipeCaption(bundle *i18n.Bundle, language string, item catalog.Item, answered, total int) string {
	title, body := itemTitleAndBody(language, item)

	var lines []string
	if title != "" {
		lines = append(lines, title)
	}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, body)
	}

	progressFormat := "wish.hub.progress"
	if bundle != nil {
		progressFormat = bundle.Text(language, "wish.hub.progress")
	}
	lines = append(lines, fmt.Sprintf(progressFormat, answered, total))

	return strings.Join(lines, "\n\n")
}

// BackKeyboard is a single row back to the module hub, used on screens that
// have nothing else to offer (the "done" screen, matches, mine).
func BackKeyboard(language string) telegram.InlineKeyboardMarkup {
	backText := "◀ Бажання"
	if language != "uk" {
		backText = "◀ Wishes"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: backText, CallbackData: "wish:open"}},
	}}
}
