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
