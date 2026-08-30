package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wrnrs/internal/modules"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

func TestModuleUserStateReflectsMaturityPairAndPremium(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(4001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	state, err := a.moduleUserState(ctx, userID)
	if err != nil {
		t.Fatalf("moduleUserState: %v", err)
	}
	if state.Is18Plus || state.MatureOptIn || state.HasActivePair || state.HasPremium {
		t.Fatalf("fresh user state = %+v, want everything false", state)
	}

	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}
	if err := a.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   userID,
		Type:     storage.EntitlementPremiumAccess,
		UnlockID: storage.EntitlementPremiumAccess,
		Source:   "admin_grant",
	}); err != nil {
		t.Fatalf("GrantEntitlement: %v", err)
	}

	state, err = a.moduleUserState(ctx, userID)
	if err != nil {
		t.Fatalf("moduleUserState after grants: %v", err)
	}
	if !state.Is18Plus || !state.MatureOptIn || !state.HasPremium {
		t.Fatalf("state after grants = %+v, want 18+, mature and premium true", state)
	}
	if state.HasActivePair {
		t.Fatalf("state.HasActivePair = true, want false — no pair was created")
	}
}

// errModuleHandlerFailed lets tests prove that a handler error propagates
// instead of being swallowed by the dispatch loop.
var errModuleHandlerFailed = errors.New("module handler failed")

type recordingModuleHandler struct {
	callbacks []string
	messages  []string
	consume   bool
}

func (h *recordingModuleHandler) HandleCallback(_ context.Context, cb *telegram.CallbackQuery) error {
	h.callbacks = append(h.callbacks, cb.Data)
	return nil
}

func (h *recordingModuleHandler) HandleMessage(_ context.Context, msg *telegram.Message) (bool, error) {
	h.messages = append(h.messages, msg.Text)
	return h.consume, nil
}

// erroringModuleHandler always fails, so tests can check that dispatch stops
// and reports the update as handled instead of falling through silently.
type erroringModuleHandler struct {
	err error
}

func (h *erroringModuleHandler) HandleCallback(_ context.Context, _ *telegram.CallbackQuery) error {
	return h.err
}

func (h *erroringModuleHandler) HandleMessage(_ context.Context, _ *telegram.Message) (bool, error) {
	return false, h.err
}

func registerTestModule(t *testing.T, a *App, gate modules.Gate, handler modules.Handler) {
	registerTestModuleWithID(t, a, "demo", "demo:", gate, handler)
}

func registerTestModuleWithID(t *testing.T, a *App, id, prefix string, gate modules.Gate, handler modules.Handler) {
	t.Helper()
	err := a.Registry().Register(modules.Module{
		ID:             id,
		TitleKey:       "module." + id,
		Icon:           "🎲",
		CallbackPrefix: prefix,
		Gate:           gate,
		Handler:        handler,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestDispatchModuleCallbackRoutesMatchingPrefix(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4101)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if !handled {
		t.Fatal("dispatchModuleCallback reported not handled, want handled")
	}
	if len(handler.callbacks) != 1 || handler.callbacks[0] != "demo:open" {
		t.Fatalf("handler callbacks = %v, want [demo:open]", handler.callbacks)
	}
}

func TestDispatchModuleCallbackIgnoresUnknownPrefix(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{}, handler)

	cb := &telegram.CallbackQuery{ID: "1", Data: "menu:main", From: telegram.User{ID: 4102}}
	handled, err := a.dispatchModuleCallback(ctx, cb, 4102, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if handled {
		t.Fatal("dispatchModuleCallback handled menu:main, want it left to the existing switch")
	}
	if len(handler.callbacks) != 0 {
		t.Fatalf("handler saw %v, want nothing", handler.callbacks)
	}
}

func TestDispatchModuleCallbackRefusedWhenRateLimited(t *testing.T) {
	a, bot, state := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4109)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	state.blockedActions["module_callback"] = true

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if !handled {
		t.Fatal("a rate-limited callback reported not handled; it must not fall through to the main switch")
	}
	if len(handler.callbacks) != 0 {
		t.Fatalf("rate-limited callback still reached the handler: %v", handler.callbacks)
	}
	if len(bot.messages) == 0 && len(bot.edits) == 0 {
		t.Fatal("rate limit refusal sent nothing to the user")
	}
	var got string
	if len(bot.edits) > 0 {
		got = bot.edits[len(bot.edits)-1].text
	} else {
		got = bot.messages[len(bot.messages)-1].text
	}
	if !strings.Contains(got, "Забагато") {
		t.Fatalf("rate-limited module callback response = %q, want the localized rate-limit message", got)
	}
}

func TestDispatchModuleCallbackBlocksOnGateAndDoesNotReachHandler(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{Needs18Plus: true}, handler)

	const userID = int64(4103)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if !handled {
		t.Fatal("a gate-blocked callback reported not handled; it must not fall through to the main switch")
	}
	if len(handler.callbacks) != 0 {
		t.Fatalf("blocked callback still reached the handler: %v", handler.callbacks)
	}
	if len(bot.messages) == 0 && len(bot.edits) == 0 {
		t.Fatal("gate block sent nothing to the user")
	}
}

func TestDispatchModuleMessageStopsAtTheFirstConsumer(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()

	// First module consumes the message
	firstHandler := &recordingModuleHandler{consume: true}
	registerTestModuleWithID(t, a, "first", "first:", modules.Gate{}, firstHandler)

	// Second module should never see the message
	secondHandler := &recordingModuleHandler{consume: false}
	registerTestModuleWithID(t, a, "second", "second:", modules.Gate{}, secondHandler)

	const userID = int64(4104)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	msg := &telegram.Message{
		MessageID: 1,
		Text:      "привіт",
		From:      &telegram.User{ID: userID},
		Chat:      telegram.Chat{ID: userID},
	}
	handled, err := a.dispatchModuleMessage(ctx, msg)
	if err != nil {
		t.Fatalf("dispatchModuleMessage: %v", err)
	}
	if !handled {
		t.Fatal("dispatchModuleMessage reported not handled, want handled")
	}
	if len(firstHandler.messages) != 1 {
		t.Fatalf("first handler messages = %v, want one entry", firstHandler.messages)
	}
	if len(secondHandler.messages) != 0 {
		t.Fatalf("second handler messages = %v, want nothing (dispatch should have stopped)", secondHandler.messages)
	}
}

func TestDispatchModuleMessageReportsNotHandledWhenTheOnlyModuleDeclines(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{consume: false}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4108)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	msg := &telegram.Message{
		MessageID: 1,
		Text:      "привіт",
		From:      &telegram.User{ID: userID},
		Chat:      telegram.Chat{ID: userID},
	}
	handled, err := a.dispatchModuleMessage(ctx, msg)
	if err != nil {
		t.Fatalf("dispatchModuleMessage: %v", err)
	}
	if handled {
		t.Fatal("dispatchModuleMessage reported handled after the only registered module declined it, want not handled — the caller must fall through to sendMainMenu")
	}
	if len(handler.messages) != 1 {
		t.Fatalf("handler messages = %v, want one entry — the module must still have been offered the message", handler.messages)
	}
}

func TestDispatchModuleMessageReportsHandledWhenHandlerErrors(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &erroringModuleHandler{err: errModuleHandlerFailed}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4105)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	msg := &telegram.Message{
		MessageID: 1,
		Text:      "привіт",
		From:      &telegram.User{ID: userID},
		Chat:      telegram.Chat{ID: userID},
	}
	handled, err := a.dispatchModuleMessage(ctx, msg)
	if !errors.Is(err, errModuleHandlerFailed) {
		t.Fatalf("dispatchModuleMessage error = %v, want %v", err, errModuleHandlerFailed)
	}
	if !handled {
		t.Fatal("dispatchModuleMessage reported not handled after a handler error; the caller would fall through to sendMainMenu and silently drop the user's message")
	}
}

func TestDispatchModuleCallbackPropagatesHandlerError(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &erroringModuleHandler{err: errModuleHandlerFailed}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4106)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if !errors.Is(err, errModuleHandlerFailed) {
		t.Fatalf("dispatchModuleCallback error = %v, want %v", err, errModuleHandlerFailed)
	}
	if !handled {
		t.Fatal("dispatchModuleCallback reported not handled after a handler error; caller would fall through to the legacy switch on an already-claimed prefix")
	}
}

func TestMainMenuKeyboardAppendsModuleRowsWithLockForBlockedOnes(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(4201)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	err := a.Registry().Register(modules.Module{
		ID:             "demo",
		TitleKey:       "module.demo",
		Icon:           "🎲",
		CallbackPrefix: "demo:",
		Gate:           modules.Gate{Needs18Plus: true},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	base := telegram.MainMenuKeyboardWithPair("uk", false)
	got := a.mainMenuKeyboard(ctx, userID, "uk", false)
	if len(got.InlineKeyboard) != len(base.InlineKeyboard)+1 {
		t.Fatalf("menu has %d rows, want %d (base plus one module row)",
			len(got.InlineKeyboard), len(base.InlineKeyboard)+1)
	}

	last := got.InlineKeyboard[len(got.InlineKeyboard)-1][0]
	if last.CallbackData != "demo:open" {
		t.Fatalf("module button callback = %q, want demo:open", last.CallbackData)
	}
	if !strings.Contains(last.Text, "🔒") {
		t.Fatalf("blocked module button text = %q, want a lock marker", last.Text)
	}

	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	unlocked := a.mainMenuKeyboard(ctx, userID, "uk", false)
	unlockedLast := unlocked.InlineKeyboard[len(unlocked.InlineKeyboard)-1][0]
	if strings.Contains(unlockedLast.Text, "🔒") {
		t.Fatalf("unlocked module button text = %q, want no lock marker", unlockedLast.Text)
	}
}

// TestSendMainMenuIncludesModuleRows pins sendMainMenu to a.mainMenuKeyboard.
// sendMainMenu is reached from /start, the "menu" reply button and reset, so
// if it ever goes back to calling telegram.MainMenuKeyboardWithPair directly,
// every one of those paths silently loses its module rows again.
func TestSendMainMenuIncludesModuleRows(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(4202)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	registerTestModule(t, a, modules.Gate{}, nil)

	if err := a.sendMainMenu(ctx, userID, "uk", "Головне меню"); err != nil {
		t.Fatalf("sendMainMenu: %v", err)
	}

	sent := lastMessageTo(t, bot, userID)
	markup, ok := sent.markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("sendMainMenu markup = %T, want telegram.InlineKeyboardMarkup", sent.markup)
	}
	if !inlineKeyboardHasCallbackPrefix(markup, "demo:") {
		t.Fatalf("sendMainMenu keyboard = %+v, want a module row with prefix demo:", markup.InlineKeyboard)
	}
}
