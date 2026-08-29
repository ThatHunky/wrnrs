package catalog

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
)

// SelectionInput drives one draw. SeedID is the pair id for paired modules and
// the telegram id for solo ones; Bucket separates independent shuffles that
// share a seed.
type SelectionInput struct {
	SeedID int64
	Bucket string
	Cycle  int
	Items  []Item
	Seen   map[string]bool
}

func SelectNext(in SelectionInput) (Item, int, bool, error) {
	if len(in.Items) == 0 {
		return Item{}, in.Cycle, false, errors.New("no eligible items")
	}

	available := make([]Item, 0, len(in.Items))
	for _, item := range in.Items {
		if !in.Seen[item.ID] {
			available = append(available, item)
		}
	}

	cycle := in.Cycle
	exhausted := false
	if len(available) == 0 {
		cycle++
		exhausted = true
		available = append(available, in.Items...)
	}

	shuffleItems(in.SeedID, in.Bucket, cycle, available)
	return available[0], cycle, exhausted, nil
}

func shuffleItems(seedID int64, bucket string, cycle int, items []Item) {
	rng := rand.New(rand.NewSource(int64(deterministicSeed(seedID, bucket, cycle))))
	rng.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
}

func deterministicSeed(seedID int64, bucket string, cycle int) uint64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%s:%d", seedID, bucket, cycle)
	return h.Sum64()
}
