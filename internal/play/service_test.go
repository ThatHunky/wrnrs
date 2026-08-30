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

func TestConsecutiveDrawsDifferWithNothingElseChanging(t *testing.T) {
	svc := play.NewService(play.ServiceOptions{Catalog: testCatalog()})

	first, state, err := svc.Next(11, play.GameState{})
	if err != nil {
		t.Fatalf("first draw: %v", err)
	}
	second, _, err := svc.Next(11, state)
	if err != nil {
		t.Fatalf("second draw: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("two consecutive draws both returned %s; the draw counter is not varying the shuffle", first.ID)
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
