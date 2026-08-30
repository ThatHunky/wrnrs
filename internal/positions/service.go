package positions

import (
	"encoding/json"
	"fmt"

	"wrnrs/internal/catalog"
	"wrnrs/internal/storage"
)

// BrowseState is the transient per-user view of the catalog. It lives in Redis
// under the module key, never in the FSM slot, so it cannot clobber onboarding.
type BrowseState struct {
	Filter catalog.Filter `json:"f"`
	// MatchesOnly narrows the browse selection to positions both partners
	// are open to, per the Wishlist module's matches (storage.WishMatch).
	// It lives beside Filter, not inside it — catalog.Filter operates on
	// facet values baked into the catalog itself, while a match lives in
	// the database and depends on the pair, so it cannot be expressed as a
	// Filter.Include entry. See Service.FilterToMatches, which applies it
	// as a separate step after Filtered().
	MatchesOnly bool `json:"m"`
	Index       int  `json:"i"`
	Cycle       int  `json:"c"`
	// Draw counts every randomiser draw the caller has made, independent of
	// Cycle (which only advances once the whole selection has been fully
	// tried). It is folded into the shuffle bucket so two consecutive draws
	// with nothing else different — the common case for a solo user, who
	// has no marks to change between taps — do not replay the exact same
	// deterministic shuffle.
	Draw int `json:"d"`
}

// EncodeState serializes a BrowseState for storage outside the process
// (Redis). The encoding is plain JSON: state is not secret, and it must
// survive round trips through cache layers unmodified.
func EncodeState(state BrowseState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode browse state: %w", err)
	}
	return string(data), nil
}

// DecodeState reverses EncodeState. Garbage input (or state persisted by a
// future, incompatible version) is reported as an error rather than a
// zero-value state, so callers can fall back to a fresh browse instead of
// silently browsing with a corrupted filter.
func DecodeState(raw string) (BrowseState, error) {
	var state BrowseState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return BrowseState{}, fmt.Errorf("decode browse state: %w", err)
	}
	return state, nil
}

// ServiceOptions configures a Service. Catalog is the loaded, immutable
// content collection this module browses.
type ServiceOptions struct {
	Catalog *catalog.Catalog
}

// Service is the pure domain layer of the positions module: what the user is
// looking at, which filters apply, and how the next random pick is chosen.
// It performs no I/O — no Telegram, no database, no Redis — so it can be
// tested and reasoned about without any of those dependencies.
type Service struct {
	catalog *catalog.Catalog
}

func NewService(options ServiceOptions) *Service {
	return &Service{catalog: options.Catalog}
}

// VisibleWithMarks applies filter to the catalog and removes positions the
// pair has hidden. A pair can bury something they never want to see again;
// that must hold across every screen, so hiding is enforced here rather than
// left to each caller to remember.
func (s *Service) VisibleWithMarks(filter catalog.Filter, marks map[string]storage.PositionMark) []catalog.Item {
	if s.catalog == nil {
		return nil
	}
	filtered := s.catalog.Filtered(filter)
	if len(marks) == 0 {
		return filtered
	}
	out := make([]catalog.Item, 0, len(filtered))
	for _, item := range filtered {
		if marks[item.ID].HiddenAt.Valid {
			continue
		}
		out = append(out, item)
	}
	return out
}

// FilterToMatches narrows items to only those whose ID is a key in
// matchIDs. It is a pure, DB-free function — the caller (Handler) is the one
// that turns a pair id into matchIDs via storage.PairWishMatches — applied
// as a separate step AFTER VisibleWithMarks, never folded into
// catalog.Filter: matches live in the database and depend on the active
// pair, while Filter operates purely on facet values baked into the catalog
// at ingest time. A nil or empty matchIDs narrows to nothing, not to
// everything, so an unpaired or match-free caller sees an empty selection
// rather than an unfiltered one.
func (s *Service) FilterToMatches(items []catalog.Item, matchIDs map[string]bool) []catalog.Item {
	out := make([]catalog.Item, 0, len(items))
	for _, item := range items {
		if matchIDs[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// At resolves an index into the selection, wrapping in both directions so
// paging never dead-ends at either edge. An empty selection reports ok=false
// without panicking. Go's % keeps the sign of the dividend, so a negative
// index is normalized by adding len(items) back in whenever the raw result
// is negative — this handles any negative index, not just -1.
func (s *Service) At(items []catalog.Item, index int) (catalog.Item, int, bool) {
	if len(items) == 0 {
		return catalog.Item{}, 0, false
	}
	normalized := index % len(items)
	if normalized < 0 {
		normalized += len(items)
	}
	return items[normalized], normalized, true
}

// Random draws an untried position first and only reshuffles the whole set
// once the pair has tried everything in the current selection. That is what
// makes the randomiser feel like a suggestion engine rather than a slot
// machine: only positions with a valid TriedAt count as seen, so favorited
// and hidden-then-unhidden items are still eligible. The returned cycle must
// be persisted by the caller and passed back in on the next call — that is
// what makes a subsequent draw against an exhausted set differ from the one
// that just exhausted it, instead of repeating forever.
//
// draw must also be persisted by the caller and passed back incremented on
// every call, exhausted or not. cycle alone is not enough: catalog.SelectNext
// only changes its shuffle when the untried pool is empty and it has to
// reshuffle the whole set, so as long as at least one untried item remains,
// calling Random again with the same (seedID, items, marks, cycle) — the
// normal case for a solo user, who has no marks to change between two
// consecutive taps — is pure and returns the exact same item every time.
// Folding draw into the shuffle bucket makes every single call independent
// regardless of whether the pool was exhausted, while Seen still keeps
// untried items preferred over tried ones.
func (s *Service) Random(seedID int64, items []catalog.Item, marks map[string]storage.PositionMark, cycle, draw int) (catalog.Item, int, error) {
	seen := make(map[string]bool, len(marks))
	for id, mark := range marks {
		if mark.TriedAt.Valid {
			seen[id] = true
		}
	}
	item, nextCycle, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: seedID,
		Bucket: fmt.Sprintf("positions:%d", draw),
		Cycle:  cycle,
		Items:  items,
		Seen:   seen,
	})
	if err != nil {
		return catalog.Item{}, cycle, err
	}
	return item, nextCycle, nil
}
