package wishlist_test

import (
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/storage"
	"wrnrs/internal/wishlist"
)

func testCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Kind: "wishes", Version: 1,
		Items: []catalog.Item{
			{ID: "w003", Facets: map[string][]string{"intensity": {"bold"}}, Text: map[string]catalog.ItemText{"uk": {Title: "третє"}}},
			{ID: "w001", Facets: map[string][]string{"intensity": {"gentle"}}, Text: map[string]catalog.ItemText{"uk": {Title: "перше"}}},
			{ID: "w004", Facets: map[string][]string{"intensity": {"gentle"}}, Text: map[string]catalog.ItemText{"uk": {Title: "четверте"}}},
			{ID: "w002", Facets: map[string][]string{"intensity": {"medium"}}, Text: map[string]catalog.ItemText{"uk": {Title: "друге"}}},
			{ID: "w005", Text: map[string]catalog.ItemText{"uk": {Title: "без інтенсивності"}}},
		},
	}
}

func queueIDs(items []catalog.Item) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item.ID
	}
	return out
}

func TestQueueOrdersByIntensityThenID(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	if got := queueIDs(svc.Queue()); got != "w001,w004,w002,w003,w005" {
		t.Fatalf("queue = %q, want w001,w004,w002,w003,w005 (gentle by id, then medium, bold, then unknown last)", got)
	}
}

func TestNextUnansweredSkipsAnsweredAndFollowsQueueOrder(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})

	item, ok := svc.NextUnanswered(nil)
	if !ok || item.ID != "w001" {
		t.Fatalf("first unanswered = %s/%v, want w001/true", item.ID, ok)
	}

	answers := map[string]storage.WishAnswer{
		wishlist.AnswerKey(storage.WishKindWish, "w001"): storage.AnswerWant,
		wishlist.AnswerKey(storage.WishKindWish, "w004"): storage.AnswerNo,
	}
	item, ok = svc.NextUnanswered(answers)
	if !ok || item.ID != "w002" {
		t.Fatalf("next unanswered = %s/%v, want w002/true", item.ID, ok)
	}
}

func TestNextUnansweredReportsExhaustion(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	answers := map[string]storage.WishAnswer{}
	for _, item := range svc.Queue() {
		answers[wishlist.AnswerKey(storage.WishKindWish, item.ID)] = storage.AnswerCurious
	}
	if _, ok := svc.NextUnanswered(answers); ok {
		t.Fatal("NextUnanswered reported an item with everything answered, want exhaustion")
	}
}

func TestNextUnansweredIgnoresPositionAnswers(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	answers := map[string]storage.WishAnswer{
		wishlist.AnswerKey(storage.WishKindPosition, "w001"): storage.AnswerWant,
	}
	item, ok := svc.NextUnanswered(answers)
	if !ok || item.ID != "w001" {
		t.Fatalf("next = %s/%v, want w001/true — a position answer must not mask the wish with the same id", item.ID, ok)
	}
}

func TestProgressCountsOnlyWishes(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	answers := map[string]storage.WishAnswer{
		wishlist.AnswerKey(storage.WishKindWish, "w001"):    storage.AnswerWant,
		wishlist.AnswerKey(storage.WishKindPosition, "042"): storage.AnswerWant,
	}
	answered, total := svc.Progress(answers)
	if answered != 1 || total != 5 {
		t.Fatalf("progress = %d/%d, want 1/5 — position answers must not inflate the wish counter", answered, total)
	}
}

func TestItemLookup(t *testing.T) {
	svc := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	if item, ok := svc.Item("w002"); !ok || item.ID != "w002" {
		t.Fatalf("Item(w002) = %s/%v, want w002/true", item.ID, ok)
	}
	if _, ok := svc.Item("nope"); ok {
		t.Fatal("Item(nope) reported ok, want not found")
	}
}
