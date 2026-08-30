package positions

import (
	"fmt"
	"strings"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/telegram"
)

// captionFacets lists, in display order, the facets shown on a browse
// caption. Position content carries many facets (act, activity, extra,
// level, penetration, stimulation, type, location); showing all of them
// would bury the description under jargon, so only the two most
// orientation-relevant ones are surfaced here.
var captionFacets = []string{"level", "location"}

// itemTitleAndBody resolves the localized title/body for an item, falling
// back to English when the requested language has no translation yet (the
// Ukrainian catalog text is still being written by a separate process at the
// time this module was built) and finally to whatever language IS present,
// so a caption is never empty just because neither preferred language matched.
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

// facetBundle holds the i18n bundle used to localize facet names and values
// in facetSummary and FiltersKeyboard. BrowseCaption and BrowseKeyboard have
// signatures fixed by the module framework and carry no bundle parameter of
// their own, so NewHandler installs the process's one bundle here once, at
// construction, instead of threading it through every call. Nil — the zero
// value, and what it stays at in every test in this package that never
// constructs a Handler — degrades every lookup to the raw facet/value slug,
// exactly the same fallback used when the bundle IS set but lacks the
// specific key.
var facetBundle *i18n.Bundle

// facetNameLabel resolves a facet's own display name (e.g. "level" ->
// "Рівень" / "Level") from the "positions.facet.<facet>" i18n key. It falls
// back to the raw facet slug — never to the raw i18n key, which is what
// i18n.Bundle.Text returns on a miss — whenever facetBundle is nil or the
// key is missing, so a taxonomy addition without a matching translation
// still renders something a user can read instead of "positions.facet.x".
func facetNameLabel(language, facet string) string {
	if facetBundle == nil {
		return facet
	}
	key := "positions.facet." + facet
	if text := facetBundle.Text(language, key); text != key {
		return text
	}
	return facet
}

// facetValueLabel resolves one facet value's display label (e.g.
// ("level", "hard") -> "складна" / "Hard") from the
// "positions.value.<facet>.<value>" i18n key, with the same nil-bundle /
// missing-key fallback to the raw value as facetNameLabel.
func facetValueLabel(language, facet, value string) string {
	if facetBundle == nil {
		return value
	}
	key := fmt.Sprintf("positions.value.%s.%s", facet, value)
	if text := facetBundle.Text(language, key); text != key {
		return text
	}
	return value
}

// facetSummary renders the curated facet values for one item, one
// "<facet name>: <value>[/<value>]" segment per facet joined with "·". A
// bare join of just the values (the pre-i18n behaviour) reads oddly in
// Ukrainian: level values are adjectives ("складна") and location values
// are nouns ("стілець"), so two bare values sitting side by side don't read
// as a phrase. Prefixing each with its own facet name keeps every segment
// self-contained in both languages. Facets absent from the item are
// skipped rather than shown empty.
func facetSummary(language string, item catalog.Item) string {
	var parts []string
	for _, facet := range captionFacets {
		values := item.Facets[facet]
		if len(values) == 0 {
			continue
		}
		labels := make([]string, len(values))
		for i, value := range values {
			labels[i] = facetValueLabel(language, facet, value)
		}
		parts = append(parts, facetNameLabel(language, facet)+": "+strings.Join(labels, "/"))
	}
	return strings.Join(parts, " · ")
}

// BrowseCaption renders the title, a facet line, the description and a
// N/M position counter for one catalog item. index is 0-based; the counter
// shown to the user is 1-based. tried/favorited append status markers so the
// pair can tell at a glance whether they have already tried or starred this
// position.
func BrowseCaption(language string, item catalog.Item, index, total int, tried, favorited bool) string {
	title, body := itemTitleAndBody(language, item)

	markers := ""
	if tried {
		markers += " ✅"
	}
	if favorited {
		markers += " ⭐"
	}

	var lines []string
	lines = append(lines, strings.TrimSpace(title+markers))
	if facets := facetSummary(language, item); facets != "" {
		lines = append(lines, facets)
	}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, body)
	}
	lines = append(lines, fmt.Sprintf("%d/%d", index+1, total))

	return strings.Join(lines, "\n\n")
}

// BrowseKeyboard builds the control rows under a browse card: navigation,
// shared marks (tried, favorited, hidden), and an escape to filters or the
// main menu. soloMode is true when the viewer has no active pair — the mark
// buttons stay visible (a user with no marks yet should not wonder where
// they went) but carry a lock marker, since marks are pair-shared and
// cannot be set solo. The hide button buries the item for the pair — once
// hidden, VisibleWithMarks drops it from every screen (browse, random and
// the bulk send alike) with no in-UI way back; that one-way "I never want
// to see this again" semantics matches how the service layer already
// documents the feature, rather than a togglable show/hide.
func BrowseKeyboard(language, itemID string, index int, tried, favorited, soloMode bool) telegram.InlineKeyboardMarkup {
	triedText := "✅"
	if tried {
		triedText = "✅✓"
	}
	favoritedText := "⭐"
	if favorited {
		favoritedText = "⭐✓"
	}
	hideText := "🙈"
	if soloMode {
		triedText += " 🔒"
		favoritedText += " 🔒"
		hideText += " 🔒"
	}

	filtersText := "☰"
	menuText := "⌂"
	if language == "uk" {
		filtersText = "☰ Фільтри"
		menuText = "⌂ Меню"
	} else {
		filtersText = "☰ Filters"
		menuText = "⌂ Menu"
	}

	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: "◀", CallbackData: fmt.Sprintf("pos:browse:%d", index-1)},
			{Text: "🎲", CallbackData: "pos:random"},
			{Text: "▶", CallbackData: fmt.Sprintf("pos:browse:%d", index+1)},
		},
		{
			{Text: triedText, CallbackData: "pos:mark:tried:" + itemID},
			{Text: favoritedText, CallbackData: "pos:mark:favorited:" + itemID},
			{Text: hideText, CallbackData: "pos:mark:hidden:" + itemID},
		},
		{
			{Text: filtersText, CallbackData: "pos:filters"},
			{Text: menuText, CallbackData: "menu:main"},
		},
	}}
}

// HubKeyboard is the entry screen for the module: randomiser, browsing,
// filters and the bulk send, plus the escape back to the main menu.
func HubKeyboard(language string) telegram.InlineKeyboardMarkup {
	random, browse, filters, dump, menu := "🎲 Random", "📖 Browse", "☰ Filters", "📬 Send all", "⌂ Menu"
	if language == "uk" {
		random, browse, filters, dump, menu = "🎲 Навмання", "📖 Гортати", "☰ Фільтри", "📬 Надіслати все", "⌂ Меню"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: random, CallbackData: "pos:random"}, {Text: browse, CallbackData: "pos:browse:0"}},
		{{Text: filters, CallbackData: "pos:filters"}},
		{{Text: dump, CallbackData: "pos:dump:confirm"}},
		{{Text: menu, CallbackData: "menu:main"}},
	}}
}

// FacetOption is one facet's curated set of values, in the order they
// should be offered on the filters screen. Callers assemble this list from
// the catalog once (see collectFacetValues in handler.go) so this file stays
// free of I/O and directly testable with literal data.
type FacetOption struct {
	Facet  string
	Values []string
}

// FiltersKeyboard renders one toggle button per (facet, value) pair, marking
// currently-included values with a check, followed by an escape row back to
// the browse view (results already reflect the filter) and the main menu.
// The result count is rendered by the caller into the screen text, not here.
//
// Each facet's values are preceded by their own header row carrying the
// localized facet name (e.g. "Рівень" / "Level") — the value buttons
// already fall into separate rows per facet (a new facet always starts a
// fresh row below), but without a label there is nothing on screen telling
// the viewer which row is which facet. The header button's callback data is
// "pos:filters", the same screen a tap already lands on, so an accidental
// tap on a header is a harmless no-op refresh rather than an unhandled
// callback. Button text is display-only and localized; callback data always
// carries the raw facet/value slugs so filter state round-trips unchanged.
func FiltersKeyboard(language string, filter catalog.Filter, facets []FacetOption) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	for _, option := range facets {
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: facetNameLabel(language, option.Facet), CallbackData: "pos:filters"},
		})
		var row []telegram.InlineKeyboardButton
		for _, value := range option.Values {
			label := facetValueLabel(language, option.Facet, value)
			if includesValue(filter.Include[option.Facet], value) {
				label = "✓ " + label
			}
			row = append(row, telegram.InlineKeyboardButton{
				Text:         label,
				CallbackData: fmt.Sprintf("pos:filter:%s:%s", option.Facet, value),
			})
			if len(row) == 3 {
				rows = append(rows, row)
				row = nil
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}

	browseText, menuText := "▶ Results", "⌂ Menu"
	if language == "uk" {
		browseText, menuText = "▶ Результати", "⌂ Меню"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: browseText, CallbackData: "pos:browse:0"},
		{Text: menuText, CallbackData: "menu:main"},
	})

	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// DumpConfirmKeyboard offers the throttled bulk-send its confirmation step:
// go ahead, or back out to the hub without sending anything.
func DumpConfirmKeyboard(language string) telegram.InlineKeyboardMarkup {
	goText, cancelText := "✅ Send", "✖ Cancel"
	if language == "uk" {
		goText, cancelText = "✅ Надіслати", "✖ Скасувати"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: goText, CallbackData: "pos:dump:go"}, {Text: cancelText, CallbackData: "pos:open"}},
	}}
}

// dumpStopKeyboard is shown while a bulk send is running, so it is not part
// of the fixed, brief-mandated keyboard set above but follows the same
// hardcoded-per-language pattern.
func dumpStopKeyboard(language string) telegram.InlineKeyboardMarkup {
	stopText := "⏹ Stop"
	if language == "uk" {
		stopText = "⏹ Зупинити"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: stopText, CallbackData: "pos:dump:stop"}},
	}}
}

func includesValue(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// ToggleFilterValue returns a copy of filter with value added to (or removed
// from) filter.Include[facet]. It never mutates its argument: BrowseState is
// round-tripped through Redis by value, and a caller that stored the
// pre-toggle filter elsewhere must not see it change out from under them.
func ToggleFilterValue(filter catalog.Filter, facet, value string) catalog.Filter {
	out := catalog.Filter{
		Include: cloneFacetMap(filter.Include),
		Exclude: cloneFacetMap(filter.Exclude),
		Tags:    append([]string(nil), filter.Tags...),
	}

	current := out.Include[facet]
	if includesValue(current, value) {
		next := make([]string, 0, len(current))
		for _, v := range current {
			if v != value {
				next = append(next, v)
			}
		}
		if len(next) == 0 {
			delete(out.Include, facet)
		} else {
			out.Include[facet] = next
		}
		return out
	}

	next := make([]string, 0, len(current)+1)
	next = append(next, current...)
	next = append(next, value)
	if out.Include == nil {
		out.Include = map[string][]string{}
	}
	out.Include[facet] = next
	return out
}

func cloneFacetMap(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
