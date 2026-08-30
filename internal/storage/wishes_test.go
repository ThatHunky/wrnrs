package storage_test

import (
	"context"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

func TestSetWishAnswerUpsertsAndReadsBack(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	_ = pairID
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers: %v", err)
	}
	if got := answers["wish:w001"]; got != storage.AnswerWant {
		t.Fatalf("answer = %q, want %q", got, storage.AnswerWant)
	}

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerNo, now); err != nil {
		t.Fatalf("SetWishAnswer overwrite: %v", err)
	}
	answers, err = repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after overwrite: %v", err)
	}
	if got := answers["wish:w001"]; got != storage.AnswerNo {
		t.Fatalf("answer after overwrite = %q, want %q — the same item must update, not duplicate", got, storage.AnswerNo)
	}
	if len(answers) != 1 {
		t.Fatalf("answers = %v, want exactly one row after an overwrite", answers)
	}
}

func TestSetWishAnswerRejectsUnknownValues(t *testing.T) {
	repo, _ := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.WishAnswer("maybe"), now); err == nil {
		t.Fatal("SetWishAnswer with an unknown answer succeeded, want an error")
	}
	if err := repo.SetWishAnswer(ctx, 1001, storage.WishItemKind("song"), "w001", storage.AnswerWant, now); err == nil {
		t.Fatal("SetWishAnswer with an unknown item kind succeeded, want an error")
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers: %v", err)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %v, want none written after rejected calls", answers)
	}
}

func TestPairWishMatchesAppliesTheMatchRule(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	cases := []struct {
		item   string
		a, b   storage.WishAnswer
		match  bool
		strong bool
	}{
		{"w001", storage.AnswerWant, storage.AnswerWant, true, true},
		{"w002", storage.AnswerWant, storage.AnswerCurious, true, false},
		{"w003", storage.AnswerCurious, storage.AnswerWant, true, false},
		{"w004", storage.AnswerCurious, storage.AnswerCurious, false, false},
		{"w005", storage.AnswerWant, storage.AnswerNo, false, false},
		{"w006", storage.AnswerNo, storage.AnswerWant, false, false},
		{"w007", storage.AnswerNo, storage.AnswerNo, false, false},
		{"w008", storage.AnswerCurious, storage.AnswerNo, false, false},
		{"w009", storage.AnswerNo, storage.AnswerCurious, false, false},
	}
	for _, c := range cases {
		if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, c.item, c.a, now); err != nil {
			t.Fatalf("SetWishAnswer A %s: %v", c.item, err)
		}
		if err := repo.SetWishAnswer(ctx, 1002, storage.WishKindWish, c.item, c.b, now); err != nil {
			t.Fatalf("SetWishAnswer B %s: %v", c.item, err)
		}
	}

	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	got := map[string]bool{}
	strong := map[string]bool{}
	for _, m := range matches {
		got[m.ItemID] = true
		strong[m.ItemID] = m.Strong
	}
	for _, c := range cases {
		if got[c.item] != c.match {
			t.Fatalf("%s (%s + %s): match = %v, want %v", c.item, c.a, c.b, got[c.item], c.match)
		}
		if c.match && strong[c.item] != c.strong {
			t.Fatalf("%s (%s + %s): strong = %v, want %v", c.item, c.a, c.b, strong[c.item], c.strong)
		}
	}
}

func TestPairWishMatchesNeedsBothAnswers(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %v, want none — only one partner has answered", matches)
	}
}

func TestPairWishMatchesSeparatesItemKinds(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Both kinds share the same item id "001" but get different, both
	// non-"no" answers. If the join ever lost its item_kind condition, these
	// rows would cross-match: wish's "want" would pair with position's
	// "curious" (and vice versa), producing extra rows and a wrong Strong
	// value instead of silently disappearing. A fixture where the second
	// kind is all "no" (as before) can't catch that: "no" is filtered out by
	// the match predicate regardless of whether the kind join exists, so
	// removing the kind join left that version of this test passing.
	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer wish A: %v", err)
	}
	if err := repo.SetWishAnswer(ctx, 1002, storage.WishKindWish, "001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer wish B: %v", err)
	}
	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindPosition, "001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer position A: %v", err)
	}
	if err := repo.SetWishAnswer(ctx, 1002, storage.WishKindPosition, "001", storage.AnswerCurious, now); err != nil {
		t.Fatalf("SetWishAnswer position B: %v", err)
	}

	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %+v, want exactly 2 (one wish, one position) — a missing item_kind join would cross-match the two kinds sharing id \"001\" into extra rows", matches)
	}

	want := map[storage.WishItemKind]storage.WishMatch{
		storage.WishKindWish:     {ItemKind: storage.WishKindWish, ItemID: "001", Strong: true},
		storage.WishKindPosition: {ItemKind: storage.WishKindPosition, ItemID: "001", Strong: false},
	}
	seen := map[storage.WishItemKind]bool{}
	for _, m := range matches {
		if seen[m.ItemKind] {
			t.Fatalf("matches = %+v, want at most one match per kind — a duplicate %q entry indicates the two kinds were cross-joined", matches, m.ItemKind)
		}
		seen[m.ItemKind] = true
		wantMatch, ok := want[m.ItemKind]
		if !ok {
			t.Fatalf("unexpected item kind %q in matches %+v", m.ItemKind, matches)
		}
		if m != wantMatch {
			t.Fatalf("match for kind %q = %+v, want %+v", m.ItemKind, m, wantMatch)
		}
	}
	for kind := range want {
		if !seen[kind] {
			t.Fatalf("matches = %+v, missing expected kind %q", matches, kind)
		}
	}
}

func TestPartnerHasAnyWishAnswerIsABooleanNotACount(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	started, err := repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if started {
		t.Fatal("partner reported as started before answering anything")
	}

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	started, err = repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if started {
		t.Fatal("the caller's own answer was counted as the partner's")
	}

	if err := repo.SetWishAnswer(ctx, 1002, storage.WishKindWish, "w001", storage.AnswerNo, now); err != nil {
		t.Fatalf("SetWishAnswer partner: %v", err)
	}
	started, err = repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if !started {
		t.Fatal("partner answered but is not reported as started")
	}
}

func TestWishAnswersSurvivePairBreak(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	_ = pairID
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	if _, err := repo.EndActivePair(ctx, 1001, now); err != nil {
		t.Fatalf("EndActivePair: %v", err)
	}

	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after break: %v", err)
	}
	if answers["wish:w001"] != storage.AnswerWant {
		t.Fatalf("answers = %v, want the answer to survive the break — wishes belong to the person, not the relationship", answers)
	}
}

func TestWishAnswersDisappearWhenUserIsDeleted(t *testing.T) {
	repo, _ := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	if err := repo.DeleteUser(ctx, 1001); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after delete: %v", err)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %v, want none after account deletion", answers)
	}
}
