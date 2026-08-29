# Каркас модулів суперапу — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Дати боту узагальнений движок каталогів і реєстр модулів, щоб кожен наступний модуль суперапу додавався записом у реєстрі, а не правками в `internal/app/app.go`.

**Architecture:** Два нові листові пакети без залежностей від `app`: `internal/catalog` (узагальнення `internal/content/deck.go` — фасетна фільтрація + детермінований вибір без повторів) і `internal/modules` (гейт доступу + реєстр з диспетчеризацією за префіксом колбека). `internal/app` отримує реєстр через `Options`, резолвить стан користувача з наявних методів репозиторію і делегує колбеки/повідомлення модулям **до** свого великого `switch`. Наявні флоу не переписуються.

**Tech Stack:** Go 1.24, стандартна бібліотека, `modernc.org/sqlite`. Тести — `testing` без зовнішніх фреймворків.

## Global Constraints

- Go 1.24. Не піднімати тулчейн без явного дозволу.
- Верифікація: `GOTOOLCHAIN=local go test ./...`.
- Форматування перед комітом: `gofmt -w cmd internal`.
- Тести пишуться **до** зміни поведінки.
- Модуль Go називається `wrnrs`; імпорти виду `wrnrs/internal/catalog`.
- Пакети тестів — зовнішні (`package catalog_test`), як у `internal/content/deck_test.go`.
- Не хардкодити токени, ID адмінів, реквізити, секрети MinIO.
- Бізнес-логіка не живе в хендлерах Telegram.
- Спек: `docs/superpowers/specs/2026-08-29-couples-superapp-positions-design.md`, розділ 4.

---

## File Structure

| Файл | Відповідальність |
|---|---|
| `internal/catalog/catalog.go` | типи `Item`/`Catalog`, `Load`, `Validate`, `Item(id)` |
| `internal/catalog/filter.go` | `Filter` і `Filtered` — фасетна фільтрація |
| `internal/catalog/select.go` | `SelectNext` — детермінований вибір без повторів |
| `internal/catalog/catalog_test.go` | тести всіх трьох файлів |
| `internal/modules/gate.go` | `Gate`, `UserState`, `Allows` |
| `internal/modules/registry.go` | `Module`, `Handler`, `Registry` |
| `internal/modules/modules_test.go` | тести гейта і реєстру |
| `internal/app/modules.go` | міст: резолв `UserState`, диспетчеризація, рядки меню з реєстру |
| `internal/app/app.go` | 3 точкові вставки: поле, `Options`, виклики диспетчера |
| `internal/app/modules_test.go` | тести диспетчеризації й гейта на рівні застосунку |
| `content/i18n/uk.json`, `content/i18n/en.json` | 4 рядки причин блокування гейтом |

---

### Task 1: Типи каталогу, завантаження і валідація

**Files:**
- Create: `internal/catalog/catalog.go`
- Test: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: нічого.
- Produces: `catalog.Item{ID string, Facets map[string][]string, Tags []string, Text map[string]ItemText, Media *MediaRef}`, `catalog.ItemText{Title, Body string}`, `catalog.MediaRef{Key string, Width, Height int}`, `catalog.Catalog{Kind string, Version int, Items []Item}`, `catalog.Load(io.Reader) (*Catalog, error)`, `(*Catalog).Validate(languages []string) error`, `(*Catalog).Item(id string) (Item, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/catalog/catalog_test.go`:

```go
package catalog_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func TestValidateRequiresTitleForEveryConfiguredLanguage(t *testing.T) {
	raw := `{
		"kind": "positions",
		"version": 1,
		"items": [
			{"id": "519", "text": {"uk": {"title": "Одкровення"}}}
		]
	}`

	c, err := catalog.Load(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	err = c.Validate([]string{"uk", "en"})
	if err == nil {
		t.Fatal("Validate succeeded, expected missing locale error")
	}
	if !strings.Contains(err.Error(), "519") || !strings.Contains(err.Error(), "en") {
		t.Fatalf("Validate error %q does not mention the item and the missing locale", err)
	}
}

func TestValidateRejectsDuplicateAndEmptyIDs(t *testing.T) {
	dup := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{ID: "1", Text: map[string]catalog.ItemText{"uk": {Title: "а"}}},
			{ID: "1", Text: map[string]catalog.ItemText{"uk": {Title: "б"}}},
		},
	}
	if err := dup.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("Validate on duplicate id returned %v, want duplication error", err)
	}

	empty := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items:   []catalog.Item{{ID: "  ", Text: map[string]catalog.ItemText{"uk": {Title: "а"}}}},
	}
	if err := empty.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("Validate on empty id returned %v, want empty id error", err)
	}
}

func TestValidateRejectsMissingKindAndVersion(t *testing.T) {
	noKind := catalog.Catalog{Version: 1}
	if err := noKind.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("Validate without kind returned %v, want kind error", err)
	}

	noVersion := catalog.Catalog{Kind: "positions"}
	if err := noVersion.Validate([]string{"uk"}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Validate without version returned %v, want version error", err)
	}
}

func TestItemLookup(t *testing.T) {
	c := catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{ID: "519", Text: map[string]catalog.ItemText{"uk": {Title: "Одкровення"}}},
		},
	}

	item, ok := c.Item("519")
	if !ok || item.Text["uk"].Title != "Одкровення" {
		t.Fatalf("Item(519) = %+v, %v; want the stored item", item, ok)
	}
	if _, ok := c.Item("missing"); ok {
		t.Fatal("Item(missing) reported ok, want not found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -run TestValidate -v`
Expected: FAIL — `no required module provides package wrnrs/internal/catalog` (пакет ще не існує).

- [ ] **Step 3: Write minimal implementation**

Create `internal/catalog/catalog.go`:

```go
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ItemText is the localized payload of a catalog item.
type ItemText struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// MediaRef points at an object in the object store rather than embedding bytes.
type MediaRef struct {
	Key    string `json:"key"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Item is one selectable entry: a position, a dare, a date idea.
type Item struct {
	ID     string              `json:"id"`
	Facets map[string][]string `json:"facets,omitempty"`
	Tags   []string            `json:"tags,omitempty"`
	Text   map[string]ItemText `json:"text"`
	Media  *MediaRef           `json:"media,omitempty"`
}

// Catalog is one immutable content collection loaded at boot.
type Catalog struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

func Load(r io.Reader) (*Catalog, error) {
	var c Catalog
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return &c, nil
}

func (c *Catalog) Validate(languages []string) error {
	if strings.TrimSpace(c.Kind) == "" {
		return errors.New("catalog kind must not be empty")
	}
	if c.Version <= 0 {
		return errors.New("catalog version must be positive")
	}
	seen := make(map[string]bool, len(c.Items))
	for i, item := range c.Items {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("item at index %d has empty id", i)
		}
		if seen[item.ID] {
			return fmt.Errorf("item %s is duplicated", item.ID)
		}
		seen[item.ID] = true
		for _, lang := range languages {
			if strings.TrimSpace(item.Text[lang].Title) == "" {
				return fmt.Errorf("item %s missing %s title", item.ID, lang)
			}
		}
	}
	return nil
}

func (c *Catalog) Item(id string) (Item, bool) {
	for _, item := range c.Items {
		if item.ID == id {
			return item, true
		}
	}
	return Item{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -v`
Expected: PASS — 4 тести.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/catalog
git add internal/catalog/catalog.go internal/catalog/catalog_test.go
git commit -m "feat(catalog): add generic catalog types, loading and validation"
```

---

### Task 2: Фасетна фільтрація

**Files:**
- Create: `internal/catalog/filter.go`
- Modify: `internal/catalog/catalog_test.go` (додати тести в кінець файлу)

**Interfaces:**
- Consumes: `catalog.Item`, `catalog.Catalog` з Task 1.
- Produces: `catalog.Filter{Include map[string][]string, Exclude map[string][]string, Tags []string}`, `(*Catalog).Filtered(f Filter) []Item`.

Семантика: у межах одного фасета значення обʼєднуються через **OR**, різні фасети — через **AND**. `Exclude` відкидає елемент, якщо він має будь-яке із заборонених значень. `Tags` вимагає **всі** перелічені теги. Порожній `Filter` повертає всі елементи. Результат відсортований за `ID`, щоб вибір був детермінованим.

- [ ] **Step 1: Write the failing test**

Додати в кінець `internal/catalog/catalog_test.go`:

```go
func testFilterCatalog() catalog.Catalog {
	return catalog.Catalog{
		Kind:    "positions",
		Version: 1,
		Items: []catalog.Item{
			{
				ID:     "1",
				Facets: map[string][]string{"level": {"easy"}, "location": {"bed"}},
				Tags:   []string{"starter_100"},
				Text:   map[string]catalog.ItemText{"uk": {Title: "перша"}},
			},
			{
				ID:     "2",
				Facets: map[string][]string{"level": {"hard"}, "location": {"bed", "sofa"}},
				Text:   map[string]catalog.ItemText{"uk": {Title: "друга"}},
			},
			{
				ID:     "3",
				Facets: map[string][]string{"level": {"easy"}, "location": {"shower"}},
				Tags:   []string{"starter_100"},
				Text:   map[string]catalog.ItemText{"uk": {Title: "третя"}},
			},
		},
	}
}

func filteredIDs(items []catalog.Item) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return strings.Join(ids, ",")
}

func TestFilteredWithoutCriteriaReturnsEverythingSortedByID(t *testing.T) {
	c := testFilterCatalog()
	if got := filteredIDs(c.Filtered(catalog.Filter{})); got != "1,2,3" {
		t.Fatalf("Filtered(empty) = %q, want 1,2,3", got)
	}
}

func TestFilteredOrsWithinFacetAndAndsAcrossFacets(t *testing.T) {
	c := testFilterCatalog()

	orWithin := c.Filtered(catalog.Filter{Include: map[string][]string{"location": {"sofa", "shower"}}})
	if got := filteredIDs(orWithin); got != "2,3" {
		t.Fatalf("Filtered(location in sofa|shower) = %q, want 2,3", got)
	}

	andAcross := c.Filtered(catalog.Filter{Include: map[string][]string{
		"level":    {"easy"},
		"location": {"bed"},
	}})
	if got := filteredIDs(andAcross); got != "1" {
		t.Fatalf("Filtered(level=easy AND location=bed) = %q, want 1", got)
	}
}

func TestFilteredExcludeAndTags(t *testing.T) {
	c := testFilterCatalog()

	excluded := c.Filtered(catalog.Filter{Exclude: map[string][]string{"level": {"hard"}}})
	if got := filteredIDs(excluded); got != "1,3" {
		t.Fatalf("Filtered(exclude level=hard) = %q, want 1,3", got)
	}

	tagged := c.Filtered(catalog.Filter{Tags: []string{"starter_100"}})
	if got := filteredIDs(tagged); got != "1,3" {
		t.Fatalf("Filtered(tag starter_100) = %q, want 1,3", got)
	}
}

func TestFilteredEmptyIncludeListIsIgnored(t *testing.T) {
	c := testFilterCatalog()
	got := c.Filtered(catalog.Filter{Include: map[string][]string{"level": {}}})
	if filteredIDs(got) != "1,2,3" {
		t.Fatalf("Filtered(level in {}) = %q, want all items", filteredIDs(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -run TestFiltered -v`
Expected: FAIL — `c.Filtered undefined (type catalog.Catalog has no field or method Filtered)`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/catalog/filter.go`:

```go
package catalog

import (
	"sort"
	"strings"
)

// Filter narrows a catalog. Values inside one facet are OR-ed, facets are
// AND-ed, Exclude removes any match, and Tags requires all listed tags.
type Filter struct {
	Include map[string][]string
	Exclude map[string][]string
	Tags    []string
}

func (c *Catalog) Filtered(f Filter) []Item {
	out := make([]Item, 0, len(c.Items))
	for _, item := range c.Items {
		if item.Matches(f) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (i Item) Matches(f Filter) bool {
	for facet, allowed := range f.Include {
		if len(allowed) == 0 {
			continue
		}
		if !anyValue(i.Facets[facet], allowed) {
			return false
		}
	}
	for facet, banned := range f.Exclude {
		if anyValue(i.Facets[facet], banned) {
			return false
		}
	}
	for _, tag := range f.Tags {
		if !hasValue(i.Tags, tag) {
			return false
		}
	}
	return true
}

func anyValue(have, want []string) bool {
	for _, w := range want {
		if hasValue(have, w) {
			return true
		}
	}
	return false
}

func hasValue(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -v`
Expected: PASS — усі тести, включно з чотирма новими.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/catalog
git add internal/catalog/filter.go internal/catalog/catalog_test.go
git commit -m "feat(catalog): add facet filtering with OR within facet and AND across facets"
```

---

### Task 3: Детермінований вибір без повторів

**Files:**
- Create: `internal/catalog/select.go`
- Modify: `internal/catalog/catalog_test.go` (додати тести в кінець файлу)

**Interfaces:**
- Consumes: `catalog.Item` з Task 1.
- Produces: `catalog.SelectionInput{SeedID int64, Bucket string, Cycle int, Items []Item, Seen map[string]bool}`, `catalog.SelectNext(SelectionInput) (Item, int, bool, error)` — повертає обраний елемент, номер циклу, чи вичерпалась вибірка, помилку.

Це та сама механіка, що `content.SelectNextCard`, з однією відмінністю: сід береться з `SeedID` + `Bucket`, а не з `PairID` + `Level`. Це дозволяє одному движку обслуговувати і парні модулі (сід = `pair_id`), і соло (сід = `telegram_id`) — див. спек, розділ 5.8.

- [ ] **Step 1: Write the failing test**

Додати в кінець `internal/catalog/catalog_test.go`:

```go
func TestSelectNextAvoidsSeenItemsUntilExhaustedThenStartsNextCycle(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	first, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 42, Bucket: "positions", Cycle: 0, Items: items,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if cycle != 0 || exhausted {
		t.Fatalf("first draw cycle=%d exhausted=%v, want 0 and false", cycle, exhausted)
	}

	seen := map[string]bool{"a": true, "b": true, "c": true}
	next, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 42, Bucket: "positions", Cycle: 0, Items: items, Seen: seen,
	})
	if err != nil {
		t.Fatalf("SelectNext after exhaustion returned error: %v", err)
	}
	if cycle != 1 || !exhausted {
		t.Fatalf("exhausted draw cycle=%d exhausted=%v, want 1 and true", cycle, exhausted)
	}
	if next.ID == "" {
		t.Fatal("exhausted draw returned an empty item")
	}
	_ = first
}

func TestSelectNextSkipsSeenItems(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	seen := map[string]bool{"a": true, "b": true}

	got, cycle, exhausted, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: 7, Bucket: "positions", Cycle: 0, Items: items, Seen: seen,
	})
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if got.ID != "c" || cycle != 0 || exhausted {
		t.Fatalf("SelectNext = %s cycle=%d exhausted=%v, want c 0 false", got.ID, cycle, exhausted)
	}
}

func TestSelectNextIsDeterministicPerSeedAndBucket(t *testing.T) {
	items := []catalog.Item{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	in := catalog.SelectionInput{SeedID: 99, Bucket: "positions", Cycle: 3, Items: items}

	first, _, _, err := catalog.SelectNext(in)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	again, _, _, err := catalog.SelectNext(in)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if first.ID != again.ID {
		t.Fatalf("same input gave %s then %s, want a stable pick", first.ID, again.ID)
	}

	otherBucket := in
	otherBucket.Bucket = "dares"
	changed, _, _, err := catalog.SelectNext(otherBucket)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	otherSeed := in
	otherSeed.SeedID = 100
	changedSeed, _, _, err := catalog.SelectNext(otherSeed)
	if err != nil {
		t.Fatalf("SelectNext returned error: %v", err)
	}
	if changed.ID == first.ID && changedSeed.ID == first.ID {
		t.Fatal("changing both bucket and seed left the pick identical; the seed is not being mixed in")
	}
}

func TestSelectNextRejectsEmptyInput(t *testing.T) {
	_, _, _, err := catalog.SelectNext(catalog.SelectionInput{SeedID: 1, Bucket: "positions"})
	if err == nil {
		t.Fatal("SelectNext on empty items succeeded, want an error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -run TestSelectNext -v`
Expected: FAIL — `undefined: catalog.SelectNext`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/catalog/select.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/catalog/ -v`
Expected: PASS — усі тести пакета.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/catalog
git add internal/catalog/select.go internal/catalog/catalog_test.go
git commit -m "feat(catalog): add deterministic no-repeat selection seeded by id and bucket"
```

---

### Task 4: Гейт доступу до модуля

**Files:**
- Create: `internal/modules/gate.go`
- Test: `internal/modules/modules_test.go`

**Interfaces:**
- Consumes: нічого.
- Produces: `modules.Gate{NeedsPair, Needs18Plus, NeedsMature, NeedsPremium bool}`, `modules.UserState{Is18Plus, MatureOptIn, HasActivePair, HasPremium bool}`, `(Gate).Allows(UserState) (bool, string)`, константи `modules.ReasonNeeds18Plus`, `ReasonNeedsMature`, `ReasonNeedsPair`, `ReasonNeedsPremium`.

Порядок перевірок фіксований і має значення: 18+ → mature → пара → преміум. Користувачу без 18+ безглуздо казати «потрібна пара».

- [ ] **Step 1: Write the failing test**

Create `internal/modules/modules_test.go`:

```go
package modules_test

import (
	"testing"

	"wrnrs/internal/modules"
)

func TestGateAllowsWhenEveryRequirementIsMet(t *testing.T) {
	gate := modules.Gate{NeedsPair: true, Needs18Plus: true, NeedsMature: true, NeedsPremium: true}
	state := modules.UserState{Is18Plus: true, MatureOptIn: true, HasActivePair: true, HasPremium: true}

	ok, reason := gate.Allows(state)
	if !ok || reason != "" {
		t.Fatalf("Allows = %v, %q; want true and no reason", ok, reason)
	}
}

func TestEmptyGateAllowsEmptyState(t *testing.T) {
	ok, reason := modules.Gate{}.Allows(modules.UserState{})
	if !ok || reason != "" {
		t.Fatalf("empty gate Allows = %v, %q; want true and no reason", ok, reason)
	}
}

func TestGateReportsTheFirstUnmetRequirementInFixedOrder(t *testing.T) {
	gate := modules.Gate{NeedsPair: true, Needs18Plus: true, NeedsMature: true, NeedsPremium: true}

	cases := []struct {
		name   string
		state  modules.UserState
		reason string
	}{
		{"nothing", modules.UserState{}, modules.ReasonNeeds18Plus},
		{"adult only", modules.UserState{Is18Plus: true}, modules.ReasonNeedsMature},
		{"adult and mature", modules.UserState{Is18Plus: true, MatureOptIn: true}, modules.ReasonNeedsPair},
		{"all but premium", modules.UserState{Is18Plus: true, MatureOptIn: true, HasActivePair: true}, modules.ReasonNeedsPremium},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := gate.Allows(tc.state)
			if ok {
				t.Fatalf("Allows(%+v) = true, want blocked", tc.state)
			}
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
}

func TestGateIgnoresRequirementsItDoesNotDeclare(t *testing.T) {
	gate := modules.Gate{Needs18Plus: true}
	ok, reason := gate.Allows(modules.UserState{Is18Plus: true})
	if !ok || reason != "" {
		t.Fatalf("Allows = %v, %q; want true — gate declares only 18+", ok, reason)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/modules/ -v`
Expected: FAIL — `no required module provides package wrnrs/internal/modules`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/modules/gate.go`:

```go
package modules

// Reason keys are i18n string keys, resolved by the caller.
const (
	ReasonNeeds18Plus  = "gate.needs_18plus"
	ReasonNeedsMature  = "gate.needs_mature"
	ReasonNeedsPair    = "gate.needs_pair"
	ReasonNeedsPremium = "gate.needs_premium"
)

// Gate declares what a module requires before it can be opened.
type Gate struct {
	NeedsPair    bool
	Needs18Plus  bool
	NeedsMature  bool
	NeedsPremium bool
}

// UserState is the resolved access state of one user.
type UserState struct {
	Is18Plus      bool
	MatureOptIn   bool
	HasActivePair bool
	HasPremium    bool
}

// Allows reports whether the user may open the module. When blocked it returns
// the i18n key of the first unmet requirement, checked in a fixed order so the
// message is the most useful one.
func (g Gate) Allows(s UserState) (bool, string) {
	if g.Needs18Plus && !s.Is18Plus {
		return false, ReasonNeeds18Plus
	}
	if g.NeedsMature && !s.MatureOptIn {
		return false, ReasonNeedsMature
	}
	if g.NeedsPair && !s.HasActivePair {
		return false, ReasonNeedsPair
	}
	if g.NeedsPremium && !s.HasPremium {
		return false, ReasonNeedsPremium
	}
	return true, ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/modules/ -v`
Expected: PASS — 4 тести.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/modules
git add internal/modules/gate.go internal/modules/modules_test.go
git commit -m "feat(modules): add module access gate with ordered blocking reasons"
```

---

### Task 5: Реєстр модулів

**Files:**
- Create: `internal/modules/registry.go`
- Modify: `internal/modules/modules_test.go` (додати тести в кінець файлу)

**Interfaces:**
- Consumes: `modules.Gate` з Task 4.
- Produces: `modules.Handler` (інтерфейс з `HandleCallback(context.Context, *telegram.CallbackQuery) error` і `HandleMessage(context.Context, *telegram.Message) (bool, error)`), `modules.Module{ID, TitleKey, Icon, CallbackPrefix string, Gate Gate, Handler Handler}`, `modules.NewRegistry() *Registry`, `(*Registry).Register(Module) error`, `(*Registry).All() []Module`, `(*Registry).ByCallback(data string) (Module, bool)`.

`Register` відхиляє порожній ID, префікс без завершальної двокрапки, дублікат ID і префікс, що перетинається з уже зареєстрованим. Останнє критично: без цієї перевірки `pos:` і `pos:fav:` мовчки з'їдали б колбеки один одного.

- [ ] **Step 1: Write the failing test**

Додати в кінець `internal/modules/modules_test.go`:

```go
import (
	"context"

	"wrnrs/internal/telegram"
)

type stubHandler struct {
	callbacks []string
	messages  []string
	handle    bool
}

func (s *stubHandler) HandleCallback(_ context.Context, cb *telegram.CallbackQuery) error {
	s.callbacks = append(s.callbacks, cb.Data)
	return nil
}

func (s *stubHandler) HandleMessage(_ context.Context, msg *telegram.Message) (bool, error) {
	s.messages = append(s.messages, msg.Text)
	return s.handle, nil
}

func TestRegisterRejectsInvalidModules(t *testing.T) {
	r := modules.NewRegistry()

	if err := r.Register(modules.Module{CallbackPrefix: "pos:"}); err == nil {
		t.Fatal("Register with empty id succeeded, want an error")
	}
	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "pos"}); err == nil {
		t.Fatal("Register with a prefix missing the trailing colon succeeded, want an error")
	}
}

func TestRegisterRejectsDuplicateIDAndCollidingPrefix(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "other:"}); err == nil {
		t.Fatal("Register with a duplicate id succeeded, want an error")
	}
	if err := r.Register(modules.Module{ID: "favourites", CallbackPrefix: "pos:fav:"}); err == nil {
		t.Fatal("Register with a colliding prefix succeeded, want an error")
	}
}

func TestByCallbackMatchesRegisteredPrefix(t *testing.T) {
	r := modules.NewRegistry()
	handler := &stubHandler{}
	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "pos:", Handler: handler}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	found, ok := r.ByCallback("pos:browse:12")
	if !ok || found.ID != "positions" {
		t.Fatalf("ByCallback(pos:browse:12) = %+v, %v; want the positions module", found, ok)
	}
	if _, ok := r.ByCallback("menu:main"); ok {
		t.Fatal("ByCallback(menu:main) matched a module, want no match")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	r := modules.NewRegistry()
	if err := r.Register(modules.Module{ID: "positions", CallbackPrefix: "pos:"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	all := r.All()
	all[0].ID = "mutated"

	again := r.All()
	if again[0].ID != "positions" {
		t.Fatalf("All() leaked its backing array: id is now %q", again[0].ID)
	}
}
```

Імпорти додаються до вже наявного блоку `import` на початку файлу — не створюй другий блок.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/modules/ -run TestRegister -v`
Expected: FAIL — `undefined: modules.NewRegistry`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/modules/registry.go`:

```go
package modules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"wrnrs/internal/telegram"
)

// Handler owns every update whose callback data starts with the module prefix.
type Handler interface {
	HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error
	// HandleMessage reports whether it consumed the message. False means the
	// caller keeps looking for another owner.
	HandleMessage(ctx context.Context, msg *telegram.Message) (bool, error)
}

// Module is one feature of the superapp: its menu entry, its access gate and
// the handler that owns its callbacks.
type Module struct {
	ID             string
	TitleKey       string
	Icon           string
	CallbackPrefix string
	Gate           Gate
	Handler        Handler
}

type Registry struct {
	modules []Module
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(m Module) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("module id must not be empty")
	}
	if !strings.HasSuffix(m.CallbackPrefix, ":") {
		return fmt.Errorf("module %s callback prefix %q must end with ':'", m.ID, m.CallbackPrefix)
	}
	for _, existing := range r.modules {
		if existing.ID == m.ID {
			return fmt.Errorf("module %s is already registered", m.ID)
		}
		if strings.HasPrefix(existing.CallbackPrefix, m.CallbackPrefix) ||
			strings.HasPrefix(m.CallbackPrefix, existing.CallbackPrefix) {
			return fmt.Errorf("module %s prefix %q collides with module %s prefix %q",
				m.ID, m.CallbackPrefix, existing.ID, existing.CallbackPrefix)
		}
	}
	r.modules = append(r.modules, m)
	return nil
}

func (r *Registry) All() []Module {
	out := make([]Module, len(r.modules))
	copy(out, r.modules)
	return out
}

func (r *Registry) ByCallback(data string) (Module, bool) {
	for _, m := range r.modules {
		if strings.HasPrefix(data, m.CallbackPrefix) {
			return m, true
		}
	}
	return Module{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/modules/ -v`
Expected: PASS — 8 тестів.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/modules
git add internal/modules/registry.go internal/modules/modules_test.go
git commit -m "feat(modules): add module registry with prefix collision detection"
```

---

### Task 6: Резолв стану користувача в застосунку

**Files:**
- Create: `internal/app/modules.go`
- Modify: `internal/app/app.go` — додати поле `registry` в `App` (після `payments payments.Catalog`, рядок ~85), поле `Registry` в `Options` (після `ObjectStore ObjectStore`, рядок ~102), ініціалізацію в `New`
- Test: `internal/app/modules_test.go`

**Interfaces:**
- Consumes: `modules.UserState`, `modules.Registry` з Tasks 4-5; `storage.Repository.UserMaturity(ctx, userID) (bool, bool, error)`, `.ActivePairForUser(ctx, userID) (*storage.Pair, error)`, `.UserHasEntitlement(ctx, userID, entitlementType, unlockID string) (bool, error)`.
- Produces: `(*App).moduleUserState(ctx, userID int64) (modules.UserState, error)`, `(*App).Registry() *modules.Registry`.

- [ ] **Step 1: Write the failing test**

Create `internal/app/modules_test.go`:

```go
package app

import (
	"context"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

func TestModuleUserStateReflectsMaturityPairAndPremium(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(4001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	state, err := a.moduleUserState(ctx, userID)
	if err != nil {
		t.Fatalf("moduleUserState: %v", err)
	}
	if state.Is18Plus || state.MatureOptIn || state.HasActivePair || state.HasPremium {
		t.Fatalf("fresh user state = %+v, want everything false", state)
	}

	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}
	if err := a.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:          userID,
		EntitlementType: storage.EntitlementPremiumAccess,
		UnlockID:        storage.EntitlementPremiumAccess,
		Source:          "admin_grant",
		GrantedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("GrantEntitlement: %v", err)
	}

	state, err = a.moduleUserState(ctx, userID)
	if err != nil {
		t.Fatalf("moduleUserState after grants: %v", err)
	}
	if !state.Is18Plus || !state.MatureOptIn || !state.HasPremium {
		t.Fatalf("state after grants = %+v, want 18+, mature and premium true", state)
	}
	if state.HasActivePair {
		t.Fatalf("state.HasActivePair = true, want false — no pair was created")
	}
}
```

Тест лежить у внутрішньому пакеті `app` (не `app_test`), бо звертається до непублічних `a.repo` і `a.moduleUserState` — так само, як наявний `internal/app/app_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestModuleUserState -v`
Expected: FAIL — `a.moduleUserState undefined (type *App has no field or method moduleUserState)`.

Якщо помилка натомість вказує на `storage.Entitlement` або `UpsertUser` — звір поля з `internal/storage/sqlite.go` і виправ виклик у тесті, не в реалізації.

- [ ] **Step 3: Write minimal implementation**

Create `internal/app/modules.go`:

```go
package app

import (
	"context"

	"wrnrs/internal/modules"
	"wrnrs/internal/storage"
)

// Registry exposes the module registry so cmd wiring can register modules
// after the App is constructed.
func (a *App) Registry() *modules.Registry {
	return a.registry
}

// moduleUserState resolves everything a module gate needs about one user.
func (a *App) moduleUserState(ctx context.Context, userID int64) (modules.UserState, error) {
	var state modules.UserState

	is18Plus, matureOptIn, err := a.repo.UserMaturity(ctx, userID)
	if err != nil {
		return modules.UserState{}, err
	}
	state.Is18Plus = is18Plus
	state.MatureOptIn = matureOptIn

	pair, err := a.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return modules.UserState{}, err
	}
	state.HasActivePair = pair != nil

	premium, err := a.repo.UserHasEntitlement(ctx, userID,
		storage.EntitlementPremiumAccess, storage.EntitlementPremiumAccess)
	if err != nil {
		return modules.UserState{}, err
	}
	state.HasPremium = premium

	return state, nil
}
```

У `internal/app/app.go` додай імпорт `"wrnrs/internal/modules"` до наявного блоку імпортів, поле в `App`:

```go
	payments    payments.Catalog
	registry    *modules.Registry
	logger      *slog.Logger
```

поле в `Options`:

```go
	ObjectStore ObjectStore
	Registry    *modules.Registry
	Logger      *slog.Logger
```

і в кінці `New`, безпосередньо перед `return`, ініціалізацію:

```go
	registry := options.Registry
	if registry == nil {
		registry = modules.NewRegistry()
	}
```

плюс `registry: registry,` у літералі `&App{...}`, який `New` повертає.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestModuleUserState -v`
Expected: PASS.

Далі прогони весь пакет, щоб переконатись, що нове поле нічого не зламало:
Run: `GOTOOLCHAIN=local go test ./internal/app/`
Expected: PASS — усі наявні тести.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/app
git add internal/app/modules.go internal/app/modules_test.go internal/app/app.go
git commit -m "feat(app): resolve module gate state from repository"
```

---

### Task 7: Диспетчеризація колбеків і повідомлень у модулі

**Files:**
- Modify: `internal/app/modules.go` (додати методи)
- Modify: `internal/app/app.go` — вставка в `handleCallback` (одразу після `_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "")`, рядок ~339, **перед** `switch {`) і в `handleMessage`
- Modify: `internal/app/modules_test.go` (додати тести)

**Interfaces:**
- Consumes: `(*App).moduleUserState` з Task 6; `modules.Registry.ByCallback`; `modules.Gate.Allows`.
- Produces: `(*App).dispatchModuleCallback(ctx, cb *telegram.CallbackQuery, chatID int64, lang string) (bool, error)`, `(*App).dispatchModuleMessage(ctx, msg *telegram.Message) (bool, error)`.

Обидва повертають `handled bool`: `false` означає «це не модульний апдейт, обробляй як раніше». Заблокований гейтом колбек вважається **обробленим** — користувач отримує пояснення, а не провалюється в наявний `switch`.

- [ ] **Step 1: Write the failing test**

Додати в кінець `internal/app/modules_test.go`:

```go
type recordingModuleHandler struct {
	callbacks []string
	messages  []string
	consume   bool
}

func (h *recordingModuleHandler) HandleCallback(_ context.Context, cb *telegram.CallbackQuery) error {
	h.callbacks = append(h.callbacks, cb.Data)
	return nil
}

func (h *recordingModuleHandler) HandleMessage(_ context.Context, msg *telegram.Message) (bool, error) {
	h.messages = append(h.messages, msg.Text)
	return h.consume, nil
}

func registerTestModule(t *testing.T, a *App, gate modules.Gate, handler modules.Handler) {
	t.Helper()
	err := a.Registry().Register(modules.Module{
		ID:             "demo",
		TitleKey:       "module.demo",
		Icon:           "🎲",
		CallbackPrefix: "demo:",
		Gate:           gate,
		Handler:        handler,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestDispatchModuleCallbackRoutesMatchingPrefix(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4101)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if !handled {
		t.Fatal("dispatchModuleCallback reported not handled, want handled")
	}
	if len(handler.callbacks) != 1 || handler.callbacks[0] != "demo:open" {
		t.Fatalf("handler callbacks = %v, want [demo:open]", handler.callbacks)
	}
}

func TestDispatchModuleCallbackIgnoresUnknownPrefix(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{}, handler)

	cb := &telegram.CallbackQuery{ID: "1", Data: "menu:main", From: telegram.User{ID: 4102}}
	handled, err := a.dispatchModuleCallback(ctx, cb, 4102, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if handled {
		t.Fatal("dispatchModuleCallback handled menu:main, want it left to the existing switch")
	}
	if len(handler.callbacks) != 0 {
		t.Fatalf("handler saw %v, want nothing", handler.callbacks)
	}
}

func TestDispatchModuleCallbackBlocksOnGateAndDoesNotReachHandler(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{}
	registerTestModule(t, a, modules.Gate{Needs18Plus: true}, handler)

	const userID = int64(4103)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	cb := &telegram.CallbackQuery{ID: "1", Data: "demo:open", From: telegram.User{ID: userID}}
	handled, err := a.dispatchModuleCallback(ctx, cb, userID, "uk")
	if err != nil {
		t.Fatalf("dispatchModuleCallback: %v", err)
	}
	if !handled {
		t.Fatal("a gate-blocked callback reported not handled; it must not fall through to the main switch")
	}
	if len(handler.callbacks) != 0 {
		t.Fatalf("blocked callback still reached the handler: %v", handler.callbacks)
	}
	if len(bot.messages) == 0 && len(bot.editedTexts) == 0 {
		t.Fatal("gate block sent nothing to the user")
	}
}

func TestDispatchModuleMessageStopsAtTheFirstConsumer(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()
	handler := &recordingModuleHandler{consume: true}
	registerTestModule(t, a, modules.Gate{}, handler)

	const userID = int64(4104)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	msg := &telegram.Message{
		MessageID: 1,
		Text:      "привіт",
		From:      &telegram.User{ID: userID},
		Chat:      telegram.Chat{ID: userID},
	}
	handled, err := a.dispatchModuleMessage(ctx, msg)
	if err != nil {
		t.Fatalf("dispatchModuleMessage: %v", err)
	}
	if !handled {
		t.Fatal("dispatchModuleMessage reported not handled, want handled")
	}
	if len(handler.messages) != 1 {
		t.Fatalf("handler messages = %v, want one entry", handler.messages)
	}
}
```

Додай `"wrnrs/internal/modules"` і `"wrnrs/internal/telegram"` до блоку імпортів файлу.

**Перед запуском** звір імена полів фейкового бота: тест читає `bot.messages` і `bot.editedTexts`. Подивись `fakeBot` у `internal/app/app_test.go` і підстав фактичні імена полів — вигадувати їх не можна.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestDispatchModule -v`
Expected: FAIL — `a.dispatchModuleCallback undefined`.

- [ ] **Step 3: Write minimal implementation**

Додати в `internal/app/modules.go`:

```go
// dispatchModuleCallback routes a callback to its module. It reports handled
// so the caller can skip the legacy switch. A gate refusal counts as handled:
// the user gets an explanation instead of falling through.
func (a *App) dispatchModuleCallback(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string) (bool, error) {
	if a.registry == nil || cb == nil {
		return false, nil
	}
	module, ok := a.registry.ByCallback(cb.Data)
	if !ok {
		return false, nil
	}

	state, err := a.moduleUserState(ctx, cb.From.ID)
	if err != nil {
		return true, err
	}
	if allowed, reason := module.Gate.Allows(state); !allowed {
		text := a.i18n.Text(language, reason)
		return true, a.editCallbackScreen(ctx, cb, chatID, text,
			telegram.MainMenuKeyboardWithPair(language, state.HasActivePair))
	}
	if module.Handler == nil {
		return true, nil
	}
	return true, module.Handler.HandleCallback(ctx, cb)
}

// dispatchModuleMessage offers the message to each module in registration
// order and stops at the first one that consumes it.
func (a *App) dispatchModuleMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.registry == nil || msg == nil || msg.From == nil {
		return false, nil
	}
	for _, module := range a.registry.All() {
		if module.Handler == nil {
			continue
		}
		handled, err := module.Handler.HandleMessage(ctx, msg)
		if err != nil {
			return true, err
		}
		if handled {
			return true, nil
		}
	}
	return false, nil
}
```

Додай імпорт `"wrnrs/internal/telegram"` до `internal/app/modules.go`.

У `internal/app/app.go`, у `handleCallback`, одразу після рядка `_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "")` і **перед** `switch {`:

```go
	if handled, err := a.dispatchModuleCallback(ctx, cb, chatID, lang); handled {
		return err
	}
```

У `handleMessage`, після `ensureTelegramUser` і перевірки рейт-ліміту, але **перед** розбором FSM-станів:

```go
	if handled, err := a.dispatchModuleMessage(ctx, msg); handled {
		return err
	}
```

Точне місце вставки в `handleMessage` знайди так: модульна перевірка має стояти після того, як користувач гарантовано існує в базі, і до першої гілки, що читає FSM. Якщо в `handleMessage` спершу обробляються команди (`/start`, `/admin`, `/paysupport`) — постав вставку **після** них, щоб команди-переривання лишались сильнішими за модулі.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -v`
Expected: PASS — 4 нові тести плюс усі наявні.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/app
git add internal/app/modules.go internal/app/modules_test.go internal/app/app.go
git commit -m "feat(app): dispatch callbacks and messages to registered modules"
```

---

### Task 8: Кнопки модулів у головному меню і рядки гейта

**Files:**
- Modify: `internal/app/modules.go` (додати метод)
- Modify: `internal/app/app.go` — замінити виклики `telegram.MainMenuKeyboardWithPair(lang, hasPair)` у гілці `case cb.Data == "menu:main"` та в `buildMainMenuText`-споживачах на новий `a.mainMenuKeyboard(...)`
- Modify: `content/i18n/uk.json`, `content/i18n/en.json`
- Modify: `internal/app/modules_test.go` (додати тест)

**Interfaces:**
- Consumes: `modules.Registry.All()`, `modules.Gate.Allows`, `(*App).moduleUserState`.
- Produces: `(*App).mainMenuKeyboard(ctx context.Context, userID int64, language string, hasPair bool) telegram.InlineKeyboardMarkup`.

Наявні кнопки меню лишаються в `telegram.MainMenuKeyboardWithPair` — вони не переносяться в реєстр. Новий метод бере цю клавіатуру за основу і **дописує** рядок на кожен зареєстрований модуль. Це і є те, чого вимагає спек: новий модуль додається записом у реєстрі, а не правкою клавіатури. Модулі, заблоковані гейтом, показуються з замком — ховати їх не можна, інакше незрозуміло, що взагалі існує.

- [ ] **Step 1: Write the failing test**

Додати в кінець `internal/app/modules_test.go`:

```go
func TestMainMenuKeyboardAppendsModuleRowsWithLockForBlockedOnes(t *testing.T) {
	a, _, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(4201)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	err := a.Registry().Register(modules.Module{
		ID:             "demo",
		TitleKey:       "module.demo",
		Icon:           "🎲",
		CallbackPrefix: "demo:",
		Gate:           modules.Gate{Needs18Plus: true},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	base := telegram.MainMenuKeyboardWithPair("uk", false)
	got := a.mainMenuKeyboard(ctx, userID, "uk", false)
	if len(got.InlineKeyboard) != len(base.InlineKeyboard)+1 {
		t.Fatalf("menu has %d rows, want %d (base plus one module row)",
			len(got.InlineKeyboard), len(base.InlineKeyboard)+1)
	}

	last := got.InlineKeyboard[len(got.InlineKeyboard)-1][0]
	if last.CallbackData != "demo:open" {
		t.Fatalf("module button callback = %q, want demo:open", last.CallbackData)
	}
	if !strings.Contains(last.Text, "🔒") {
		t.Fatalf("blocked module button text = %q, want a lock marker", last.Text)
	}

	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	unlocked := a.mainMenuKeyboard(ctx, userID, "uk", false)
	unlockedLast := unlocked.InlineKeyboard[len(unlocked.InlineKeyboard)-1][0]
	if strings.Contains(unlockedLast.Text, "🔒") {
		t.Fatalf("unlocked module button text = %q, want no lock marker", unlockedLast.Text)
	}
}
```

Додай `"strings"` до блоку імпортів файлу.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestMainMenuKeyboard -v`
Expected: FAIL — `a.mainMenuKeyboard undefined`.

- [ ] **Step 3: Write minimal implementation**

Додати в `internal/app/modules.go`:

```go
// mainMenuKeyboard takes the existing main menu and appends one row per
// registered module. Blocked modules stay visible with a lock so users can see
// what exists and what unlocks it.
func (a *App) mainMenuKeyboard(ctx context.Context, userID int64, language string, hasPair bool) telegram.InlineKeyboardMarkup {
	keyboard := telegram.MainMenuKeyboardWithPair(language, hasPair)
	if a.registry == nil {
		return keyboard
	}
	registered := a.registry.All()
	if len(registered) == 0 {
		return keyboard
	}

	state, err := a.moduleUserState(ctx, userID)
	if err != nil {
		a.logger.Warn("resolve module state for menu failed", "user_id", userID, "err", err)
		state = modules.UserState{HasActivePair: hasPair}
	}

	for _, module := range registered {
		label := strings.TrimSpace(module.Icon + " " + a.i18n.Text(language, module.TitleKey))
		if allowed, _ := module.Gate.Allows(state); !allowed {
			label += " 🔒"
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []telegram.InlineKeyboardButton{
			{Text: label, CallbackData: module.CallbackPrefix + "open"},
		})
	}
	return keyboard
}
```

Додай `"strings"` до імпортів `internal/app/modules.go`.

У `internal/app/app.go` заміни в гілці `case cb.Data == "menu:main"`:

```go
	case cb.Data == "menu:main":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		text, hasPair := a.buildMainMenuText(ctx, cb.From.ID, lang)
		return a.editCallbackScreen(ctx, cb, chatID, text, a.mainMenuKeyboard(ctx, cb.From.ID, lang, hasPair))
```

Інші виклики `telegram.MainMenuKeyboardWithPair` **не чіпай** — вони стоять на шляхах помилок і скидання, де зайвий запит до бази ні до чого.

Додай у `content/i18n/uk.json` у обʼєкт `strings`:

```json
    "gate.needs_18plus": "Спочатку підтверди, що тобі є 18+. Це в налаштуваннях.",
    "gate.needs_mature": "Цей розділ вимагає згоди на контент 18+. Увімкни її в налаштуваннях.",
    "gate.needs_pair": "Спочатку створи пару — цей розділ для двох.",
    "gate.needs_premium": "Цей розділ доступний з преміумом.",
```

і у `content/i18n/en.json`:

```json
    "gate.needs_18plus": "Confirm that you are 18+ first. It is in settings.",
    "gate.needs_mature": "This section needs your consent to 18+ content. Enable it in settings.",
    "gate.needs_pair": "Create a pair first — this section is for two.",
    "gate.needs_premium": "This section is available with premium.",
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -v`
Expected: PASS.

Перевір валідність JSON перед комітом:
Run: `python3 -c "import json; [json.load(open(p)) for p in ['content/i18n/uk.json','content/i18n/en.json']]; print('ok')"`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/app
git add internal/app/modules.go internal/app/modules_test.go internal/app/app.go content/i18n/uk.json content/i18n/en.json
git commit -m "feat(app): build main menu module rows from the registry"
```

---

### Task 9: Повна верифікація і документація

**Files:**
- Modify: `docs/ARCHITECTURE.md` — додати `internal/catalog` і `internal/modules` у «Package Map»
- Modify: `AGENTS.md` — додати правило про реєстр

- [ ] **Step 1: Прогнати весь набір тестів**

Run: `GOTOOLCHAIN=local go test ./...`
Expected: PASS в усіх пакетах. Якщо `internal/app` падає — причина майже напевно в тому, що наявний тест порівнює кількість рядків головного меню. Полагодь тест, а не приховуй кнопки модулів.

- [ ] **Step 2: Перевірити форматування**

Run: `gofmt -l cmd internal`
Expected: порожній вивід.

- [ ] **Step 3: Оновити ARCHITECTURE.md**

У розділ «Package Map» додати два рядки після `internal/content`:

```markdown
- `internal/catalog`: generic content catalogs for superapp modules — facet filtering and deterministic no-repeat selection seeded by pair or user id.
- `internal/modules`: module registry and access gate (`18+`, mature opt-in, active pair, premium) with callback-prefix dispatch.
```

- [ ] **Step 4: Оновити AGENTS.md**

У розділ «Engineering Rules» додати:

```markdown
- Нові модулі суперапу реєструються в `internal/modules`, а не додаються гілками в `internal/app/app.go`. Префікс колбека модуля має завершуватись двокрапкою і не перетинатися з наявними.
```

- [ ] **Step 5: Commit**

```bash
git add docs/ARCHITECTURE.md AGENTS.md
git commit -m "docs: document the catalog engine and module registry"
```

---

## Self-Review

**Покриття спека (розділ 4).**

| Вимога спека | Задача |
|---|---|
| 4.1 `Item`/`Catalog`, `Validate` | Task 1 |
| 4.1 `Filtered` з OR/AND семантикою | Task 2 |
| 4.1 `SelectNext` з узагальненим `SeedID` | Task 3 |
| 4.2 `Gate` з чотирма вимогами | Task 4 |
| 4.2 `Module`, `Registry` | Task 5 |
| 4.2 меню будується з реєстру | Task 8 |
| 4.3 диспетчеризація замість гілок у `app.go` | Task 7 |
| колода питань **не** мігрує | не входить — Task 1-3 створюють паралельну абстракцію, `internal/content` не змінюється |

**Узгодженість типів.** `catalog.Item` з Task 1 використовується в Tasks 2-3 без змін полів. `modules.Gate` і `modules.UserState` з Task 4 споживаються в Tasks 6-8 з тими самими іменами полів. `modules.Handler` з Task 5 реалізується стабами в Tasks 5 і 7 з однаковими сигнатурами. `(*App).moduleUserState` з Task 6 викликається в Tasks 7-8.

**Ризик, який виконавцю треба тримати в голові.** Task 7 вставляє диспетчеризацію в `handleMessage`, а точне місце залежить від структури функції, яку виконавець побачить на місці. Крок 3 задає критерій вибору місця (після команд-переривань, до розбору FSM) замість номера рядка, бо номер до того часу зсунеться.
