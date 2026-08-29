package modules_test

import (
	"context"
	"testing"

	"wrnrs/internal/modules"
	"wrnrs/internal/telegram"
)

func TestGateAllowsWhenEveryRequirementIsMet(t *testing.T) {
	gate := modules.Gate{NeedsPair: true, Needs18Plus: true, NeedsMature: true, NeedsPremium: true}
	state := modules.UserState{Is18Plus: true, MatureOptIn: true, HasActivePair: true, HasPremium: true}

	ok, reason := gate.Allows(state)
	if !ok || reason != "" {
		t.Fatalf("Allows = %v, %q; want true and no reason", ok, reason)
	}
}

func TestEmptyGateAllowsEmptyState(t *testing.T) {
	ok, reason := modules.Gate{}.Allows(modules.UserState{})
	if !ok || reason != "" {
		t.Fatalf("empty gate Allows = %v, %q; want true and no reason", ok, reason)
	}
}

func TestGateReportsTheFirstUnmetRequirementInFixedOrder(t *testing.T) {
	gate := modules.Gate{NeedsPair: true, Needs18Plus: true, NeedsMature: true, NeedsPremium: true}

	cases := []struct {
		name   string
		state  modules.UserState
		reason string
	}{
		{"nothing", modules.UserState{}, modules.ReasonNeeds18Plus},
		{"adult only", modules.UserState{Is18Plus: true}, modules.ReasonNeedsMature},
		{"adult and mature", modules.UserState{Is18Plus: true, MatureOptIn: true}, modules.ReasonNeedsPair},
		{"all but premium", modules.UserState{Is18Plus: true, MatureOptIn: true, HasActivePair: true}, modules.ReasonNeedsPremium},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := gate.Allows(tc.state)
			if ok {
				t.Fatalf("Allows(%+v) = true, want blocked", tc.state)
			}
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

func TestGateIgnoresRequirementsItDoesNotDeclare(t *testing.T) {
	gate := modules.Gate{Needs18Plus: true}
	ok, reason := gate.Allows(modules.UserState{Is18Plus: true})
	if !ok || reason != "" {
		t.Fatalf("Allows = %v, %q; want true — gate declares only 18+", ok, reason)
	}
}

type stubHandler struct {
	callbacks []string
	messages  []string
	handle    bool
}

func (s *stubHandler) HandleCallback(_ context.Context, cb *telegram.CallbackQuery) error {
	s.callbacks = append(s.callbacks, cb.Data)
	return nil
}

func (s *stubHandler) HandleMessage(_ context.Context, msg *telegram.Message) (bool, error) {
	s.messages = append(s.messages, msg.Text)
	return s.handle, nil
}

func TestRegisterRejectsInvalidModules(t *testing.T) {
	r := modules.NewRegistry()

	if err := r.Register(modules.Module{CallbackPrefix: "pos:"}); err == nil {
		t.Fatal("Register with empty id succeeded, want an error")
	}
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos"}); err == nil {
		t.Fatal("Register with a prefix missing the trailing colon succeeded, want an error")
	}
}

func TestRegisterRejectsInvalidTitleKey(t *testing.T) {
	r := modules.NewRegistry()

	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "pos:"}); err == nil {
		t.Fatal("Register with empty title key succeeded, want an error")
	}
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "   ", CallbackPrefix: "pos:"}); err == nil {
		t.Fatal("Register with a whitespace-only title key succeeded, want an error")
	}
}

func TestRegisterRejectsDuplicateIDAndCollidingPrefix(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions2", CallbackPrefix: "other:"}); err == nil {
		t.Fatal("Register with a duplicate id succeeded, want an error")
	}
	if err := r.Register(modules.Module{ID: "favourites", TitleKey: "module.favourites", CallbackPrefix: "pos:fav:"}); err == nil {
		t.Fatal("Register with a colliding prefix succeeded, want an error")
	}
}

func TestByCallbackMatchesRegisteredPrefix(t *testing.T) {
	r := modules.NewRegistry()
	handler := &stubHandler{}
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos:", Handler: handler}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	found, ok := r.ByCallback("pos:browse:12")
	if !ok || found.ID != "positions" {
		t.Fatalf("ByCallback(pos:browse:12) = %+v, %v; want the positions module", found, ok)
	}
	if _, ok := r.ByCallback("menu:main"); ok {
		t.Fatal("ByCallback(menu:main) matched a module, want no match")
	}
}

func TestByIDFindsARegisteredModuleAndMissesAnUnregisteredOne(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	found, ok := r.ByID("positions")
	if !ok || found.CallbackPrefix != "pos:" {
		t.Fatalf("ByID(positions) = %+v, %v; want the positions module", found, ok)
	}
	if _, ok := r.ByID("dares"); ok {
		t.Fatal("ByID(dares) matched a module, want no match")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	all := r.All()
	all[0].ID = "mutated"

	again := r.All()
	if again[0].ID != "positions" {
		t.Fatalf("All() leaked its backing array: id is now %q", again[0].ID)
	}
}

func TestRegisterRejectsCollidingPrefixRegisteredInEitherOrder(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "favourites", TitleKey: "module.favourites", CallbackPrefix: "pos:fav:"}); err != nil {
		t.Fatalf("first Register with longer prefix succeeded, got error: %v", err)
	}

	if err := r.Register(modules.Module{ID: "positions", TitleKey: "module.positions", CallbackPrefix: "pos:"}); err == nil {
		t.Fatal("Register with shorter prefix that collides with existing longer prefix succeeded, want an error — collision must be caught regardless of registration order")
	}
}

func TestRegisterRejectsReservedCallbackPrefix(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "storefront", TitleKey: "module.storefront", CallbackPrefix: "store:"}); err == nil {
		t.Fatal("Register with the reserved store: prefix succeeded, want an error — it would silently shadow the live store screen")
	}
}

func TestRegisterRejectsPrefixThatExtendsAReservedOne(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "storefront", TitleKey: "module.storefront", CallbackPrefix: "store:x:"}); err == nil {
		t.Fatal("Register with a prefix that extends the reserved store: prefix succeeded, want an error")
	}
}

func TestRegisterTrimsIDBeforeStoringAndComparing(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "pos", TitleKey: "module.pos", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	if err := r.Register(modules.Module{ID: "pos ", TitleKey: "module.pos2", CallbackPrefix: "pos2:"}); err == nil {
		t.Fatal("Register with an id that only differs by surrounding whitespace succeeded, want a duplicate-id error")
	}

	all := r.All()
	if len(all) != 1 || all[0].ID != "pos" {
		t.Fatalf("registered modules = %+v, want exactly one module stored with its trimmed id %q", all, "pos")
	}
}
