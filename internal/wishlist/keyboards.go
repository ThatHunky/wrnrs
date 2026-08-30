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

// stripCount removes a trailing "(%d)" placeholder from an i18n format
// string, so the same "wish.hub.matches" copy ("🔥 Збіги (%d)" / "🔥 Matches
// (%d)") can serve both the paired button (rendered with Sprintf) and the
// solo one (rendered with the count omitted entirely) without a second key
// or any language branching in Go.
func stripCount(format string) string {
	return strings.TrimSpace(strings.Replace(format, "(%d)", "", 1))
}

// HubKeyboard is the module's entry screen: start swiping, see matches, see
// your own answers, back to the app's main menu. Every button's copy comes
// from bundle so the JSON files stay the single source of truth for
// user-facing text; only the "menu:main" button is exempt, by design — that
// button's callback and label belong to the app shell, and every other
// module (internal/positions, internal/telegram, internal/app itself) draws
// it identically rather than through a module-owned i18n key.
//
// The matches button shows a count only when hasPair is true. Without a
// pair the button stays visible — the feature should be discoverable even
// solo — but never carries a number: there is no partner to match with yet,
// which is a different state from having matched with nobody, and printing
// "(0)" would misleadingly read as the latter.
func HubKeyboard(bundle *i18n.Bundle, language string, hasPair bool, matches int) telegram.InlineKeyboardMarkup {
	swipeText := bundle.Text(language, "wish.hub.swipe")
	mineText := bundle.Text(language, "wish.hub.mine")

	matchesFormat := bundle.Text(language, "wish.hub.matches")
	matchesText := stripCount(matchesFormat)
	if hasPair {
		matchesText = fmt.Sprintf(matchesFormat, matches)
	}

	menuText := "⌂ Меню"
	if language != "uk" {
		menuText = "⌂ Menu"
	}

	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: swipeText, CallbackData: "wish:open"}},
		{{Text: matchesText, CallbackData: "wish:matches"}, {Text: mineText, CallbackData: "wish:mine"}},
		{{Text: menuText, CallbackData: "menu:main"}},
	}}
}

// SwipeKeyboard is shown under one wish card: the three private answers,
// plus skip-to-next. Button copy comes from the "wish.answer.*" i18n keys.
//
// itemID is embedded raw in every callback; kind is always literally "wish"
// because this screen only ever shows items from Service.Queue, which draws
// exclusively from the wishes catalog. The longest id space this module is
// documented to share ("position", up to "519") plus "curious" (the longest
// answer word) sets the worst-case cap this callback shape can reach:
// "wish:answer:position:519:curious" is 32 bytes — comfortably inside
// Telegram's 64-byte callback_data limit even though this function itself
// only ever emits the shorter "wish:answer:wish:<id>:<answer>" form.
func SwipeKeyboard(bundle *i18n.Bundle, language, itemID string) telegram.InlineKeyboardMarkup {
	wantText := bundle.Text(language, "wish.answer.want")
	curiousText := bundle.Text(language, "wish.answer.curious")
	noText := bundle.Text(language, "wish.answer.no")
	skipText := bundle.Text(language, "wish.answer.skip")

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
// have nothing else to offer (the "done" screen, matches, mine). Its label
// reuses "wish.hub.title" — the module's own name — rather than a dedicated
// key, since "back to <module name>" is exactly what that key already says.
func BackKeyboard(bundle *i18n.Bundle, language string) telegram.InlineKeyboardMarkup {
	backText := "◀ " + bundle.Text(language, "wish.hub.title")
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: backText, CallbackData: "wish:open"}},
	}}
}
