# Рандомізатор поз — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Модуль поз: каталог на 519 позицій із джерела, браузер однією карткою, рандомайзер, фасетні фільтри, спільна для пари відмітка «пробували» і дамп відфільтрованої вибірки окремими повідомленнями.

**Architecture:** `internal/positions` — доменний пакет (таксономія, парсер сторінок, сервіс стану перегляду) плюс Telegram-хендлер, що реєструється в `internal/modules`. Каталог зберігається як `content/positions.v1.json` з метаданими; зображення живуть у MinIO і **не потрапляють у git**. Разова тулза `cmd/ingest-positions` наповнює і те, й інше. Зображення відправляються за `file_id` після першого завантаження — це нарешті задіює давно наявний, але невикористаний Redis-хелпер `render:file:{hash}`.

**Tech Stack:** Go 1.24, `golang.org/x/net/html` (уже в графі модуля як indirect — промотується в direct), `modernc.org/sqlite`, Redis, MinIO, Telegram Bot API.

## Global Constraints

- Go 1.24. Не піднімати тулчейн без явного дозволу.
- Верифікація: `GOTOOLCHAIN=local go test ./...`.
- Форматування перед комітом: `gofmt -w cmd internal`.
- Тести пишуться **до** зміни поведінки.
- Модуль Go — `wrnrs`.
- **Зображення джерела не комітяться в git.** `content/positions.v1.json` містить лише метадані й ключі обʼєктів.
- **Вотермарки джерела не видаляються.** Зображення зберігаються байт-у-байт, без ресайзу й перекодування.
- Атрибуція з посиланням на джерело присутня в екрані модуля.
- Модуль **не монетизується**: у його `Gate` `NeedsPremium` завжди `false`.
- Інжест ходить до джерела не частіше **1 запиту в секунду**.
- Telegram: не більше **1 повідомлення в секунду** в межах одного чату.
- Залежність цього плану: `docs/superpowers/plans/2026-08-29-modules-framework.md` має бути виконаний повністю.
- **ID позиції — рядок, доповнений нулями до трьох знаків** (`003`, `067`, `519`). `catalog.Filtered` сортує лексикографічно за `ID`, тому неподовжені числа дали б порядок `1, 10, 100, 101, …, 11, 110`, і гортання каталогу пішло б навскіс. Те саме стосується ключів обʼєктів у MinIO, щоб імена файлів сортувались так само, як каталог.
- Спек: `docs/superpowers/specs/2026-08-29-couples-superapp-positions-design.md`, розділ 5.

---

## File Structure

| Файл | Відповідальність |
|---|---|
| `content/positions.taxonomy.json` | мапа `slug → (facet, value)` |
| `internal/positions/taxonomy.go` | завантаження таксономії, нормалізація слагів у фасети |
| `internal/positions/parse.go` | парсер однієї сторінки позиції |
| `internal/positions/testdata/*.html` | мінімальні фікстури для парсера |
| `cmd/ingest-positions/main.go` | разовий обхід джерела, вивантаження в JSON + MinIO |
| `internal/storage/positions.go` | таблиця й репозиторій `pair_position_marks` |
| `internal/state/redis.go` | `SetModuleState` / `ModuleState` — стан модуля окремо від FSM |
| `internal/telegram/photo.go` | відправка фото за `file_id` і повернення `file_id` |
| `internal/positions/service.go` | стан перегляду, фільтри, відмітки — без Telegram |
| `internal/positions/handler.go` | екрани модуля й роутинг колбеків |
| `internal/positions/keyboards.go` | клавіатури модуля |
| `internal/positions/dump.go` | тротльована масова відправка з перериванням |
| `content/positions.v1.json` | каталог (генерується Task 4, комітиться) |

---

### Task 1: Таксономія тегів

**Files:**
- Create: `content/positions.taxonomy.json`
- Create: `internal/positions/taxonomy.go`
- Test: `internal/positions/taxonomy_test.go`

**Interfaces:**
- Consumes: нічого.
- Produces: `positions.Taxonomy`, `positions.LoadTaxonomy(io.Reader) (*Taxonomy, error)`, `(*Taxonomy).Facets(slugs []string) (map[string][]string, []string)` — повертає фасети й **список невідомих слагів**.

Невідомий слаг не ігнорується мовчки: він повертається окремо, і Task 3 валить на ньому інжест. Мовчазне ігнорування призвело б до каталогу з дірками у фільтрах, які ніхто б не помітив.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/taxonomy_test.go`:

```go
package positions_test

import (
	"strings"
	"testing"

	"wrnrs/internal/positions"
)

const taxonomyFixture = `{
	"version": 1,
	"slugs": {
		"easy-level": {"facet": "level", "value": "easy"},
		"medium-level": {"facet": "level", "value": "medium"},
		"bed": {"facet": "location", "value": "bed"},
		"sofa": {"facet": "location", "value": "sofa"},
		"cowgirl": {"facet": "type", "value": "cowgirl"},
		"we-support-ukraine": {"facet": "", "value": ""}
	}
}`

func TestFacetsGroupsSlugsByFacet(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, unknown := tax.Facets([]string{"medium-level", "sofa", "bed", "cowgirl"})
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
	if got := strings.Join(facets["location"], ","); got != "bed,sofa" {
		t.Fatalf("location facet = %q, want bed,sofa (sorted)", got)
	}
	if got := strings.Join(facets["level"], ","); got != "medium" {
		t.Fatalf("level facet = %q, want medium", got)
	}
	if got := strings.Join(facets["type"], ","); got != "cowgirl" {
		t.Fatalf("type facet = %q, want cowgirl", got)
	}
}

func TestFacetsReportsUnknownSlugs(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	_, unknown := tax.Facets([]string{"bed", "brand-new-tag", "another-one"})
	if strings.Join(unknown, ",") != "another-one,brand-new-tag" {
		t.Fatalf("unknown = %v, want both new slugs sorted", unknown)
	}
}

func TestFacetsDropsSlugsMappedToAnEmptyFacet(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, unknown := tax.Facets([]string{"we-support-ukraine", "bed"})
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none — the slug is known and deliberately ignored", unknown)
	}
	if len(facets) != 1 || strings.Join(facets["location"], ",") != "bed" {
		t.Fatalf("facets = %v, want only location=bed", facets)
	}
}

func TestFacetsDeduplicatesRepeatedValues(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(taxonomyFixture))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	facets, _ := tax.Facets([]string{"bed", "bed", "sofa"})
	if got := strings.Join(facets["location"], ","); got != "bed,sofa" {
		t.Fatalf("location facet = %q, want bed,sofa without duplicates", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: FAIL — `no required module provides package wrnrs/internal/positions`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/positions/taxonomy.go`:

```go
package positions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// SlugMapping maps one source tag slug onto a catalog facet. An empty Facet
// means the slug is known and deliberately ignored (site chrome, campaigns).
type SlugMapping struct {
	Facet string `json:"facet"`
	Value string `json:"value"`
}

type Taxonomy struct {
	Version int                    `json:"version"`
	Slugs   map[string]SlugMapping `json:"slugs"`
}

func LoadTaxonomy(r io.Reader) (*Taxonomy, error) {
	var t Taxonomy
	if err := json.NewDecoder(r).Decode(&t); err != nil {
		return nil, fmt.Errorf("decode taxonomy: %w", err)
	}
	if t.Version <= 0 {
		return nil, errors.New("taxonomy version must be positive")
	}
	if len(t.Slugs) == 0 {
		return nil, errors.New("taxonomy has no slugs")
	}
	return &t, nil
}

// Facets converts source slugs into catalog facets. Unknown slugs are returned
// separately so the caller can fail loudly instead of silently losing filters.
func (t *Taxonomy) Facets(slugs []string) (map[string][]string, []string) {
	facets := map[string]map[string]bool{}
	unknownSet := map[string]bool{}

	for _, slug := range slugs {
		mapping, ok := t.Slugs[slug]
		if !ok {
			unknownSet[slug] = true
			continue
		}
		if mapping.Facet == "" {
			continue
		}
		if facets[mapping.Facet] == nil {
			facets[mapping.Facet] = map[string]bool{}
		}
		facets[mapping.Facet][mapping.Value] = true
	}

	out := make(map[string][]string, len(facets))
	for facet, values := range facets {
		list := make([]string, 0, len(values))
		for value := range values {
			list = append(list, value)
		}
		sort.Strings(list)
		out[facet] = list
	}

	unknown := make([]string, 0, len(unknownSet))
	for slug := range unknownSet {
		unknown = append(unknown, slug)
	}
	sort.Strings(unknown)

	return out, unknown
}
```

Create `content/positions.taxonomy.json`. Це стартова мапа за спостереженими на джерелі слагами (спек, розділ 5.2). Інжест у Task 3 впаде на будь-якому слагу, якого тут немає — тоді допиши його сюди й перезапусти:

```json
{
  "version": 1,
  "slugs": {
    "easy-level":                {"facet": "level", "value": "easy"},
    "medium-level":              {"facet": "level", "value": "medium"},
    "hard-level":                {"facet": "level", "value": "hard"},
    "crazy":                     {"facet": "level", "value": "crazy"},

    "missionary":                {"facet": "type", "value": "missionary"},
    "doggy-style":               {"facet": "type", "value": "doggy-style"},
    "cowgirl":                   {"facet": "type", "value": "cowgirl"},
    "69-sex-position":           {"facet": "type", "value": "69"},
    "man-on-top":                {"facet": "type", "value": "man-on-top"},
    "woman-on-top":              {"facet": "type", "value": "woman-on-top"},
    "from-behind":               {"facet": "type", "value": "from-behind"},
    "face-to-face":              {"facet": "type", "value": "face-to-face"},
    "criss-cross":               {"facet": "type", "value": "criss-cross"},
    "lying-down":                {"facet": "type", "value": "lying-down"},
    "sitting":                   {"facet": "type", "value": "sitting"},
    "standing":                  {"facet": "type", "value": "standing"},

    "clitoral-stimulation":      {"facet": "stimulation", "value": "clitoral"},
    "g-spot-stimulation":        {"facet": "stimulation", "value": "g-spot"},
    "a-spot-stimulation":        {"facet": "stimulation", "value": "a-spot"},
    "deep-spot-stimulation":     {"facet": "stimulation", "value": "deep-spot"},
    "hand-clitoris-stimulation": {"facet": "stimulation", "value": "hand-clitoris"},

    "deep-penetration":          {"facet": "penetration", "value": "deep"},
    "middle-penetration":        {"facet": "penetration", "value": "middle"},
    "shallow-penetration":       {"facet": "penetration", "value": "shallow"},
    "no-penetration":            {"facet": "penetration", "value": "none"},

    "bed":                       {"facet": "location", "value": "bed"},
    "sofa":                      {"facet": "location", "value": "sofa"},
    "chair":                     {"facet": "location", "value": "chair"},
    "table":                     {"facet": "location", "value": "table"},
    "armchair":                  {"facet": "location", "value": "armchair"},
    "car":                       {"facet": "location", "value": "car"},
    "shower":                    {"facet": "location", "value": "shower"},
    "fitness-ball":              {"facet": "location", "value": "fitness-ball"},

    "man-active":                {"facet": "activity", "value": "man-active"},
    "woman-active":              {"facet": "activity", "value": "woman-active"},

    "vaginal-sex":               {"facet": "act", "value": "vaginal"},
    "anal-sex":                  {"facet": "act", "value": "anal"},
    "anal-play":                 {"facet": "act", "value": "anal-play"},
    "blowjob":                   {"facet": "act", "value": "blowjob"},
    "cunnilingus":               {"facet": "act", "value": "cunnilingus"},
    "anilingus-rimming":         {"facet": "act", "value": "rimming"},

    "kissing":                   {"facet": "extra", "value": "kissing"},
    "breast-kissing":            {"facet": "extra", "value": "breast-kissing"},
    "breasts-touching":          {"facet": "extra", "value": "breasts-touching"}
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS — 4 тести.

Перевір валідність JSON:
Run: `python3 -c "import json; d=json.load(open('content/positions.taxonomy.json')); print(len(d['slugs']), 'slugs')"`
Expected: `44 slugs`

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/positions
git add internal/positions/taxonomy.go internal/positions/taxonomy_test.go content/positions.taxonomy.json
git commit -m "feat(positions): add tag taxonomy with loud failure on unknown slugs"
```

---

### Task 2: Парсер сторінки позиції

**Files:**
- Create: `internal/positions/parse.go`
- Test: `internal/positions/parse_test.go`
- Modify: `go.mod` — промотувати `golang.org/x/net` в direct

**Interfaces:**
- Consumes: нічого.
- Produces: `positions.ParsedPage{Number int, Name, Description, ImageURL string, TagSlugs []string}`, `positions.ParsePage(io.Reader) (ParsedPage, error)`, `positions.ErrNotAPositionPage`.

Ключова поведінка: `<h1>` виду `Sex position #519. Revelation` парситься; будь-який інший `<h1>` дає `ErrNotAPositionPage`. Це відсіює ~18 статейних сторінок на кшталт `#67 Missionary — 42 Variations`, у яких теги злиплись з усіх варіацій одразу і каталог від них зіпсувався б.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/parse_test.go`:

```go
package positions_test

import (
	"errors"
	"strings"
	"testing"

	"wrnrs/internal/positions"
)

const singlePositionPage = `<!DOCTYPE html><html><head>
<meta property="og:description" content="Sex position #519 - Revelation (on the couch). Kamasutra.">
<meta property="og:image" content="https://example.test/uploads/2019/06/18_55.png">
</head><body>
<h1 class="entry-title" itemprop="headline">Sex position #519. Revelation</h1>
<a href="https://example.test/tag/medium-level">medium</a>
<a href="https://example.test/tag/sofa">sofa</a>
<a href="https://example.test/tag/cowgirl">cowgirl</a>
<a href="https://example.test/positions/518.html">previous</a>
</body></html>`

const articlePage = `<!DOCTYPE html><html><head>
<meta property="og:description" content="42 awesome ways to switch it up.">
<meta property="og:image" content="https://example.test/uploads/2016/03/3_8.png">
</head><body>
<h1 class="entry-title" itemprop="headline">Missionary Sex Position - 42 Variations + Tips</h1>
<a href="https://example.test/tag/easy-level">easy</a>
<a href="https://example.test/tag/crazy">crazy</a>
</body></html>`

func TestParsePageExtractsEveryField(t *testing.T) {
	page, err := positions.ParsePage(strings.NewReader(singlePositionPage))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	if page.Number != 519 {
		t.Fatalf("Number = %d, want 519", page.Number)
	}
	if page.Name != "Revelation" {
		t.Fatalf("Name = %q, want Revelation", page.Name)
	}
	if !strings.HasPrefix(page.Description, "Sex position #519 - Revelation") {
		t.Fatalf("Description = %q, want the og:description text", page.Description)
	}
	if page.ImageURL != "https://example.test/uploads/2019/06/18_55.png" {
		t.Fatalf("ImageURL = %q, want the og:image url", page.ImageURL)
	}
	if got := strings.Join(page.TagSlugs, ","); got != "cowgirl,medium-level,sofa" {
		t.Fatalf("TagSlugs = %q, want cowgirl,medium-level,sofa (sorted, links only from /tag/)", got)
	}
}

func TestParsePageRejectsArticleStylePages(t *testing.T) {
	_, err := positions.ParsePage(strings.NewReader(articlePage))
	if !errors.Is(err, positions.ErrNotAPositionPage) {
		t.Fatalf("ParsePage on an article page returned %v, want ErrNotAPositionPage", err)
	}
}

func TestParsePageRejectsPageWithoutImage(t *testing.T) {
	noImage := strings.Replace(singlePositionPage,
		`<meta property="og:image" content="https://example.test/uploads/2019/06/18_55.png">`, "", 1)

	_, err := positions.ParsePage(strings.NewReader(noImage))
	if err == nil {
		t.Fatal("ParsePage without og:image succeeded, want an error")
	}
	if errors.Is(err, positions.ErrNotAPositionPage) {
		t.Fatal("missing image reported as ErrNotAPositionPage; it is a different failure")
	}
}

func TestParsePageDeduplicatesTagLinks(t *testing.T) {
	doubled := strings.Replace(singlePositionPage,
		`<a href="https://example.test/tag/sofa">sofa</a>`,
		`<a href="https://example.test/tag/sofa">sofa</a><a href="https://example.test/tag/sofa">sofa again</a>`, 1)

	page, err := positions.ParsePage(strings.NewReader(doubled))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if got := strings.Join(page.TagSlugs, ","); got != "cowgirl,medium-level,sofa" {
		t.Fatalf("TagSlugs = %q, want each slug once", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -run TestParsePage -v`
Expected: FAIL — `undefined: positions.ParsePage`.

- [ ] **Step 3: Write minimal implementation**

Промотуй залежність:

```bash
GOTOOLCHAIN=local go get golang.org/x/net/html
```

Create `internal/positions/parse.go`:

```go
package positions

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ErrNotAPositionPage marks a page that is an article about a family of
// positions rather than a single catalog entry. Those pages carry the merged
// tags of every variation they show and must never enter the catalog.
var ErrNotAPositionPage = errors.New("not a single-position page")

var (
	headingPattern = regexp.MustCompile(`^Sex position #(\d+)\.\s*(.+)$`)
	tagPathPattern = regexp.MustCompile(`/tag/([^/?#"]+)`)
)

type ParsedPage struct {
	Number      int
	Name        string
	Description string
	ImageURL    string
	TagSlugs    []string
}

func ParsePage(r io.Reader) (ParsedPage, error) {
	root, err := html.Parse(r)
	if err != nil {
		return ParsedPage{}, fmt.Errorf("parse html: %w", err)
	}

	var (
		heading  string
		metas    = map[string]string{}
		slugSet  = map[string]bool{}
		inH1     bool
		headText strings.Builder
	)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1":
				if heading == "" {
					inH1 = true
					headText.Reset()
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
					inH1 = false
					heading = strings.TrimSpace(headText.String())
					return
				}
			case "meta":
				var property, content string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "property":
						property = attr.Val
					case "content":
						content = attr.Val
					}
				}
				if property != "" {
					metas[property] = content
				}
			case "a":
				for _, attr := range n.Attr {
					if attr.Key != "href" {
						continue
					}
					if m := tagPathPattern.FindStringSubmatch(attr.Val); m != nil {
						slugSet[m[1]] = true
					}
				}
			}
		}
		if n.Type == html.TextNode && inH1 {
			headText.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	match := headingPattern.FindStringSubmatch(heading)
	if match == nil {
		return ParsedPage{}, fmt.Errorf("heading %q: %w", heading, ErrNotAPositionPage)
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return ParsedPage{}, fmt.Errorf("heading %q: %w", heading, ErrNotAPositionPage)
	}

	image := strings.TrimSpace(metas["og:image"])
	if image == "" {
		return ParsedPage{}, errors.New("page has no og:image")
	}

	slugs := make([]string, 0, len(slugSet))
	for slug := range slugSet {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	return ParsedPage{
		Number:      number,
		Name:        strings.TrimSpace(match[2]),
		Description: strings.TrimSpace(metas["og:description"]),
		ImageURL:    image,
		TagSlugs:    slugs,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS — 8 тестів (4 з Task 1 + 4 нові).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/positions
git add internal/positions/parse.go internal/positions/parse_test.go go.mod go.sum
git commit -m "feat(positions): parse source pages and reject article-style ones"
```

---

### Task 3: Тулза інжесту

**Files:**
- Create: `cmd/ingest-positions/main.go`
- Create: `cmd/ingest-positions/main_test.go`
- Modify: `.gitignore` — додати `positions-images/` і `ingest-progress.json`

**Interfaces:**
- Consumes: `positions.LoadTaxonomy`, `positions.ParsePage`, `catalog.Item`, `catalog.Catalog`, `objectstore` (MinIO-адаптер).
- Produces: виконуваний файл; функція `buildItem(page positions.ParsedPage, tax *positions.Taxonomy) (catalog.Item, []string, error)`, придатна до юніт-тесту.

Тулза не є частиною рантайму бота. Прапорці: `--base-url`, `--out`, `--images-dir`, `--taxonomy`, `--from`, `--to`, `--delay`, `--resume`, `--review`.

- [ ] **Step 1: Write the failing test**

Create `cmd/ingest-positions/main_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"wrnrs/internal/positions"
)

const testTaxonomy = `{
	"version": 1,
	"slugs": {
		"medium-level": {"facet": "level", "value": "medium"},
		"sofa": {"facet": "location", "value": "sofa"}
	}
}`

func TestBuildItemMapsPageOntoCatalogItem(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	page := positions.ParsedPage{
		Number:      519,
		Name:        "Revelation",
		Description: "Sex position #519 - Revelation (on the couch).",
		ImageURL:    "https://example.test/uploads/18_55.png",
		TagSlugs:    []string{"medium-level", "sofa"},
	}

	item, unknown, err := buildItem(page, tax)
	if err != nil {
		t.Fatalf("buildItem: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
	if item.ID != "519" {
		t.Fatalf("ID = %q, want 519", item.ID)
	}
	if item.Text["en"].Title != "Revelation" {
		t.Fatalf("en title = %q, want Revelation", item.Text["en"].Title)
	}
	if item.Text["en"].Body != page.Description {
		t.Fatalf("en body = %q, want the description", item.Text["en"].Body)
	}
	if item.Media == nil || item.Media.Key != "positions/519.png" {
		t.Fatalf("Media = %+v, want key positions/519.png", item.Media)
	}
	if strings.Join(item.Facets["location"], ",") != "sofa" {
		t.Fatalf("location facet = %v, want sofa", item.Facets["location"])
	}
}

func TestBuildItemReportsUnknownSlugsInsteadOfDroppingThem(t *testing.T) {
	tax, err := positions.LoadTaxonomy(strings.NewReader(testTaxonomy))
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}

	page := positions.ParsedPage{
		Number:   3,
		Name:     "Test",
		ImageURL: "https://example.test/a.png",
		TagSlugs: []string{"sofa", "totally-new-tag"},
	}

	_, unknown, err := buildItem(page, tax)
	if err != nil {
		t.Fatalf("buildItem: %v", err)
	}
	if strings.Join(unknown, ",") != "totally-new-tag" {
		t.Fatalf("unknown = %v, want [totally-new-tag]", unknown)
	}
}

func TestObjectKeyUsesTheSourceExtension(t *testing.T) {
	if got := objectKey(519, "https://example.test/uploads/18_55.png"); got != "positions/519.png" {
		t.Fatalf("objectKey = %q, want positions/519.png", got)
	}
	if got := objectKey(7, "https://example.test/uploads/a.jpg?v=2"); got != "positions/007.jpg" {
		t.Fatalf("objectKey with query = %q, want positions/007.jpg (zero-padded)", got)
	}
	if got := objectKey(9, "https://example.test/uploads/noext"); got != "positions/009.png" {
		t.Fatalf("objectKey without extension = %q, want the zero-padded png default", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./cmd/ingest-positions/ -v`
Expected: FAIL — `undefined: buildItem`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/ingest-positions/main.go`:

```go
// Command ingest-positions performs a one-off crawl of the position source and
// writes a catalog JSON plus the raw images. It is not part of the bot runtime.
//
// Images are stored byte for byte: no resize, no re-encode, no watermark
// removal. Attribution and watermark preservation are product requirements.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
)

const userAgent = "wrnrs-ingest/1.0 (+https://github.com/ThatHunky/wrnrs)"

type progress struct {
	Done   map[string]bool `json:"done"`
	Review []reviewEntry   `json:"review"`
}

type reviewEntry struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

func main() {
	baseURL := flag.String("base-url", "https://sexpositions.club", "source base url")
	out := flag.String("out", "content/positions.v1.json", "catalog output path")
	imagesDir := flag.String("images-dir", "positions-images", "directory for downloaded images")
	taxonomyPath := flag.String("taxonomy", "content/positions.taxonomy.json", "taxonomy path")
	from := flag.Int("from", 1, "first position number")
	to := flag.Int("to", 519, "last position number")
	delay := flag.Duration("delay", time.Second, "delay between requests to the source")
	resumePath := flag.String("resume", "ingest-progress.json", "progress file")
	reviewPath := flag.String("review", "review.json", "path for pages needing manual review")
	flag.Parse()

	if *delay < time.Second {
		log.Fatalf("--delay must be at least 1s; the source must not be hammered")
	}

	taxFile, err := os.Open(*taxonomyPath)
	if err != nil {
		log.Fatalf("open taxonomy: %v", err)
	}
	tax, err := positions.LoadTaxonomy(taxFile)
	_ = taxFile.Close()
	if err != nil {
		log.Fatalf("load taxonomy: %v", err)
	}

	if err := os.MkdirAll(*imagesDir, 0o755); err != nil {
		log.Fatalf("create images dir: %v", err)
	}

	state := loadProgress(*resumePath)
	existing := loadCatalog(*out)
	items := map[string]catalog.Item{}
	for _, item := range existing.Items {
		items[item.ID] = item
	}

	client := &http.Client{Timeout: 30 * time.Second}
	unknownSlugs := map[string]bool{}

	for number := *from; number <= *to; number++ {
		id := strconv.Itoa(number)
		if state.Done[id] {
			continue
		}
		pageURL := fmt.Sprintf("%s/positions/%d.html", strings.TrimRight(*baseURL, "/"), number)

		body, status, err := fetch(client, pageURL)
		time.Sleep(*delay)
		if err != nil {
			log.Printf("position %d: fetch failed: %v", number, err)
			continue
		}
		if status == http.StatusNotFound {
			state.Done[id] = true
			continue
		}
		if status != http.StatusOK {
			log.Printf("position %d: status %d", number, status)
			continue
		}

		page, err := positions.ParsePage(strings.NewReader(string(body)))
		if err != nil {
			state.Review = append(state.Review, reviewEntry{Number: number, URL: pageURL, Reason: err.Error()})
			state.Done[id] = true
			log.Printf("position %d: needs manual review: %v", number, err)
			saveProgress(*resumePath, state)
			continue
		}

		item, unknown, err := buildItem(page, tax)
		if err != nil {
			log.Printf("position %d: build item: %v", number, err)
			continue
		}
		for _, slug := range unknown {
			unknownSlugs[slug] = true
		}

		imageBytes, imageStatus, err := fetch(client, page.ImageURL)
		time.Sleep(*delay)
		if err != nil || imageStatus != http.StatusOK {
			log.Printf("position %d: image fetch failed (status %d): %v", number, imageStatus, err)
			continue
		}
		target := filepath.Join(*imagesDir, filepath.Base(item.Media.Key))
		if err := os.WriteFile(target, imageBytes, 0o644); err != nil {
			log.Printf("position %d: write image: %v", number, err)
			continue
		}

		items[item.ID] = item
		state.Done[id] = true
		saveProgress(*resumePath, state)
		log.Printf("position %d: %s", number, item.Text["en"].Title)
	}

	if len(unknownSlugs) > 0 {
		slugs := make([]string, 0, len(unknownSlugs))
		for slug := range unknownSlugs {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		log.Fatalf("unknown tag slugs encountered: %s\nadd them to %s and re-run", strings.Join(slugs, ", "), *taxonomyPath)
	}

	writeCatalog(*out, items)
	writeReview(*reviewPath, state.Review)
	log.Printf("wrote %d items to %s; %d pages need manual review", len(items), *out, len(state.Review))
}

func buildItem(page positions.ParsedPage, tax *positions.Taxonomy) (catalog.Item, []string, error) {
	facets, unknown := tax.Facets(page.TagSlugs)
	item := catalog.Item{
		ID:     fmt.Sprintf("%03d", page.Number),
		Facets: facets,
		Text: map[string]catalog.ItemText{
			"en": {Title: page.Name, Body: page.Description},
		},
		Media: &catalog.MediaRef{Key: objectKey(page.Number, page.ImageURL)},
	}
	return item, unknown, nil
}

func objectKey(number int, imageURL string) string {
	ext := path.Ext(path.Base(strings.SplitN(imageURL, "?", 2)[0]))
	if ext == "" {
		ext = ".png"
	}
	return fmt.Sprintf("positions/%03d%s", number, ext)
}

func fetch(client *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, resp.StatusCode, err
}

func loadProgress(path string) progress {
	state := progress{Done: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if state.Done == nil {
		state.Done = map[string]bool{}
	}
	return state
}

func saveProgress(path string, state progress) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func loadCatalog(path string) catalog.Catalog {
	c := catalog.Catalog{Kind: "positions", Version: 1}
	file, err := os.Open(path)
	if err != nil {
		return c
	}
	defer file.Close()
	loaded, err := catalog.Load(file)
	if err != nil {
		return c
	}
	return *loaded
}

func writeCatalog(path string, items map[string]catalog.Item) {
	list := make([]catalog.Item, 0, len(items))
	for _, item := range items {
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool {
		a, _ := strconv.Atoi(list[i].ID)
		b, _ := strconv.Atoi(list[j].ID)
		return a < b
	})
	c := catalog.Catalog{Kind: "positions", Version: 1, Items: list}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Fatalf("marshal catalog: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write catalog: %v", err)
	}
}

func writeReview(path string, entries []reviewEntry) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
```

Додай у `.gitignore`:

```
positions-images/
ingest-progress.json
review.json
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./cmd/ingest-positions/ -v`
Expected: PASS — 3 тести.

Перевір, що бінарник збирається:
Run: `GOTOOLCHAIN=local go build ./cmd/ingest-positions/`
Expected: без виводу.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/ingest-positions
git add cmd/ingest-positions .gitignore
git commit -m "feat(ingest): add one-off position catalog ingest tool"
```

---

### Task 4: Наповнення каталогу, `starter_100` і український переклад

**Files:**
- Create: `content/positions.v1.json` (генерується, комітиться)
- Create: `scripts/positions_starter100.py`
- Test: `internal/positions/catalog_test.go`

**Interfaces:**
- Consumes: тулзу з Task 3, `catalog.Load`, `catalog.Catalog.Validate`.
- Produces: `content/positions.v1.json`, який проходить `Validate([]string{"uk","en"})`.

Це контентна задача. Її «тест» — валідація каталогу, і вона має провалюватись, доки переклад не повний. Це і є той механізм, що не дає забути половину контенту.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/catalog_test.go`:

```go
package positions_test

import (
	"os"
	"strconv"
	"testing"

	"wrnrs/internal/catalog"
)

func loadPositionsCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/positions.v1.json")
	if err != nil {
		t.Fatalf("open positions catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })

	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load positions catalog: %v", err)
	}
	return c
}

func TestPositionsCatalogValidatesForBothLanguages(t *testing.T) {
	c := loadPositionsCatalog(t)
	if err := c.Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPositionsCatalogHasBodyTextInBothLanguages(t *testing.T) {
	c := loadPositionsCatalog(t)
	for _, item := range c.Items {
		for _, lang := range []string{"uk", "en"} {
			if item.Text[lang].Body == "" {
				t.Fatalf("item %s has no %s body", item.ID, lang)
			}
		}
	}
}

func TestEveryItemHasMediaAndNumericID(t *testing.T) {
	c := loadPositionsCatalog(t)
	if len(c.Items) < 400 {
		t.Fatalf("catalog has %d items, want the full crawl (expected around 500)", len(c.Items))
	}
	for _, item := range c.Items {
		if _, err := strconv.Atoi(item.ID); err != nil {
			t.Fatalf("item id %q is not numeric", item.ID)
		}
		if item.Media == nil || item.Media.Key == "" {
			t.Fatalf("item %s has no media key", item.ID)
		}
	}
}

func TestStarter100TagCoversExactlyOneHundredItems(t *testing.T) {
	c := loadPositionsCatalog(t)
	tagged := c.Filtered(catalog.Filter{Tags: []string{"starter_100"}})
	if len(tagged) != 100 {
		t.Fatalf("starter_100 covers %d items, want exactly 100", len(tagged))
	}
}

func TestStarter100CoversEveryLevelAndSeveralLocations(t *testing.T) {
	c := loadPositionsCatalog(t)
	tagged := c.Filtered(catalog.Filter{Tags: []string{"starter_100"}})

	levels := map[string]int{}
	locations := map[string]bool{}
	for _, item := range tagged {
		for _, level := range item.Facets["level"] {
			levels[level]++
		}
		for _, location := range item.Facets["location"] {
			locations[location] = true
		}
	}
	for _, want := range []string{"easy", "medium"} {
		if levels[want] == 0 {
			t.Fatalf("starter_100 has no %s items; the curation skewed", want)
		}
	}
	if len(locations) < 3 {
		t.Fatalf("starter_100 covers %d locations, want at least 3", len(locations))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -run TestPositions -v`
Expected: FAIL — `open positions catalog: no such file or directory` (каталог ще не згенеровано).

- [ ] **Step 3: Наповнити каталог**

Запусти інжест. Це близько 17 хвилин; він резюмиться, тому обрив не страшний:

```bash
GOTOOLCHAIN=local go run ./cmd/ingest-positions --from 1 --to 519 --delay 1s
```

Якщо тулза впаде з `unknown tag slugs encountered: ...` — додай перелічені слаги в `content/positions.taxonomy.json` (з правильним фасетом, або з порожнім `facet`, якщо це не контентний тег) і перезапусти. Прогрес збережеться.

Розбери `review.json` вручну: це ~18 статейних сторінок. Для кожної або додай запис у каталог руками з коректною назвою й тегами, або пропусти — у каталозі 500 інших.

Створи `scripts/positions_starter100.py` для курації сотні:

```python
#!/usr/bin/env python3
"""Tag exactly 100 catalog items as starter_100.

Seeds are the site's own "most popular" list plus the positions linked from the
homepage article. The rest is filled by facet coverage so the starter set is not
skewed toward one level or location.
"""
import json
import sys
from collections import defaultdict

SEEDS = []  # заповни номерами з секції "Most popular" і зі статті на головній

CATALOG = "content/positions.v1.json"


def main() -> int:
    with open(CATALOG, encoding="utf-8") as fh:
        data = json.load(fh)
    items = data["items"]
    by_id = {item["id"]: item for item in items}

    chosen = [str(n) for n in SEEDS if str(n) in by_id]
    if len(chosen) > 100:
        print(f"seeds alone give {len(chosen)} items, trim SEEDS", file=sys.stderr)
        return 1

    # Fill by round-robin over (level, location) buckets so coverage stays even.
    buckets = defaultdict(list)
    for item in items:
        if item["id"] in chosen:
            continue
        levels = item.get("facets", {}).get("level") or ["unknown"]
        locations = item.get("facets", {}).get("location") or ["unknown"]
        buckets[(levels[0], locations[0])].append(item["id"])

    keys = sorted(buckets)
    for key in keys:
        buckets[key].sort(key=int)

    cursor = 0
    while len(chosen) < 100:
        progressed = False
        for key in keys:
            if len(chosen) >= 100:
                break
            if cursor < len(buckets[key]):
                chosen.append(buckets[key][cursor])
                progressed = True
        if not progressed:
            print(f"ran out of items at {len(chosen)}", file=sys.stderr)
            return 1
        cursor += 1

    starter = set(chosen[:100])
    for item in items:
        tags = [t for t in item.get("tags", []) if t != "starter_100"]
        if item["id"] in starter:
            tags.append("starter_100")
        if tags:
            item["tags"] = sorted(tags)
        else:
            item.pop("tags", None)

    with open(CATALOG, "w", encoding="utf-8") as fh:
        json.dump(data, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    print(f"tagged {len(starter)} items as starter_100")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

Заповни `SEEDS` номерами позицій із секції «Most popular» джерела і зі статті на головній, дедуплікуй, тоді запусти:

```bash
python3 scripts/positions_starter100.py
```

- [ ] **Step 4: Перекласти українською**

Кожен елемент має отримати `text.uk.title` і `text.uk.body`. Переклад робиться пакетами по ~50 позицій; після кожного пакета прогоняй валідацію, щоб бачити, скільки лишилось:

```bash
GOTOOLCHAIN=local go test ./internal/positions/ -run TestPositionsCatalog -v
```

Помилка виду `item 273 missing uk title` — це і є список того, що ще не зроблено.

Правила перекладу: назви позицій — короткі, без калькування («Revelation» → «Одкровення», а не «Ревелейшн»); описи — нейтральні й фактичні, без вульгаризмів і без медичного канцеляриту. Порядок слів український, а не калька з англійської. Готовий переклад вичитує власник продукту перед мерджем — це вимога спека, розділ 5.4.

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS — усі тести, включно з `TestStarter100TagCoversExactlyOneHundredItems`.

- [ ] **Step 6: Commit**

```bash
git add content/positions.v1.json content/positions.taxonomy.json scripts/positions_starter100.py internal/positions/catalog_test.go
git commit -m "feat(positions): add the full localized position catalog with starter_100"
```

Перевір, що зображення **не** потрапили в коміт:

```bash
git show --stat HEAD | grep -c "positions-images/" || echo "clean: no images committed"
```

---

### Task 5: Спільні для пари відмітки

**Files:**
- Create: `internal/storage/positions.go`
- Modify: `internal/storage/sqlite.go` — додати `CREATE TABLE` у `schemaSQL` (рядок ~1812)
- Modify: `migrations/001_init.sql` — те саме DDL
- Test: `internal/storage/positions_test.go`

**Interfaces:**
- Consumes: `storage.Repository`.
- Produces: `storage.PositionMarkKind` з константами `MarkTried`, `MarkFavorited`, `MarkHidden`; `storage.PositionMark{PositionID string, TriedAt, FavoritedAt, HiddenAt sql.NullTime, MarkedBy sql.NullInt64}`; `(*Repository).TogglePositionMark(ctx, pairID int64, positionID string, kind PositionMarkKind, markedBy int64, now time.Time) (bool, error)`; `(*Repository).PairPositionMarks(ctx, pairID int64) (map[string]PositionMark, error)`.

`TogglePositionMark` повертає новий стан прапорця. Відмітка спільна: обидва партнери бачать одну (спек, Р8).

- [ ] **Step 1: Write the failing test**

Create `internal/storage/positions_test.go`:

```go
package storage_test

import (
	"context"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

func TestTogglePositionMarkFlipsAndPersists(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	on, err := repo.TogglePositionMark(ctx, pairID, "519", storage.MarkTried, 1001, now)
	if err != nil {
		t.Fatalf("TogglePositionMark: %v", err)
	}
	if !on {
		t.Fatal("first toggle returned false, want true")
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	if !marks["519"].TriedAt.Valid {
		t.Fatal("tried mark was not persisted")
	}
	if marks["519"].MarkedBy.Int64 != 1001 {
		t.Fatalf("MarkedBy = %d, want 1001", marks["519"].MarkedBy.Int64)
	}

	off, err := repo.TogglePositionMark(ctx, pairID, "519", storage.MarkTried, 1002, now)
	if err != nil {
		t.Fatalf("second TogglePositionMark: %v", err)
	}
	if off {
		t.Fatal("second toggle returned true, want false")
	}

	marks, err = repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks after unset: %v", err)
	}
	if marks["519"].TriedAt.Valid {
		t.Fatal("tried mark survived the second toggle")
	}
}

func TestMarkKindsAreIndependent(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.TogglePositionMark(ctx, pairID, "007", storage.MarkTried, 1001, now); err != nil {
		t.Fatalf("toggle tried: %v", err)
	}
	if _, err := repo.TogglePositionMark(ctx, pairID, "007", storage.MarkFavorited, 1001, now); err != nil {
		t.Fatalf("toggle favorited: %v", err)
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	mark := marks["007"]
	if !mark.TriedAt.Valid || !mark.FavoritedAt.Valid {
		t.Fatalf("mark = %+v, want both tried and favorited set", mark)
	}
	if mark.HiddenAt.Valid {
		t.Fatal("hidden was set without being toggled")
	}
}

func TestMarksAreSharedAcrossThePairNotPerUser(t *testing.T) {
	repo, pairID := newRepoWithPair(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.TogglePositionMark(ctx, pairID, "012", storage.MarkTried, 1001, now); err != nil {
		t.Fatalf("toggle by first partner: %v", err)
	}

	marks, err := repo.PairPositionMarks(ctx, pairID)
	if err != nil {
		t.Fatalf("PairPositionMarks: %v", err)
	}
	if !marks["012"].TriedAt.Valid {
		t.Fatal("the second partner does not see the mark set by the first")
	}
}
```

`newRepoWithPair` створюй за зразком наявних гелперів у `internal/storage/sqlite_test.go`: відкрий `:memory:` базу, створи двох користувачів через `UpsertUser`, створи пару через наявний шлях (`CreatePairRequest` + `AcceptPairRequest`) і поверни `*storage.Repository` та `pairID`. Не вигадуй нових методів репозиторію — подивись, які вже є.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/storage/ -run TestTogglePositionMark -v`
Expected: FAIL — `undefined: storage.MarkTried`.

- [ ] **Step 3: Write minimal implementation**

Додай у `schemaSQL` в `internal/storage/sqlite.go` і в `migrations/001_init.sql` однаковий DDL:

```sql
CREATE TABLE IF NOT EXISTS pair_position_marks (
    pair_id      INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
    position_id  TEXT    NOT NULL,
    tried_at     TIMESTAMP,
    favorited_at TIMESTAMP,
    hidden_at    TIMESTAMP,
    marked_by    INTEGER REFERENCES users(telegram_id) ON DELETE SET NULL,
    updated_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (pair_id, position_id)
);
```

Create `internal/storage/positions.go`:

```go
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PositionMarkKind is one of the independent flags a pair can set on a position.
type PositionMarkKind string

const (
	MarkTried     PositionMarkKind = "tried_at"
	MarkFavorited PositionMarkKind = "favorited_at"
	MarkHidden    PositionMarkKind = "hidden_at"
)

// PositionMark is the shared state of one position for one pair. Marks belong
// to the pair, not to a partner: both see the same flags.
type PositionMark struct {
	PositionID  string
	TriedAt     sql.NullTime
	FavoritedAt sql.NullTime
	HiddenAt    sql.NullTime
	MarkedBy    sql.NullInt64
}

func (k PositionMarkKind) valid() bool {
	switch k {
	case MarkTried, MarkFavorited, MarkHidden:
		return true
	default:
		return false
	}
}

// TogglePositionMark flips one flag and returns its new state.
func (r *Repository) TogglePositionMark(ctx context.Context, pairID int64, positionID string, kind PositionMarkKind, markedBy int64, now time.Time) (bool, error) {
	if !kind.valid() {
		return false, fmt.Errorf("unknown position mark kind %q", kind)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin toggle position mark: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current sql.NullTime
	query := fmt.Sprintf(`SELECT %s FROM pair_position_marks WHERE pair_id = ? AND position_id = ?`, string(kind))
	err = tx.QueryRowContext(ctx, query, pairID, positionID).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("read position mark: %w", err)
	}

	setOn := !current.Valid
	var value any
	if setOn {
		value = now
	}

	upsert := fmt.Sprintf(`
		INSERT INTO pair_position_marks (pair_id, position_id, %s, marked_by, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pair_id, position_id) DO UPDATE SET
			%s = excluded.%s,
			marked_by = excluded.marked_by,
			updated_at = excluded.updated_at
	`, string(kind), string(kind), string(kind))
	if _, err := tx.ExecContext(ctx, upsert, pairID, positionID, value, markedBy, now); err != nil {
		return false, fmt.Errorf("write position mark: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit position mark: %w", err)
	}
	return setOn, nil
}

func (r *Repository) PairPositionMarks(ctx context.Context, pairID int64) (map[string]PositionMark, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT position_id, tried_at, favorited_at, hidden_at, marked_by
		FROM pair_position_marks
		WHERE pair_id = ?
	`, pairID)
	if err != nil {
		return nil, fmt.Errorf("load position marks: %w", err)
	}
	defer rows.Close()

	marks := map[string]PositionMark{}
	for rows.Next() {
		var mark PositionMark
		if err := rows.Scan(&mark.PositionID, &mark.TriedAt, &mark.FavoritedAt, &mark.HiddenAt, &mark.MarkedBy); err != nil {
			return nil, fmt.Errorf("scan position mark: %w", err)
		}
		marks[mark.PositionID] = mark
	}
	return marks, rows.Err()
}
```

`kind` підставляється в SQL як імʼя колонки, тому `valid()` — це не косметика, а захист від інʼєкції. Ніколи не приймай сюди рядок ззовні без цієї перевірки.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/storage/ -v`
Expected: PASS — 3 нові тести плюс усі наявні.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/storage
git add internal/storage/positions.go internal/storage/positions_test.go internal/storage/sqlite.go migrations/001_init.sql
git commit -m "feat(storage): add pair-shared position marks"
```

---

### Task 6: Відправка фото за file_id

**Files:**
- Create: `internal/telegram/photo.go`
- Modify: `internal/state/redis.go` — додати `SetModuleState` / `ModuleState`
- Test: `internal/telegram/photo_test.go`

**Interfaces:**
- Consumes: `(*telegram.Client).do`.
- Produces: `telegram.SentPhoto{MessageID int64, FileID string}`; `(*Client).SendPhotoBytes(ctx, chatID int64, data []byte, caption string, replyMarkup any) (SentPhoto, error)`; `(*Client).SendPhotoRef(ctx, chatID int64, fileID, caption string, replyMarkup any) (SentPhoto, error)`; `(*Client).EditMessageMediaRef(ctx, chatID, messageID int64, fileID, caption string, replyMarkup any) error`; `(*state.RedisStore).SetModuleState(ctx, userID int64, module, value string, ttl time.Duration) error`; `(*state.RedisStore).ModuleState(ctx, userID int64, module string) (string, error)`.

Наявний `SendPhoto` не чіпаємо — на ньому тримається відправка карток гри. Нові методи додаються поруч. `SendPhotoBytes` повертає `file_id`, який кешується і далі використовується `SendPhotoRef` — це усуває повторне завантаження тих самих 519 картинок і робить дамп на 100 повідомлень реалістичним.

- [ ] **Step 1: Write the failing test**

Create `internal/telegram/photo_test.go`:

```go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPhotoBytesReturnsTheLargestFileID(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"photo":[
			{"file_id":"small","width":90,"height":69},
			{"file_id":"large","width":500,"height":384}
		]}}`))
	}))
	defer server.Close()

	client := NewClient("token", server.URL)
	sent, err := client.SendPhotoBytes(context.Background(), 42, []byte("png-bytes"), "підпис", nil)
	if err != nil {
		t.Fatalf("SendPhotoBytes: %v", err)
	}
	if sent.MessageID != 77 {
		t.Fatalf("MessageID = %d, want 77", sent.MessageID)
	}
	if sent.FileID != "large" {
		t.Fatalf("FileID = %q, want the largest size file id", sent.FileID)
	}
	if !strings.HasSuffix(gotMethod, "/sendPhoto") {
		t.Fatalf("called %q, want sendPhoto", gotMethod)
	}
}

func TestSendPhotoRefSendsFileIDAsAPlainField(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5,"photo":[{"file_id":"reused","width":500,"height":384}]}}`))
	}))
	defer server.Close()

	client := NewClient("token", server.URL)
	sent, err := client.SendPhotoRef(context.Background(), 42, "reused", "підпис", nil)
	if err != nil {
		t.Fatalf("SendPhotoRef: %v", err)
	}
	if sent.FileID != "reused" {
		t.Fatalf("FileID = %q, want reused", sent.FileID)
	}
	if payload["photo"] != "reused" {
		t.Fatalf("payload photo = %v, want the raw file id string", payload["photo"])
	}
	if payload["caption"] != "підпис" {
		t.Fatalf("payload caption = %v, want the caption", payload["caption"])
	}
}

func TestSendPhotoBytesFailsLoudlyWhenTelegramReturnsNoPhoto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"photo":[]}}`))
	}))
	defer server.Close()

	client := NewClient("token", server.URL)
	_, err := client.SendPhotoBytes(context.Background(), 42, []byte("png"), "", nil)
	if err == nil {
		t.Fatal("SendPhotoBytes with an empty photo array succeeded, want an error")
	}
}
```

**Перед запуском** звір конструктор: тест викликає `NewClient("token", server.URL)`. Подивись фактичну сигнатуру в `internal/telegram/client.go` і підстав правильну — можливо, базовий URL передається інакше.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/telegram/ -run TestSendPhoto -v`
Expected: FAIL — `client.SendPhotoBytes undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/telegram/photo.go`:

```go
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strconv"
)

// SentPhoto carries back what the caller needs to reuse the upload: the file id
// Telegram assigned, so the same image is never uploaded twice.
type SentPhoto struct {
	MessageID int64
	FileID    string
}

type sendPhotoResponse struct {
	OK     bool    `json:"ok"`
	Result Message `json:"result"`
}

func largestPhotoFileID(sizes []PhotoSize) (string, bool) {
	best := ""
	bestArea := -1
	for _, size := range sizes {
		area := size.Width * size.Height
		if area > bestArea {
			bestArea = area
			best = size.FileID
		}
	}
	return best, best != ""
}

// SendPhotoBytes uploads image bytes and returns the resulting file id.
func (c *Client) SendPhotoBytes(ctx context.Context, chatID int64, data []byte, caption string, replyMarkup any) (SentPhoto, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if caption != "" {
		_ = writer.WriteField("caption", caption)
	}
	if replyMarkup != nil {
		markup, _ := json.Marshal(replyMarkup)
		_ = writer.WriteField("reply_markup", string(markup))
	}
	part, err := writer.CreateFormFile("photo", "photo.png")
	if err != nil {
		return SentPhoto{}, fmt.Errorf("create photo form: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return SentPhoto{}, fmt.Errorf("write photo form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return SentPhoto{}, fmt.Errorf("close photo form: %w", err)
	}

	var response sendPhotoResponse
	if err := c.do(ctx, "sendPhoto", writer.FormDataContentType(), &body, &response); err != nil {
		return SentPhoto{}, err
	}
	fileID, ok := largestPhotoFileID(response.Result.Photo)
	if !ok {
		return SentPhoto{}, errors.New("sendPhoto response carried no photo sizes")
	}
	return SentPhoto{MessageID: response.Result.MessageID, FileID: fileID}, nil
}

// SendPhotoRef sends an already-uploaded photo by its file id.
func (c *Client) SendPhotoRef(ctx context.Context, chatID int64, fileID, caption string, replyMarkup any) (SentPhoto, error) {
	payload := map[string]any{
		"chat_id": chatID,
		"photo":   fileID,
	}
	if caption != "" {
		payload["caption"] = caption
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	var response sendPhotoResponse
	if err := c.postJSON(ctx, "sendPhoto", payload, &response); err != nil {
		return SentPhoto{}, err
	}
	fileID, ok := largestPhotoFileID(response.Result.Photo)
	if !ok {
		return SentPhoto{}, errors.New("sendPhoto response carried no photo sizes")
	}
	return SentPhoto{MessageID: response.Result.MessageID, FileID: fileID}, nil
}

// EditMessageMediaRef swaps the photo of an existing message by file id.
func (c *Client) EditMessageMediaRef(ctx context.Context, chatID, messageID int64, fileID, caption string, replyMarkup any) error {
	media := map[string]any{"type": "photo", "media": fileID}
	if caption != "" {
		media["caption"] = caption
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"media":      media,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "editMessageMedia", payload, nil)
}
```

Додай у `internal/state/redis.go`:

```go
func (s *RedisStore) SetModuleState(ctx context.Context, userID int64, module, value string, ttl time.Duration) error {
	return s.client.Set(ctx, moduleStateKey(userID, module), value, ttl).Err()
}

func (s *RedisStore) ModuleState(ctx context.Context, userID int64, module string) (string, error) {
	value, err := s.client.Get(ctx, moduleStateKey(userID, module)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func moduleStateKey(userID int64, module string) string {
	return fmt.Sprintf("mod:%s:user:%d", module, userID)
}
```

Це окремий від FSM ключ — використати `SetFSM` для стану перегляду означало б затерти незавершений онбординг користувача. Звір імена полів (`s.client`) і вже імпортовані пакети з наявного коду файлу.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/telegram/ ./internal/state/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/telegram internal/state
git add internal/telegram/photo.go internal/telegram/photo_test.go internal/state/redis.go
git commit -m "feat(telegram): send photos by file id and add per-module redis state"
```

---

### Task 7: Сервіс модуля — стан перегляду й відмітки

**Files:**
- Create: `internal/positions/service.go`
- Test: `internal/positions/service_test.go`

**Interfaces:**
- Consumes: `catalog.Catalog`, `catalog.Filter`, `catalog.SelectNext`, `storage.PositionMark`, `storage.PositionMarkKind`.
- Produces: `positions.BrowseState{Filter catalog.Filter, Index int, Cycle int}`; `positions.EncodeState(BrowseState) (string, error)`; `positions.DecodeState(string) (BrowseState, error)`; `positions.Service` з `NewService(ServiceOptions)`; методи `Visible(ctx, pairID int64, state BrowseState) ([]catalog.Item, error)`, `At(items []catalog.Item, index int) (catalog.Item, int, bool)`, `Random(seedID int64, items []catalog.Item, marks map[string]storage.PositionMark, cycle int) (catalog.Item, int, error)`.

`Visible` застосовує фільтр і прибирає приховані парою позиції. `Random` вважає «побаченими» вже спробовані, тому крутилка спершу пропонує нове — це і є та цінність, заради якої відмітка спільна.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/service_test.go`:

```go
package positions_test

import (
	"database/sql"
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

	got, _, err := svc.Random(4242, items, tried, 0)
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

	got, cycle, err := svc.Random(4242, items, all, 0)
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

func itemIDs(items []catalog.Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -run "TestEncodeDecode|TestVisible|TestAt|TestRandom" -v`
Expected: FAIL — `undefined: positions.NewService`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/positions/service.go`:

```go
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
	Index  int            `json:"i"`
	Cycle  int            `json:"c"`
}

func EncodeState(state BrowseState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode browse state: %w", err)
	}
	return string(data), nil
}

func DecodeState(raw string) (BrowseState, error) {
	var state BrowseState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return BrowseState{}, fmt.Errorf("decode browse state: %w", err)
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

// VisibleWithMarks applies the filter and removes positions the pair hid.
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

// At resolves an index into the selection, wrapping in both directions so the
// browser never dead-ends at either edge.
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

// Random draws an untried position first and only reshuffles the whole set once
// the pair has tried everything in the current selection.
func (s *Service) Random(seedID int64, items []catalog.Item, marks map[string]storage.PositionMark, cycle int) (catalog.Item, int, error) {
	seen := make(map[string]bool, len(marks))
	for id, mark := range marks {
		if mark.TriedAt.Valid {
			seen[id] = true
		}
	}
	item, nextCycle, _, err := catalog.SelectNext(catalog.SelectionInput{
		SeedID: seedID,
		Bucket: "positions",
		Cycle:  cycle,
		Items:  items,
		Seen:   seen,
	})
	if err != nil {
		return catalog.Item{}, cycle, err
	}
	return item, nextCycle, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS — усі тести пакета.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/positions
git add internal/positions/service.go internal/positions/service_test.go
git commit -m "feat(positions): add browse state, filtering and untried-first randomisation"
```

---

### Task 8: Тротльований дамп із перериванням

**Files:**
- Create: `internal/positions/dump.go`
- Test: `internal/positions/dump_test.go`

**Interfaces:**
- Consumes: `catalog.Item`.
- Produces: `positions.Sender` (інтерфейс `SendItem(ctx context.Context, item catalog.Item) error`); `positions.DumpOptions{Items []catalog.Item, Interval time.Duration, Sleep func(context.Context, time.Duration) error, Stopped func() bool}`; `positions.Dump(ctx, sender Sender, options DumpOptions) (sent int, err error)`.

`Sleep` інʼєктується, щоб тест не чекав по-справжньому. `Stopped` перевіряється **перед кожною** відправкою — так кнопка «стоп» спрацьовує в межах однієї секунди, а не після всієї сотні.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/dump_test.go`:

```go
package positions_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
)

type recordingSender struct {
	sent []string
	fail error
}

func (s *recordingSender) SendItem(_ context.Context, item catalog.Item) error {
	if s.fail != nil {
		return s.fail
	}
	s.sent = append(s.sent, item.ID)
	return nil
}

func dumpItems(n int) []catalog.Item {
	items := make([]catalog.Item, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, catalog.Item{ID: string(rune('0' + i))})
	}
	return items
}

func TestDumpSendsEveryItemAndThrottlesBetweenThem(t *testing.T) {
	sender := &recordingSender{}
	var slept []time.Duration

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(3),
		Interval: time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if sent != 3 || len(sender.sent) != 3 {
		t.Fatalf("sent %d items (%v), want 3", sent, sender.sent)
	}
	if len(slept) != 2 {
		t.Fatalf("slept %d times, want 2 — between items, not after the last", len(slept))
	}
	for _, d := range slept {
		if d != time.Second {
			t.Fatalf("slept %v, want the configured 1s interval", d)
		}
	}
}

func TestDumpStopsBeforeTheNextSendWhenStoppedFlips(t *testing.T) {
	sender := &recordingSender{}
	stopAfter := 2
	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(10),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
		Stopped:  func() bool { return len(sender.sent) >= stopAfter },
	})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if sent != 2 {
		t.Fatalf("sent = %d, want 2 — the stop flag must be checked before each send", sent)
	}
}

func TestDumpStopsOnCancelledContext(t *testing.T) {
	sender := &recordingSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sent, err := positions.Dump(ctx, sender, positions.DumpOptions{
		Items:    dumpItems(5),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dump on a cancelled context returned %v, want context.Canceled", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d on a cancelled context, want 0", sent)
	}
}

func TestDumpReturnsTheSendErrorAndTheCountSoFar(t *testing.T) {
	sender := &recordingSender{fail: errors.New("telegram exploded")}

	sent, err := positions.Dump(context.Background(), sender, positions.DumpOptions{
		Items:    dumpItems(3),
		Interval: time.Second,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
	if err == nil {
		t.Fatal("Dump with a failing sender succeeded, want the error surfaced")
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0 — the first send already failed", sent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -run TestDump -v`
Expected: FAIL — `undefined: positions.Dump`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/positions/dump.go`:

```go
package positions

import (
	"context"
	"time"

	"wrnrs/internal/catalog"
)

// Sender delivers one item to the chat. Kept narrow so Dump stays testable
// without a Telegram client.
type Sender interface {
	SendItem(ctx context.Context, item catalog.Item) error
}

type DumpOptions struct {
	Items    []catalog.Item
	Interval time.Duration
	// Sleep is injected so tests do not wait in real time.
	Sleep func(ctx context.Context, d time.Duration) error
	// Stopped is polled before every send so the stop button takes effect
	// within one interval rather than after the whole batch.
	Stopped func() bool
}

func Dump(ctx context.Context, sender Sender, options DumpOptions) (int, error) {
	sleep := options.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}

	sent := 0
	for i, item := range options.Items {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		if options.Stopped != nil && options.Stopped() {
			return sent, nil
		}
		if err := sender.SendItem(ctx, item); err != nil {
			return sent, err
		}
		sent++
		if i < len(options.Items)-1 {
			if err := sleep(ctx, options.Interval); err != nil {
				return sent, err
			}
		}
	}
	return sent, nil
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS — 4 нові тести плюс усі попередні.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/positions
git add internal/positions/dump.go internal/positions/dump_test.go
git commit -m "feat(positions): add throttled catalog dump with interruption"
```

---

### Task 9: Telegram-хендлер модуля

**Files:**
- Create: `internal/positions/keyboards.go`
- Create: `internal/positions/handler.go`
- Test: `internal/positions/handler_test.go`
- Modify: `content/i18n/uk.json`, `content/i18n/en.json`

**Interfaces:**
- Consumes: `positions.Service`, `positions.Dump`, `storage.Repository`, `telegram.Client` (через вузькі інтерфейси), `modules.Handler`.
- Produces: `positions.Handler` — реалізація `modules.Handler`; `positions.NewHandler(HandlerOptions) *Handler`.

Колбеки модуля (префікс `pos:`): `pos:open` — хаб; `pos:browse:{index}` — картка за індексом; `pos:random` — крутилка; `pos:mark:{kind}:{id}` — перемикання відмітки; `pos:filters` — екран фільтрів; `pos:filter:{facet}:{value}` — перемикання фасета; `pos:dump:confirm` і `pos:dump:go`, `pos:dump:stop`.

Ключове для перегляду: одне повідомлення з фото, яке редагується через `EditMessageMediaRef`. Якщо Telegram відмовляє в редагуванні застарілого повідомлення — надсилається одне нове, як це вже робить `internal/app` для карток гри.

- [ ] **Step 1: Write the failing test**

Create `internal/positions/handler_test.go` з тестами:

```go
package positions_test

import (
	"context"
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/positions"
	"wrnrs/internal/telegram"
)

type fakePhotoBot struct {
	sentRefs  []string
	editedIDs []string
	captions  []string
	markups   []telegram.InlineKeyboardMarkup
}

func (b *fakePhotoBot) SendPhotoRef(_ context.Context, _ int64, fileID, caption string, markup any) (telegram.SentPhoto, error) {
	b.sentRefs = append(b.sentRefs, fileID)
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return telegram.SentPhoto{MessageID: int64(len(b.sentRefs)), FileID: fileID}, nil
}

func (b *fakePhotoBot) SendPhotoBytes(_ context.Context, _ int64, _ []byte, caption string, markup any) (telegram.SentPhoto, error) {
	b.sentRefs = append(b.sentRefs, "uploaded")
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return telegram.SentPhoto{MessageID: int64(len(b.sentRefs)), FileID: "new-file-id"}, nil
}

func (b *fakePhotoBot) EditMessageMediaRef(_ context.Context, _, _ int64, fileID, caption string, markup any) error {
	b.editedIDs = append(b.editedIDs, fileID)
	b.captions = append(b.captions, caption)
	if m, ok := markup.(telegram.InlineKeyboardMarkup); ok {
		b.markups = append(b.markups, m)
	}
	return nil
}

func TestBrowseCaptionCarriesTitleFacetsAndPosition(t *testing.T) {
	item := catalog.Item{
		ID:     "519",
		Facets: map[string][]string{"level": {"medium"}, "location": {"sofa"}},
		Text:   map[string]catalog.ItemText{"uk": {Title: "Одкровення", Body: "Опис пози."}},
	}

	caption := positions.BrowseCaption("uk", item, 4, 100, true, false)

	if !strings.Contains(caption, "Одкровення") {
		t.Fatalf("caption %q does not contain the title", caption)
	}
	if !strings.Contains(caption, "Опис пози.") {
		t.Fatalf("caption %q does not contain the body", caption)
	}
	if !strings.Contains(caption, "5/100") {
		t.Fatalf("caption %q does not contain the 1-based position 5/100", caption)
	}
	if !strings.Contains(caption, "✅") {
		t.Fatalf("caption %q does not mark the position as tried", caption)
	}
}

func TestBrowseKeyboardExposesEveryControl(t *testing.T) {
	markup := positions.BrowseKeyboard("uk", "519", 4, false, false, false)

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{"pos:browse:3", "pos:browse:5", "pos:random", "pos:mark:tried:519", "pos:mark:favorited:519", "pos:filters"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
}

func TestBrowseKeyboardDisablesSharedMarksWithoutAPair(t *testing.T) {
	withoutPair := positions.BrowseKeyboard("uk", "519", 0, false, false, true)

	var texts []string
	for _, row := range withoutPair.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "pos:mark:") {
				texts = append(texts, button.Text)
			}
		}
	}
	if len(texts) == 0 {
		t.Fatal("solo users lost the mark buttons entirely; they must stay visible with a hint")
	}
	for _, text := range texts {
		if !strings.Contains(text, "🔒") {
			t.Fatalf("solo mark button %q has no lock marker", text)
		}
	}
}
```

Це не повне покриття хендлера — це покриття тих його частин, які є чистими функціями. Логіку роутингу колбеків покриє інтеграційний тест у Task 10, де вже є справжній `App` і база.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -run "TestBrowse" -v`
Expected: FAIL — `undefined: positions.BrowseCaption`.

- [ ] **Step 3: Write minimal implementation**

Створи `internal/positions/keyboards.go` з `BrowseCaption`, `BrowseKeyboard`, `HubKeyboard`, `FiltersKeyboard`, `DumpConfirmKeyboard`. Сигнатури, які фіксують тести:

```go
func BrowseCaption(language string, item catalog.Item, index, total int, tried, favorited bool) string
func BrowseKeyboard(language, itemID string, index int, tried, favorited, soloMode bool) telegram.InlineKeyboardMarkup
```

`BrowseCaption` складає: назву жирним, рядок фасетів через `·`, опис, і хвіст виду `5/100` (індекс подається 0-базовим, у тексті — 1-базовий). Прапорці `tried`/`favorited` додають `✅` і `⭐`.

`BrowseKeyboard` будує рядки:
1. `◀ pos:browse:{index-1}` · `🎲 pos:random` · `▶ pos:browse:{index+1}`
2. `✅ pos:mark:tried:{id}` · `⭐ pos:mark:favorited:{id}` — при `soloMode` до тексту обох додається ` 🔒`
3. `☰ pos:filters` · `⌂ menu:main`

Створи `internal/positions/handler.go` з `Handler`, що реалізує `modules.Handler`. `HandleCallback` розбирає префікс `pos:` і викликає відповідний екран. `HandleMessage` повертає `false, nil` — модуль не читає вільний текст.

Кеш `file_id`: перед відправкою читай Redis-хелпер `FileID(ctx, "positions:"+item.ID)`; якщо порожньо — завантаж байти з `objectStore.Get(ctx, item.Media.Key)`, відправ через `SendPhotoBytes`, збережи отриманий `file_id` через `CacheFileID`. Далі завжди `SendPhotoRef`/`EditMessageMediaRef`.

Додай рядки в `content/i18n/uk.json`:

```json
    "module.positions": "Пози",
    "positions.hub": "Пози\n\nКрути навмання, гортай каталог або став фільтри.",
    "positions.attribution": "Ілюстрації: Sex Positions Club — https://sexpositions.club",
    "positions.empty": "За такими фільтрами нічого немає. Спробуй прибрати щось.",
    "positions.needs_pair_for_marks": "Відмітки спільні для пари — створи пару, щоб ними користуватись.",
    "positions.dump_confirm": "Надіслати всі %d поз окремими повідомленнями? Це займе близько %d хв.",
    "positions.dump_started": "Надсилаю. Можеш зупинити кнопкою нижче.",
    "positions.dump_stopped": "Зупинено. Надіслано %d.",
    "positions.dump_done": "Готово. Надіслано %d.",
    "positions.filters": "Фільтри\n\nЗнайдено: %d",
```

і дзеркально в `content/i18n/en.json`.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=local go test ./internal/positions/ -v`
Expected: PASS.

Run: `python3 -c "import json; [json.load(open(p)) for p in ['content/i18n/uk.json','content/i18n/en.json']]; print('ok')"`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/positions
git add internal/positions/handler.go internal/positions/keyboards.go internal/positions/handler_test.go content/i18n/uk.json content/i18n/en.json
git commit -m "feat(positions): add module screens, keyboards and file-id reuse"
```

---

### Task 10: Підключення модуля, конфіг і документація

**Files:**
- Modify: `internal/config/config.go` — `PositionsBucket`, `PositionsPrefix`, `PositionsCatalogPath`
- Modify: `cmd/wrnrs/main.go` — завантаження каталогу, реєстрація модуля
- Modify: `.env.example`
- Test: `internal/app/positions_integration_test.go`
- Modify: `docs/ARCHITECTURE.md`, `docs/PLAN.md`, `README.md`

**Interfaces:**
- Consumes: `positions.NewHandler`, `modules.Registry.Register`, `catalog.Load`.
- Produces: працюючий модуль у боті.

- [ ] **Step 1: Write the failing test**

Create `internal/app/positions_integration_test.go`:

```go
package app

import (
	"context"
	"strings"
	"testing"

	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

func TestPositionsModuleIsBlockedWithoutMatureOptIn(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(5001)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}

	registerPositionsForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "pos:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	if !botSaidSomethingContaining(bot, "18+") {
		t.Fatal("a user without mature opt-in was not told why the module is closed")
	}
}

func TestPositionsModuleOpensForAMatureUser(t *testing.T) {
	a, bot, _ := newTestApp(t)
	ctx := context.Background()

	const userID = int64(5002)
	if err := a.repo.UpsertUser(ctx, storage.User{TelegramID: userID, DisplayName: "Тест", Language: "uk"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := a.repo.UpdateAdultConfirmation(ctx, userID, true); err != nil {
		t.Fatalf("UpdateAdultConfirmation: %v", err)
	}
	if err := a.repo.UpdateMatureOptIn(ctx, userID, true); err != nil {
		t.Fatalf("UpdateMatureOptIn: %v", err)
	}

	registerPositionsForTest(t, a)

	cb := &telegram.CallbackQuery{ID: "1", Data: "pos:open", From: telegram.User{ID: userID}}
	if err := a.handleCallback(ctx, cb); err != nil {
		t.Fatalf("handleCallback: %v", err)
	}

	if !botSaidSomethingContaining(bot, "sexpositions.club") {
		t.Fatal("the module hub did not show the source attribution")
	}
}

func botSaidSomethingContaining(bot *fakeBot, needle string) bool {
	for _, text := range collectBotTexts(bot) {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
```

`registerPositionsForTest` і `collectBotTexts` напиши на місці: перший будує `positions.NewHandler` з маленьким каталогом на 2-3 елементи й реєструє його в `a.Registry()`; другий збирає всі тексти, що бачив `fakeBot` — звір фактичні імена полів у `internal/app/app_test.go`, не вигадуй їх.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/app/ -run TestPositionsModule -v`
Expected: FAIL — `undefined: registerPositionsForTest`, далі — відсутність атрибуції.

- [ ] **Step 3: Write minimal implementation**

У `internal/config/config.go` додай у `Config`:

```go
	PositionsCatalogPath string
	PositionsBucket      string
	PositionsPrefix      string
```

і в `Load`:

```go
		PositionsCatalogPath: withDefault(getenv("POSITIONS_CATALOG_PATH"), "content/positions.v1.json"),
		PositionsBucket:      withDefault(getenv("POSITIONS_BUCKET"), "wrnrs-assets"),
		PositionsPrefix:      withDefault(getenv("POSITIONS_PREFIX"), "positions/"),
```

У `cmd/wrnrs/main.go` після побудови `App` завантаж каталог і зареєструй модуль:

```go
	positionsCatalog, err := loadPositionsCatalog(cfg.PositionsCatalogPath)
	if err != nil {
		logger.Warn("positions catalog unavailable; module disabled", "err", err)
	} else if err := positionsCatalog.Validate([]string{"uk", "en"}); err != nil {
		logger.Warn("positions catalog invalid; module disabled", "err", err)
	} else {
		handler := positions.NewHandler(positions.HandlerOptions{
			Catalog:     positionsCatalog,
			Repo:        repo,
			Bot:         botClient,
			State:       redisStore,
			ObjectStore: objectStore,
			I18N:        bundle,
			Logger:      logger,
		})
		if err := application.Registry().Register(modules.Module{
			ID:             "positions",
			TitleKey:       "module.positions",
			Icon:           "🎲",
			CallbackPrefix: "pos:",
			Gate:           modules.Gate{Needs18Plus: true, NeedsMature: true},
			Handler:        handler,
		}); err != nil {
			return fmt.Errorf("register positions module: %w", err)
		}
	}
```

Відсутній або невалідний каталог **не валить бот** — модуль просто не зʼявляється в меню. Це дає змогу викотити код до того, як завершено переклад.

Додай `loadPositionsCatalog` за зразком наявних `loadDeck`/`loadStyleCatalog` у тому ж файлі.

У `.env.example` додай:

```
POSITIONS_CATALOG_PATH=content/positions.v1.json
POSITIONS_BUCKET=wrnrs-assets
POSITIONS_PREFIX=positions/
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOTOOLCHAIN=local go test ./...`
Expected: PASS в усіх пакетах.

Run: `GOTOOLCHAIN=local go build ./...`
Expected: без виводу.

Run: `docker compose config >/dev/null && echo "compose ok"`
Expected: `compose ok`

- [ ] **Step 5: Оновити документацію**

У `docs/ARCHITECTURE.md`, «Package Map»:

```markdown
- `internal/positions`: position catalog module — source page parsing, tag taxonomy, browse state, pair-shared marks, throttled dump.
```

У `docs/ARCHITECTURE.md`, «Remaining Follow-Ups» — прибрати пункт про невикористаний `render:file:{hash}`: модуль поз тепер його використовує. Замість нього додати:

```markdown
- Position images are third-party content used without a licence that covers this use. See the risk section of `docs/superpowers/specs/2026-08-29-couples-superapp-positions-design.md`. The asset layer is source-swappable by config.
```

У `docs/PLAN.md` додати в «Unfinished Planned Features» рядок про решту модулів суперапу за картою зі спека.

У `README.md` додати згадку модуля поз і команду інжесту:

````markdown
Наповнити каталог поз (разова операція, ~17 хв):

```bash
GOTOOLCHAIN=local go run ./cmd/ingest-positions --from 1 --to 519 --delay 1s
```
````

- [ ] **Step 6: Commit**

```bash
gofmt -w cmd internal
git add internal/config/config.go cmd/wrnrs/main.go .env.example internal/app/positions_integration_test.go docs/ARCHITECTURE.md docs/PLAN.md README.md
git commit -m "feat(positions): wire the positions module into the bot"
```

---

## Self-Review

**Покриття спека (розділ 5).**

| Вимога спека | Задача |
|---|---|
| 5.1 знімання полів зі сторінки | Task 2 |
| 5.2 нормалізація тегів у фасети | Task 1 |
| 5.3 інжест, тротлінг, `--resume`, детектор статейних сторінок | Task 3 |
| 5.3 зображення не в git | Task 3 (`.gitignore`), перевірка в Task 4 |
| 5.3 зображення байт-у-байт, вотермарки цілі | Task 3 (без ресайзу/перекодування) |
| 5.4 повний переклад 519 | Task 4 |
| 5.5 `starter_100` рівно 100 з дедуплікацією | Task 4 |
| 5.6 `pair_position_marks`, спільні відмітки, каскад | Task 5 |
| 5.6 фільтри в Redis, не в FSM | Task 6 (`SetModuleState`), Task 7 (`BrowseState`) |
| 5.7 хаб, браузер одним повідомленням, фільтри | Task 9 |
| 5.7 дамп із підтвердженням, тротлінгом і «стоп» | Task 8 (движок), Task 9 (екрани) |
| 5.7 атрибуція джерела | Task 9 (`positions.attribution`), перевірено в Task 10 |
| 5.8 гейт: свої 18+ і mature | Task 10 (`modules.Gate`) |
| 5.8 соло-режим, відмітки заблоковані з підказкою | Task 9 (`soloMode` в `BrowseKeyboard`) |
| 5.8 модуль не монетизується | Task 10 (`NeedsPremium` не встановлюється) |
| Р10 змінне джерело асетів | Task 10 (`POSITIONS_BUCKET`, `POSITIONS_PREFIX`) |

**Узгодженість типів.** `positions.ParsedPage` з Task 2 споживається `buildItem` у Task 3. `catalog.Item`/`catalog.MediaRef` (план каркасу, Task 1) використовуються в Tasks 3, 7, 8, 9 без змін. `storage.PositionMark` з Task 5 споживається `VisibleWithMarks` і `Random` у Task 7 і хендлером у Task 9. `telegram.SentPhoto` з Task 6 повертається обома методами відправки й використовується в Task 9. `positions.Sender` з Task 8 реалізується хендлером у Task 9. `modules.Gate`/`modules.Module` з плану каркасу споживаються в Task 10.

**Три місця, де виконавцю доведеться звірятися з кодом, а не з планом.** По кожному в кроці явно сказано звірити, а не вгадувати: конструктор `telegram.NewClient` (Task 6), поля `fakeBot` у `internal/app/app_test.go` (Tasks 9-10), і гелпер створення пари в `internal/storage/sqlite_test.go` (Task 5). Це навмисно: точні імена в цих місцях залежать від коду, якого план не бачить цілком.

**Свідома межа покриття.** Task 9 покриває юніт-тестами лише чисті функції хендлера — підпис і клавіатури. Роутинг колбеків перевіряється інтеграційно в Task 10 на двох сценаріях: заблокований гейтом і відкритий. Повний матричний тест усіх дев'яти колбеків не пишеться — він дублював би тести сервісу з Task 7 і дампу з Task 8, які вже покривають логіку під ними.
