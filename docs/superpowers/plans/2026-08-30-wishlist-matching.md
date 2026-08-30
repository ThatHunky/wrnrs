# Вішліст бажань із матчингом — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Модуль, у якому партнери приватно відмічають бажання «Хочу / Цікаво / Ні», а бот показує лише збіги — і збіги по позах живлять рандомайзер.

**Architecture:** Новий пакет `internal/wishlist` за взірцем `internal/positions`: чистий сервіс без I/O плюс Telegram-хендлер, що залежить від вузьких інтерфейсів, оголошених у себе. Відповіді зберігаються в новій таблиці `wish_answers`, ключованій **користувачем**, а не парою. Збіги обчислюються одним SQL-join'ом усередині сховища; назовні індивідуальні відповіді партнера не виходять ніколи. Модуль поз читає збіги через `internal/storage`, тому імпорту модуля в модуль не виникає.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, Redis, Telegram Bot API, `internal/catalog`, `internal/modules`.

## Global Constraints

- Go 1.24. Не піднімати тулчейн без явного дозволу. Не чіпати `go.mod`/`go.sum`.
- Верифікація: `GOTOOLCHAIN=local go test ./... -count=1`.
- Форматування перед комітом: `gofmt -w cmd internal`.
- Тести пишуться **до** зміни поведінки.
- Модуль Go — `wrnrs`. Тестові пакети зовнішні (`package wishlist_test`, `package storage_test`), окрім тих, що торкаються неекспортованого.
- **Бот працює наживо в Docker на цій машині.** Не перезапускати його, не перезбирати образ, не виконувати `docker compose` нічого.
- Шкала відповідей: рівно `want` / `curious` / `no`.
- Правило збігу: `a != 'no' AND b != 'no' AND (a = 'want' OR b = 'want')`. `curious + curious` збігом **не є**.
- **Жодна окрема відповідь партнера не виходить зі сховища.** Немає методу, що повертає чужі відповіді. Назовні — лише збіги й булеве «партнер почав».
- Прогрес партнера не показується ніде: ні лічильник, ні відсоток.
- Відповіді належать користувачу: розрив пари їх **не** видаляє. Видалення акаунта — видаляє каскадом.
- Гейт модуля: `Needs18Plus: true, NeedsMature: true`, `NeedsPair: false`, `NeedsPremium: false`.
- ID бажань — нуль-доповнені `w001`…`w060`: `catalog.Filtered` сортує лексикографічно.
- Каталог — рівно 60 елементів, обидві мови обовʼязкові.
- Кожен доданий ключ i18n має існувати в **обох** `content/i18n/{uk,en}.json`.
- Telegram: `callback_data` ≤ 64 байти.
- Не хардкодити токени, ID адмінів, реквізити, секрети MinIO.
- Спек: `docs/superpowers/specs/2026-08-30-wishlist-matching-design.md`.

---

## File Structure

| Файл | Відповідальність |
|---|---|
| `internal/storage/wishes.go` | таблиця `wish_answers`, запис відповіді, читання своїх, обчислення збігів, «партнер почав» |
| `internal/storage/wishes_test.go` | тести сховища, включно з приватністю й каскадами |
| `internal/wishlist/service.go` | чиста логіка: правило збігу, черга наступного невідміченого, статистика |
| `internal/wishlist/service_test.go` | тести чистої логіки |
| `internal/wishlist/keyboards.go` | клавіатури й тексти екранів |
| `internal/wishlist/handler.go` | вузькі інтерфейси, роутинг колбеків, екрани |
| `internal/wishlist/handler_test.go` | тести чистих функцій хендлера |
| `content/wishes.v1.json` | каталог на 60 бажань (генерується Task 6, комітиться) |
| `internal/positions/handler.go` | кнопка бажання на картці, фільтр «тільки збіги» |
| `internal/positions/keyboards.go` | ті самі кнопка й пункт фільтра |
| `cmd/wrnrs/main.go` | завантаження каталогу, реєстрація модуля |
| `internal/config/config.go` | `WISHES_CATALOG_PATH` |
| `content/i18n/{uk,en}.json` | рядки модуля |

---

### Task 1: Сховище відповідей і збігів

**Files:**
- Create: `internal/storage/wishes.go`
- Create: `internal/storage/wishes_test.go`
- Modify: `internal/storage/sqlite.go` — додати `CREATE TABLE` у константу `schemaSQL`
- Modify: `migrations/001_init.sql` — той самий DDL

**Interfaces:**
- Consumes: наявні `storage.Repository`, `storage.Pair`, і тестовий гелпер `newRepoWithPair(t)` з `internal/storage/positions_test.go`.
- Produces:
  - `storage.WishAnswer` — рядок `string` з константами `storage.AnswerWant = "want"`, `storage.AnswerCurious = "curious"`, `storage.AnswerNo = "no"`
  - `storage.WishItemKind` — рядок `string` з константами `storage.WishKindWish = "wish"`, `storage.WishKindPosition = "position"`
  - `storage.WishMatch{ItemKind WishItemKind, ItemID string, Strong bool}` — `Strong` істинне для `want+want`
  - `(*Repository).SetWishAnswer(ctx context.Context, userID int64, kind WishItemKind, itemID string, answer WishAnswer, now time.Time) error`
  - `(*Repository).UserWishAnswers(ctx context.Context, userID int64) (map[string]WishAnswer, error)` — ключ `string(kind) + ":" + itemID`
  - `(*Repository).PairWishMatches(ctx context.Context, pairID int64) ([]WishMatch, error)`
  - `(*Repository).PartnerHasAnyWishAnswer(ctx context.Context, pairID, userID int64) (bool, error)`

**DDL** (однаковий у `schemaSQL` і в `migrations/001_init.sql`):

```sql
CREATE TABLE IF NOT EXISTS wish_answers (
    user_id     INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    item_kind   TEXT    NOT NULL CHECK (item_kind IN ('wish', 'position')),
    item_id     TEXT    NOT NULL,
    answer      TEXT    NOT NULL CHECK (answer IN ('want', 'curious', 'no')),
    answered_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, item_kind, item_id)
);
```

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/wishes_test.go`:

```go
package storage_test

import (
	"context"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

func TestSetWishAnswerUpsertsAndReadsBack(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	_ = pairID
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers: %v", err)
	}
	if got := answers["wish:w001"]; got != storage.AnswerWant {
		t.Fatalf("answer = %q, want %q", got, storage.AnswerWant)
	}

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerNo, now); err != nil {
		t.Fatalf("SetWishAnswer overwrite: %v", err)
	}
	answers, err = repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after overwrite: %v", err)
	}
	if got := answers["wish:w001"]; got != storage.AnswerNo {
		t.Fatalf("answer after overwrite = %q, want %q — the same item must update, not duplicate", got, storage.AnswerNo)
	}
	if len(answers) != 1 {
		t.Fatalf("answers = %v, want exactly one row after an overwrite", answers)
	}
}

func TestSetWishAnswerRejectsUnknownValues(t *testing.T) {
	repo, _ := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.WishAnswer("maybe"), now); err == nil {
		t.Fatal("SetWishAnswer with an unknown answer succeeded, want an error")
	}
	if err := repo.SetWishAnswer(ctx, 1001, storage.WishItemKind("song"), "w001", storage.AnswerWant, now); err == nil {
		t.Fatal("SetWishAnswer with an unknown item kind succeeded, want an error")
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers: %v", err)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %v, want none written after rejected calls", answers)
	}
}

func TestPairWishMatchesAppliesTheMatchRule(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	cases := []struct {
		item   string
		a, b   storage.WishAnswer
		match  bool
		strong bool
	}{
		{"w001", storage.AnswerWant, storage.AnswerWant, true, true},
		{"w002", storage.AnswerWant, storage.AnswerCurious, true, false},
		{"w003", storage.AnswerCurious, storage.AnswerWant, true, false},
		{"w004", storage.AnswerCurious, storage.AnswerCurious, false, false},
		{"w005", storage.AnswerWant, storage.AnswerNo, false, false},
		{"w006", storage.AnswerNo, storage.AnswerWant, false, false},
		{"w007", storage.AnswerNo, storage.AnswerNo, false, false},
		{"w008", storage.AnswerCurious, storage.AnswerNo, false, false},
		{"w009", storage.AnswerNo, storage.AnswerCurious, false, false},
	}
	for _, c := range cases {
		if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, c.item, c.a, now); err != nil {
			t.Fatalf("SetWishAnswer A %s: %v", c.item, err)
		}
		if err := repo.SetWishAnswer(ctx, 2002, storage.WishKindWish, c.item, c.b, now); err != nil {
			t.Fatalf("SetWishAnswer B %s: %v", c.item, err)
		}
	}

	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	got := map[string]bool{}
	strong := map[string]bool{}
	for _, m := range matches {
		got[m.ItemID] = true
		strong[m.ItemID] = m.Strong
	}
	for _, c := range cases {
		if got[c.item] != c.match {
			t.Fatalf("%s (%s + %s): match = %v, want %v", c.item, c.a, c.b, got[c.item], c.match)
		}
		if c.match && strong[c.item] != c.strong {
			t.Fatalf("%s (%s + %s): strong = %v, want %v", c.item, c.a, c.b, strong[c.item], c.strong)
		}
	}
}

func TestPairWishMatchesNeedsBothAnswers(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %v, want none — only one partner has answered", matches)
	}
}

func TestPairWishMatchesSeparatesItemKinds(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, u := range []int64{1001, 2002} {
		if err := repo.SetWishAnswer(ctx, u, storage.WishKindWish, "001", storage.AnswerWant, now); err != nil {
			t.Fatalf("SetWishAnswer wish: %v", err)
		}
		if err := repo.SetWishAnswer(ctx, u, storage.WishKindPosition, "001", storage.AnswerNo, now); err != nil {
			t.Fatalf("SetWishAnswer position: %v", err)
		}
	}

	matches, err := repo.PairWishMatches(ctx, pairID)
	if err != nil {
		t.Fatalf("PairWishMatches: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %v, want exactly one — the same id in two kinds must not be conflated", matches)
	}
	if matches[0].ItemKind != storage.WishKindWish {
		t.Fatalf("match kind = %q, want %q", matches[0].ItemKind, storage.WishKindWish)
	}
}

func TestPartnerHasAnyWishAnswerIsABooleanNotACount(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	started, err := repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if started {
		t.Fatal("partner reported as started before answering anything")
	}

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	started, err = repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if started {
		t.Fatal("the caller's own answer was counted as the partner's")
	}

	if err := repo.SetWishAnswer(ctx, 2002, storage.WishKindWish, "w001", storage.AnswerNo, now); err != nil {
		t.Fatalf("SetWishAnswer partner: %v", err)
	}
	started, err = repo.PartnerHasAnyWishAnswer(ctx, pairID, 1001)
	if err != nil {
		t.Fatalf("PartnerHasAnyWishAnswer: %v", err)
	}
	if !started {
		t.Fatal("partner answered but is not reported as started")
	}
}

func TestWishAnswersSurvivePairBreak(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	_ = pairID
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	if _, err := repo.EndActivePair(ctx, 1001, now); err != nil {
		t.Fatalf("EndActivePair: %v", err)
	}

	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after break: %v", err)
	}
	if answers["wish:w001"] != storage.AnswerWant {
		t.Fatalf("answers = %v, want the answer to survive the break — wishes belong to the person, not the relationship", answers)
	}
}

func TestWishAnswersDisappearWhenUserIsDeleted(t *testing.T) {
	repo, _ := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.SetWishAnswer(ctx, 1001, storage.WishKindWish, "w001", storage.AnswerWant, now); err != nil {
		t.Fatalf("SetWishAnswer: %v", err)
	}
	if err := repo.DeleteUser(ctx, 1001); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	answers, err := repo.UserWishAnswers(ctx, 1001)
	if err != nil {
		t.Fatalf("UserWishAnswers after delete: %v", err)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %v, want none after account deletion", answers)
	}
}
```

**Перед запуском** прочитай `internal/storage/positions_test.go` і звір, як саме `newRepoWithPair` створює пару та які telegram id вона використовує. Якщо там не `1001`/`2002` — підстав фактичні. Так само звір сигнатури `EndActivePair` і `DeleteUser` у `internal/storage/sqlite.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/storage/ -run TestWish -count=1`
Expected: FAIL — `undefined: storage.WishKindWish`.

- [ ] **Step 3: Write the DDL**

Додай блок із «DDL» вище в кінець константи `schemaSQL` у `internal/storage/sqlite.go` і в кінець `migrations/001_init.sql`. Дві копії мають бути посимвольно однакові — це не косметика: файл міграції застосовує наявний деплой, а `schemaSQL` отримує свіжа база, і розходження між ними — продакшн-загроза.

Оскільки DDL використовує `CREATE TABLE IF NOT EXISTS`, наявна база отримає таблицю на першому ж старті без окремої міграції. Перевір це в Step 6.

- [ ] **Step 4: Write the implementation**

Create `internal/storage/wishes.go`:

```go
package storage

import (
	"context"
	"fmt"
	"time"
)

// WishAnswer is one person's private stance on one item.
type WishAnswer string

const (
	AnswerWant    WishAnswer = "want"
	AnswerCurious WishAnswer = "curious"
	AnswerNo      WishAnswer = "no"
)

// WishItemKind separates the two id spaces a wish answer can point at: the
// wishes catalog and the positions catalog. Without it, wish "001" and
// position "001" would collide.
type WishItemKind string

const (
	WishKindWish     WishItemKind = "wish"
	WishKindPosition WishItemKind = "position"
)

// WishMatch is one item both partners are open to. Strong marks the case
// where both said "want" rather than one merely being curious.
//
// This is deliberately the ONLY shape in which one partner's stance reaches
// the other: a match says "you are both open to this" and never reveals an
// individual answer. There is no repository method that returns another
// user's answers, and adding one would break the module's core promise.
type WishMatch struct {
	ItemKind WishItemKind
	ItemID   string
	Strong   bool
}

func (a WishAnswer) valid() bool {
	switch a {
	case AnswerWant, AnswerCurious, AnswerNo:
		return true
	default:
		return false
	}
}

func (k WishItemKind) valid() bool {
	switch k {
	case WishKindWish, WishKindPosition:
		return true
	default:
		return false
	}
}

// SetWishAnswer records or replaces one person's answer for one item.
func (r *Repository) SetWishAnswer(ctx context.Context, userID int64, kind WishItemKind, itemID string, answer WishAnswer, now time.Time) error {
	if !kind.valid() {
		return fmt.Errorf("unknown wish item kind %q", kind)
	}
	if !answer.valid() {
		return fmt.Errorf("unknown wish answer %q", answer)
	}
	if itemID == "" {
		return fmt.Errorf("wish item id is required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wish_answers (user_id, item_kind, item_id, answer, answered_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, item_kind, item_id) DO UPDATE SET
			answer = excluded.answer,
			answered_at = excluded.answered_at
	`, userID, string(kind), itemID, string(answer), now)
	if err != nil {
		return fmt.Errorf("write wish answer: %w", err)
	}
	return nil
}

// UserWishAnswers returns the caller's own answers keyed "kind:itemID".
func (r *Repository) UserWishAnswers(ctx context.Context, userID int64) (map[string]WishAnswer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT item_kind, item_id, answer FROM wish_answers WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("load wish answers: %w", err)
	}
	defer rows.Close()

	out := map[string]WishAnswer{}
	for rows.Next() {
		var kind, id, answer string
		if err := rows.Scan(&kind, &id, &answer); err != nil {
			return nil, fmt.Errorf("scan wish answer: %w", err)
		}
		out[kind+":"+id] = WishAnswer(answer)
	}
	return out, rows.Err()
}

// PairWishMatches computes the pair's matches inside the database so that no
// individual answer ever leaves this package. A match requires both partners
// to have answered, neither to have said "no", and at least one to have said
// "want".
func (r *Repository) PairWishMatches(ctx context.Context, pairID int64) ([]WishMatch, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.item_kind, a.item_id,
		       (a.answer = 'want' AND b.answer = 'want') AS strong
		FROM pairs p
		JOIN wish_answers a ON a.user_id = p.user_a_id
		JOIN wish_answers b ON b.user_id = p.user_b_id
		                   AND b.item_kind = a.item_kind
		                   AND b.item_id = a.item_id
		WHERE p.id = ?
		  AND a.answer <> 'no'
		  AND b.answer <> 'no'
		  AND (a.answer = 'want' OR b.answer = 'want')
		ORDER BY a.item_kind, a.item_id
	`, pairID)
	if err != nil {
		return nil, fmt.Errorf("load wish matches: %w", err)
	}
	defer rows.Close()

	var out []WishMatch
	for rows.Next() {
		var m WishMatch
		var kind string
		if err := rows.Scan(&kind, &m.ItemID, &m.Strong); err != nil {
			return nil, fmt.Errorf("scan wish match: %w", err)
		}
		m.ItemKind = WishItemKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// PartnerHasAnyWishAnswer reports only whether the other partner has started.
// It is deliberately a boolean and not a count: a count would let a partner
// subtract matches from progress and infer the caller's "no" answers.
func (r *Repository) PartnerHasAnyWishAnswer(ctx context.Context, pairID, userID int64) (bool, error) {
	var found int
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pairs p
			JOIN wish_answers w
			  ON w.user_id = CASE WHEN p.user_a_id = ? THEN p.user_b_id ELSE p.user_a_id END
			WHERE p.id = ?
		)
	`, userID, pairID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check partner wish activity: %w", err)
	}
	return found == 1, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/storage/ -count=1 -v`
Expected: PASS — вісім нових тестів плюс усі наявні.

- [ ] **Step 6: Prove the table appears on an existing database**

Наявний деплой має базу без цієї таблиці. Напиши тимчасовий тест, який створює файл SQLite зі **старою** схемою (без `wish_answers`), викликає `OpenSQLite` і перевіряє, що таблиця зʼявилась. Це та сама перевірка, що врятувала попередню міграцію. Запиши вивід у звіт і видали тимчасовий файл.

- [ ] **Step 7: Prove the DDL copies match**

Run: витягни блок `CREATE TABLE IF NOT EXISTS wish_answers` з обох файлів і порівняй їх `diff`. Вивід має бути порожній.

- [ ] **Step 8: Commit**

```bash
gofmt -w cmd internal
git add internal/storage/wishes.go internal/storage/wishes_test.go internal/storage/sqlite.go migrations/001_init.sql
git commit -m "feat(storage): add private wish answers and pair match computation"
```

---

### Task 2: Чиста логіка модуля

**Files:**
- Create: `internal/wishlist/service.go`
- Create: `internal/wishlist/service_test.go`

**Interfaces:**
- Consumes: `catalog.Catalog`, `catalog.Item`, `storage.WishAnswer`, `storage.WishItemKind`, `storage.WishMatch` з Task 1.
- Produces:
  - `wishlist.ServiceOptions{Catalog *catalog.Catalog}`
  - `wishlist.NewService(ServiceOptions) *Service`
  - `(*Service).Queue() []catalog.Item` — весь каталог у порядку черги
  - `(*Service).NextUnanswered(answers map[string]storage.WishAnswer) (catalog.Item, bool)`
  - `(*Service).Progress(answers map[string]storage.WishAnswer) (answered, total int)`
  - `(*Service).Item(id string) (catalog.Item, bool)`
  - `wishlist.AnswerKey(kind storage.WishItemKind, itemID string) string`

Порядок черги детермінований: спершу за інтенсивністю `gentle` → `medium` → `bold`, за рівної інтенсивності — за зростанням `ID`. Елемент без відомої інтенсивності йде **після** всіх відомих, щоб новий фасет не вискакував першим.

`Progress` рахує лише бажання (`wish`), не позиції: позиції голосуються ліниво й не мають «повного» знаменника.

- [ ] **Step 1: Write the failing tests**

Create `internal/wishlist/service_test.go`:

```go
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
		wishlist.AnswerKey(storage.WishKindWish, "w001"):     storage.AnswerWant,
		wishlist.AnswerKey(storage.WishKindPosition, "042"):  storage.AnswerWant,
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1`
Expected: FAIL — `no required module provides package wrnrs/internal/wishlist`.

- [ ] **Step 3: Write the implementation**

Create `internal/wishlist/service.go`:

```go
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

type ServiceOptions struct {
	Catalog *catalog.Catalog
}

type Service struct {
	queue []catalog.Item
}

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

// Queue returns the wishes in the order they are offered.
func (s *Service) Queue() []catalog.Item {
	out := make([]catalog.Item, len(s.queue))
	copy(out, s.queue)
	return out
}

// NextUnanswered returns the first wish the caller has not answered yet.
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
// module and have no meaningful denominator here.
func (s *Service) Progress(answers map[string]storage.WishAnswer) (int, int) {
	answered := 0
	for _, item := range s.queue {
		if _, done := answers[AnswerKey(storage.WishKindWish, item.ID)]; done {
			answered++
		}
	}
	return answered, len(s.queue)
}

func (s *Service) Item(id string) (catalog.Item, bool) {
	for _, item := range s.queue {
		if item.ID == id {
			return item, true
		}
	}
	return catalog.Item{}, false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1 -v`
Expected: PASS — шість тестів.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/wishlist/service.go internal/wishlist/service_test.go
git commit -m "feat(wishlist): add the I/O-free queue and progress logic"
```

---

### Task 3: Клавіатури й тексти

**Files:**
- Create: `internal/wishlist/keyboards.go`
- Create: `internal/wishlist/handler_test.go` (тести чистих функцій; хендлер додається в Task 4)
- Modify: `content/i18n/uk.json`, `content/i18n/en.json`

**Interfaces:**
- Consumes: `catalog.Item`, `telegram.InlineKeyboardMarkup`, `i18n.Bundle`.
- Produces:
  - `wishlist.HubKeyboard(language string, hasPair bool, matches int) telegram.InlineKeyboardMarkup`
  - `wishlist.SwipeKeyboard(language, itemID string) telegram.InlineKeyboardMarkup`
  - `wishlist.SwipeCaption(bundle *i18n.Bundle, language string, item catalog.Item, answered, total int) string`
  - `wishlist.BackKeyboard(language string) telegram.InlineKeyboardMarkup`

Колбеки: `wish:open`, `wish:next`, `wish:answer:{kind}:{id}:{answer}`, `wish:matches`, `wish:mine`. Найдовший реальний — `wish:answer:position:519:curious` = 32 байти, добре в межах 64.

i18n-ключі (додати в **обидва** файли):

```
wish.hub.title          Бажання
wish.hub.intro          Відмічай, чого хочеш. Партнер не побачить твоїх «ні» — тільки те, у чому ви збіглися.
wish.hub.progress       Відмічено: %d з %d
wish.hub.partner_active Партнер теж відмічає
wish.hub.swipe          💛 Відмічати
wish.hub.matches        🔥 Збіги (%d)
wish.hub.mine           📊 Мої відповіді
wish.answer.want        💛 Хочу
wish.answer.curious     🤔 Цікаво
wish.answer.no          🚫 Ні
wish.answer.skip        ⏭ Пропустити
wish.done               Це все, що є. Можеш переглянути свої відповіді або зазирнути у збіги.
wish.matches.title      Збіги
wish.matches.empty      Поки порожньо. Це не означає «не збіглося» — можливо, партнер ще не дійшов.
wish.matches.needs_pair Збіги зʼявляться, коли буде пара. Відмічати можна вже зараз — відповіді збережуться.
wish.mine.title         Мої відповіді
wish.mine.empty         Ти ще нічого не відмічав.
wish.privacy_note       Партнер бачить лише збіги, ніколи — окремі відповіді.
```

Англійські — переклади того самого змісту, у стилі решти `en.json`.

- [ ] **Step 1: Write the failing tests**

Create `internal/wishlist/handler_test.go`:

```go
package wishlist_test

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/wishlist"
)

func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"wish.hub.progress": "Відмічено: %d з %d",
	}})
	return b
}

func TestSwipeKeyboardOffersThreeAnswersAndSkip(t *testing.T) {
	markup := wishlist.SwipeKeyboard("uk", "w007")

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{
		"wish:answer:wish:w007:want",
		"wish:answer:wish:w007:curious",
		"wish:answer:wish:w007:no",
		"wish:next",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
	for _, d := range data {
		if len(d) > 64 {
			t.Fatalf("callback data %q is %d bytes, over Telegram's 64-byte cap", d, len(d))
		}
	}
}

func TestHubKeyboardHidesMatchesCountWithoutAPair(t *testing.T) {
	withPair := wishlist.HubKeyboard("uk", true, 4)
	withoutPair := wishlist.HubKeyboard("uk", false, 0)

	var withText, withoutText string
	for _, row := range withPair.InlineKeyboard {
		for _, b := range row {
			withText += b.Text + " "
		}
	}
	for _, row := range withoutPair.InlineKeyboard {
		for _, b := range row {
			withoutText += b.Text + " "
		}
	}
	if !strings.Contains(withText, "4") {
		t.Fatalf("paired hub %q does not show the match count", withText)
	}
	if strings.Contains(withoutText, "(0)") {
		t.Fatalf("solo hub %q shows a zero match count; it should not promise matches without a pair", withoutText)
	}
}

func TestSwipeCaptionShowsTitleAndProgress(t *testing.T) {
	item := catalog.Item{
		ID:   "w007",
		Text: map[string]catalog.ItemText{"uk": {Title: "Свічки", Body: "Кімната, освітлена лише свічками."}},
	}
	caption := wishlist.SwipeCaption(testBundle(), "uk", item, 6, 60)

	if !strings.Contains(caption, "Свічки") {
		t.Fatalf("caption %q does not contain the title", caption)
	}
	if !strings.Contains(caption, "Кімната") {
		t.Fatalf("caption %q does not contain the body", caption)
	}
	if !strings.Contains(caption, "6") || !strings.Contains(caption, "60") {
		t.Fatalf("caption %q does not contain the 6/60 progress", caption)
	}
}
```


- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -run "TestSwipe|TestHub" -count=1`
Expected: FAIL — `undefined: wishlist.SwipeKeyboard`.

- [ ] **Step 3: Write the implementation and the i18n strings**

Create `internal/wishlist/keyboards.go` з чотирма функціями зі списку «Produces». Читай `internal/positions/keyboards.go` і повторюй його стиль: локалізовані тексти беруться з `i18n.Bundle`, кнопка повернення веде на `menu:main`, який належить застосунку.

`HubKeyboard` показує кнопку збігів із лічильником лише коли `hasPair`. Без пари кнопка лишається видимою (щоб було видно, що фіча існує), але без числа — обіцяти «0 збігів» без партнера неправильно.

Додай перелічені ключі в **обидва** `content/i18n/*.json` у обʼєкт `strings`, дотримуючись наявного форматування.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1 -v`
Expected: PASS.

Run: `python3 -c "import json; u=json.load(open('content/i18n/uk.json'))['strings']; e=json.load(open('content/i18n/en.json'))['strings']; print(len(u), len(e), set(u)==set(e))"`
Expected: однакові розміри й `True`.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/wishlist content/i18n
git commit -m "feat(wishlist): add module keyboards, captions and strings"
```

---

### Task 4: Хендлер модуля

**Files:**
- Create: `internal/wishlist/handler.go`
- Modify: `internal/wishlist/handler_test.go` (додати тести роутингу, що не потребують Telegram)

**Interfaces:**
- Consumes: `wishlist.Service` (Task 2), клавіатури (Task 3), `storage` API (Task 1), `modules.Handler`.
- Produces:
  - `wishlist.HandlerOptions{Service *Service, Repository Repository, Bot Bot, I18n *i18n.Bundle, Logger *slog.Logger}`
  - `wishlist.NewHandler(HandlerOptions) *Handler` — реалізує `modules.Handler`
  - вузькі інтерфейси `wishlist.Bot`, `wishlist.Repository`

**Оголоси вузькі інтерфейси в себе**, а не залежай від конкретних типів — рівно як це робить `internal/positions/handler.go`, який варто прочитати першим. Мінімально потрібне:

```go
type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

type Repository interface {
	ActivePairForUser(ctx context.Context, userID int64) (*storage.Pair, error)
	UserLanguage(ctx context.Context, telegramID int64) (string, error)
	SetWishAnswer(ctx context.Context, userID int64, kind storage.WishItemKind, itemID string, answer storage.WishAnswer, now time.Time) error
	UserWishAnswers(ctx context.Context, userID int64) (map[string]storage.WishAnswer, error)
	PairWishMatches(ctx context.Context, pairID int64) ([]storage.WishMatch, error)
	PartnerHasAnyWishAnswer(ctx context.Context, pairID, userID int64) (bool, error)
}
```

`HandleMessage` повертає `(false, nil)` — модуль не читає вільний текст (це Р5 спека).

**Обовʼязкові правила поведінки:**

- **Група відхиляється.** Модуль поз уже має цей запобіжник: контент 18+ не має потрапляти в груповий чат лише тому, що один учасник має опт-ін. Прочитай, як `internal/positions/handler.go` це робить (`isGroupChat`), і повтори той самий підхід. Додай тест.
- **`AnswerCallbackQuery` не викликати.** Застосунок уже відповідає на колбек до диспетчеризації.
- **Гейт 18+/mature не перевіряти повторно** — каркас робить це до виклику хендлера. А от **екран збігів вимагає пари**, чого гейт не покриває: без пари показуй `wish.matches.needs_pair`, а не порожній список.
- **Розбір колбеків має бути стійким.** `wish:answer:wish:w007:bogus`, `wish:answer:song:w007:want`, `wish:answer:` — жоден не має панікувати. Невідома відповідь чи вид — показати екран заново.
- Ліміт колбека 64 байти вже перевірено в Task 3; не емітуй довших.

- [ ] **Step 1: Write the failing tests**

Додай у `internal/wishlist/handler_test.go` тести, які не потребують Telegram:
- розбір `wish:answer:wish:w007:want` дає вид `wish`, id `w007`, відповідь `want`;
- розбір `wish:answer:song:w007:want` і `wish:answer:wish:w007:bogus` повертають «не розпізнано» без паніки;
- колбек із групового чату не доходить до сховища (фейковий репозиторій не отримує жодного виклику).

Для третього знадобиться фейковий `Bot` і `Repository`. Пиши їх у стилі стабів із `internal/positions/handler_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1`
Expected: FAIL — `undefined: wishlist.NewHandler`.

- [ ] **Step 3: Write the implementation**

Створи `internal/wishlist/handler.go` з екранами:

- `wish:open` — хаб: заголовок, вступ, прогрес, рядок «партнер теж відмічає» коли `PartnerHasAnyWishAnswer`, і `HubKeyboard`
- `wish:next` — наступне невідмічене через `Service.NextUnanswered`; коли нічого не лишилось — `wish.done`
- `wish:answer:{kind}:{id}:{answer}` — записати через `SetWishAnswer`, тоді одразу показати наступне
- `wish:matches` — `PairWishMatches`; без пари `wish.matches.needs_pair`; порожньо — `wish.matches.empty`
- `wish:mine` — свої відповіді, згруповані за значенням

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/wishlist
git commit -m "feat(wishlist): add module screens and callback routing"
```

---

### Task 5: Ліниве голосування по позах і фільтр збігів

**Files:**
- Modify: `internal/positions/handler.go`
- Modify: `internal/positions/keyboards.go`
- Modify: `internal/positions/service.go` — поле в `BrowseState`
- Modify: `internal/positions/handler_test.go`, `internal/positions/service_test.go`

**Interfaces:**
- Consumes: `storage.SetWishAnswer`, `storage.PairWishMatches`, `storage.UserWishAnswers` з Task 1.
- Produces: нових експортованих імен не додає, крім поля `BrowseState.MatchesOnly bool` з тегом `json:"m"`.

**Дві зміни:**

1. **Кнопка бажання на картці позиції.** Пише `wish_answers` із `item_kind='position'`. Колбек `pos:wish:{id}:{answer}` — 22 байти для найдовшого. Показує поточну відповідь, якщо вона є. Працює **соло**: це особиста відповідь, пари не потребує, на відміну від відміток «пробували».

   Розшир інтерфейс `Repository` в `internal/positions/handler.go` рівно на потрібні методи.

2. **Фільтр «тільки збіги».**

   **Це не фасет.** `catalog.Filter` оперує значеннями фасетів із каталогу, а збіги живуть у базі й залежать від пари. Перемикач не можна класти у `Filter.Include`. Він застосовується **окремим кроком після** `Filtered()` — там же, де `VisibleWithMarks` уже відсіює приховані позиції. Стан живе в `BrowseState.MatchesOnly`, поряд із фільтром, не всередині.

   Без пари перемикач неактивний: показувати з замком, як зроблено для відміток.

- [ ] **Step 1: Write the failing tests**

Тести, які треба додати:
- `BrowseState` із `MatchesOnly: true` переживає `EncodeState`/`DecodeState` (у `service_test.go`, поруч із наявним тестом round-trip);
- клавіатура картки містить `pos:wish:{id}:want` і решту трьох відповідей, усі ≤64 байти;
- застосування фільтра збігів звужує список до переданих id і не чіпає фасетні фільтри — чиста функція, тестується без Telegram.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -count=1`
Expected: FAIL — невідоме поле `MatchesOnly`.

- [ ] **Step 3: Write the implementation**

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ ./internal/wishlist/ -count=1 -v`
Expected: PASS — старі тести модуля поз теж мають лишитись зеленими.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd internal
git add internal/positions
git commit -m "feat(positions): add wish voting and a matches-only filter"
```

---

### Task 6: Каталог на 60 бажань

**Files:**
- Create: `content/wishes.v1.json`
- Create: `internal/wishlist/catalog_test.go`

**Interfaces:**
- Consumes: `catalog.Load`, `(*Catalog).Validate`.
- Produces: `content/wishes.v1.json`.

Це контентна задача. Її тест — валідація, і він має падати, доки контент неповний.

**Формат** — як `content/positions.v1.json`:

```json
{
  "kind": "wishes",
  "version": 1,
  "items": [
    {
      "id": "w001",
      "facets": {"kind": ["mood"], "intensity": ["gentle"]},
      "text": {
        "en": {"title": "Candlelight", "body": "A room lit only by candles."},
        "uk": {"title": "При свічках", "body": "Кімната, освітлена лише свічками."}
      }
    }
  ]
}
```

**Правила контенту:**
- рівно 60 елементів, ID `w001`…`w060`, нуль-доповнені
- фасет `kind` — одне з `place`, `role`, `pace`, `mood`, `toys`, `scenario`
- фасет `intensity` — одне з `gentle`, `medium`, `bold`
- розподіл: приблизно 20 `gentle`, 25 `medium`, 15 `bold` — щоб перші екрани були мʼякими
- усі шість значень `kind` представлені, жодне не менше ніж пʼятьма пунктами
- обидві мови, `title` до 40 символів, `body` — одне речення 60–160 символів
- тон як у каталозі поз: фактично й тепло, без вульгаризмів і без канцеляриту
- кожен пункт формулюється так, щоб на нього можна було відповісти «Хочу / Цікаво / Ні» без уточнень
- **жодних згадок третіх осіб, примусу чи чогось, що потребує згоди поза парою** — модуль про двох

- [ ] **Step 1: Write the failing test**

Create `internal/wishlist/catalog_test.go`:

```go
package wishlist_test

import (
	"os"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
)

func loadWishes(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/wishes.v1.json")
	if err != nil {
		t.Fatalf("open wishes catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load wishes catalog: %v", err)
	}
	return c
}

func TestWishesCatalogValidatesForBothLanguages(t *testing.T) {
	if err := loadWishes(t).Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestWishesCatalogHasExactlySixtyPaddedIDs(t *testing.T) {
	c := loadWishes(t)
	if len(c.Items) != 60 {
		t.Fatalf("catalog has %d items, want exactly 60", len(c.Items))
	}
	for i, item := range c.Items {
		if len(item.ID) != 4 || !strings.HasPrefix(item.ID, "w") {
			t.Fatalf("item %d id = %q, want the zero-padded form wNNN", i, item.ID)
		}
	}
	for i := 1; i < len(c.Items); i++ {
		if c.Items[i-1].ID >= c.Items[i].ID {
			t.Fatalf("ids are not strictly ascending at %d: %q then %q", i, c.Items[i-1].ID, c.Items[i].ID)
		}
	}
}

func TestWishesCatalogFacetsAreWithinTheAllowedVocabulary(t *testing.T) {
	kinds := map[string]int{"place": 0, "role": 0, "pace": 0, "mood": 0, "toys": 0, "scenario": 0}
	intensities := map[string]int{"gentle": 0, "medium": 0, "bold": 0}

	for _, item := range loadWishes(t).Items {
		ks := item.Facets["kind"]
		if len(ks) != 1 {
			t.Fatalf("item %s has %d kind values, want exactly one", item.ID, len(ks))
		}
		if _, ok := kinds[ks[0]]; !ok {
			t.Fatalf("item %s has unknown kind %q", item.ID, ks[0])
		}
		kinds[ks[0]]++

		is := item.Facets["intensity"]
		if len(is) != 1 {
			t.Fatalf("item %s has %d intensity values, want exactly one", item.ID, len(is))
		}
		if _, ok := intensities[is[0]]; !ok {
			t.Fatalf("item %s has unknown intensity %q", item.ID, is[0])
		}
		intensities[is[0]]++
	}

	for kind, n := range kinds {
		if n < 5 {
			t.Fatalf("kind %q has only %d items, want at least 5 so filtering by it is useful", kind, n)
		}
	}
	if intensities["gentle"] < 15 {
		t.Fatalf("only %d gentle items; the first screens must be soft", intensities["gentle"])
	}
}

func TestWishesCatalogBodiesAreOneSentenceEach(t *testing.T) {
	for _, item := range loadWishes(t).Items {
		for _, lang := range []string{"uk", "en"} {
			body := item.Text[lang].Body
			if n := len([]rune(body)); n < 40 || n > 200 {
				t.Fatalf("item %s %s body is %d runes, want 40-200", item.ID, lang, n)
			}
			if n := len([]rune(item.Text[lang].Title)); n > 40 {
				t.Fatalf("item %s %s title is %d runes, want at most 40", item.ID, lang, n)
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -run TestWishes -count=1`
Expected: FAIL — `open wishes catalog: no such file or directory`.

- [ ] **Step 3: Write the catalog**

Напиши всі 60 пунктів обома мовами за правилами вище. Пиши кожну мову окремо так, щоб вона природно звучала сама по собі, а не як переклад.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/wishlist/ -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add content/wishes.v1.json internal/wishlist/catalog_test.go
git commit -m "feat(wishlist): add the wishes catalog"
```

---

### Task 7: Підключення, конфіг і документація

**Files:**
- Modify: `internal/config/config.go` — `WishesCatalogPath`
- Modify: `cmd/wrnrs/main.go` — завантаження каталогу, реєстрація модуля
- Modify: `.env.example`, `docs/ARCHITECTURE.md`, `docs/PLAN.md`
- Create: `internal/app/wishlist_integration_test.go`

**Interfaces:**
- Consumes: `wishlist.NewHandler`, `modules.Registry.Register`, `catalog.Load`.
- Produces: працюючий модуль у боті.

Конфіг: `WISHES_CATALOG_PATH` із дефолтом `content/wishes.v1.json`.

**Відсутній або невалідний каталог не має валити бот** — попередження в лог і модуль просто не зʼявляється в меню. Прочитай, як це зроблено для каталогу поз у `cmd/wrnrs/main.go`, і повтори точно. Помилка `Register` — інша річ: це помилка програмування, і вона має валити старт.

Реєстрація:

```go
Gate: modules.Gate{Needs18Plus: true, NeedsMature: true},
```

`NeedsPair` і `NeedsPremium` лишаються нулем — модуль працює соло й не монетизується.

- [ ] **Step 1: Write the failing integration test**

`internal/app/wishlist_integration_test.go`, внутрішній пакет (`package app`), два сценарії через `a.handleCallback` з `wish:open`:
1. користувач із 18+, але без mature-опт-іну — отримує відмову, і текст пояснює саме брак згоди на mature, а не брак 18+;
2. користувач з обома прапорцями — отримує хаб, і в ньому є рядок про приватність.

Будуй хендлер із маленьким каталогом на 2–3 пункти, а не з реальним файлом. Читай `internal/app/positions_integration_test.go` — там уже є рівно ця форма, включно з тим, як реєструється модуль у тесті й як звати поля фейкового бота.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestWishlist -count=1`
Expected: FAIL.

- [ ] **Step 3: Wire it**

- [ ] **Step 4: Update the docs**

`docs/ARCHITECTURE.md` — рядок у Package Map:

```markdown
- `internal/wishlist`: private wish matching — per-user answers, mutual-match computation, and the swipe/matches screens.
```

`docs/PLAN.md` — зазначити, що етап 2 карти суперапу виконано.

`.env.example` — `WISHES_CATALOG_PATH=content/wishes.v1.json`.

- [ ] **Step 5: Run everything**

Run: `GOTOOLCHAIN=local go test ./... -count=1`
Expected: PASS.

Run: `GOTOOLCHAIN=local go build ./...`
Expected: чисто.

Run: `docker compose config >/dev/null && echo ok`
Expected: `ok`. **Це лише валідація конфігурації — не запускай і не перезапускай контейнери.**

- [ ] **Step 6: Commit**

```bash
gofmt -w cmd internal
git add internal/config/config.go cmd/wrnrs/main.go .env.example docs internal/app/wishlist_integration_test.go
git commit -m "feat(wishlist): wire the wishlist module into the bot"
```

---

## Self-Review

**Покриття спека.**

| Вимога спека | Задача |
|---|---|
| §4.1 `internal/storage/wishes.go` | Task 1 |
| §4.1 `internal/wishlist/service.go` | Task 2 |
| §4.1 клавіатури | Task 3 |
| §4.1 хендлер | Task 4 |
| §4.1 `content/wishes.v1.json` | Task 6 |
| §4.2 зміни в модулі поз | Task 5 |
| §4.2 конфіг, `main.go`, i18n | Task 3 (i18n), Task 7 (решта) |
| §4.3 без імпорту модуля в модуль | Task 5 — читає через `storage`, перевіряється рев'ю |
| §5 DDL і ключ на користувачі | Task 1 |
| §5 правило збігу, `curious+curious` не збіг | Task 1, таблиця з девʼяти комбінацій |
| §6 немає методу для чужих відповідей | Task 1 — «Produces» такого методу не містить; рев'ю має це перевірити |
| §6 прогрес партнера не показується | Task 1 (`PartnerHasAnyWishAnswer` булеве), Task 3 (хаб) |
| §7 екрани й колбеки | Tasks 3-4 |
| §7 «тільки збіги» не фасет | Task 5, явним попередженням |
| §8 контент, фасети, нуль-доповнення | Task 6 |
| §10 план тестування | розкладено по задачах 1-7 |
| §11 ризики | помʼякшення закладені: соло-режим (Task 7 гейт), ліниве голосування (Task 5), інтенсивність (Tasks 2, 6) |

**Узгодженість типів.** `storage.WishAnswer`, `WishItemKind`, `WishMatch` з Task 1 споживаються в Tasks 2, 4, 5. `wishlist.AnswerKey` з Task 2 використовується в тестах Task 2 і в хендлері Task 4. `Service` з Task 2 йде в `HandlerOptions` Task 4. Клавіатури Task 3 викликаються з Task 4. `BrowseState.MatchesOnly` зʼявляється лише в Task 5.

**Свідома межа.** Tasks 4 і 5 описані менш дослівно, ніж 1-3: там більше інтеграції з наявним кодом, який виконавець має прочитати. Замість вигаданих номерів рядків обидві задачі називають файл-взірець (`internal/positions/handler.go`) і фіксують інваріанти, які мають виконуватись, — груповий чат, повторний гейт, стійкий розбір, ліміт колбека.

**Ризик, який виконавцю треба тримати в голові.** Task 5 змінює живий модуль поз, у якому вже є фінальне рев'ю і мутаційно перевірені тести. Будь-яка зміна там не має ламати наявні тести — зокрема ті, що фіксують порядок черги рандомайзера й ідентифікацію запусків дампу.
