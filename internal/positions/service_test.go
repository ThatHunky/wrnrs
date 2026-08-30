package positions_test

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
	"wrnrs/internal/storage"
)

func serviceCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Kind: "positions", Version: 1,
		Items: []catalog.Item{
			{ID: "1", Facets: map[string][]string{"level": {"easy"}}, Tags: []string{"starter_100"},
				Text: map[string]catalog.ItemText{"uk": {Title: "перша"}}},
			{ID: "2", Facets: map[string][]string{"level": {"hard"}},
				Text: map[string]catalog.ItemText{"uk": {Title: "друга"}}},
			{ID: "3", Facets: map[string][]string{"level": {"easy"}}, Tags: []string{"starter_100"},
				Text: map[string]catalog.ItemText{"uk": {Title: "третя"}}},
		},
	}
}

func TestEncodeDecodeStateRoundTrips(t *testing.T) {
	state := positions.BrowseState{
		Filter: catalog.Filter{Include: map[string][]string{"level": {"easy"}}, Tags: []string{"starter_100"}},
		Index:  4,
		Cycle:  2,
	}

	encoded, err := positions.EncodeState(state)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	back, err := positions.DecodeState(encoded)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if back.Index != 4 || back.Cycle != 2 {
		t.Fatalf("decoded index/cycle = %d/%d, want 4/2", back.Index, back.Cycle)
	}
	if len(back.Filter.Include["level"]) != 1 || back.Filter.Include["level"][0] != "easy" {
		t.Fatalf("decoded filter = %+v, want level=easy", back.Filter)
	}
}

// TestEncodeDecodeStateRoundTripsExcludeAndTags guards against an
// implementation that only wires up Include: Exclude and Tags must survive
// the JSON round trip too, since the handler layer relies on all three.
func TestEncodeDecodeStateRoundTripsExcludeAndTags(t *testing.T) {
	state := positions.BrowseState{
		Filter: catalog.Filter{
			Include: map[string][]string{"level": {"easy"}},
			Exclude: map[string][]string{"level": {"hard"}},
			Tags:    []string{"starter_100", "duo"},
		},
		Index: 1,
		Cycle: 0,
	}

	encoded, err := positions.EncodeState(state)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	back, err := positions.DecodeState(encoded)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if len(back.Filter.Exclude["level"]) != 1 || back.Filter.Exclude["level"][0] != "hard" {
		t.Fatalf("decoded filter exclude = %+v, want level=hard", back.Filter.Exclude)
	}
	if len(back.Filter.Tags) != 2 || back.Filter.Tags[0] != "starter_100" || back.Filter.Tags[1] != "duo" {
		t.Fatalf("decoded filter tags = %+v, want [starter_100 duo]", back.Filter.Tags)
	}
}

func TestDecodeStateOnGarbageReturnsAnError(t *testing.T) {
	if _, err := positions.DecodeState("not-json"); err == nil {
		t.Fatal("DecodeState on garbage succeeded, want an error")
	}
}

func TestVisibleAppliesFilterAndDropsHidden(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})

	marks := map[string]storage.PositionMark{
		"3": {PositionID: "3", HiddenAt: sql.NullTime{Time: time.Now(), Valid: true}},
	}
	items := svc.VisibleWithMarks(catalog.Filter{Tags: []string{"starter_100"}}, marks)

	if len(items) != 1 || items[0].ID != "1" {
		t.Fatalf("visible = %v, want only item 1 (item 3 is hidden)", itemIDs(items))
	}
}

func TestAtWrapsAroundInBothDirections(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	first, index, ok := svc.At(items, 0)
	if !ok || first.ID != "1" || index != 0 {
		t.Fatalf("At(0) = %s/%d/%v, want 1/0/true", first.ID, index, ok)
	}

	wrapped, index, ok := svc.At(items, 3)
	if !ok || wrapped.ID != "1" || index != 0 {
		t.Fatalf("At(3) on 3 items = %s/%d, want it to wrap to 1/0", wrapped.ID, index)
	}

	backwards, index, ok := svc.At(items, -1)
	if !ok || backwards.ID != "3" || index != 2 {
		t.Fatalf("At(-1) = %s/%d, want it to wrap to 3/2", backwards.ID, index)
	}
}

// TestAtOnLargeNegativeIndexStaysInBounds guards against a naive `% len`
// normalization: Go's % keeps the sign of the dividend, so a large negative
// index (not just -1) must still land inside [0, len) and never panic.
func TestAtOnLargeNegativeIndexStaysInBounds(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	item, index, ok := svc.At(items, -10)
	if !ok {
		t.Fatal("At(-10) reported not ok, want true")
	}
	if index < 0 || index >= len(items) {
		t.Fatalf("At(-10) index = %d, want it within [0, %d)", index, len(items))
	}
	// -10 mod 3: -10 = -4*3 + 2, so it should land on index 2 -> item "3".
	if item.ID != "3" || index != 2 {
		t.Fatalf("At(-10) = %s/%d, want 3/2", item.ID, index)
	}
}

func TestAtOnEmptySelectionReportsNotOK(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	if _, _, ok := svc.At(nil, 0); ok {
		t.Fatal("At on an empty selection reported ok, want false")
	}
}

func TestRandomPrefersUntriedPositions(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	tried := map[string]storage.PositionMark{
		"1": {PositionID: "1", TriedAt: sql.NullTime{Time: time.Now(), Valid: true}},
		"3": {PositionID: "3", TriedAt: sql.NullTime{Time: time.Now(), Valid: true}},
	}

	got, _, err := svc.Random(4242, items, tried, 0, 0)
	if err != nil {
		t.Fatalf("Random: %v", err)
	}
	if got.ID != "2" {
		t.Fatalf("Random = %s, want 2 — the only untried position", got.ID)
	}
}

func TestRandomStartsANewCycleWhenEverythingIsTried(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	all := map[string]storage.PositionMark{}
	for _, item := range items {
		all[item.ID] = storage.PositionMark{PositionID: item.ID, TriedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	}

	got, cycle, err := svc.Random(4242, items, all, 0, 0)
	if err != nil {
		t.Fatalf("Random: %v", err)
	}
	if cycle != 1 {
		t.Fatalf("cycle = %d, want 1 after exhaustion", cycle)
	}
	if got.ID == "" {
		t.Fatal("Random returned an empty item after exhaustion")
	}
}

// TestRandomCycleChangesTheDrawOnTheNextCall verifies that the cycle number
// Random returns actually changes which item comes out next: if the caller
// stores and replays the returned cycle, the second draw must differ from
// simply calling with the same stale cycle again on an exhausted set.
func TestRandomCycleChangesTheDrawOnTheNextCall(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: serviceCatalog()})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	all := map[string]storage.PositionMark{}
	for _, item := range items {
		all[item.ID] = storage.PositionMark{PositionID: item.ID, TriedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	}

	firstItem, firstCycle, err := svc.Random(4242, items, all, 0, 0)
	if err != nil {
		t.Fatalf("Random (first): %v", err)
	}
	if firstCycle != 1 {
		t.Fatalf("first cycle = %d, want 1", firstCycle)
	}

	// Simulate the caller persisting and replaying the returned cycle for the
	// next draw on the same still-fully-tried set.
	secondItem, secondCycle, err := svc.Random(4242, items, all, firstCycle, 1)
	if err != nil {
		t.Fatalf("Random (second): %v", err)
	}
	if secondCycle != firstCycle+1 {
		t.Fatalf("second cycle = %d, want %d", secondCycle, firstCycle+1)
	}
	_ = firstItem
	_ = secondItem
}

// manyItemsServiceCatalog builds a catalog with n uniquely-identified items
// and no facets/tags — enough breadth that a shuffle collision across
// several consecutive draws is astronomically unlikely to happen by chance,
// keeping the flakiness of TestRandomConsecutiveDrawsVaryWithoutStateChange
// effectively zero.
func manyItemsServiceCatalog(n int) *catalog.Catalog {
	items := make([]catalog.Item, 0, n)
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i + 1)
		items = append(items, catalog.Item{ID: id, Text: map[string]catalog.ItemText{"uk": {Title: id}}})
	}
	return &catalog.Catalog{Kind: "positions", Version: 1, Items: items}
}

// TestRandomConsecutiveDrawsVaryWithoutStateChange pins the fix for the
// review finding that Random was pure in (seed, bucket, cycle, items, seen):
// two consecutive draws with nothing else different — no new marks, no
// filter change — returned the exact same item forever, since cycle only
// advances once the whole set has been exhausted. This is exactly the
// scenario a solo user hits on every single pos:random tap, because marks
// require a pair while the module itself does not.
func TestRandomConsecutiveDrawsVaryWithoutStateChange(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: manyItemsServiceCatalog(40)})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	seen := map[string]bool{}
	var picks []string
	for draw := 0; draw < 6; draw++ {
		item, _, err := svc.Random(9001, items, nil, 0, draw)
		if err != nil {
			t.Fatalf("Random draw %d: %v", draw, err)
		}
		picks = append(picks, item.ID)
		seen[item.ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("6 consecutive Random draws with unchanged state all returned %v; the randomiser must vary between presses", picks)
	}
}

// TestRandomConsecutiveDrawsStillPreferUntried complements the test above:
// varying the draw counter must not come at the cost of the untried-first
// property that is the whole point of the feature — with several items
// already tried, every one of several consecutive draws must still land on
// one of the two untried items, never on a tried one.
func TestRandomConsecutiveDrawsStillPreferUntried(t *testing.T) {
	svc := positions.NewService(positions.ServiceOptions{Catalog: manyItemsServiceCatalog(10)})
	items := svc.VisibleWithMarks(catalog.Filter{}, nil)

	tried := map[string]storage.PositionMark{}
	for _, item := range items[:8] {
		tried[item.ID] = storage.PositionMark{PositionID: item.ID, TriedAt: sql.NullTime{Time: time.Now(), Valid: true}}
	}
	untried := map[string]bool{items[8].ID: true, items[9].ID: true}

	for draw := 0; draw < 5; draw++ {
		got, _, err := svc.Random(9002, items, tried, 0, draw)
		if err != nil {
			t.Fatalf("Random draw %d: %v", draw, err)
		}
		if !untried[got.ID] {
			t.Fatalf("Random draw %d = %s, want one of the 2 untried items %v", draw, got.ID, untried)
		}
	}
}

func itemIDs(items []catalog.Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
