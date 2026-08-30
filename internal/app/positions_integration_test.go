package app

import (
	"context"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/modules"
	"wrnrs/internal/positions"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// gateNeedsMatureFragment is a substring of the "gate.needs_mature" text
// (content/i18n/uk.json) that does not appear in "gate.needs_18plus". Both
// strings contain "18+", so asserting on that alone cannot tell the two
// refusal reasons apart - this fragment can.
const gateNeedsMatureFragment = "вимагає згоди"

// gateNeeds18PlusFragment is a substring of the "gate.needs_18plus" text
// that does not appear in "gate.needs_mature", for the same reason as above.
const gateNeeds18PlusFragment = "Спочатку підтверди"

func TestPositionsModuleIsBlockedWithoutMatureOptIn(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(5001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}

	registerPositionsForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "pos:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	// This user confirmed 18+ but did not opt into mature content, so the
	// gate must refuse with gate.needs_mature specifically, not gate.needs_18plus.
	if !botSaidSomethingContaining(bot, gateNeedsMatureFragment) {
		t.Fatal("a user without mature opt-in was not told gate.needs_mature specifically")
	}
}

func TestPositionsModuleIsBlockedWithoutAdultConfirmation(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(5003)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	// Neither 18+ confirmation nor mature opt-in has been set.

	registerPositionsForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "pos:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	// This user has confirmed neither, so the gate must refuse with
	// gate.needs_18plus specifically, not gate.needs_mature.
	if !botSaidSomethingContaining(bot, gateNeeds18PlusFragment) {
		t.Fatal("a user who has confirmed neither was not told gate.needs_18plus specifically")
	}
}

func TestPositionsModuleOpensForAMatureUser(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(5002)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}

	registerPositionsForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "pos:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	if !botSaidSomethingContaining(bot, "sexpositions.club") {
		t.Fatal("the module hub did not show the source attribution")
	}
}

// registerPositionsForTest builds a positions.Handler around a tiny
// in-memory catalog (never the real 519-entry content/positions.v1.json —
// that belongs in the content-layer tests, not here) and registers it under
// the same id, prefix and gate cmd/wrnrs/main.go uses in production. It
// reaches into App's unexported fields (this file is package app) rather
// than taking a *fakeBot/*i18n.Bundle parameter, so its signature matches
// what the two tests above call: registerPositionsForTest(t, a).
func registerPositionsForTest(t *testing.T, a *App) {
	t.Helper()

	bot, ok := a.bot.(*fakeBot)
	if !ok {
		t.Fatalf("a.bot = %T, want *fakeBot (positions.Bot needs the ref-based photo methods added to the fake)", a.bot)
	}

	miniCatalog := &catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "001",
				Facets: map[string][]string{"level": {"easy"}, "location": {"bed"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Перша поза"},
					"en": {Title: "First position"},
				},
			},
			{
				ID:     "002",
				Facets: map[string][]string{"level": {"medium"}, "location": {"sofa"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Друга поза"},
					"en": {Title: "Second position"},
				},
			},
		},
	}

	handler := positions.NewHandler(positions.HandlerOptions{
		Service:    positions.NewService(positions.ServiceOptions{Catalog: miniCatalog}),
		Catalog:    miniCatalog,
		Repository: a.repo,
		Bot:        bot,
		I18n:       a.i18n,
	})

	if err := a.Registry().Register(modules.Module{
		ID:             "positions",
		TitleKey:       "module.positions",
		Icon:           "🎲",
		CallbackPrefix: "pos:",
		Gate:           modules.Gate{Needs18Plus: true, NeedsMature: true},
		Handler:        handler,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// collectBotTexts gathers every piece of user-visible text the fakeBot saw
// across the message and edit shapes the positions screens use: a fresh
// send (the hub, when there is no message to edit into) and a text edit
// (the hub, editing the triggering pos:open callback's message in place).
func collectBotTexts(bot *fakeBot) []string {
	texts := make([]string, 0, len(bot.messages)+len(bot.edits))
	for _, m := range bot.messages {
		texts = append(texts, m.text)
	}
	for _, e := range bot.edits {
		texts = append(texts, e.text)
	}
	return texts
}

func botSaidSomethingContaining(bot *fakeBot, needle string) bool {
	for _, text := range collectBotTexts(bot) {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
