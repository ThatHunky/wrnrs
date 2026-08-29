package modules_test

import (
	"testing"

	"wrnrs/internal/modules"
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
