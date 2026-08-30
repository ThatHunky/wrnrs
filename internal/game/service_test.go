package game_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"wrnrs/internal/content"
	"wrnrs/internal/game"
	"wrnrs/internal/storage"
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

func TestServiceStartCreatesOnlyOnePendingInvitePerPair(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newServiceFixture(t, []content.Card{
		{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання 1", "en": "Question 1"}},
	})

	first, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if first.Kind != game.StartPendingInvite {
		t.Fatalf("start kind = %s, want pending invite", first.Kind)
	}
	if first.PartnerID != 2002 {
		t.Fatalf("partner id = %d, want 2002", first.PartnerID)
	}
	if first.Session.Status != storage.GameSessionPendingAcceptance {
		t.Fatalf("session status = %s, want pending_acceptance", first.Session.Status)
	}

	second, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}
	if second.Session.ID != first.Session.ID {
		t.Fatalf("second start created session %d, want existing %d", second.Session.ID, first.Session.ID)
	}

	current, err := repo.CurrentGameSessionForPair(ctx, first.Pair.ID)
	if err != nil {
		t.Fatalf("CurrentGameSessionForPair returned error: %v", err)
	}
	if current == nil || current.ID != first.Session.ID {
		t.Fatalf("current session = %#v, want first session", current)
	}
}

func TestServiceDeclineAndExpiredInviteDoNotCreateHistory(t *testing.T) {
	ctx := context.Background()
	svc, repo, clock, _ := newServiceFixture(t, []content.Card{
		{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання 1", "en": "Question 1"}},
	})

	pending, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := svc.Decline(ctx, 2002, pending.Session.ID); err != nil {
		t.Fatalf("Decline returned error: %v", err)
	}
	if count, err := repo.PairCardCount(ctx, pending.Pair.ID, 1); err != nil {
		t.Fatalf("PairCardCount returned error: %v", err)
	} else if count != 0 {
		t.Fatalf("history count after decline = %d, want 0", count)
	}

	expiring, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}
	clock.now = clock.now.Add(16 * time.Minute)
	if _, err := svc.Accept(ctx, 2002, expiring.Session.ID); err == nil {
		t.Fatal("Accept succeeded for expired invite")
	}
	if count, err := repo.PairCardCount(ctx, expiring.Pair.ID, 1); err != nil {
		t.Fatalf("PairCardCount after expiry returned error: %v", err)
	} else if count != 0 {
		t.Fatalf("history count after expiry = %d, want 0", count)
	}
}

func TestServiceSelectsRandomizedNoRepeatCardsForPair(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newServiceFixture(t, []content.Card{
		{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання 1", "en": "Question 1"}},
		{ID: "q002", Level: 1, Text: map[string]string{"uk": "Питання 2", "en": "Question 2"}},
		{ID: "q003", Level: 1, Text: map[string]string{"uk": "Питання 3", "en": "Question 3"}},
	})

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		pending, err := svc.Start(ctx, 1001)
		if err != nil {
			t.Fatalf("Start #%d returned error: %v", i+1, err)
		}
		started, err := svc.Accept(ctx, 2002, pending.Session.ID)
		if err != nil {
			t.Fatalf("Accept #%d returned error: %v", i+1, err)
		}
		if seen[started.Card.ID] {
			t.Fatalf("card %s repeated before deck exhaustion", started.Card.ID)
		}
		seen[started.Card.ID] = true
		if _, err := svc.Submit(ctx, 1001, started.Session.ID, game.CompletionSkip, ""); err != nil {
			t.Fatalf("first submit #%d returned error: %v", i+1, err)
		}
		if _, err := svc.Submit(ctx, 2002, started.Session.ID, game.CompletionSkip, ""); err != nil {
			t.Fatalf("second submit #%d returned error: %v", i+1, err)
		}
		if i < 2 {
			if _, err := svc.Next(ctx, 1001, started.Session.ID); err != nil {
				t.Fatalf("Next #%d returned error: %v", i+1, err)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("seen cards = %v, want all 3", seen)
	}
}

func TestServiceStoresTypedAnswersEncryptedAndRevealsAfterBothComplete(t *testing.T) {
	ctx := context.Background()
	svc, _, _, db := newServiceFixture(t, []content.Card{
		{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання 1", "en": "Question 1"}},
	})
	pending, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	started, err := svc.Accept(ctx, 2002, pending.Session.ID)
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}

	plaintext := "I feel loved when you listen."
	firstSubmit, err := svc.Submit(ctx, 1001, started.Session.ID, game.CompletionTyped, plaintext)
	if err != nil {
		t.Fatalf("typed Submit returned error: %v", err)
	}
	if firstSubmit.Revealed {
		t.Fatal("first submit revealed the card before both partners completed")
	}

	var encrypted []byte
	if err := db.QueryRowContext(ctx, `
		SELECT answer_text_encrypted
		FROM game_answers
		WHERE session_id = ? AND user_id = ?
	`, started.Session.ID, 1001).Scan(&encrypted); err != nil {
		t.Fatalf("load encrypted answer returned error: %v", err)
	}
	if bytes.Contains(encrypted, []byte(plaintext)) {
		t.Fatalf("encrypted answer contains plaintext: %q", encrypted)
	}

	revealed, err := svc.Submit(ctx, 2002, started.Session.ID, game.CompletionSkip, "")
	if err != nil {
		t.Fatalf("second Submit returned error: %v", err)
	}
	if !revealed.Revealed {
		t.Fatal("second submit did not reveal the card")
	}
	if revealed.Answers[1001].AnswerText != plaintext {
		t.Fatalf("revealed answer = %#v, want plaintext for user 1001", revealed.Answers[1001])
	}
}

func TestServiceAdvancesLevelAfterSixCompletedCards(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newServiceFixture(t, []content.Card{
		{ID: "q001", Level: 1, Text: map[string]string{"uk": "П1", "en": "Q1"}},
		{ID: "q002", Level: 1, Text: map[string]string{"uk": "П2", "en": "Q2"}},
		{ID: "q003", Level: 1, Text: map[string]string{"uk": "П3", "en": "Q3"}},
		{ID: "q004", Level: 1, Text: map[string]string{"uk": "П4", "en": "Q4"}},
		{ID: "q005", Level: 1, Text: map[string]string{"uk": "П5", "en": "Q5"}},
		{ID: "q006", Level: 1, Text: map[string]string{"uk": "П6", "en": "Q6"}},
		{ID: "q101", Level: 2, Text: map[string]string{"uk": "П7", "en": "Q7"}},
	})

	var sessionID int64
	for i := 0; i < 6; i++ {
		pending, err := svc.Start(ctx, 1001)
		if err != nil {
			t.Fatalf("Start #%d returned error: %v", i+1, err)
		}
		started, err := svc.Accept(ctx, 2002, pending.Session.ID)
		if err != nil {
			t.Fatalf("Accept #%d returned error: %v", i+1, err)
		}
		sessionID = started.Session.ID
		if _, err := svc.Submit(ctx, 1001, sessionID, game.CompletionSkip, ""); err != nil {
			t.Fatalf("first Submit #%d returned error: %v", i+1, err)
		}
		if _, err := svc.Submit(ctx, 2002, sessionID, game.CompletionSkip, ""); err != nil {
			t.Fatalf("second Submit #%d returned error: %v", i+1, err)
		}
		if i < 5 {
			if _, err := svc.Next(ctx, 1001, sessionID); err != nil {
				t.Fatalf("Next #%d returned error: %v", i+1, err)
			}
		}
	}

	pair, err := repo.ActivePairForUser(ctx, 1001)
	if err != nil {
		t.Fatalf("ActivePairForUser returned error: %v", err)
	}
	if pair.ActiveLevel != 2 {
		t.Fatalf("active level = %d, want 2", pair.ActiveLevel)
	}
}

func TestServiceSelectsCustomQuestionAndKeepsSnapshotAfterDeletion(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newServiceFixture(t, nil)
	customID, err := repo.CreateCustomQuestion(ctx, 1001, "What small ritual should we protect?")
	if err != nil {
		t.Fatalf("CreateCustomQuestion returned error: %v", err)
	}

	pending, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if pending.Card.ID != "custom:1" || customID != 1 {
		t.Fatalf("pending card = %#v, custom id = %d", pending.Card, customID)
	}
	if err := repo.DeleteCustomQuestion(ctx, customID, 1001); err != nil {
		t.Fatalf("DeleteCustomQuestion returned error: %v", err)
	}
	started, err := svc.Accept(ctx, 2002, pending.Session.ID)
	if err != nil {
		t.Fatalf("Accept returned error after custom question deletion: %v", err)
	}
	if text, _ := started.Card.LocalizedText("en"); text != "What small ritual should we protect?" {
		t.Fatalf("started custom card text = %q", text)
	}

	if _, err := svc.Submit(ctx, 1001, started.Session.ID, game.CompletionSkip, ""); err != nil {
		t.Fatalf("first Submit returned error: %v", err)
	}
	revealed, err := svc.Submit(ctx, 2002, started.Session.ID, game.CompletionSkip, "")
	if err != nil {
		t.Fatalf("second Submit returned error: %v", err)
	}
	if !revealed.Revealed {
		t.Fatal("custom question session did not reveal")
	}
}

// TestServiceDoesNotReserveCustomQuestionAfterLevelAdvance guards against a
// regression where selectNextCard re-derived a custom question's level from
// pair.ActiveLevel on every call instead of using the level it was fixed at
// creation time. pair_card_history has no level-independent key for custom
// questions, so a question completed at level 1 would look "unseen" again
// the moment the pair advanced to level 2 and would be served forever,
// silently dropped from history by INSERT OR IGNORE on top of that. With
// the fix, a custom question created at level 1 must never be offered once
// the pair is at level 2.
func TestServiceDoesNotReserveCustomQuestionAfterLevelAdvance(t *testing.T) {
	ctx := context.Background()
	// Deliberately no stock deck cards: the only candidate card at any level
	// is the level-1 custom question, so if it leaks into level 2 it will
	// deterministically be re-served there (no random shuffle can hide the
	// bug behind an alternate card). If it correctly stays pinned to level
	// 1, level 2 has zero eligible cards and Start must fail with
	// ErrNoEligibleCards instead.
	svc, repo, _, _ := newServiceFixture(t, nil)

	// Created while the pair is at level 1 (set up by newServiceFixture), so
	// it must be pinned to level 1.
	customID, err := repo.CreateCustomQuestion(ctx, 1001, "What small ritual should we protect?")
	if err != nil {
		t.Fatalf("CreateCustomQuestion returned error: %v", err)
	}

	pending, err := svc.Start(ctx, 1001)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if pending.Card.ID != fmt.Sprintf("custom:%d", customID) {
		t.Fatalf("pending card = %#v, want the level-1 custom question", pending.Card)
	}
	started, err := svc.Accept(ctx, 2002, pending.Session.ID)
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	if _, err := svc.Submit(ctx, 1001, started.Session.ID, game.CompletionSkip, ""); err != nil {
		t.Fatalf("first Submit returned error: %v", err)
	}
	if _, err := svc.Submit(ctx, 2002, started.Session.ID, game.CompletionSkip, ""); err != nil {
		t.Fatalf("second Submit returned error: %v", err)
	}

	// Force the pair to level 2, the way advanceLevelIfReady eventually
	// would, without needing to grind through the unlock threshold.
	if err := repo.UpdatePairLevel(ctx, started.Pair.ID, 2); err != nil {
		t.Fatalf("UpdatePairLevel returned error: %v", err)
	}

	if _, err := svc.Next(ctx, 1001, started.Session.ID); !errors.Is(err, game.ErrNoEligibleCards) {
		t.Fatalf("Next at level 2 returned err=%v, want ErrNoEligibleCards because the level-1 custom question must not be re-served", err)
	}
}

func newServiceFixture(t *testing.T, cards []content.Card) (*game.Service, *storage.Repository, *testClock, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := storage.NewRepository(db)
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "alice", DisplayName: "Alice", Language: "en", Is18Plus: true, MatureOptIn: true},
		{TelegramID: 2002, Username: "bob", DisplayName: "Bob", Language: "en", Is18Plus: true, MatureOptIn: true},
	} {
		if err := repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}
	request, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "pair-token",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest returned error: %v", err)
	}
	if _, err := repo.AcceptPairRequest(ctx, request.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}
	answerCipher, err := storage.NewAnswerCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewAnswerCipher returned error: %v", err)
	}
	clock := &testClock{now: time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)}
	svc := game.NewService(game.ServiceOptions{
		Repo:                 repo,
		Deck:                 &content.Deck{Version: 1, Cards: cards},
		AnswerCipher:         answerCipher,
		Now:                  clock.Now,
		InviteTTL:            15 * time.Minute,
		LevelUnlockThreshold: 6,
	})
	return svc, repo, clock, db
}
