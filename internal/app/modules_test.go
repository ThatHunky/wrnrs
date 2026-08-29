package app

import (
	"context"
	"testing"

	"wrnrs/internal/storage"
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
