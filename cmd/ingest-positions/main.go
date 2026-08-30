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
