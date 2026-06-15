package content_test

import (
	"strings"
	"testing"

	"wrnrs/internal/content"
)

func TestDeckValidationRequiresAllConfiguredLanguages(t *testing.T) {
	raw := `{
		"version": 1,
		"cards": [
			{
				"id": "q001",
				"level": 1,
				"tags": ["warmup"],
				"mode": "both_players",
				"text": {"uk": "Привіт"}
			}
		]
	}`

	deck, err := content.LoadDeck(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadDeck returned error: %v", err)
	}

	err = deck.Validate([]string{"uk", "en"})
	if err == nil {
		t.Fatal("Validate succeeded, expected missing locale error")
	}
	if !strings.Contains(err.Error(), "q001") || !strings.Contains(err.Error(), "en") {
		t.Fatalf("Validate error %q does not mention missing card and locale", err)
	}
}

func TestEligibleCardsExcludeMatureUntilPairIsMatureOptedIn(t *testing.T) {
	deck := content.Deck{
		Version: 1,
		Cards: []content.Card{
			{ID: "safe", Level: 3, Text: map[string]string{"uk": "safe", "en": "safe"}},
			{ID: "mature", Level: 3, RequiresMatureOptIn: true, Tags: []string{"mature", "sex"}, Text: map[string]string{"uk": "mature", "en": "mature"}},
			{ID: "other-level", Level: 2, Text: map[string]string{"uk": "other", "en": "other"}},
		},
	}

	safe := deck.EligibleCards(content.Eligibility{Level: 3, BothUsersMatureOptedIn: false})
	if got := cardIDs(safe); strings.Join(got, ",") != "safe" {
		t.Fatalf("safe eligible IDs = %v, want only safe", got)
	}

	mature := deck.EligibleCards(content.Eligibility{Level: 3, BothUsersMatureOptedIn: true})
	if got := cardIDs(mature); strings.Join(got, ",") != "mature,safe" {
		t.Fatalf("mature eligible IDs = %v, want mature and safe sorted by deterministic selector order", got)
	}
}

func TestSelectNextCardAvoidsSeenCardsUntilExhaustedThenStartsNextCycle(t *testing.T) {
	cards := []content.Card{
		{ID: "q001", Level: 1},
		{ID: "q002", Level: 1},
		{ID: "q003", Level: 1},
	}

	first, cycle, exhausted, err := content.SelectNextCard(content.SelectionInput{
		PairID: 42,
		Level:  1,
		Cycle:  0,
		Cards:  cards,
		Seen:   map[string]bool{"q001": true, "q003": true},
	})
	if err != nil {
		t.Fatalf("SelectNextCard returned error: %v", err)
	}
	if first.ID != "q002" || cycle != 0 || exhausted {
		t.Fatalf("selection = (%s, cycle %d, exhausted %v), want q002 cycle 0 not exhausted", first.ID, cycle, exhausted)
	}

	next, cycle, exhausted, err := content.SelectNextCard(content.SelectionInput{
		PairID: 42,
		Level:  1,
		Cycle:  0,
		Cards:  cards,
		Seen:   map[string]bool{"q001": true, "q002": true, "q003": true},
	})
	if err != nil {
		t.Fatalf("SelectNextCard returned error for exhausted deck: %v", err)
	}
	if cycle != 1 || !exhausted {
		t.Fatalf("exhausted selection cycle=%d exhausted=%v, want cycle 1 exhausted true", cycle, exhausted)
	}
	if next.ID == "" {
		t.Fatal("expected a card from the next cycle")
	}
}

func cardIDs(cards []content.Card) []string {
	ids := make([]string, len(cards))
	for i, card := range cards {
		ids[i] = card.ID
	}
	return ids
}
