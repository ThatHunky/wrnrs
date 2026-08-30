package app

import (
	"context"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/modules"
	"wrnrs/internal/play"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// playHubIntroFragment is a substring of play.hub.intro (content/i18n/uk.json)
// unique to the play module's hub screen.
const playHubIntroFragment = "Тягніть картки по черзі"

func TestPlayModuleIsBlockedWithoutMatureOptIn(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(7001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	// Mature opt-in is deliberately left unset.

	registerPlayForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "play:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	// This user confirmed 18+ but did not opt into mature content, so the
	// gate must refuse with gate.needs_mature specifically, not
	// gate.needs_18plus. Both refusal strings contain "18+", so this asserts
	// on gateNeedsMatureFragment (defined in positions_integration_test.go),
	// a fragment unique to the mature refusal.
	if !botSaidSomethingContaining(bot, gateNeedsMatureFragment) {
		t.Fatal("a user without mature opt-in was not told gate.needs_mature specifically")
	}
}

func TestPlayModuleOpensForAMatureUser(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(7002)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}

	registerPlayForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "play:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	if !botSaidSomethingContaining(bot, playHubIntroFragment) {
		t.Fatal("the play hub did not render")
	}
}

// registerPlayForTest builds a play.Handler around a tiny in-memory catalog
// (never the real 80-card content/play.v1.json — that belongs in the
// content-layer tests, not here) and registers it under the same id, prefix
// and gate cmd/wrnrs/main.go uses in production. It reaches into App's
// unexported fields (this file is package app), mirroring
// registerPositionsForTest and registerWishlistForTest.
func registerPlayForTest(t *testing.T, a *App) {
	t.Helper()

	bot, ok := a.bot.(*fakeBot)
	if !ok {
		t.Fatalf("a.bot = %T, want *fakeBot", a.bot)
	}

	miniCatalog := &catalog.Catalog{
		Kind:    "play",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "p001",
				Facets: map[string][]string{"kind": {"truth"}, "intensity": {"gentle"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Перша правда"},
					"en": {Title: "First truth"},
				},
			},
			{
				ID:     "p002",
				Facets: map[string][]string{"kind": {"dare"}, "intensity": {"medium"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Перша дія"},
					"en": {Title: "First dare"},
				},
			},
		},
	}

	// A dedicated small bundle for the handler's own screens, distinct from
	// a.i18n (which newTestApp already stocks with the gate.* strings the
	// mature-refusal test above depends on). Only the keys showHub actually
	// reads are supplied.
	playBundle := i18n.NewBundle()
	playBundle.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"play.hub.title":     "Правда або дія",
		"play.hub.intro":     "Тягніть картки по черзі — бот сам чергує, кому випадає.",
		"play.hub.solo_hint": "Коли зʼявиться пара, картки звертатимуться на імена.",
		"play.next":          "▶ Далі",
		"play.filters":       "☰ Фільтри",
	}})
	playBundle.Add(i18n.Catalog{Language: "en", Brand: "between us.", Strings: map[string]string{
		"play.hub.title":     "Truth or dare",
		"play.hub.intro":     "Draw cards in turn — the bot rotates whose turn it is.",
		"play.hub.solo_hint": "Once a pair exists, cards will address you by name.",
		"play.next":          "▶ Next",
		"play.filters":       "☰ Filters",
	}})

	handler := play.NewHandler(play.HandlerOptions{
		Service:    play.NewService(play.ServiceOptions{Catalog: miniCatalog}),
		Repository: a.repo,
		Bot:        bot,
		I18n:       playBundle,
	})

	if err := a.Registry().Register(modules.Module{
		ID:             "play",
		TitleKey:       "module.play",
		Icon:           "🃏",
		CallbackPrefix: "play:",
		Gate:           modules.Gate{Needs18Plus: true, NeedsMature: true},
		Handler:        handler,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
}
