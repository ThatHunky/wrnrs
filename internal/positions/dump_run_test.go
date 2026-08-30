package positions

// This file is a white-box (package positions, not positions_test) test
// file so it can reach the unexported h.runs map directly. That is
// deliberately narrower than adding a test-only exported accessor method to
// handler.go itself: the run map stays purely an implementation detail, and
// only this one test file needs to reach past it.

import (
	"context"
	"errors"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// runStubBot is a minimal Bot implementation for exercising startDump. Only
// SendMessage's behaviour is configurable; every other method is a no-op
// satisfying the interface, since startDump's confirmation always goes
// through presentText -> SendMessage when the triggering callback carries no
// message to edit.
type runStubBot struct {
	sendErr error
}

func (b *runStubBot) SendMessage(context.Context, int64, string, any) error {
	return b.sendErr
}

func (b *runStubBot) EditMessageText(context.Context, int64, int64, string, any) error {
	return errors.New("not used in this test")
}

func (b *runStubBot) SendPhotoBytes(context.Context, int64, []byte, string, any) (telegram.SentPhoto, error) {
	return telegram.SentPhoto{}, errors.New("not used in this test")
}

func (b *runStubBot) SendPhotoRef(context.Context, int64, string, string, any) (telegram.SentPhoto, error) {
	return telegram.SentPhoto{}, errors.New("not used in this test")
}

func (b *runStubBot) EditMessageMediaRef(context.Context, int64, int64, string, string, any) error {
	return errors.New("not used in this test")
}

func (b *runStubBot) DeleteMessage(context.Context, int64, int64) error {
	return nil
}

// runStubRepo reports no active pair, so pairAndMarks short-circuits to an
// empty (non-nil) marks map without needing a PairPositionMarks stub.
type runStubRepo struct{}

func (runStubRepo) ActivePairForUser(context.Context, int64) (*storage.Pair, error) {
	return nil, nil
}

func (runStubRepo) PairPositionMarks(context.Context, int64) (map[string]storage.PositionMark, error) {
	return map[string]storage.PositionMark{}, nil
}

func (runStubRepo) TogglePositionMark(context.Context, int64, string, storage.PositionMarkKind, int64, time.Time) (bool, error) {
	return false, nil
}

func (runStubRepo) UserLanguage(context.Context, int64) (string, error) {
	return "uk", nil
}

func newRunTestHandler(t *testing.T, sendErr error) *Handler {
	t.Helper()
	cat := &catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{ID: "001", Text: map[string]catalog.ItemText{"uk": {Title: "перша"}}},
			{ID: "002", Text: map[string]catalog.ItemText{"uk": {Title: "друга"}}},
		},
	}
	return NewHandler(HandlerOptions{
		Service:    NewService(ServiceOptions{Catalog: cat}),
		Catalog:    cat,
		Repository: runStubRepo{},
		Bot:        &runStubBot{sendErr: sendErr},
		I18n:       i18n.NewBundle(),
		// Two items plus a huge interval: on success, runDump's goroutine
		// sends the first item and then blocks in the (real, context-aware)
		// sleep before the second — it cannot possibly reach its deferred
		// cleanup before the test gets to assert on h.runs. That keeps the
		// success test deterministic instead of racing the goroutine.
		DumpInterval: time.Hour,
	})
}

// runCount is a tiny, test-only helper reading the unexported run map under
// the handler's own lock, mirroring how stopDump reads it in production.
func (h *Handler) runCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.runs)
}

func (h *Handler) hasRun(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.runs[userID]
	return ok
}

// TestStartDumpDoesNotTrackARunWhenTheConfirmationSendFails pins the fix for
// the review finding: if presentText's SendMessage call fails, startDump
// must return that error WITHOUT leaving an entry in h.runs behind. Before
// the fix, the map entry was installed before presentText ran, so a failed
// confirmation left a stale cancel func in the map forever — a later
// pos:dump:stop for that user would cancel a context nobody listens to and
// silently do nothing.
func TestStartDumpDoesNotTrackARunWhenTheConfirmationSendFails(t *testing.T) {
	h := newRunTestHandler(t, errors.New("telegram: transient failure"))
	const userID = int64(4242)
	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:dump:go"}

	err := h.startDump(context.Background(), cb, userID, userID, "uk")
	if err == nil {
		t.Fatal("startDump succeeded despite the confirmation send failing, want the error surfaced")
	}

	if h.hasRun(userID) {
		t.Fatalf("h.runs still tracks user %d after the confirmation send failed; the entry must never be installed for a run that did not start", userID)
	}
	if n := h.runCount(); n != 0 {
		t.Fatalf("h.runs has %d entries after a failed startDump, want 0", n)
	}
}

// TestStartDumpTracksARunOnSuccess is the counterpart that keeps the test
// above honest: it proves a normal, successful startDump DOES install an
// entry, so the failure-path test cannot pass merely because nothing is ever
// installed.
func TestStartDumpTracksARunOnSuccess(t *testing.T) {
	h := newRunTestHandler(t, nil)
	const userID = int64(4343)
	cb := &telegram.CallbackQuery{From: telegram.User{ID: userID}, Data: "pos:dump:go"}

	if err := h.startDump(context.Background(), cb, userID, userID, "uk"); err != nil {
		t.Fatalf("startDump: %v", err)
	}

	if !h.hasRun(userID) {
		t.Fatalf("h.runs does not track user %d after a successful startDump", userID)
	}

	// Clean up the goroutine startDump launched so it does not outlive the
	// test.
	h.mu.Lock()
	cancel, ok := h.runs[userID]
	h.mu.Unlock()
	if ok {
		cancel()
	}
}
