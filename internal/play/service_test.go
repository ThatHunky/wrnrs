package play_test

import (
	"fmt"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/play"
)

func testCatalog() *catalog.Catalog {
	items := []catalog.Item{}
	for _, spec := range []struct {
		id, kind, intensity string
	}{
		{"p001", "truth", "gentle"},
		{"p002", "dare", "gentle"},
		{"p003", "truth", "medium"},
		{"p004", "dare", "medium"},
		{"p005", "truth", "bold"},
		{"p006", "dare", "bold"},
	} {
		items = append(items, catalog.Item{
			ID:     spec.id,
			Facets: map[string][]string{"kind": {spec.kind}, "intensity": {spec.intensity}},
			Text:   map[string]catalog.ItemText{"uk": {Title: spec.id, Body: "текст " + spec.id}},
		})
	}
	return &catalog.Catalog{Kind: "play", Version: 1, Items: items}
}

func TestAvailableAppliesTheFilter(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

	all := svc.Available(catalog.Filter{})
	if len(all) != 6 {
		t.Fatalf("Available(empty) = %d items, want 6", len(all))
	}

	truths := svc.Available(catalog.Filter{Include: map[string][]string{"kind": {"truth"}}})
	if len(truths) != 3 {
		t.Fatalf("Available(kind=truth) = %d items, want 3", len(truths))
	}
	for _, item := range truths {
		if item.Facets["kind"][0] != "truth" {
			t.Fatalf("item %s is %s, want truth", item.ID, item.Facets["kind"][0])
		}
	}
}

func TestNextIncrementsDrawAndRecordsRecent(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

	item, state, err := svc.Next(42, play.GameState{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if state.Draw != 1 {
		t.Fatalf("Draw = %d, want 1", state.Draw)
	}
	if len(state.Recent) != 1 || state.Recent[0] != item.ID {
		t.Fatalf("Recent = %v, want just the drawn id %s", state.Recent, item.ID)
	}
	if state.TurnB {
		t.Fatal("Next flipped the turn; that is the handler's decision, not the service's")
	}
}

// TestNextPreservesTurnB pins the other half of the "Next never flips the
// turn" contract: TestNextIncrementsDrawAndRecordsRecent only ever calls
// Next with TurnB false (the zero value), so an implementation that reset
// TurnB unconditionally — e.g. built its result from a fresh zero-value
// GameState instead of `next := state` — would pass every existing test.
// Running both false and true closes that gap; true is the case that
// actually exercises preservation rather than merely reproducing a zero
// value.
func TestNextPreservesTurnB(t *testing.T) {
	for _, turnB := range []bool{false, true} {
		t.Run(fmt.Sprintf("TurnB=%v", turnB), func(t *testing.T) {
			svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

			_, state, err := svc.Next(42, play.GameState{TurnB: turnB})
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			if state.TurnB != turnB {
				t.Fatalf("TurnB = %v, want %v; Next must not change whose turn it is", state.TurnB, turnB)
			}
		})
	}
}

func TestNextAvoidsRecentCards(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

	state := play.GameState{}
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		var item catalog.Item
		var err error
		item, state, err = svc.Next(7, state)
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if seen[item.ID] {
			t.Fatalf("draw %d returned %s again while only %d of 6 cards had been drawn", i, item.ID, i)
		}
		seen[item.ID] = true
	}
}

func TestNextClearsRecentWhenEverythingHasBeenSeen(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

	state := play.GameState{}
	for i := 0; i < 6; i++ {
		var err error
		_, state, err = svc.Next(7, state)
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
	}
	item, state, err := svc.Next(7, state)
	if err != nil {
		t.Fatalf("draw after exhaustion: %v", err)
	}
	if item.ID == "" {
		t.Fatal("draw after exhaustion returned an empty item; the ring must clear rather than dead-end")
	}
	if len(state.Recent) != 1 {
		t.Fatalf("Recent = %v, want the ring cleared down to just the new draw", state.Recent)
	}
}

func TestNextBoundsTheRecentRing(t *testing.T) {
	items := []catalog.Item{}
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("p%03d", i)
		items = append(items, catalog.Item{
			ID:     id,
			Facets: map[string][]string{"kind": {"dare"}, "intensity": {"gentle"}},
			Text:   map[string]catalog.ItemText{"uk": {Title: id, Body: "текст"}},
		})
	}
	svc := play.NewService(play.ServiceOptions{Catalog: &catalog.Catalog{Kind: "play", Version: 1, Items: items}})

	state := play.GameState{}
	for i := 0; i < 30; i++ {
		var err error
		_, state, err = svc.Next(3, state)
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
	}
	if len(state.Recent) > play.RecentLimit {
		t.Fatalf("Recent grew to %d, want at most %d", len(state.Recent), play.RecentLimit)
	}
}

// TestNextVariesTheShuffleBucketWhenSeenIsUnchanged pins Recent (and so
// Seen) identical across both calls, with enough items that the ring never
// clears, so the only thing that can differ between the two draws is Draw
// folded into the shuffle bucket. testCatalog's 6 items are too few for
// this: draw 1 always adds its card to Recent, so draw 2's Seen set is
// necessarily different from draw 1's — Seen alone could then explain a
// different result, which would make the assertion pass even with a
// constant bucket. Holding Recent fixed by hand, over a catalog large
// enough that Recent never approaches the full item count, closes that gap.
func TestNextVariesTheShuffleBucketWhenSeenIsUnchanged(t *testing.T) {
	items := []catalog.Item{}
	for i := 1; i <= 40; i++ {
		id := fmt.Sprintf("p%03d", i)
		items = append(items, catalog.Item{
			ID:     id,
			Facets: map[string][]string{"kind": {"dare"}, "intensity": {"gentle"}},
			Text:   map[string]catalog.ItemText{"uk": {Title: id, Body: "текст"}},
		})
	}
	svc := play.NewService(play.ServiceOptions{Catalog: &catalog.Catalog{Kind: "play", Version: 1, Items: items}})

	recent := []string{"p001", "p002", "p003", "p004", "p005", "p006", "p007", "p008", "p009", "p010"}
	first, _, err := svc.Next(11, play.GameState{Draw: 0, Recent: append([]string(nil), recent...)})
	if err != nil {
		t.Fatalf("first draw: %v", err)
	}
	second, _, err := svc.Next(11, play.GameState{Draw: 1, Recent: append([]string(nil), recent...)})
	if err != nil {
		t.Fatalf("second draw: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("two draws with identical Recent (so identical Seen) but Draw 0 vs 1 both returned %s; the draw counter is not varying the shuffle bucket", first.ID)
	}
}

func TestNextOnAnEmptySelectionReturnsAnError(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})
	_, _, err := svc.Next(1, play.GameState{
		Filter: catalog.Filter{Include: map[string][]string{"kind": {"nothing"}}},
	})
	if err == nil {
		t.Fatal("Next on an empty selection succeeded, want an error the handler can turn into a message")
	}
}

func TestEncodeDecodeStateRoundTripsEveryField(t *testing.T) {
	state := play.GameState{
		Filter: catalog.Filter{Include: map[string][]string{"kind": {"dare"}, "intensity": {"bold"}}},
		Draw:   9,
		Recent: []string{"p003", "p007"},
		TurnB:  true,
	}
	encoded, err := play.EncodeState(state)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	back, err := play.DecodeState(encoded)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if back.Draw != 9 || !back.TurnB {
		t.Fatalf("decoded Draw/TurnB = %d/%v, want 9/true", back.Draw, back.TurnB)
	}
	if len(back.Recent) != 2 || back.Recent[0] != "p003" || back.Recent[1] != "p007" {
		t.Fatalf("decoded Recent = %v, want [p003 p007]", back.Recent)
	}
	if len(back.Filter.Include["intensity"]) != 1 || back.Filter.Include["intensity"][0] != "bold" {
		t.Fatalf("decoded filter = %+v, want intensity=bold preserved", back.Filter)
	}
}

func TestDecodeStateOnGarbageReturnsAnError(t *testing.T) {
	if _, err := play.DecodeState("not-json"); err == nil {
		t.Fatal("DecodeState on garbage succeeded, want an error")
	}
}

func TestItemLookup(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})
	if item, ok := svc.Item("p004"); !ok || item.ID != "p004" {
		t.Fatalf("Item(p004) = %s/%v, want p004/true", item.ID, ok)
	}
	if _, ok := svc.Item("nope"); ok {
		t.Fatal("Item(nope) reported ok, want not found")
	}
}

func TestNilCatalogDoesNotPanic(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{})
	if got := svc.Available(catalog.Filter{}); len(got) != 0 {
		t.Fatalf("Available on a nil catalog = %v, want empty", got)
	}
	if _, _, err := svc.Next(1, play.GameState{}); err == nil {
		t.Fatal("Next on a nil catalog succeeded, want an error")
	}
	if _, ok := svc.Item("p001"); ok {
		t.Fatal("Item on a nil catalog reported ok")
	}
}
