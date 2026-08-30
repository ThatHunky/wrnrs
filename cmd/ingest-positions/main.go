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

		unknown, err := processPage(number, page, tax, func() ([]byte, int, error) {
			imageBytes, imageStatus, fetchErr := fetch(client, page.ImageURL)
			time.Sleep(*delay)
			return imageBytes, imageStatus, fetchErr
		}, *imagesDir, items, state, *resumePath, *out)
		if err != nil {
			log.Printf("position %d: %v", number, err)
			continue
		}
		if len(unknown) > 0 {
			// Do NOT mark this page done: its item was neither built nor
			// persisted, so once the operator extends the taxonomy a later
			// run must re-fetch it rather than silently skip it forever.
			for _, slug := range unknown {
				unknownSlugs[slug] = true
			}
			log.Printf("position %d: skipped, unknown tag slugs: %s", number, strings.Join(unknown, ", "))
			continue
		}
		log.Printf("position %d: %s", number, page.Name)
	}

	// Write the catalog and review file BEFORE failing loudly on unknown
	// slugs. Unknown slugs are an expected outcome of a first crawl over
	// real data, not an edge case, so the run's completed work must be
	// safely on disk before the process can exit non-zero.
	if err := writeCatalog(*out, items); err != nil {
		log.Fatalf("write catalog: %v", err)
	}
	writeReview(*reviewPath, state.Review)
	log.Printf("wrote %d items to %s; %d pages need manual review", len(items), *out, len(state.Review))

	if len(unknownSlugs) > 0 {
		slugs := make([]string, 0, len(unknownSlugs))
		for slug := range unknownSlugs {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		log.Fatalf("unknown tag slugs encountered: %s\nadd them to %s and re-run", strings.Join(slugs, ", "), *taxonomyPath)
	}
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

// processPage runs the per-page pipeline once a page's HTML has already been
// fetched and parsed: it builds the catalog item and, only if every tag slug
// on the page is known, fetches the image (via fetchImage, so this function
// itself performs no network I/O) and commits the result. A page carrying
// unknown tag slugs is rejected before fetchImage is ever called: its slugs
// are returned so the caller can accumulate them into the run-wide set, and
// the page is left neither added to items nor marked done, so a later run
// re-fetches it once the taxonomy is extended.
func processPage(number int, page positions.ParsedPage, tax *positions.Taxonomy, fetchImage func() ([]byte, int, error), imagesDir string, items map[string]catalog.Item, state progress, resumePath, catalogPath string) ([]string, error) {
	item, unknown, err := buildItem(page, tax)
	if err != nil {
		return nil, fmt.Errorf("build item: %w", err)
	}
	if len(unknown) > 0 {
		return unknown, nil
	}

	imageBytes, imageStatus, err := fetchImage()
	if err != nil || imageStatus != http.StatusOK {
		return nil, fmt.Errorf("image fetch failed (status %d): %w", imageStatus, err)
	}

	return nil, commitPage(number, item, imageBytes, imagesDir, items, state, resumePath, catalogPath)
}

// commitPage writes item's image to disk, adds item to items, marks the page
// done, and persists both the progress file and the catalog immediately.
// Persisting the catalog on every successful page — not just once at the end
// of the whole crawl — is what makes the crawl safe to kill or crash at any
// point: state.Done and the catalog file are updated together, so a resumed
// run's "skip if already done" check can never silently outrun what the
// catalog actually contains. The crawl is I/O-bound at 1 request/second, so
// re-marshaling a few hundred KB of JSON on every page is immaterial next to
// the network wait.
func commitPage(number int, item catalog.Item, imageBytes []byte, imagesDir string, items map[string]catalog.Item, state progress, resumePath, catalogPath string) error {
	target := filepath.Join(imagesDir, filepath.Base(item.Media.Key))
	if err := os.WriteFile(target, imageBytes, 0o644); err != nil {
		return fmt.Errorf("write image: %w", err)
	}

	items[item.ID] = item
	state.Done[strconv.Itoa(number)] = true
	saveProgress(resumePath, state)

	if err := writeCatalog(catalogPath, items); err != nil {
		// Non-fatal per page: one write hiccup must not abort a
		// ~17-minute crawl, but it must never be silent either — this
		// file is the run's entire deliverable.
		log.Printf("position %d: write catalog: %v", number, err)
	}
	return nil
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

func writeCatalog(path string, items map[string]catalog.Item) error {
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
		return fmt.Errorf("marshal catalog: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write catalog: %w", err)
	}
	return nil
}

func writeReview(path string, entries []reviewEntry) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
