# Модуль «Гра»: правда або дія — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Модуль, у якому пара тягне картки «правда» або «дія» з одного телефона, а бот чергує їхні імена.

**Architecture:** Новий пакет `internal/play` за взірцем `internal/wishlist`: чистий сервіс без I/O плюс Telegram-хендлер на вузьких інтерфейсах, оголошених у себе. Увесь стан партії — фільтр, лічильник кидків, кільце нещодавніх карток і чия черга — живе в Redis під ключем модуля. **Сховище не змінюється взагалі:** ні таблиць, ні міграцій, ні нових методів репозиторію.

**Tech Stack:** Go 1.24, Redis, Telegram Bot API, `internal/catalog`, `internal/modules`.

## Global Constraints

- Go 1.24. Не піднімати тулчейн без явного дозволу. Не чіпати `go.mod`/`go.sum`.
- Верифікація: `GOTOOLCHAIN=local go test ./... -count=1`.
- Форматування перед комітом: `gofmt -w cmd internal`.
- Тести пишуться **до** зміни поведінки.
- Модуль Go — `wrnrs`. Тестові пакети зовнішні (`package play_test`).
- **Бот працює наживо в Docker на цій машині.** Не перезапускати його, не перезбирати образ, не виконувати `docker compose` нічого. `docker compose config` — лише валідація, вона нічого не запускає.
- **Сховище не змінюється.** Жодних правок у `internal/storage/`, жодних міграцій.
- Стан партії живе в Redis (`SetModuleState`/`ModuleState`, модуль `play`), не в SQLite.
- Видана картка перемикає чергу; **пропущена — ні**.
- ID карток — нуль-доповнені `p001`…`p080`: `catalog.Filtered` сортує лексикографічно.
- Каталог — рівно 80 карток: 40 `truth`, 40 `dare`; інтенсивність 30 `gentle` / 30 `medium` / 20 `bold`, у межах кожного `kind` — 15 / 15 / 10.
- Гейт: `Needs18Plus: true, NeedsMature: true`; `NeedsPair` і `NeedsPremium` лишаються `false`.
- Груповий чат відхиляється, як у позах і вішлісті.
- Кожен доданий ключ i18n має існувати в **обох** `content/i18n/{uk,en}.json`.
- Telegram: `callback_data` ≤ 64 байти.
- Не хардкодити токени, ID адмінів, реквізити, секрети MinIO.
- Спек: `docs/superpowers/specs/2026-08-30-play-module-design.md`.

---

## File Structure

| Файл | Відповідальність |
|---|---|
| `internal/play/service.go` | чиста логіка: стан партії, вибір наступної картки, кільце нещодавніх |
| `internal/play/service_test.go` | тести чистої логіки |
| `internal/play/keyboards.go` | клавіатури й підпис картки |
| `internal/play/handler.go` | вузькі інтерфейси, роутинг колбеків, екрани |
| `internal/play/handler_test.go` | тести чистих функцій і роутингу |
| `internal/play/catalog_test.go` | тести каталогу |
| `content/play.v1.json` | 80 карток (генерується Task 4, комітиться) |
| `cmd/wrnrs/main.go` | завантаження каталогу, реєстрація модуля |
| `internal/config/config.go` | `PLAY_CATALOG_PATH` |
| `content/i18n/{uk,en}.json` | рядки модуля |

---

### Task 1: Стан партії і вибір картки

**Files:**
- Create: `internal/play/service.go`
- Create: `internal/play/service_test.go`

**Interfaces:**
- Consumes: `catalog.Catalog`, `catalog.Item`, `catalog.Filter`, `catalog.SelectionInput`, `catalog.SelectNext`.
- Produces:
  - `play.GameState{Filter catalog.Filter `json:"f"`, Draw int `json:"d"`, Recent []string `json:"r"`, TurnB bool `json:"t"`}`
  - `play.EncodeState(GameState) (string, error)`, `play.DecodeState(string) (GameState, error)`
  - `play.ServiceOptions{Catalog *catalog.Catalog}`, `play.NewService(ServiceOptions) *Service`
  - `(*Service).Available(filter catalog.Filter) []catalog.Item`
  - `(*Service).Next(seedID int64, state GameState) (catalog.Item, GameState, error)` — повертає картку і **оновлений** стан (інкрементований `Draw`, доповнене `Recent`); чергу НЕ перемикає, це рішення хендлера
  - `(*Service).Item(id string) (catalog.Item, bool)`
  - `play.RecentLimit = 15`

`Next` розглядає `Recent` як «побачені» через `SelectionInput.Seen`, а `Draw` вплітає у `Bucket`, щоб два натискання поспіль не давали ту саму детерміновану перетасовку. Якщо `Recent` покриває всю вибірку, воно очищується перед вибором — інакше гра застрягла б.

- [ ] **Step 1: Write the failing tests**

Create `internal/play/service_test.go`:

```go
package play_test

import (
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
		id := "p" + string(rune('0'+i/100%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1`
Expected: FAIL — `no required module provides package wrnrs/internal/play`.

- [ ] **Step 3: Write the implementation**

Create `internal/play/service.go`:

```go
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
func (s *Service) Next(seedID int64, state GameState) (catalog.Item, GameState, error) {
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

	item, _, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: seedID,
		Bucket: fmt.Sprintf("play:%d", state.Draw),
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1 -v`
Expected: PASS — одинадцять тестів.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/play/service.go internal/play/service_test.go
git commit -m "feat(play): add the I/O-free game state and card draw"
```

---

### Task 2: Клавіатури, підпис картки і рядки

**Files:**
- Create: `internal/play/keyboards.go`
- Create: `internal/play/handler_test.go` (тести чистих функцій; хендлер додається в Task 3)
- Modify: `content/i18n/uk.json`, `content/i18n/en.json`

**Interfaces:**
- Consumes: `catalog.Item`, `telegram.InlineKeyboardMarkup`, `i18n.Bundle`, `catalog.Filter`.
- Produces:
  - `play.CardCaption(bundle *i18n.Bundle, language string, item catalog.Item, actorName string) string`
  - `play.CardKeyboard(bundle *i18n.Bundle, language string) telegram.InlineKeyboardMarkup`
  - `play.HubKeyboard(bundle *i18n.Bundle, language string) telegram.InlineKeyboardMarkup`
  - `play.FiltersKeyboard(bundle *i18n.Bundle, language string, filter catalog.Filter) telegram.InlineKeyboardMarkup`
  - `play.ToggleFilterValue(filter catalog.Filter, facet, value string) catalog.Filter` — не мутує аргумент

Колбеки: `play:open`, `play:next`, `play:skip`, `play:filters`, `play:filter:{facet}:{value}`. Найдовший — `play:filter:intensity:gentle` = 28 байт.

`CardCaption` із порожнім `actorName` не друкує префікс і не лишає осиротілої двокрапки.

**Читай `internal/wishlist/keyboards.go` перед тим, як писати.** Усі підписи беруться з `i18n.Bundle` — жодних `if language == "uk"` літералів для тексту, який бачить користувач. Виняток лише один, уже усталений: кнопка повернення в головне меню (`menu:main`) належить застосунку.

i18n-ключі (додати в **обидва** файли):

```
module.play            Гра
play.hub.title         Правда або дія
play.hub.intro         Тягніть картки по черзі — бот сам чергує, кому випадає.
play.hub.solo_hint     Коли зʼявиться пара, картки звертатимуться на імена.
play.next              ▶ Далі
play.skip              ⏭ Пропустити
play.filters           ☰ Фільтри
play.kind.truth        Правда
play.kind.dare         Дія
play.intensity.gentle  Мʼяко
play.intensity.medium  Середньо
play.intensity.bold    Сміливо
play.filters.title     Фільтри
play.empty             За такими фільтрами карток немає. Прибери щось із фільтрів.
```

Англійські — той самий зміст у регістрі решти `en.json`.

- [ ] **Step 1: Write the failing tests**

Create `internal/play/handler_test.go`:

```go
package play_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/play"
)

func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"play.next":             "ДАЛІ-МАРКЕР",
		"play.skip":             "ПРОПУСК-МАРКЕР",
		"play.filters":          "ФІЛЬТРИ-МАРКЕР",
		"play.kind.truth":       "Правда",
		"play.kind.dare":        "Дія",
		"play.intensity.gentle": "Мʼяко",
		"play.intensity.bold":   "Сміливо",
	}})
	return b
}

func testCard() catalog.Item {
	return catalog.Item{
		ID:     "p007",
		Facets: map[string][]string{"kind": {"dare"}, "intensity": {"gentle"}},
		Text:   map[string]catalog.ItemText{"uk": {Title: "Обійми", Body: "Обійми партнера і не відпускай хвилину."}},
	}
}

func TestCardCaptionShowsActorTypeAndBody(t *testing.T) {
	caption := play.CardCaption(testBundle(), "uk", testCard(), "Оля")

	if !strings.Contains(caption, "Оля") {
		t.Fatalf("caption %q does not name the actor", caption)
	}
	if !strings.Contains(caption, "Обійми партнера") {
		t.Fatalf("caption %q does not contain the card body", caption)
	}
	if !strings.Contains(caption, "Дія") {
		t.Fatalf("caption %q does not say whether this is a truth or a dare", caption)
	}
}

func TestCardCaptionWithoutAnActorHasNoOrphanedSeparator(t *testing.T) {
	caption := play.CardCaption(testBundle(), "uk", testCard(), "")

	if strings.HasPrefix(strings.TrimSpace(caption), ":") {
		t.Fatalf("caption %q starts with an orphaned separator", caption)
	}
	if !strings.Contains(caption, "Обійми партнера") {
		t.Fatalf("caption %q lost the body when there was no actor", caption)
	}
}

func TestCardKeyboardOffersNextSkipAndFilters(t *testing.T) {
	markup := play.CardKeyboard(testBundle(), "uk")

	var data, texts []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
			texts = append(texts, button.Text)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{"play:next", "play:skip", "play:filters"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
	for _, d := range data {
		if len(d) > 64 {
			t.Fatalf("callback data %q is %d bytes, over Telegram's 64-byte cap", d, len(d))
		}
	}
	labels := strings.Join(texts, " ")
	if !strings.Contains(labels, "ДАЛІ-МАРКЕР") || !strings.Contains(labels, "ПРОПУСК-МАРКЕР") {
		t.Fatalf("button labels %q do not come from the bundle", labels)
	}
}

func TestFiltersKeyboardMarksActiveValuesAndFitsTheCallbackCap(t *testing.T) {
	filter := catalog.Filter{Include: map[string][]string{"kind": {"dare"}}}
	markup := play.FiltersKeyboard(testBundle(), "uk", filter)

	var data []string
	activeMarked := false
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
			if button.CallbackData == "play:filter:kind:dare" && strings.Contains(button.Text, "✓") {
				activeMarked = true
			}
			if len(button.CallbackData) > 64 {
				t.Fatalf("callback data %q is %d bytes, over the cap", button.CallbackData, len(button.CallbackData))
			}
		}
	}
	if !activeMarked {
		t.Fatalf("the active kind=dare filter is not marked; callbacks were %v", data)
	}
	if !strings.Contains(strings.Join(data, " "), "play:filter:intensity:gentle") {
		t.Fatalf("callbacks %v do not offer the intensity facet", data)
	}
}

func TestToggleFilterValueDoesNotMutateItsArgument(t *testing.T) {
	original := catalog.Filter{Include: map[string][]string{"kind": {"dare"}}}

	added := play.ToggleFilterValue(original, "intensity", "bold")
	if len(original.Include["intensity"]) != 0 {
		t.Fatalf("ToggleFilterValue mutated the caller's filter: %+v", original)
	}
	if len(added.Include["intensity"]) != 1 || added.Include["intensity"][0] != "bold" {
		t.Fatalf("added filter = %+v, want intensity=bold", added.Include)
	}

	removed := play.ToggleFilterValue(added, "intensity", "bold")
	if len(removed.Include["intensity"]) != 0 {
		t.Fatalf("toggling twice left %v, want the value removed", removed.Include["intensity"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -run "TestCard|TestFilters|TestToggle" -count=1`
Expected: FAIL — `undefined: play.CardCaption`.

- [ ] **Step 3: Write the implementation and the strings**

Create `internal/play/keyboards.go` з пʼятьма функціями зі списку «Produces», у стилі `internal/wishlist/keyboards.go`.

`CardCaption` складає: рядок актора (лише коли `actorName` не порожнє), локалізований тип картки й рівень інтенсивності, тоді текст. Локалізація типу — через ключі `play.kind.{value}`, рівня — `play.intensity.{value}`. **Якщо ключа немає, показуй сире значення фасета, а не сирий ключ** — `Bundle.Text` повертає ключ при промаху, і `positions/keyboards.go` уже має саме такий запобіжник; прочитай і повтори.

Додай перелічені ключі в **обидва** `content/i18n/*.json` у обʼєкт `strings`, дотримуючись наявного форматування.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1 -v`
Expected: PASS.

Run: `python3 -c "import json; u=json.load(open('content/i18n/uk.json'))['strings']; e=json.load(open('content/i18n/en.json'))['strings']; print(len(u), len(e), set(u)==set(e))"`
Expected: однакові розміри й `True`.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/play content/i18n
git commit -m "feat(play): add card captions, keyboards and strings"
```

---

### Task 3: Хендлер модуля

**Files:**
- Create: `internal/play/handler.go`
- Modify: `internal/play/handler_test.go` (додати тести роутингу)

**Interfaces:**
- Consumes: `play.Service` (Task 1), клавіатури (Task 2), `modules.Handler`.
- Produces:
  - `play.HandlerOptions{Service *Service, Repository Repository, Bot Bot, State StateStore, I18n *i18n.Bundle, Logger *slog.Logger}`
  - `play.NewHandler(HandlerOptions) *Handler` — реалізує `modules.Handler`
  - вузькі інтерфейси `play.Bot`, `play.Repository`, `play.StateStore`

**Оголоси вузькі інтерфейси в себе.** Мінімально потрібне:

```go
type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

type Repository interface {
	ActivePairForUser(ctx context.Context, userID int64) (*storage.Pair, error)
	UserDisplayName(ctx context.Context, telegramID int64) (string, error)
	UserLanguage(ctx context.Context, telegramID int64) (string, error)
}

type StateStore interface {
	SetModuleState(ctx context.Context, userID int64, module, value string, ttl time.Duration) error
	ModuleState(ctx context.Context, userID int64, module string) (string, error)
}
```

`HandleMessage` повертає `(false, nil)` — модуль не читає вільний текст.

**Обовʼязкові правила поведінки:**

- **Груповий чат відхиляється** першим ділом у `HandleCallback`. Прочитай `isGroupChat` у `internal/positions/handler.go` і повтори підхід. Додай тест.
- **`AnswerCallbackQuery` не викликати** — застосунок відповідає на колбек до диспетчеризації.
- **Гейт 18+/mature не перевіряти повторно** — каркас робить це до виклику хендлера.
- **`play:next` перемикає чергу, `play:skip` — ні.** Це головна поведінкова відмінність модуля; вона має бути очевидною в коді й покритою тестом.
- **Чергування працює лише з парою.** Без пари `actorName` порожнє, і `TurnB` не змінюється.
- **`StateStore` може бути nil.** У живому боті цього не станеться — `redisStore` створюється безумовно, а його `Ping` ретраїться перед стартом, — але хендлер усе одно має витримувати nil без деференсу, як це роблять позиції й вішліст. Тоді стан не читається й не зберігається, а гра лишається робочою: чергування вироджується в постійно перше імʼя, «без повторів» — у чистий випадок.
- **Порожня вибірка не мовчить**: помилка від `Service.Next` перетворюється на `play.empty`, а не на порожній екран чи мовчазний збій.
- **Розбір колбеків стійкий**: `play:filter:`, `play:filter:kind`, `play:filter:bogus:x` — жоден не панікує.

Імена акторів: `A` — `pair.UserAID`, `B` — `pair.UserBID`. Порожнє імʼя замінюється локалізованим `menu.partner_fallback`, який уже є в каталозі.

- [ ] **Step 1: Write the failing tests**

Додай у `internal/play/handler_test.go` тести:
- `play:next` перемикає `TurnB`, `play:skip` — ні (перевіряй через фейковий `StateStore`, що фіксує збережений стан);
- колбек із групового чату не доходить до сховища й нічого не надсилає;
- без пари підпис не містить імені;
- порожня вибірка дає текст `play.empty`;
- `HandleMessage` завжди повертає `(false, nil)`.

Фейкові `Bot`, `Repository` і `StateStore` пиши у стилі стабів із `internal/wishlist/handler_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1`
Expected: FAIL — `undefined: play.NewHandler`.

- [ ] **Step 3: Write the implementation**

Створи `internal/play/handler.go` з екранами `play:open`, `play:next`, `play:skip`, `play:filters`, `play:filter:{facet}:{value}`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/play
git commit -m "feat(play): add module screens and turn rotation"
```

---

### Task 4: Каталог на 80 карток

**Files:**
- Create: `content/play.v1.json`
- Create: `internal/play/catalog_test.go`

**Interfaces:**
- Consumes: `catalog.Load`, `(*Catalog).Validate`.
- Produces: `content/play.v1.json`.

Контентна задача. Тест — специфікація; він має падати, доки контент неповний.

**Правила контенту** (зі спека, розділ 8):
- рівно 80 карток, ID `p001`…`p080`, нуль-доповнені, за зростанням
- `kind` — рівно одне значення, `truth` або `dare`, по 40 кожного
- `intensity` — рівно одне значення; усього 30 `gentle`, 30 `medium`, 20 `bold`; у межах кожного `kind` — 15 / 15 / 10
- `title` до 40 символів (короткий ярлик, на картці не показується)
- `body` — одне речення, 30–160 символів; наказовий спосіб для `dare`, питальний для `truth`
- дія виконувана тут і зараз, без реквізиту, якого може не бути вдома
- **нічого, що потребує третьої особи або згоди поза парою**
- кожна мова пишеться природно, а не як переклад іншої

- [ ] **Step 1: Write the failing test**

Create `internal/play/catalog_test.go`:

```go
package play_test

import (
	"os"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func loadPlayCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/play.v1.json")
	if err != nil {
		t.Fatalf("open play catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load play catalog: %v", err)
	}
	return c
}

func TestPlayCatalogValidatesForBothLanguages(t *testing.T) {
	if err := loadPlayCatalog(t).Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPlayCatalogHasExactlyEightyPaddedAscendingIDs(t *testing.T) {
	c := loadPlayCatalog(t)
	if len(c.Items) != 80 {
		t.Fatalf("catalog has %d items, want exactly 80", len(c.Items))
	}
	for i, item := range c.Items {
		if len(item.ID) != 4 || !strings.HasPrefix(item.ID, "p") {
			t.Fatalf("item %d id = %q, want the zero-padded form pNNN", i, item.ID)
		}
		if i > 0 && c.Items[i-1].ID >= item.ID {
			t.Fatalf("ids are not strictly ascending at %d: %q then %q", i, c.Items[i-1].ID, item.ID)
		}
	}
}

func TestPlayCatalogFacetDistributionIsExact(t *testing.T) {
	kinds := map[string]int{"truth": 0, "dare": 0}
	intensities := map[string]int{"gentle": 0, "medium": 0, "bold": 0}
	perKind := map[string]map[string]int{
		"truth": {"gentle": 0, "medium": 0, "bold": 0},
		"dare":  {"gentle": 0, "medium": 0, "bold": 0},
	}

	for _, item := range loadPlayCatalog(t).Items {
		ks := item.Facets["kind"]
		if len(ks) != 1 {
			t.Fatalf("item %s has %d kind values, want exactly one", item.ID, len(ks))
		}
		if _, ok := kinds[ks[0]]; !ok {
			t.Fatalf("item %s has unknown kind %q", item.ID, ks[0])
		}
		is := item.Facets["intensity"]
		if len(is) != 1 {
			t.Fatalf("item %s has %d intensity values, want exactly one", item.ID, len(is))
		}
		if _, ok := intensities[is[0]]; !ok {
			t.Fatalf("item %s has unknown intensity %q", item.ID, is[0])
		}
		kinds[ks[0]]++
		intensities[is[0]]++
		perKind[ks[0]][is[0]]++
	}

	if kinds["truth"] != 40 || kinds["dare"] != 40 {
		t.Fatalf("kind split = truth %d / dare %d, want 40/40", kinds["truth"], kinds["dare"])
	}
	if intensities["gentle"] != 30 || intensities["medium"] != 30 || intensities["bold"] != 20 {
		t.Fatalf("intensity split = %v, want gentle 30 / medium 30 / bold 20", intensities)
	}
	for kind, want := range map[string]map[string]int{
		"truth": {"gentle": 15, "medium": 15, "bold": 10},
		"dare":  {"gentle": 15, "medium": 15, "bold": 10},
	} {
		for intensity, n := range want {
			if perKind[kind][intensity] != n {
				t.Fatalf("%s/%s = %d, want %d", kind, intensity, perKind[kind][intensity], n)
			}
		}
	}
}

func TestPlayCatalogTextLengthsAreWithinBounds(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		for _, lang := range []string{"uk", "en"} {
			title := []rune(item.Text[lang].Title)
			body := []rune(item.Text[lang].Body)
			if len(title) == 0 || len(title) > 40 {
				t.Fatalf("item %s %s title is %d runes, want 1-40", item.ID, lang, len(title))
			}
			if len(body) < 30 || len(body) > 160 {
				t.Fatalf("item %s %s body is %d runes, want 30-160", item.ID, lang, len(body))
			}
		}
	}
}

func TestPlayCatalogTruthsAskAndDaresInstruct(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		body := item.Text["uk"].Body
		isTruth := item.Facets["kind"][0] == "truth"
		endsWithQuestion := strings.HasSuffix(strings.TrimSpace(body), "?")
		if isTruth && !endsWithQuestion {
			t.Fatalf("truth %s does not end in a question mark: %q", item.ID, body)
		}
		if !isTruth && endsWithQuestion {
			t.Fatalf("dare %s reads as a question: %q", item.ID, body)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -run TestPlayCatalog -count=1`
Expected: FAIL — `open play catalog: no such file or directory`.

- [ ] **Step 3: Write the catalog**

Напиши всі 80 карток обома мовами за правилами вище. Працюй скриптом під скретчпадом, а не редагуй файл на 80 записів руками; видали скрипт по завершенні. `ensure_ascii=False`, у кінці файлу — новий рядок.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/play/ -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add content/play.v1.json internal/play/catalog_test.go
git commit -m "feat(play): add the truth-or-dare catalog"
```

---

### Task 5: Підключення, конфіг і документація

**Files:**
- Modify: `internal/config/config.go` — `PlayCatalogPath`
- Modify: `cmd/wrnrs/main.go` — завантаження каталогу, реєстрація модуля
- Modify: `.env.example`, `docs/ARCHITECTURE.md`, `docs/PLAN.md`
- Create: `internal/app/play_integration_test.go`

**Interfaces:**
- Consumes: `play.NewHandler`, `modules.Registry.Register`, `catalog.Load`.
- Produces: працюючий модуль у боті.

Конфіг: `PLAY_CATALOG_PATH` із дефолтом `content/play.v1.json`, за взірцем `PositionsCatalogPath` і `WishesCatalogPath`.

**Відсутній або невалідний каталог не має валити бот** — попередження в лог і модуль не зʼявляється в меню. Прочитай, як це зроблено для двох наявних каталогів у `cmd/wrnrs/main.go`, і повтори точно. Помилка `Register` — навпаки, валить старт.

Реєстрація:

```go
Gate: modules.Gate{Needs18Plus: true, NeedsMature: true},
```

`NeedsPair` і `NeedsPremium` лишаються нулем.

- [ ] **Step 1: Write the failing integration test**

`internal/app/play_integration_test.go`, внутрішній пакет (`package app`), два сценарії через `a.handleCallback` з `play:open`:
1. користувач із 18+, але без mature-опт-іну — відмова, і текст називає саме брак згоди на mature (обидва рядки відмови містять «18+», тож перевіряй фрагмент, унікальний для mature-відмови; прочитай фактичні рядки в `content/i18n/uk.json`);
2. користувач з обома прапорцями — отримує хаб.

Будуй хендлер із каталогом на 2–3 картки, а не з реальним файлом. `internal/app/wishlist_integration_test.go` — той самий взірець; читай його для `newTestApp`, імен полів фейкового бота й того, як модуль реєструється в тесті.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestPlay -count=1`
Expected: FAIL.

- [ ] **Step 3: Wire it**

- [ ] **Step 4: Update the docs**

`docs/ARCHITECTURE.md` — рядок у Package Map:

```markdown
- `internal/play`: truth-or-dare module — one couple on one phone, turn rotation, Redis-only session state.
```

`docs/PLAN.md` — зазначити, що етап 3 карти суперапу виконано.

`.env.example` — `PLAY_CATALOG_PATH=content/play.v1.json`.

- [ ] **Step 5: Run everything**

Run: `GOTOOLCHAIN=local go test ./... -count=1`
Expected: PASS.

Run: `GOTOOLCHAIN=local go build ./...`
Expected: чисто.

Run: `docker compose config >/dev/null && echo ok`
Expected: `ok`. **Це лише валідація — не запускай і не перезапускай контейнери.**

- [ ] **Step 6: Commit**

```bash
gofmt -w cmd internal
git add internal/config/config.go cmd/wrnrs/main.go .env.example docs internal/app/play_integration_test.go
git commit -m "feat(play): wire the play module into the bot"
```

---

## Self-Review

**Покриття спека.**

| Вимога спека | Задача |
|---|---|
| §4.1 `service.go` | Task 1 |
| §4.1 `keyboards.go` | Task 2 |
| §4.1 `handler.go` | Task 3 |
| §4.1 `content/play.v1.json` | Task 4 |
| §4.2 конфіг, `main.go`, i18n | Task 2 (i18n), Task 5 (решта) |
| §4.2 сховище не змінюється | жодна задача не торкається `internal/storage/` — рев'ю має це підтвердити |
| §5 `GameState`, Redis, `Recent` ≤ 15 | Task 1 (структура, межа), Task 3 (читання/запис) |
| §5 деградація без Redis | Task 3, тест на nil `StateStore` |
| §6 чергування, пропуск не перемикає | Task 3, окремий тест |
| §6 запасне імʼя партнера | Task 3 |
| §7 екрани й колбеки | Tasks 2-3 |
| §7 порожня вибірка пояснюється | Task 1 (помилка), Task 3 (текст) |
| §8 контент, фасети, розподіли | Task 4 |
| §9 гейт, груповий чат | Task 5 (гейт), Task 3 (група) |
| §11 план тестування | розкладено по задачах 1-5 |

**Узгодженість типів.** `play.GameState`, `EncodeState`/`DecodeState`, `Service`, `RecentLimit` з Task 1 споживаються в Tasks 2-3. Клавіатури Task 2 викликаються з Task 3. `HandlerOptions` Task 3 використовується в Task 5. `catalog.SelectionInput` має поле `Cycle`, яке цей модуль не використовує — `Next` лишає його нулем і варіює `Bucket`; це свідомо, бо тут немає поняття циклу колоди.

**Свідома межа.** Tasks 3 і 5 описані менш дослівно, ніж 1, 2 і 4: там більше інтеграції з наявним кодом, який виконавець має прочитати. Замість вигаданих номерів рядків обидві задачі називають файл-взірець і фіксують інваріанти, які мають виконуватись.

**Ризик, який виконавцю треба тримати в голові.** Task 5 чіпає `cmd/wrnrs/main.go`, де вже зареєстровані два модулі. Гелпери `appObjectStore`/`positionsObjectStore` існують там, щоб не покласти типізований nil в інтерфейсне поле — але цьому модулю `ObjectStore` не потрібен узагалі, тож вони його не стосуються. Що стосується: `redisStore` створюється безумовно і його `Ping` ретраїться 30 разів перед стартом, тому в живому боті `State` ніколи не nil. Захист від nil у Task 3 — це страховка для тестів і майбутнього, а не реальний рантайм-сценарій; не викидай його, але й не вигадуй навколо нього логіки.
