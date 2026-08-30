// Package wishlist implements the private wish-matching module: each partner
// answers privately and only mutual matches are ever surfaced. This file is
// the I/O-free half — no Telegram, no database, no Redis.
package wishlist

import (
	"sort"

	"wrnrs/internal/catalog"
	"wrnrs/internal/storage"
)

// intensityRank orders the queue so a person meets the gentler items first
// and can stop whenever they like. An item whose intensity is unknown sorts
// after every known one, so introducing a new value never makes it jump to
// the front of everybody's queue.
var intensityRank = map[string]int{"gentle": 0, "medium": 1, "bold": 2}

const unknownIntensityRank = 99

// AnswerKey builds the key UserWishAnswers is keyed by. Kind is part of the
// key because the wishes and positions catalogs have overlapping ids.
func AnswerKey(kind storage.WishItemKind, itemID string) string {
	return string(kind) + ":" + itemID
}

// ServiceOptions configures a Service. Catalog is the loaded, immutable
// wishes catalog this module offers.
type ServiceOptions struct {
	Catalog *catalog.Catalog
}

// Service is the pure domain layer of the wishlist module: which wish to
// offer next, in what order, and how far along a person is. It performs no
// I/O — no Telegram, no database, no Redis — so it can be tested and
// reasoned about without any of those dependencies.
type Service struct {
	queue []catalog.Item
}

// NewService builds the deterministic queue once, up front, so every later
// call just walks a precomputed slice instead of re-sorting on every use.
func NewService(options ServiceOptions) *Service {
	s := &Service{}
	if options.Catalog == nil {
		return s
	}
	s.queue = make([]catalog.Item, len(options.Catalog.Items))
	copy(s.queue, options.Catalog.Items)
	sort.SliceStable(s.queue, func(i, j int) bool {
		ri, rj := rankOf(s.queue[i]), rankOf(s.queue[j])
		if ri != rj {
			return ri < rj
		}
		return s.queue[i].ID < s.queue[j].ID
	})
	return s
}

func rankOf(item catalog.Item) int {
	values := item.Facets["intensity"]
	if len(values) == 0 {
		return unknownIntensityRank
	}
	if rank, ok := intensityRank[values[0]]; ok {
		return rank
	}
	return unknownIntensityRank
}

// Queue returns the wishes in the order they are offered. The returned slice
// is a copy, so a caller mutating it cannot reorder the service's own queue.
func (s *Service) Queue() []catalog.Item {
	out := make([]catalog.Item, len(s.queue))
	copy(out, s.queue)
	return out
}

// NextUnanswered returns the first wish, in queue order, that answers does
// not yet cover. Only storage.WishKindWish keys are consulted: a position
// answer with the same item id must never mask a wish.
func (s *Service) NextUnanswered(answers map[string]storage.WishAnswer) (catalog.Item, bool) {
	for _, item := range s.queue {
		if _, done := answers[AnswerKey(storage.WishKindWish, item.ID)]; !done {
			return item, true
		}
	}
	return catalog.Item{}, false
}

// Progress counts answered wishes against the catalog size. Position answers
// are deliberately excluded: positions are voted on lazily from the other
// module and have no meaningful denominator here — counting them would show
// nonsense like "63 of 60".
func (s *Service) Progress(answers map[string]storage.WishAnswer) (answered, total int) {
	for _, item := range s.queue {
		if _, done := answers[AnswerKey(storage.WishKindWish, item.ID)]; done {
			answered++
		}
	}
	return answered, len(s.queue)
}

// Item looks up a single wish by id.
func (s *Service) Item(id string) (catalog.Item, bool) {
	for _, item := range s.queue {
		if item.ID == id {
			return item, true
		}
	}
	return catalog.Item{}, false
}
