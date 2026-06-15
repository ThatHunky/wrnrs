package telegram_test

import (
	"testing"

	"wrnrs/internal/telegram"
)

func TestControlsKeyboardIncludesMenuAndReset(t *testing.T) {
	uk := telegram.ControlsKeyboard("uk")
	if len(uk.Keyboard) != 1 || len(uk.Keyboard[0]) != 2 {
		t.Fatalf("uk controls keyboard shape = %#v", uk.Keyboard)
	}
	if uk.Keyboard[0][0].Text != telegram.MenuTextUK {
		t.Fatalf("uk menu text = %q", uk.Keyboard[0][0].Text)
	}
	if uk.Keyboard[0][1].Text != telegram.CancelResetTextUK {
		t.Fatalf("uk reset text = %q", uk.Keyboard[0][1].Text)
	}

	en := telegram.ControlsKeyboard("en")
	if en.Keyboard[0][0].Text != telegram.MenuTextEN {
		t.Fatalf("en menu text = %q", en.Keyboard[0][0].Text)
	}
	if en.Keyboard[0][1].Text != telegram.CancelResetText {
		t.Fatalf("en reset text = %q", en.Keyboard[0][1].Text)
	}
}

func TestCardControlsCanScopeCallbacksToQuestion(t *testing.T) {
	controls := telegram.CardControlsForQuestion("uk", "q001")
	got := []string{
		controls.InlineKeyboard[0][0].CallbackData,
		controls.InlineKeyboard[1][0].CallbackData,
		controls.InlineKeyboard[1][1].CallbackData,
		controls.InlineKeyboard[2][0].CallbackData,
	}
	want := []string{
		"game:answer:q001",
		"game:in_person:q001",
		"game:skip:q001",
		"game:pause:q001",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("callback %d = %q, want %q", i, got[i], want[i])
		}
	}
}
