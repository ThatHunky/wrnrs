package content

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"sort"
	"strings"
)

type Deck struct {
	Version int    `json:"version"`
	Cards   []Card `json:"cards"`
}

type Card struct {
	ID                  string            `json:"id"`
	Level               int               `json:"level"`
	Tags                []string          `json:"tags,omitempty"`
	RequiresMatureOptIn bool              `json:"requires_mature_opt_in,omitempty"`
	Mode                string            `json:"mode,omitempty"`
	Text                map[string]string `json:"text"`
}

type Eligibility struct {
	Level                  int
	BothUsersMatureOptedIn bool
}

type SelectionInput struct {
	PairID int64
	Level  int
	Cycle  int
	Cards  []Card
	Seen   map[string]bool
}

func LoadDeck(r io.Reader) (*Deck, error) {
	var deck Deck
	if err := json.NewDecoder(r).Decode(&deck); err != nil {
		return nil, fmt.Errorf("decode deck: %w", err)
	}
	return &deck, nil
}

func (d *Deck) Validate(languages []string) error {
	if d.Version <= 0 {
		return errors.New("deck version must be positive")
	}
	seen := map[string]bool{}
	for i, card := range d.Cards {
		if strings.TrimSpace(card.ID) == "" {
			return fmt.Errorf("card at index %d has empty id", i)
		}
		if seen[card.ID] {
			return fmt.Errorf("card %s is duplicated", card.ID)
		}
		seen[card.ID] = true
		if card.Level <= 0 {
			return fmt.Errorf("card %s has invalid level %d", card.ID, card.Level)
		}
		for _, lang := range languages {
			if strings.TrimSpace(card.Text[lang]) == "" {
				return fmt.Errorf("card %s missing %s text", card.ID, lang)
			}
		}
		if card.RequiresMatureOptIn && !hasTag(card.Tags, "mature") {
			return fmt.Errorf("card %s requires mature opt-in but lacks mature tag", card.ID)
		}
	}
	return nil
}

func (d *Deck) EligibleCards(e Eligibility) []Card {
	cards := make([]Card, 0, len(d.Cards))
	for _, card := range d.Cards {
		if card.Level != e.Level {
			continue
		}
		if card.RequiresMatureOptIn && !e.BothUsersMatureOptedIn {
			continue
		}
		cards = append(cards, card)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		return cards[i].ID < cards[j].ID
	})
	return cards
}

func (c Card) LocalizedText(language string) (string, bool) {
	text, ok := c.Text[language]
	return text, ok && strings.TrimSpace(text) != ""
}

func SelectNextCard(input SelectionInput) (Card, int, bool, error) {
	if len(input.Cards) == 0 {
		return Card{}, input.Cycle, false, errors.New("no eligible cards")
	}

	available := make([]Card, 0, len(input.Cards))
	for _, card := range input.Cards {
		if !input.Seen[card.ID] {
			available = append(available, card)
		}
	}

	cycle := input.Cycle
	exhausted := false
	if len(available) == 0 {
		cycle++
		exhausted = true
		available = append(available, input.Cards...)
	}

	shuffleCards(input.PairID, input.Level, cycle, available)
	return available[0], cycle, exhausted, nil
}

func shuffleCards(pairID int64, level, cycle int, cards []Card) {
	seed := deterministicSeed(pairID, level, cycle)
	rng := rand.New(rand.NewSource(int64(seed)))
	rng.Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}

func deterministicSeed(pairID int64, level, cycle int) uint64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d:%d", pairID, level, cycle)
	return h.Sum64()
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}
