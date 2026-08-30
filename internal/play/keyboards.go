package play

import (
	"strings"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/telegram"
)

// kindValues and intensityValues are the truth-or-dare module's fixed facet
// vocabularies. Unlike internal/positions, which discovers its facet values
// from the catalog at runtime (collectFacetValues in handler.go), play's
// filter screen is small and closed — two card kinds, three intensity
// levels — so the values are listed here rather than threaded through as a
// handler-supplied parameter.
var kindValues = []string{"truth", "dare"}
var intensityValues = []string{"gentle", "medium", "bold"}

// itemBody resolves the localized body for an item, falling back to English
// and then to whatever language IS present, mirroring internal/positions and
// internal/wishlist's itemTitleAndBody: a caption should never come up empty
// just because neither preferred language has a translation yet. Unlike
// those two, play returns the body alone — the card's `title` is a short
// label for logs and filters and is never rendered (see spec §8), so
// resolving it here would only produce a value with no consumer.
func itemBody(language string, item catalog.Item) string {
	if text, ok := item.Text[language]; ok && strings.TrimSpace(text.Body) != "" {
		return text.Body
	}
	if text, ok := item.Text["en"]; ok && strings.TrimSpace(text.Body) != "" {
		return text.Body
	}
	for _, text := range item.Text {
		if strings.TrimSpace(text.Body) != "" {
			return text.Body
		}
	}
	return ""
}

// facetLabel resolves one facet value's display label from the
// "play.<facet>.<value>" i18n key (e.g. "play.kind.dare" -> "Дія",
// "play.intensity.gentle" -> "Мʼяко"). It falls back to the raw facet value
// itself — never to the raw i18n key, which is what i18n.Bundle.Text
// returns on a miss — whenever bundle is nil, value is empty, or the key is
// missing. This mirrors internal/positions/keyboards.go's facetValueLabel;
// it is the guard internal/wishlist/keyboards.go's SwipeCaption skips, which
// is why that function can render a raw, unresolved i18n key straight onto
// the screen on a miss.
func facetLabel(bundle *i18n.Bundle, language, facet, value string) string {
	if value == "" {
		return ""
	}
	if bundle == nil {
		return value
	}
	key := "play." + facet + "." + value
	if text := bundle.Text(language, key); text != key {
		return text
	}
	return value
}

// firstFacetValue returns the first value stored under facet, or "" if the
// item carries none. Play cards carry exactly one kind and one intensity
// each (see the content pipeline that builds this catalog), so "first" is
// "the" value in practice; this just guards the empty-slice case.
func firstFacetValue(item catalog.Item, facet string) string {
	values := item.Facets[facet]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// CardCaption renders one drawn card: who it is for (when actorName is
// known), what kind of card it is and how intense, then the card's body.
// The card's `title` is deliberately NOT rendered — spec §8: «`title` не
// використовується як заголовок картки — картка показує `body`… слугує
// коротким ярликом у логах і фільтрах». Printing it was redundant at best
// and spoiled the card at worst (p071 rendered «Тільки зубами» directly
// above the line that was supposed to reveal it).
//
// actorName is empty until the app has a paired partner to name (see
// play.hub.solo_hint) or before the handler has resolved whose turn it is;
// in that case the actor prefix and its trailing ":" are both omitted
// rather than left as an orphaned separator like "Обійми партнера і не
// відпускай хвилину." starting with a stray ": ".
func CardCaption(bundle *i18n.Bundle, language string, item catalog.Item, actorName string) string {
	body := itemBody(language, item)

	kindLabel := facetLabel(bundle, language, "kind", firstFacetValue(item, "kind"))
	intensityLabel := facetLabel(bundle, language, "intensity", firstFacetValue(item, "intensity"))

	header := kindLabel
	if intensityLabel != "" {
		if header != "" {
			header += " · " + intensityLabel
		} else {
			header = intensityLabel
		}
	}
	if strings.TrimSpace(actorName) != "" {
		if header != "" {
			header = actorName + ": " + header
		} else {
			header = actorName
		}
	}

	var lines []string
	if header != "" {
		lines = append(lines, header)
	}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, body)
	}

	return strings.Join(lines, "\n\n")
}

// menuButtonText returns the label for the app's shared "back to main menu"
// button. This is the one hardcoded, per-language exception the brief
// carries over from internal/positions and internal/wishlist: the
// "menu:main" callback and its label belong to the app shell, not to any
// one module's i18n keys, and every module draws it the same way.
func menuButtonText(language string) string {
	if language == "uk" {
		return "⌂ Меню"
	}
	return "⌂ Menu"
}

// CardKeyboard is shown under a drawn card: draw the next one, skip this
// one, or go to filters. Every label comes from the bundle via the
// "play.next" / "play.skip" / "play.filters" keys so the JSON files stay
// the single source of truth for user-facing text.
func CardKeyboard(bundle *i18n.Bundle, language string) telegram.InlineKeyboardMarkup {
	nextText := bundle.Text(language, "play.next")
	skipText := bundle.Text(language, "play.skip")
	filtersText := bundle.Text(language, "play.filters")

	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: nextText, CallbackData: "play:next"},
			{Text: skipText, CallbackData: "play:skip"},
		},
		{{Text: filtersText, CallbackData: "play:filters"}},
		{{Text: menuButtonText(language), CallbackData: "menu:main"}},
	}}
}

// HubKeyboard is the module's entry screen: draw the first card, adjust
// filters, or back to the app's main menu. Its primary button carries
// "play:next" — the same callback CardKeyboard's own "Next" button uses —
// not "play:open": the hub itself is reached via "play:open" (the
// module's entry callback, see internal/app/modules.go's
// CallbackPrefix+"open"), so wiring the hub's own button to that same
// callback would just redisplay the hub forever, with no way to ever reach
// a card. It reuses the "play.next" label ("▶ Далі") for the opening draw
// rather than a dedicated "start" key: the same word carries the reader
// from the hub into the first card and from card to card afterward, so the
// button never has to say something different once play is underway — see
// internal/play/handler.go's showCard, which treats this exactly like any
// other play:next draw (including flipping whose turn is next).
func HubKeyboard(bundle *i18n.Bundle, language string) telegram.InlineKeyboardMarkup {
	openText := bundle.Text(language, "play.next")
	filtersText := bundle.Text(language, "play.filters")

	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: openText, CallbackData: "play:next"}},
		{{Text: filtersText, CallbackData: "play:filters"}},
		{{Text: menuButtonText(language), CallbackData: "menu:main"}},
	}}
}

// includesValue reports whether want is present in list.
func includesValue(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// filterRow builds one row of toggle buttons for a single facet's fixed
// value set, marking values already present in filter.Include[facet] with a
// leading checkmark, mirroring how internal/positions/keyboards.go's
// FiltersKeyboard marks active values.
func filterRow(bundle *i18n.Bundle, language string, filter catalog.Filter, facet string, values []string) []telegram.InlineKeyboardButton {
	row := make([]telegram.InlineKeyboardButton, 0, len(values))
	for _, value := range values {
		label := facetLabel(bundle, language, facet, value)
		if includesValue(filter.Include[facet], value) {
			label = "✓ " + label
		}
		row = append(row, telegram.InlineKeyboardButton{
			Text:         label,
			CallbackData: "play:filter:" + facet + ":" + value,
		})
	}
	return row
}

// FiltersKeyboard renders one toggle button per (facet, value) pair over
// play's two fixed facets — kind (truth/dare) and intensity
// (gentle/medium/bold) — followed by the escape row back to the main menu.
// Button text is display-only and localized via facetLabel; callback data
// always carries the raw facet/value slugs so filter state round-trips
// unchanged. The longest callback this emits is
// "play:filter:intensity:gentle" at 28 bytes, comfortably inside Telegram's
// 64-byte callback_data cap.
func FiltersKeyboard(bundle *i18n.Bundle, language string, filter catalog.Filter) telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		filterRow(bundle, language, filter, "kind", kindValues),
		filterRow(bundle, language, filter, "intensity", intensityValues),
		{{Text: menuButtonText(language), CallbackData: "menu:main"}},
	}}
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

// ToggleFilterValue returns a copy of filter with value added to (or
// removed from) filter.Include[facet]. It never mutates its argument:
// GameState is round-tripped through Redis by value, and a caller that
// stored the pre-toggle filter elsewhere must not see it change out from
// under them. This mirrors internal/positions/keyboards.go's
// ToggleFilterValue exactly.
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
