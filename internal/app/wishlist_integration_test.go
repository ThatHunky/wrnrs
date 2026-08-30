package app

import (
	"context"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/modules"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
	"wrnrs/internal/wishlist"
)

// wishHubPrivacyFragment is a substring of wish.hub.intro (content/i18n/uk.json)
// that names the actual privacy mechanism the wishlist hub explains: a "no"
// answer never reaches the partner, only a mutual match does.
const wishHubPrivacyFragment = "не побачить"

func TestWishlistModuleIsBlockedWithoutMatureOptIn(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(6001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	// Mature opt-in is deliberately left unset.

	registerWishlistForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "wish:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	// This user confirmed 18+ but did not opt into mature content, so the
	// gate must refuse with gate.needs_mature specifically, not
	// gate.needs_18plus. Both refusal strings contain "18+", so this
	// asserts on gateNeedsMatureFragment (defined in
	// positions_integration_test.go), a fragment unique to the mature
	// refusal.
	if !botSaidSomethingContaining(bot, gateNeedsMatureFragment) {
		t.Fatal("a user without mature opt-in was not told gate.needs_mature specifically")
	}
}

func TestWishlistModuleOpensForAMatureUser(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(6002)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}

	registerWishlistForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "wish:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	if !botSaidSomethingContaining(bot, wishHubPrivacyFragment) {
		t.Fatal("the wishlist hub did not carry the privacy note")
	}
}

// registerWishlistForTest builds a wishlist.Handler around a tiny in-memory
// catalog (never the real 60-entry content/wishes.v1.json — that belongs in
// content-layer tests, not here) and registers it under the same id, prefix
// and gate cmd/wrnrs/main.go uses in production. It reaches into App's
// unexported fields (this file is package app), mirroring
// registerPositionsForTest in positions_integration_test.go.
func registerWishlistForTest(t *testing.T, a *App) {
	t.Helper()

	bot, ok := a.bot.(*fakeBot)
	if !ok {
		t.Fatalf("a.bot = %T, want *fakeBot", a.bot)
	}

	miniCatalog := &catalog.Catalog{
		Kind:    "wishes",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "w001",
				Facets: map[string][]string{"intensity": {"gentle"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Перше бажання", Body: "Опис першого бажання."},
					"en": {Title: "First wish", Body: "Description of the first wish."},
				},
			},
			{
				ID:     "w002",
				Facets: map[string][]string{"intensity": {"medium"}},
				Text: map[string]catalog.ItemText{
					"uk": {Title: "Друге бажання", Body: "Опис другого бажання."},
					"en": {Title: "Second wish", Body: "Description of the second wish."},
				},
			},
		},
	}

	// A dedicated small bundle for the handler's own screens, distinct from
	// a.i18n (which newTestApp already stocks with the gate.* strings the
	// mature-refusal test above depends on). Only the keys showHub actually
	// reads are supplied; that's enough to exercise the real i18n.Bundle
	// lookup path instead of falling back to Bundle.Text's raw-key default.
	wishBundle := i18n.NewBundle()
	wishBundle.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"wish.hub.title":          "Бажання",
		"wish.hub.intro":          "Відмічай, чого хочеш. Партнер не побачить твоїх «ні» — тільки те, у чому ви збіглися.",
		"wish.hub.progress":       "Відмічено: %d з %d",
		"wish.hub.partner_active": "Партнер теж відмічає",
	}})
	wishBundle.Add(i18n.Catalog{Language: "en", Brand: "between us.", Strings: map[string]string{
		"wish.hub.title":          "Wishes",
		"wish.hub.intro":          "Mark what you want. Your partner never sees your \"no\" — only what you both agreed on.",
		"wish.hub.progress":       "Marked: %d of %d",
		"wish.hub.partner_active": "Your partner is marking too",
	}})

	handler := wishlist.NewHandler(wishlist.HandlerOptions{
		Service:    wishlist.NewService(wishlist.ServiceOptions{Catalog: miniCatalog}),
		Repository: a.repo,
		Bot:        bot,
		I18n:       wishBundle,
	})

	if err := a.Registry().Register(modules.Module{
		ID:             "wishlist",
		TitleKey:       "wish.hub.title",
		Icon:           "💛",
		CallbackPrefix: "wish:",
		Gate:           modules.Gate{Needs18Plus: true, NeedsMature: true},
		Handler:        handler,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
}
