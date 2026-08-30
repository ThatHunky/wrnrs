package storage_test

import (
	"context"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

// newRepoWithPair opens an in-memory database, creates two paired users via
// the real pairing flow (CreatePairRequest + AcceptPairRequest), and returns
// the repository plus the resulting pair id.
func newRepoWithPair(t *testing.T) (*storage.Repository, int64) {
	t.Helper()

	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repo := storage.NewRepository(db)
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "alice", DisplayName: "Alice", Language: "en", ThemeBaseColor: "#d98c9f", SelectedStyleID: "default_warm"},
		{TelegramID: 1002, Username: "bob", DisplayName: "Bob", Language: "en", ThemeBaseColor: "#8da68f", SelectedStyleID: "default_warm"},
	} {
		if err := repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}

	request, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "token-positions",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest returned error: %v", err)
	}

	pair, err := repo.AcceptPairRequest(ctx, request.InviteToken, 1002)
	if err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}

	return repo, pair.ID
}

func TestTogglePositionMarkFlipsAndPersists(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	on, err := repo.TogglePositionMark(ctx, pairID, "519", storage.MarkTried, 1001, now)
	if err != nil {
		t.Fatalf("TogglePositionMark: %v", err)
	}
	if !on {
		t.Fatal("first toggle returned false, want true")
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	if !marks["519"].TriedAt.Valid {
		t.Fatal("tried mark was not persisted")
	}
	if marks["519"].MarkedBy.Int64 != 1001 {
		t.Fatalf("MarkedBy = %d, want 1001", marks["519"].MarkedBy.Int64)
	}

	off, err := repo.TogglePositionMark(ctx, pairID, "519", storage.MarkTried, 1002, now)
	if err != nil {
		t.Fatalf("second TogglePositionMark: %v", err)
	}
	if off {
		t.Fatal("second toggle returned true, want false")
	}

	marks, err = repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks after unset: %v", err)
	}
	if marks["519"].TriedAt.Valid {
		t.Fatal("tried mark survived the second toggle")
	}
}

func TestMarkKindsAreIndependent(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.TogglePositionMark(ctx, pairID, "007", storage.MarkTried, 1001, now); err != nil {
		t.Fatalf("toggle tried: %v", err)
	}
	if _, err := repo.TogglePositionMark(ctx, pairID, "007", storage.MarkFavorited, 1001, now); err != nil {
		t.Fatalf("toggle favorited: %v", err)
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	mark := marks["007"]
	if !mark.TriedAt.Valid || !mark.FavoritedAt.Valid {
		t.Fatalf("mark = %+v, want both tried and favorited set", mark)
	}
	if mark.HiddenAt.Valid {
		t.Fatal("hidden was set without being toggled")
	}
}

func TestMarksAreSharedAcrossThePairNotPerUser(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.TogglePositionMark(ctx, pairID, "012", storage.MarkTried, 1001, now); err != nil {
		t.Fatalf("toggle by first partner: %v", err)
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	if !marks["012"].TriedAt.Valid {
		t.Fatal("the second partner does not see the mark set by the first")
	}
}

func TestTogglePositionMarkRejectsInvalidKinds(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	tests := []struct {
		name string
		kind storage.PositionMarkKind
	}{
		{"arbitrary string", storage.PositionMarkKind("nonexistent")},
		{"empty string", storage.PositionMarkKind("")},
		{"SQL fragment", storage.PositionMarkKind("tried_at = 1, favorited_at")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.TogglePositionMark(ctx, pairID, "attack-pos", tt.kind, 1001, now)
			if err == nil {
				t.Fatal("TogglePositionMark returned nil error for invalid kind, want non-nil")
			}

			// Verify no row was created by checking the marks are empty
			marks, err := repo.PairPositionMarks(ctx, pairID)
			if err != nil {
				t.Fatalf("PairPositionMarks: %v", err)
			}
			if len(marks) != 0 {
				t.Fatalf("expected no marks to be persisted, but got %d marks: %+v", len(marks), marks)
			}
		})
	}
}

func TestPositionMarkssurviveEndActivePair(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Set a mark on a position
	if _, err := repo.TogglePositionMark(ctx, pairID, "position-123", storage.MarkFavorited, 1001, now); err != nil {
		t.Fatalf("TogglePositionMark: %v", err)
	}

	// Verify the mark exists
	marksBefore, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks before end: %v", err)
	}
	if !marksBefore["position-123"].FavoritedAt.Valid {
		t.Fatal("mark was not persisted before ending pair")
	}

	// End the pair (via user 1001's perspective)
	if _, err := repo.EndActivePair(ctx, 1001, now); err != nil {
		t.Fatalf("EndActivePair: %v", err)
	}

	// Verify the mark still exists after ending the pair
	marksAfter, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks after end: %v", err)
	}
	if !marksAfter["position-123"].FavoritedAt.Valid {
		t.Fatal("mark did not survive EndActivePair")
	}
}

func TestPositionMarksDisappearWhenUserIsDeleted(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Set a mark on a position
	if _, err := repo.TogglePositionMark(ctx, pairID, "position-456", storage.MarkHidden, 1001, now); err != nil {
		t.Fatalf("TogglePositionMark: %v", err)
	}

	// Verify the mark exists
	marksBefore, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks before delete: %v", err)
	}
	if !marksBefore["position-456"].HiddenAt.Valid {
		t.Fatal("mark was not persisted before deleting user")
	}

	// Delete one of the users (which cascades to pairs and their marks)
	if err := repo.DeleteUser(ctx, 1001); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Verify the marks for this pair are gone
	marksAfter, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks after delete: %v", err)
	}
	if len(marksAfter) != 0 {
		t.Fatalf("marks should be gone after user deletion, but got %d marks: %+v", len(marksAfter), marksAfter)
	}
}
