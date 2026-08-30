package positions_test

// This file collects the shared test doubles and the two integration tests
// added for the final whole-branch review: the randomiser must vary between
// consecutive taps (finding 2), and a callback from a shared chat must never
// deliver content to that chat (finding 3). Both need a full HandleCallback
// round trip rather than a single pure function, so they live together here
// rather than alongside the narrower unit tests in handler_test.go and
// service_test.go.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/positions"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// botCall records one invocation made against recordingBot, so tests can
// assert both which chat was touched and what was said, across every Bot
// method rather than just SendMessage.
type botCall struct {
	method string
	chatID int64
	text   string
}

type recordingBot struct {
	calls []botCall
}

func (b *recordingBot) SendMessage(_ context.Context, chatID int64, text string, _ any) error {
	b.calls = append(b.calls, botCall{"SendMessage", chatID, text})
	return nil
}

func (b *recordingBot) EditMessageText(_ context.Context, chatID, _ int64, text string, _ any) error {
	b.calls = append(b.calls, botCall{"EditMessageText", chatID, text})
	return nil
}

func (b *recordingBot) SendPhotoBytes(_ context.Context, chatID int64, _ []byte, caption string, _ any) (telegram.SentPhoto, error) {
	b.calls = append(b.calls, botCall{"SendPhotoBytes", chatID, caption})
	return telegram.SentPhoto{FileID: "fid"}, nil
}

func (b *recordingBot) SendPhotoRef(_ context.Context, chatID int64, _ string, caption string, _ any) (telegram.SentPhoto, error) {
	b.calls = append(b.calls, botCall{"SendPhotoRef", chatID, caption})
	return telegram.SentPhoto{FileID: "fid"}, nil
}

func (b *recordingBot) EditMessageMediaRef(_ context.Context, chatID, _ int64, _ string, caption string, _ any) error {
	b.calls = append(b.calls, botCall{"EditMessageMediaRef", chatID, caption})
	return nil
}

func (b *recordingBot) DeleteMessage(_ context.Context, chatID, _ int64) error {
	b.calls = append(b.calls, botCall{"DeleteMessage", chatID, ""})
	return nil
}

// noPairRepo reports no active pair for anyone, so pairAndMarks always
// short-circuits to an empty (non-nil) marks map without needing further
// stubbing.
type noPairRepo struct{}

func (noPairRepo) ActivePairForUser(context.Context, int64) (*storage.Pair, error) { return nil, nil }
func (noPairRepo) PairPositionMarks(context.Context, int64) (map[string]storage.PositionMark, error) {
	return map[string]storage.PositionMark{}, nil
}
func (noPairRepo) TogglePositionMark(context.Context, int64, string, storage.PositionMarkKind, int64, time.Time) (bool, error) {
	return false, nil
}
func (noPairRepo) UserLanguage(context.Context, int64) (string, error) { return "uk", nil }

// The three wish-related methods below exist only so noPairRepo keeps
// satisfying Repository after Task 5 extended it; none of the integration
// tests in this file touch wishes, so every stub is a harmless zero value.
func (noPairRepo) SetWishAnswer(context.Context, int64, storage.WishItemKind, string, storage.WishAnswer, time.Time) error {
	return nil
}

func (noPairRepo) UserWishAnswers(context.Context, int64) (map[string]storage.WishAnswer, error) {
	return map[string]storage.WishAnswer{}, nil
}

func (noPairRepo) PairWishMatches(context.Context, int64) ([]storage.WishMatch, error) {
	return nil, nil
}

// wishRepo is a configurable Repository double for the wish-button and
// matches-only tests below: unlike noPairRepo (always solo) and runStubRepo
// (never touches wishes), it can be set up with a pair, marks, matches and
// pre-existing wish answers, and it records every SetWishAnswer call so
// tests can assert on exactly what was written.
type wishRepo struct {
	pair        *storage.Pair
	marks       map[string]storage.PositionMark
	wishAnswers map[string]storage.WishAnswer
	matches     []storage.WishMatch
	setCalls    []storage.WishAnswer
}

func (r *wishRepo) ActivePairForUser(context.Context, int64) (*storage.Pair, error) {
	return r.pair, nil
}

func (r *wishRepo) PairPositionMarks(context.Context, int64) (map[string]storage.PositionMark, error) {
	if r.marks == nil {
		return map[string]storage.PositionMark{}, nil
	}
	return r.marks, nil
}

func (r *wishRepo) TogglePositionMark(context.Context, int64, string, storage.PositionMarkKind, int64, time.Time) (bool, error) {
	return false, nil
}

func (r *wishRepo) UserLanguage(context.Context, int64) (string, error) { return "uk", nil }

func (r *wishRepo) SetWishAnswer(_ context.Context, _ int64, kind storage.WishItemKind, itemID string, answer storage.WishAnswer, _ time.Time) error {
	if r.wishAnswers == nil {
		r.wishAnswers = map[string]storage.WishAnswer{}
	}
	r.wishAnswers[string(kind)+":"+itemID] = answer
	r.setCalls = append(r.setCalls, answer)
	return nil
}

func (r *wishRepo) UserWishAnswers(context.Context, int64) (map[string]storage.WishAnswer, error) {
	out := make(map[string]storage.WishAnswer, len(r.wishAnswers))
	for k, v := range r.wishAnswers {
		out[k] = v
	}
	return out, nil
}

func (r *wishRepo) PairWishMatches(context.Context, int64) ([]storage.WishMatch, error) {
	return r.matches, nil
}

// markupCall is botCall's counterpart that also keeps the keyboard markup a
// Bot call carried, for tests that need to assert on button text or
// callback_data rather than just the caption/text recordingBot captures.
type markupCall struct {
	method string
	chatID int64
	text   string
	markup telegram.InlineKeyboardMarkup
}

// markupRecordingBot is a second, separate Bot double (rather than adding a
// markup field to recordingBot's botCall) precisely so recordingBot's own
// struct and every pre-existing test constructing a botCall{...} literal
// positionally stay untouched.
type markupRecordingBot struct {
	calls []markupCall
}

func toInlineMarkup(v any) telegram.InlineKeyboardMarkup {
	if m, ok := v.(telegram.InlineKeyboardMarkup); ok {
		return m
	}
	return telegram.InlineKeyboardMarkup{}
}

func (b *markupRecordingBot) SendMessage(_ context.Context, chatID int64, text string, markup any) error {
	b.calls = append(b.calls, markupCall{"SendMessage", chatID, text, toInlineMarkup(markup)})
	return nil
}

func (b *markupRecordingBot) EditMessageText(_ context.Context, chatID, _ int64, text string, markup any) error {
	b.calls = append(b.calls, markupCall{"EditMessageText", chatID, text, toInlineMarkup(markup)})
	return nil
}

func (b *markupRecordingBot) SendPhotoBytes(_ context.Context, chatID int64, _ []byte, caption string, markup any) (telegram.SentPhoto, error) {
	b.calls = append(b.calls, markupCall{"SendPhotoBytes", chatID, caption, toInlineMarkup(markup)})
	return telegram.SentPhoto{FileID: "fid"}, nil
}

func (b *markupRecordingBot) SendPhotoRef(_ context.Context, chatID int64, _ string, caption string, markup any) (telegram.SentPhoto, error) {
	b.calls = append(b.calls, markupCall{"SendPhotoRef", chatID, caption, toInlineMarkup(markup)})
	return telegram.SentPhoto{FileID: "fid"}, nil
}

func (b *markupRecordingBot) EditMessageMediaRef(_ context.Context, chatID, _ int64, _ string, caption string, markup any) error {
	b.calls = append(b.calls, markupCall{"EditMessageMediaRef", chatID, caption, toInlineMarkup(markup)})
	return nil
}

func (b *markupRecordingBot) DeleteMessage(_ context.Context, chatID, _ int64) error {
	b.calls = append(b.calls, markupCall{"DeleteMessage", chatID, "", telegram.InlineKeyboardMarkup{}})
	return nil
}

// lastMarkup returns the markup from the most recent call markupRecordingBot
// recorded, so a test can assert on the keyboard the current screen ended up
// with regardless of which particular Bot method rendered it.
func (b *markupRecordingBot) lastMarkup() telegram.InlineKeyboardMarkup {
	if len(b.calls) == 0 {
		return telegram.InlineKeyboardMarkup{}
	}
	return b.calls[len(b.calls)-1].markup
}

func (b *markupRecordingBot) lastText() string {
	if len(b.calls) == 0 {
		return ""
	}
	return b.calls[len(b.calls)-1].text
}

// buttonByCallback finds a button by exact callback_data across every row
// of markup, or reports found=false.
func buttonByCallback(markup telegram.InlineKeyboardMarkup, callback string) (telegram.InlineKeyboardButton, bool) {
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == callback {
				return button, true
			}
		}
	}
	return telegram.InlineKeyboardButton{}, false
}

// TestHandleWishRecordsAnswerSoloAndMarksTheCard pins both halves of the
// wish button: it writes wish_answers with item_kind='position' via
// storage.SetWishAnswer even with no active pair (wishes are personal, and
// this module never gates them behind a pair the way marks are), and the
// re-rendered card shows the just-recorded answer with a selection marker.
func TestHandleWishRecordsAnswerSoloAndMarksTheCard(t *testing.T) {
	cat := manyPositionItems(1)
	bot := &markupRecordingBot{}
	repo := &wishRepo{}
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: repo,
		Bot:        bot,
		State:      newMemStateStore(),
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(30001)
	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:wish:001:want"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:wish:001:want: %v", err)
	}

	if got := repo.wishAnswers["position:001"]; got != storage.AnswerWant {
		t.Fatalf("SetWishAnswer wrote %q for position:001, want %q", got, storage.AnswerWant)
	}

	button, found := buttonByCallback(bot.lastMarkup(), "pos:wish:001:want")
	if !found {
		t.Fatalf("re-rendered keyboard %+v is missing the pos:wish:001:want button", bot.lastMarkup().InlineKeyboard)
	}
	if !strings.Contains(button.Text, "✓") {
		t.Fatalf("just-recorded wish answer button %q has no selection marker", button.Text)
	}
}

// TestMatchesOnlyFilterNarrowsBrowseToMatchedItems is the payoff scenario
// from the task brief: with MatchesOnly saved in BrowseState and a pair with
// exactly one matched position, pos:browse:0 must land on that matched item
// and report a 1-item total, even though the underlying catalog has three.
func TestMatchesOnlyFilterNarrowsBrowseToMatchedItems(t *testing.T) {
	cat := manyPositionItems(3)
	bot := &markupRecordingBot{}
	repo := &wishRepo{
		pair:    &storage.Pair{ID: 77},
		matches: []storage.WishMatch{{ItemKind: storage.WishKindPosition, ItemID: "002"}},
	}
	state := newMemStateStore()
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: repo,
		Bot:        bot,
		State:      state,
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(30002)
	encoded, err := positions.EncodeState(positions.BrowseState{MatchesOnly: true})
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if err := state.SetModuleState(context.Background(), userID, "positions", encoded, time.Hour); err != nil {
		t.Fatalf("seed module state: %v", err)
	}

	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:browse:0"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:browse:0: %v", err)
	}

	text := bot.lastText()
	if !strings.Contains(text, "Позиція 002") {
		t.Fatalf("browse text %q does not show the one matched item (002)", text)
	}
	if !strings.Contains(text, "1/1") {
		t.Fatalf("browse text %q does not report a 1-item total under MatchesOnly", text)
	}
}

// TestMatchesOnlyWithZeroMatchesShowsExplanationNotADeadEnd pins the
// self-review requirement: MatchesOnly with zero matches must not render an
// unexplained empty screen. It must use the matches-specific empty copy
// (distinct from the generic "no results" text) and still offer a way out
// via HubKeyboard's own Filters button.
func TestMatchesOnlyWithZeroMatchesShowsExplanationNotADeadEnd(t *testing.T) {
	cat := manyPositionItems(3)
	bot := &markupRecordingBot{}
	repo := &wishRepo{pair: &storage.Pair{ID: 78}}
	state := newMemStateStore()
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: repo,
		Bot:        bot,
		State:      state,
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(30003)
	encoded, err := positions.EncodeState(positions.BrowseState{MatchesOnly: true})
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if err := state.SetModuleState(context.Background(), userID, "positions", encoded, time.Hour); err != nil {
		t.Fatalf("seed module state: %v", err)
	}

	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:browse:0"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:browse:0: %v", err)
	}

	text := bot.lastText()
	if text == "" {
		t.Fatal("MatchesOnly with zero matches produced no text at all")
	}
	if _, found := buttonByCallback(bot.lastMarkup(), "pos:filters"); !found {
		t.Fatalf("empty matches-only screen %+v offers no way to reach Filters and turn the toggle off", bot.lastMarkup().InlineKeyboard)
	}
}

// TestHandleMatchesToggleFlipsStateAndPersists pins that pos:matches:toggle
// actually flips BrowseState.MatchesOnly and that the flip survives a
// second, independent HandleCallback round trip through the same
// StateStore-backed persistence every other piece of BrowseState already
// relies on.
func TestHandleMatchesToggleFlipsStateAndPersists(t *testing.T) {
	cat := manyPositionItems(3)
	bot := &markupRecordingBot{}
	repo := &wishRepo{pair: &storage.Pair{ID: 79}}
	state := newMemStateStore()
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: repo,
		Bot:        bot,
		State:      state,
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(30004)
	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:matches:toggle"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:matches:toggle: %v", err)
	}

	raw, err := state.ModuleState(context.Background(), userID, "positions")
	if err != nil {
		t.Fatalf("ModuleState: %v", err)
	}
	decoded, err := positions.DecodeState(raw)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if !decoded.MatchesOnly {
		t.Fatal("pos:matches:toggle did not persist MatchesOnly=true")
	}

	button, found := buttonByCallback(bot.lastMarkup(), "pos:matches:toggle")
	if !found {
		t.Fatalf("filters keyboard %+v is missing the pos:matches:toggle button", bot.lastMarkup().InlineKeyboard)
	}
	if !strings.Contains(button.Text, "✓") {
		t.Fatalf("matches-only toggle button %q has no selection marker after being turned on", button.Text)
	}
}

// TestHandleMatchesToggleWithoutAPairIsANoOp pins the lock semantics end to
// end: a pos:matches:toggle callback reaching the handler without an active
// pair (e.g. a stale callback from before the pair was dissolved) must not
// flip MatchesOnly — the UI never offers an interactive toggle without a
// pair in the first place.
func TestHandleMatchesToggleWithoutAPairIsANoOp(t *testing.T) {
	cat := manyPositionItems(3)
	bot := &markupRecordingBot{}
	repo := &wishRepo{}
	state := newMemStateStore()
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: repo,
		Bot:        bot,
		State:      state,
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(30005)
	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:matches:toggle"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:matches:toggle: %v", err)
	}

	raw, err := state.ModuleState(context.Background(), userID, "positions")
	if err != nil {
		t.Fatalf("ModuleState: %v", err)
	}
	if raw != "" {
		decoded, err := positions.DecodeState(raw)
		if err != nil {
			t.Fatalf("DecodeState: %v", err)
		}
		if decoded.MatchesOnly {
			t.Fatal("pos:matches:toggle flipped MatchesOnly=true for a solo user; the toggle must be a no-op without a pair")
		}
	}

	button, found := buttonByCallback(bot.lastMarkup(), "pos:matches:toggle")
	if !found {
		t.Fatalf("filters keyboard %+v is missing the pos:matches:toggle button", bot.lastMarkup().InlineKeyboard)
	}
	if !strings.Contains(button.Text, "🔒") {
		t.Fatalf("matches-only toggle button %q has no lock marker for a solo user", button.Text)
	}
}

// memStateStore is a minimal in-process StateStore so BrowseState (and, in
// particular, its Draw counter) actually persists across calls the way
// Redis would in production. Without it, loadState resets to the zero value
// on every call and the randomiser fix could not be observed end-to-end.
type memStateStore struct {
	byUser map[int64]string
}

func newMemStateStore() *memStateStore { return &memStateStore{byUser: map[int64]string{}} }

func (m *memStateStore) SetModuleState(_ context.Context, userID int64, _, value string, _ time.Duration) error {
	m.byUser[userID] = value
	return nil
}

func (m *memStateStore) ModuleState(_ context.Context, userID int64, _ string) (string, error) {
	return m.byUser[userID], nil
}

func (m *memStateStore) CacheFileID(context.Context, string, string, time.Duration) error { return nil }
func (m *memStateStore) FileID(context.Context, string) (string, error)                   { return "", nil }

// recordingObjectStore is a minimal ObjectStore that records every key
// requested via Get and always returns a small fixed payload, so a test can
// assert exactly which object-store key the handler asked for without
// needing a real MinIO, and so presentCard's cache-miss ladder proceeds all
// the way to SendPhotoBytes instead of falling back to a text-only screen.
type recordingObjectStore struct {
	keys []string
}

func (o *recordingObjectStore) Get(_ context.Context, objectKey string) ([]byte, error) {
	o.keys = append(o.keys, objectKey)
	return []byte("fake-image-bytes"), nil
}

// mediaCatalog builds a one-item catalog whose media key matches exactly
// what a real crawl produces: content/positions.v1.json stores
// "positions/NNN.png", baked in at crawl time by cmd/ingest-positions.
func mediaCatalog(id, mediaKey string) *catalog.Catalog {
	return &catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:    id,
				Text:  map[string]catalog.ItemText{"uk": {Title: "Позиція " + id}},
				Media: &catalog.MediaRef{Key: mediaKey},
			},
		},
	}
}

// TestPresentCardComposesKeyFromConfiguredPrefix pins the fix for the
// documented follow-up that positions.Handler trusted the catalog's
// baked-in media.key verbatim instead of composing it from the configured
// prefix. cmd/ingest-positions's seedCatalog writes uploads under
// POSITIONS_PREFIX + the image's file name (see cmd/ingest-positions/
// main.go's seedCatalog and objectKey); this pins that the handler's read
// path now asks the object store for that exact same key rather than for
// the catalog's baked-in "positions/007.png" — the bug this fix closes.
func TestPresentCardComposesKeyFromConfiguredPrefix(t *testing.T) {
	cat := mediaCatalog("007", "positions/007.png")
	bot := &recordingBot{}
	store := &recordingObjectStore{}
	h := positions.NewHandler(positions.HandlerOptions{
		Service:     positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:     cat,
		Repository:  noPairRepo{},
		Bot:         bot,
		State:       newMemStateStore(),
		ObjectStore: store,
		I18n:        i18n.NewBundle(),
		Prefix:      "custom/dir/",
	})

	cb := &telegram.CallbackQuery{From: telegram.User{ID: 1}, Data: "pos:browse:0"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:browse:0: %v", err)
	}

	if len(store.keys) != 1 || store.keys[0] != "custom/dir/007.png" {
		t.Fatalf("object store keys = %v, want exactly [\"custom/dir/007.png\"]", store.keys)
	}
}

// TestPresentCardEmptyPrefixKeepsMediaKeyVerbatim pins the other half of the
// same fix: an unconfigured Prefix (the zero value, e.g. an older deployment
// that never sets it) must keep reading the catalog's baked-in media.key
// unchanged, so existing deployments are unaffected by this change.
func TestPresentCardEmptyPrefixKeepsMediaKeyVerbatim(t *testing.T) {
	cat := mediaCatalog("007", "positions/007.png")
	bot := &recordingBot{}
	store := &recordingObjectStore{}
	h := positions.NewHandler(positions.HandlerOptions{
		Service:     positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:     cat,
		Repository:  noPairRepo{},
		Bot:         bot,
		State:       newMemStateStore(),
		ObjectStore: store,
		I18n:        i18n.NewBundle(),
	})

	cb := &telegram.CallbackQuery{From: telegram.User{ID: 1}, Data: "pos:browse:0"}
	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback pos:browse:0: %v", err)
	}

	if len(store.keys) != 1 || store.keys[0] != "positions/007.png" {
		t.Fatalf("object store keys = %v, want exactly [\"positions/007.png\"]", store.keys)
	}
}

// manyPositionItems builds a catalog with n uniquely-titled items and no
// media, so every card renders as a distinct text-only message.
func manyPositionItems(n int) *catalog.Catalog {
	items := make([]catalog.Item, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%03d", i+1)
		items = append(items, catalog.Item{
			ID:   id,
			Text: map[string]catalog.ItemText{"uk": {Title: "Позиція " + id}},
		})
	}
	return &catalog.Catalog{Kind: "positions", Version: 1, Items: items}
}

// TestConsecutivePosRandomTapsVaryTheItem pins the fix for the review
// finding that a solo user (no marks, since marks require a pair) tapping
// pos:random repeatedly got the exact same item forever: Random was pure in
// (seed, bucket, cycle, items, seen), and none of those changed between two
// presses with nothing else different. Before the fix this failed reliably
// — every one of the 6 taps produced the identical card.
func TestConsecutivePosRandomTapsVaryTheItem(t *testing.T) {
	cat := manyPositionItems(30)
	bot := &recordingBot{}
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: noPairRepo{},
		Bot:        bot,
		State:      newMemStateStore(),
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(20260830)
	for i := 0; i < 6; i++ {
		cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:random"}
		if err := h.HandleCallback(context.Background(), cb); err != nil {
			t.Fatalf("HandleCallback pos:random (tap %d): %v", i, err)
		}
	}

	var texts []string
	for _, c := range bot.calls {
		if c.method == "SendMessage" {
			texts = append(texts, c.text)
		}
	}
	if len(texts) != 6 {
		t.Fatalf("got %d SendMessage calls, want 6: %v", len(texts), bot.calls)
	}
	distinct := map[string]bool{}
	for _, text := range texts {
		distinct[text] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("6 consecutive pos:random taps all produced the same card %q; the randomiser must vary between presses", texts[0])
	}
}

// TestHandleCallbackRefusesNonPrivateChats pins the fix for the review
// finding that explicit content was sent to whatever chat a callback came
// from: the maturity gate resolves the tapping USER, not the chat, so one
// 18+ opted-in member in a group could otherwise dump the whole catalog into
// a shared chat. Before the fix, a pos:random tap from a group rendered
// straight into that group.
func TestHandleCallbackRefusesNonPrivateChats(t *testing.T) {
	cat := manyPositionItems(5)
	bot := &recordingBot{}
	h := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: noPairRepo{},
		Bot:        bot,
		State:      newMemStateStore(),
		I18n:       i18n.NewBundle(),
	})

	const userID = int64(777)
	const groupChatID = int64(-100123456)
	cb := &telegram.CallbackQuery{
		From: telegram.User{ID: userID},
		Message: &telegram.Message{
			MessageID: 1,
			Chat:      telegram.Chat{ID: groupChatID, Type: "group"},
		},
		Data: "pos:random",
	}

	if err := h.HandleCallback(context.Background(), cb); err != nil {
		t.Fatalf("HandleCallback from a group chat: %v", err)
	}

	for _, c := range bot.calls {
		if c.chatID == groupChatID {
			t.Fatalf("HandleCallback delivered content to the group chat %d via %s(%q); a non-private callback must be refused outright", groupChatID, c.method, c.text)
		}
	}
	if len(bot.calls) != 0 {
		t.Fatalf("HandleCallback made %d bot call(s) for a non-private callback, want 0: %+v", len(bot.calls), bot.calls)
	}
}
