package game_test

import (
	"testing"
	"time"

	"wrnrs/internal/game"
)

func TestTypedAnswerRevealsOnlyAfterBothUsersComplete(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	session := game.NewSession(100, 10, "q001", []int64{1, 2})

	ready, err := session.Submit(1, game.CompletionTyped, "I feel loved when you listen.", now)
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if ready {
		t.Fatal("first answer made session ready to reveal")
	}

	ready, err = session.Submit(2, game.CompletionSkip, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if !ready {
		t.Fatal("session was not ready after both users completed")
	}

	reveal, err := session.Reveal(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatalf("Reveal returned error: %v", err)
	}
	if len(reveal.Answers) != 2 {
		t.Fatalf("reveal answers length = %d, want 2", len(reveal.Answers))
	}
	if reveal.Answers[1].AnswerText != "I feel loved when you listen." {
		t.Fatalf("typed answer not preserved in reveal: %#v", reveal.Answers[1])
	}
	if reveal.Answers[2].Completion != game.CompletionSkip {
		t.Fatalf("second user completion = %s, want skip", reveal.Answers[2].Completion)
	}
}

func TestTypedAnswerCanBeEditedBeforeRevealButNotAfter(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	session := game.NewSession(100, 10, "q001", []int64{1, 2})

	if _, err := session.Submit(1, game.CompletionTyped, "first draft", now); err != nil {
		t.Fatalf("initial submit failed: %v", err)
	}
	if _, err := session.Submit(1, game.CompletionTyped, "edited draft", now.Add(time.Minute)); err != nil {
		t.Fatalf("edit before reveal failed: %v", err)
	}
	if _, err := session.Submit(2, game.CompletionInPerson, "", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("second completion failed: %v", err)
	}
	if _, err := session.Reveal(now.Add(3 * time.Minute)); err != nil {
		t.Fatalf("reveal failed: %v", err)
	}

	if _, err := session.Submit(1, game.CompletionTyped, "too late", now.Add(4*time.Minute)); err == nil {
		t.Fatal("edit after reveal succeeded")
	}
}

func TestInPersonCompletionRequiresBothUsers(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	session := game.NewSession(100, 10, "q001", []int64{1, 2})

	ready, err := session.Submit(1, game.CompletionInPerson, "", now)
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if ready {
		t.Fatal("one in-person tap completed the card for the pair")
	}

	ready, err = session.Submit(2, game.CompletionInPerson, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if !ready {
		t.Fatal("two in-person taps did not complete the card")
	}
}

func TestSupportPromptCadenceSkipsWhenEitherPartnerHasPremium(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-47 * time.Hour)
	old := now.Add(-49 * time.Hour)

	if !game.ShouldShowSupportPrompt(game.SupportPromptInput{Now: now}) {
		t.Fatal("pair with no prior prompt should see support prompt")
	}
	if game.ShouldShowSupportPrompt(game.SupportPromptInput{Now: now, LastPromptedAt: &recent}) {
		t.Fatal("pair prompted within 48 hours should not see support prompt")
	}
	if !game.ShouldShowSupportPrompt(game.SupportPromptInput{Now: now, LastPromptedAt: &old}) {
		t.Fatal("pair prompted more than 48 hours ago should see support prompt")
	}
	if game.ShouldShowSupportPrompt(game.SupportPromptInput{Now: now, LastPromptedAt: &old, UserAPremium: true}) {
		t.Fatal("premium partner did not suppress support prompt")
	}
	if game.ShouldShowSupportPrompt(game.SupportPromptInput{Now: now, LastPromptedAt: &old, UserBPremium: true}) {
		t.Fatal("premium partner did not suppress support prompt")
	}
}
