package play_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/play"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
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

// --- Handler routing tests (no Telegram network involved: fakePlayBot,
// fakePlayRepo and fakePlayState are in-memory stubs, in the same style as
// internal/wishlist/handler_test.go's fakeBot/fakeWishRepo) ----------------

// handlerCatalog is deliberately distinct from service_test.go's own
// testCatalog: both live in the same play_test package, and this file must
// not touch that one (see the brief: "Do not restructure ... the tests
// already in handler_test.go").
func handlerCatalog() *catalog.Catalog {
	items := []catalog.Item{}
	for _, spec := range []struct {
		id, kind, intensity string
	}{
		{"h001", "truth", "gentle"},
		{"h002", "dare", "gentle"},
		{"h003", "truth", "medium"},
	} {
		items = append(items, catalog.Item{
			ID:     spec.id,
			Facets: map[string][]string{"kind": {spec.kind}, "intensity": {spec.intensity}},
			Text:   map[string]catalog.ItemText{"uk": {Title: spec.id, Body: "текст " + spec.id}},
		})
	}
	return &catalog.Catalog{Kind: "play", Version: 1, Items: items}
}

func handlerBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"play.next":             "▶ Далі",
		"play.skip":             "⏭ Пропустити",
		"play.filters":          "☰ Фільтри",
		"play.hub.title":        "Правда або дія",
		"play.hub.intro":        "Тягніть картки по черзі.",
		"play.hub.solo_hint":    "Коли зʼявиться пара, картки звертатимуться на імена.",
		"play.filters.title":    "Фільтри",
		"play.empty":            "За такими фільтрами карток немає.",
		"play.kind.truth":       "Правда",
		"play.kind.dare":        "Дія",
		"play.intensity.gentle": "Мʼяко",
		"play.intensity.medium": "Середньо",
		"menu.partner_fallback": "Партнер",
	}})
	return b
}

type fakePlayBot struct {
	sentTexts   []string
	editedTexts []string
	deletedIDs  []int64
}

func (b *fakePlayBot) SendMessage(_ context.Context, _ int64, text string, _ any) error {
	b.sentTexts = append(b.sentTexts, text)
	return nil
}

func (b *fakePlayBot) EditMessageText(_ context.Context, _, _ int64, text string, _ any) error {
	b.editedTexts = append(b.editedTexts, text)
	return nil
}

func (b *fakePlayBot) DeleteMessage(_ context.Context, _, messageID int64) error {
	b.deletedIDs = append(b.deletedIDs, messageID)
	return nil
}

func lastText(bot *fakePlayBot) string {
	if len(bot.editedTexts) > 0 {
		return bot.editedTexts[len(bot.editedTexts)-1]
	}
	if len(bot.sentTexts) > 0 {
		return bot.sentTexts[len(bot.sentTexts)-1]
	}
	return ""
}

type fakePlayRepo struct {
	pair     *storage.Pair
	names    map[int64]string
	language string

	activePairCalls  int
	displayNameCalls []int64
}

func (r *fakePlayRepo) ActivePairForUser(_ context.Context, _ int64) (*storage.Pair, error) {
	r.activePairCalls++
	return r.pair, nil
}

func (r *fakePlayRepo) UserDisplayName(_ context.Context, telegramID int64) (string, error) {
	r.displayNameCalls = append(r.displayNameCalls, telegramID)
	return r.names[telegramID], nil
}

func (r *fakePlayRepo) UserLanguage(_ context.Context, _ int64) (string, error) {
	return r.language, nil
}

// fakePlayState keys by userID alone (never by module name): every test in
// this file drives exactly one module (play), so there is nothing to
// disambiguate, and hardcoding the handler's private module-state key here
// would couple the test to an implementation detail it has no business
// knowing.
type fakePlayState struct {
	values   map[int64]string
	setCalls int
}

func (s *fakePlayState) SetModuleState(_ context.Context, userID int64, _, value string, _ time.Duration) error {
	if s.values == nil {
		s.values = map[int64]string{}
	}
	s.values[userID] = value
	s.setCalls++
	return nil
}

func (s *fakePlayState) ModuleState(_ context.Context, userID int64, _ string) (string, error) {
	return s.values[userID], nil
}

// newPlayHandler builds a Handler with no StateStore wired at all — a true
// nil interface, not a typed nil pointer wrapped in one — mirroring how the
// live bot's own module wiring leaves State unset until Redis is available.
func newPlayHandler(repo *fakePlayRepo, bot *fakePlayBot) *play.Handler {
	service := play.NewService(play.ServiceOptions{Catalog: handlerCatalog()})
	return play.NewHandler(play.HandlerOptions{
		Service:    service,
		Repository: repo,
		Bot:        bot,
		I18n:       handlerBundle(),
	})
}

func newPlayHandlerWithState(repo *fakePlayRepo, bot *fakePlayBot, state *fakePlayState) *play.Handler {
	service := play.NewService(play.ServiceOptions{Catalog: handlerCatalog()})
	return play.NewHandler(play.HandlerOptions{
		Service:    service,
		Repository: repo,
		Bot:        bot,
		State:      state,
		I18n:       handlerBundle(),
	})
}

func playCallback(userID int64, data string) *telegram.CallbackQuery {
	return &telegram.CallbackQuery{
		From:    telegram.User{ID: userID},
		Message: &telegram.Message{MessageID: 10, Chat: telegram.Chat{ID: userID, Type: "private"}},
		Data:    data,
	}
}

// TestPlayNextFlipsTurnButSkipDoesNot pins the module's central rule: tapping
// "play:next" must flip whose turn is next, but tapping "play:skip" must
// not, or skipping becomes a way to dump the current card on your partner
// instead of drawing again yourself. Both start from the same fresh
// (TurnB=false) state so the only variable is which callback fires.
func TestPlayNextFlipsTurnButSkipDoesNot(t *testing.T) {
	pair := &storage.Pair{ID: 1, UserAID: 10, UserBID: 20}

	nextState := &fakePlayState{}
	nextHandler := newPlayHandlerWithState(&fakePlayRepo{pair: pair}, &fakePlayBot{}, nextState)
	if err := nextHandler.HandleCallback(context.Background(), playCallback(10, "play:next")); err != nil {
		t.Fatalf("HandleCallback(play:next): %v", err)
	}
	savedNext, ok := nextState.values[10]
	if !ok {
		t.Fatal("play:next did not save any state")
	}
	decodedNext, err := play.DecodeState(savedNext)
	if err != nil {
		t.Fatalf("decode saved state: %v", err)
	}
	if !decodedNext.TurnB {
		t.Fatal("play:next did not flip TurnB; the next card should now address the other partner")
	}

	skipState := &fakePlayState{}
	skipHandler := newPlayHandlerWithState(&fakePlayRepo{pair: pair}, &fakePlayBot{}, skipState)
	if err := skipHandler.HandleCallback(context.Background(), playCallback(10, "play:skip")); err != nil {
		t.Fatalf("HandleCallback(play:skip): %v", err)
	}
	savedSkip, ok := skipState.values[10]
	if !ok {
		t.Fatal("play:skip did not save any state")
	}
	decodedSkip, err := play.DecodeState(savedSkip)
	if err != nil {
		t.Fatalf("decode saved state: %v", err)
	}
	if decodedSkip.TurnB {
		t.Fatal("play:skip flipped TurnB; skipping must not push the turn onto the partner")
	}
}

// TestPlayHandleCallbackRefusesGroupChats pins the same guard
// internal/positions and internal/wishlist apply: a callback whose message
// came from a shared chat must never reach the repository, regardless of
// which play: screen it names — the maturity gate resolves the tapping
// user, not the chat, so a shared chat with one opted-in member would
// otherwise expose explicit content to everyone in it.
func TestPlayHandleCallbackRefusesGroupChats(t *testing.T) {
	for _, chatType := range []string{"group", "supergroup", "channel"} {
		t.Run(chatType, func(t *testing.T) {
			repo := &fakePlayRepo{}
			bot := &fakePlayBot{}
			h := newPlayHandler(repo, bot)
			cb := &telegram.CallbackQuery{
				From:    telegram.User{ID: 1},
				Message: &telegram.Message{MessageID: 10, Chat: telegram.Chat{ID: -100, Type: chatType}},
				Data:    "play:open",
			}

			if err := h.HandleCallback(context.Background(), cb); err != nil {
				t.Fatalf("HandleCallback: %v", err)
			}
			if repo.activePairCalls != 0 {
				t.Fatalf("group chat callback reached the repository: %d ActivePairForUser calls", repo.activePairCalls)
			}
			if len(bot.sentTexts)+len(bot.editedTexts) != 0 {
				t.Fatal("group chat callback produced a bot reply")
			}
		})
	}
}

// TestPlayCardCaptionWithoutPairHasNoActorName pins the solo behaviour: with
// no active pair, the card must carry no actor name — and the handler must
// not even bother resolving one, since there is nobody to resolve.
func TestPlayCardCaptionWithoutPairHasNoActorName(t *testing.T) {
	repo := &fakePlayRepo{pair: nil, names: map[int64]string{1: "Оля"}}
	bot := &fakePlayBot{}
	h := newPlayHandler(repo, bot)

	if err := h.HandleCallback(context.Background(), playCallback(1, "play:next")); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if len(repo.displayNameCalls) != 0 {
		t.Fatalf("a solo draw looked up a display name: %v", repo.displayNameCalls)
	}
	text := lastText(bot)
	if strings.Contains(text, "Оля") {
		t.Fatalf("solo card text %q names an actor that cannot exist without a pair", text)
	}
	if strings.HasPrefix(strings.TrimSpace(text), ":") {
		t.Fatalf("solo card text %q starts with an orphaned separator", text)
	}
}

// TestPlayNextOnEmptySelectionShowsPlayEmpty pins the rule that an empty
// filter selection must surface as the "play.empty" message, not a blank
// screen or a swallowed error.
func TestPlayNextOnEmptySelectionShowsPlayEmpty(t *testing.T) {
	encoded, err := play.EncodeState(play.GameState{
		Filter: catalog.Filter{Include: map[string][]string{"kind": {"nonexistent"}}},
	})
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	state := &fakePlayState{values: map[int64]string{1: encoded}}
	bot := &fakePlayBot{}
	h := newPlayHandlerWithState(&fakePlayRepo{}, bot, state)

	if err := h.HandleCallback(context.Background(), playCallback(1, "play:next")); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	text := lastText(bot)
	want := handlerBundle().Text("uk", "play.empty")
	if !strings.Contains(text, want) {
		t.Fatalf("empty-selection text %q does not contain play.empty (%q)", text, want)
	}
}

// TestPlayHandleMessageNeverConsumesText pins the rule that this module reads
// no free text: HandleMessage must always report (false, nil).
func TestPlayHandleMessageNeverConsumesText(t *testing.T) {
	h := newPlayHandler(&fakePlayRepo{}, &fakePlayBot{})
	handled, err := h.HandleMessage(context.Background(), &telegram.Message{Text: "hello"})
	if handled || err != nil {
		t.Fatalf("HandleMessage = (%v, %v), want (false, nil)", handled, err)
	}
}

// TestPlayFilterToggleNeverPanicsOnMalformedCallbacks pins the defensive
// parsing rule: "play:filter:", "play:filter:kind" (no value to split on)
// and "play:filter:bogus:x" (an unrecognised facet) must never panic, and
// must each re-render a screen rather than going silent.
func TestPlayFilterToggleNeverPanicsOnMalformedCallbacks(t *testing.T) {
	for _, data := range []string{"play:filter:", "play:filter:kind", "play:filter:bogus:x"} {
		t.Run(data, func(t *testing.T) {
			repo := &fakePlayRepo{}
			bot := &fakePlayBot{}
			h := newPlayHandler(repo, bot)

			if err := h.HandleCallback(context.Background(), playCallback(1, data)); err != nil {
				t.Fatalf("HandleCallback(%q): %v", data, err)
			}
			if len(bot.sentTexts)+len(bot.editedTexts) == 0 {
				t.Fatalf("HandleCallback(%q) produced no screen", data)
			}
		})
	}
}

// TestPlayHandlerToleratesNilStateStore drives every screen with no
// StateStore wired at all. Redis is never actually absent in the live bot,
// but the handler must still not dereference a nil StateStore: state simply
// stops being read or written, and the game stays playable (turn rotation
// degrades to always-the-same-actor, no-repeat degrades to always-fresh).
func TestPlayHandlerToleratesNilStateStore(t *testing.T) {
	repo := &fakePlayRepo{pair: &storage.Pair{ID: 1, UserAID: 10, UserBID: 20}}
	bot := &fakePlayBot{}
	h := newPlayHandler(repo, bot)

	for _, data := range []string{"play:open", "play:next", "play:skip", "play:filters", "play:filter:kind:dare"} {
		if err := h.HandleCallback(context.Background(), playCallback(10, data)); err != nil {
			t.Fatalf("HandleCallback(%q) with nil StateStore: %v", data, err)
		}
	}
	if len(bot.sentTexts)+len(bot.editedTexts) == 0 {
		t.Fatal("nil-StateStore flow produced no output")
	}
}
