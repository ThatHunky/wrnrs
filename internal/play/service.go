// Package play implements the truth-or-dare module: one couple, one phone,
// cards drawn in turn. This file is the I/O-free half — no Telegram, no
// database, no Redis.
package play

import (
	"encoding/json"
	"fmt"

	"wrnrs/internal/catalog"
)

// RecentLimit bounds the ring of recently drawn cards. The ring exists so a
// card does not come back immediately; the real no-repeat work is done by
// catalog.SelectNext, so a longer ring would buy nothing.
const RecentLimit = 15

// GameState is the whole of a play session. It lives in Redis under the
// module key, never in SQLite: it is the state of an evening, not history.
type GameState struct {
	Filter catalog.Filter `json:"f"`
	// Draw counts every card drawn. It is folded into the shuffle bucket so
	// two consecutive taps with nothing else changed cannot replay the same
	// deterministic shuffle.
	Draw int `json:"d"`
	// Recent holds the last RecentLimit card ids drawn.
	Recent []string `json:"r"`
	// TurnB is true when the next card addresses partner B rather than A.
	// The service never changes it — whose turn it is depends on whether the
	// player took the card or skipped it, which only the handler knows.
	TurnB bool `json:"t"`
}

func EncodeState(state GameState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode play state: %w", err)
	}
	return string(data), nil
}

func DecodeState(raw string) (GameState, error) {
	var state GameState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return GameState{}, fmt.Errorf("decode play state: %w", err)
	}
	return state, nil
}

type ServiceOptions struct {
	Catalog *catalog.Catalog
}

type Service struct {
	catalog *catalog.Catalog
}

func NewService(options ServiceOptions) *Service {
	return &Service{catalog: options.Catalog}
}

// Available returns the cards the filter admits, in catalog order.
func (s *Service) Available(filter catalog.Filter) []catalog.Item {
	if s.catalog == nil {
		return nil
	}
	return s.catalog.Filtered(filter)
}

// Next draws a card and returns the updated state. It does not flip the
// turn: that is the handler's call, because a skipped card must not.
//
// nonce is normally "", which keeps the draw fully deterministic in
// (seedID, Draw): the same tap on the same state always deals the same
// card. The handler passes a non-empty nonce only when the state it just
// drew from could not be persisted — Draw will never advance, so without
// something varying per tap every subsequent draw would replay the same
// shuffle and deal the identical card forever. Keeping the nonce a
// parameter is what lets this package stay I/O-free: the clock lives in the
// handler, not here.
func (s *Service) Next(seedID int64, nonce string, state GameState) (catalog.Item, GameState, error) {
	items := s.Available(state.Filter)
	if len(items) == 0 {
		return catalog.Item{}, state, fmt.Errorf("no cards match the current filter")
	}

	recent := state.Recent
	if len(recent) >= len(items) {
		// Everything on offer is in the ring. Clearing it is the only way
		// to keep playing; without this the draw would dead-end.
		recent = nil
	}
	seen := make(map[string]bool, len(recent))
	for _, id := range recent {
		seen[id] = true
	}

	bucket := fmt.Sprintf("play:%d", state.Draw)
	if nonce != "" {
		bucket += ":" + nonce
	}

	item, _, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: seedID,
		Bucket: bucket,
		Items:  items,
		Seen:   seen,
	})
	if err != nil {
		return catalog.Item{}, state, err
	}

	next := state
	next.Draw = state.Draw + 1
	next.Recent = append(append([]string{}, recent...), item.ID)
	if len(next.Recent) > RecentLimit {
		next.Recent = next.Recent[len(next.Recent)-RecentLimit:]
	}
	return item, next, nil
}

func (s *Service) Item(id string) (catalog.Item, bool) {
	if s.catalog == nil {
		return catalog.Item{}, false
	}
	return s.catalog.Item(id)
}
