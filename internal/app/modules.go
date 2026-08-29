package app

import (
	"context"

	"wrnrs/internal/modules"
	"wrnrs/internal/storage"
)

// Registry exposes the module registry so cmd wiring can register modules
// after the App is constructed.
func (a *App) Registry() *modules.Registry {
	return a.registry
}

// moduleUserState resolves everything a module gate needs about one user.
func (a *App) moduleUserState(ctx context.Context, userID int64) (modules.UserState, error) {
	var state modules.UserState

	is18Plus, matureOptIn, err := a.repo.UserMaturity(ctx, userID)
	if err != nil {
		return modules.UserState{}, err
	}
	state.Is18Plus = is18Plus
	state.MatureOptIn = matureOptIn

	pair, err := a.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return modules.UserState{}, err
	}
	state.HasActivePair = pair != nil

	premium, err := a.repo.UserHasEntitlement(ctx, userID,
		storage.EntitlementPremiumAccess, storage.EntitlementPremiumAccess)
	if err != nil {
		return modules.UserState{}, err
	}
	state.HasPremium = premium

	return state, nil
}
