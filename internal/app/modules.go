package app

import (
	"context"

	"wrnrs/internal/modules"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
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

// dispatchModuleCallback routes a callback to its module. It reports handled
// so the caller can skip the legacy switch. A gate refusal counts as handled:
// the user gets an explanation instead of falling through to the switch.
func (a *App) dispatchModuleCallback(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string) (bool, error) {
	if a.registry == nil || cb == nil {
		return false, nil
	}
	module, ok := a.registry.ByCallback(cb.Data)
	if !ok {
		return false, nil
	}

	state, err := a.moduleUserState(ctx, cb.From.ID)
	if err != nil {
		return true, err
	}
	if allowed, reason := module.Gate.Allows(state); !allowed {
		text := a.i18n.Text(language, reason)
		return true, a.editCallbackScreen(ctx, cb, chatID, text,
			telegram.MainMenuKeyboardWithPair(language, state.HasActivePair))
	}
	if module.Handler == nil {
		return true, nil
	}
	return true, module.Handler.HandleCallback(ctx, cb)
}

// dispatchModuleMessage offers the message to each module in registration
// order and stops at the first one that consumes it. A handler error stops
// the loop immediately and is reported as handled so the caller propagates
// the error instead of silently falling through to the main menu.
func (a *App) dispatchModuleMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.registry == nil || msg == nil || msg.From == nil {
		return false, nil
	}
	for _, module := range a.registry.All() {
		if module.Handler == nil {
			continue
		}
		handled, err := module.Handler.HandleMessage(ctx, msg)
		if err != nil {
			return true, err
		}
		if handled {
			return true, nil
		}
	}
	return false, nil
}
