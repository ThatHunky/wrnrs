// Command ingest-positions performs a one-off crawl of the position source and
// writes a catalog JSON plus the raw images. It is not part of the bot runtime.
//
// Images are stored byte for byte: no resize, no re-encode, no watermark
// removal. Attribution and watermark preservation are product requirements.
package main

import (
	"bytes"
	"context"
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
	"wrnrs/internal/config"
	"wrnrs/internal/objectstore"
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
	seedOnly := flag.Bool("seed-only", false, "upload the already-downloaded images in --images-dir to the object store (POSITIONS_BUCKET/POSITIONS_PREFIX) and exit; makes no request to the source site and does not crawl")
	flag.Parse()

	if *seedOnly {
		if err := runSeed(*imagesDir, *out); err != nil {
			log.Fatalf("seed: %v", err)
		}
		return
	}

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

	if err := finish(*out, *reviewPath, *taxonomyPath, items, state.Review, unknownSlugs); err != nil {
		log.Fatalf("%v", err)
	}
}

// finish runs the post-loop finishing sequence: it writes the catalog and
// the review file, and only once both writes have happened does it report
// unknown tag slugs as an error. This ordering matters: unknown slugs are an
// expected outcome of a first crawl over real data, not an edge case, so the
// run's completed work must be safely on disk before the process can exit
// non-zero. main calls this and does the log.Fatal itself, so a caller (or a
// test) can observe the writes and the error independently of process exit.
func finish(catalogPath, reviewPath, taxonomyPath string, items map[string]catalog.Item, review []reviewEntry, unknown map[string]bool) error {
	if err := writeCatalog(catalogPath, items); err != nil {
		return fmt.Errorf("write catalog: %w", err)
	}
	writeReview(reviewPath, review)
	log.Printf("wrote %d items to %s; %d pages need manual review", len(items), catalogPath, len(review))

	if len(unknown) > 0 {
		slugs := make([]string, 0, len(unknown))
		for slug := range unknown {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		return fmt.Errorf("unknown tag slugs encountered: %s\nadd them to %s and re-run", strings.Join(slugs, ", "), taxonomyPath)
	}
	return nil
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

// commitPage writes item's image to disk, then persists the catalog with
// item included, and ONLY once that write has succeeded marks the page done
// and persists progress. This order is load-bearing: state.Done is the
// resume loop's sole "skip this page" signal, so if it were set before the
// catalog write landed, a kill between the two writes would leave the page
// marked done while its item never reached the catalog — silently losing it
// forever. Persisting the catalog on every successful page — not just once
// at the end of the whole crawl — is what makes the crawl safe to kill or
// crash at any point: a crash between the two writes now costs at most one
// harmless re-fetch on resume, never a lost item. The crawl is I/O-bound at
// 1 request/second, so re-marshaling a few hundred KB of JSON on every page
// is immaterial next to the network wait.
func commitPage(number int, item catalog.Item, imageBytes []byte, imagesDir string, items map[string]catalog.Item, state progress, resumePath, catalogPath string) error {
	target := filepath.Join(imagesDir, filepath.Base(item.Media.Key))
	if err := os.WriteFile(target, imageBytes, 0o644); err != nil {
		return fmt.Errorf("write image: %w", err)
	}

	items[item.ID] = item
	if err := writeCatalog(catalogPath, items); err != nil {
		// Roll back the in-memory addition so items keeps mirroring what
		// actually made it to disk, and do NOT mark the page done: a
		// later run must re-fetch it rather than silently skip it.
		delete(items, item.ID)
		return fmt.Errorf("write catalog: %w", err)
	}

	state.Done[strconv.Itoa(number)] = true
	saveProgress(resumePath, state)
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

// loadProgress reads the resume file, distinguishing "file does not exist"
// (the normal state on a first run — stay silent) from "file exists but
// failed to parse" (corruption — log a clear warning and fall back to empty
// progress rather than fail the run). Silently resetting to empty progress
// without a warning would be dangerous here: the catalog on disk may still
// contain everything the progress file forgot, but the operator needs to
// know the two have diverged.
func loadProgress(path string) progress {
	state := progress{Done: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("load progress: read %s: %v", path, err)
		}
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("load progress: %s exists but failed to parse, starting from empty progress: %v", path, err)
		return progress{Done: map[string]bool{}}
	}
	if state.Done == nil {
		state.Done = map[string]bool{}
	}
	return state
}

func saveProgress(path string, state progress) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("save progress: marshal: %v", err)
		return
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		log.Printf("save progress: %v", err)
	}
}

// loadCatalog reads the catalog file, distinguishing "file does not exist"
// (the normal state on a first run — stay silent) from "file exists but
// failed to parse" (corruption — log a clear warning and fall back to an
// empty catalog rather than fail the run). Without the warning, a corrupted
// catalog would silently reset the whole crawl to empty while the progress
// file still marks pages done, orphaning every item already collected with
// no sign anything went wrong.
func loadCatalog(path string) catalog.Catalog {
	c := catalog.Catalog{Kind: "positions", Version: 1}
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("load catalog: open %s: %v", path, err)
		}
		return c
	}
	defer file.Close()
	loaded, err := catalog.Load(file)
	if err != nil {
		log.Printf("load catalog: %s exists but failed to parse, starting from empty catalog: %v", path, err)
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
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write catalog: %w", err)
	}
	return nil
}

func writeReview(path string, entries []reviewEntry) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Printf("write review: marshal: %v", err)
		return
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		log.Printf("write review: %v", err)
	}
}

// objectPutGetter is the narrow slice of *objectstore.MinIOStore that
// seedCatalog needs. Declaring it here (rather than depending on the
// concrete type) lets tests exercise seedCatalog against an in-memory fake
// instead of a real MinIO server, matching the pattern the rest of this
// codebase uses for its other narrow interfaces.
type objectPutGetter interface {
	Get(ctx context.Context, objectKey string) ([]byte, error)
	Put(ctx context.Context, objectKey, contentType string, data []byte) error
}

// runSeed is the entry point for --seed-only. It never fetches anything
// from the source website: it only reads the catalog already committed at
// catalogPath and the image files the earlier crawl already downloaded
// under imagesDir, and uploads them to the object store named by
// POSITIONS_BUCKET/POSITIONS_PREFIX (via config.LoadAssetConfig, so those
// settings are the single source of truth rather than being duplicated or
// hardcoded here).
func runSeed(imagesDir, catalogPath string) error {
	assets := config.LoadAssetConfig(os.Getenv)
	if assets.MinIO.AccessKey == "" && assets.MinIO.SecretKey == "" {
		return fmt.Errorf("MINIO_ACCESS_KEY/MINIO_SECRET_KEY must be set to seed the object store")
	}

	store, err := objectstore.NewMinIOStore(objectstore.MinIOConfig{
		Endpoint:  assets.MinIO.Endpoint,
		AccessKey: assets.MinIO.AccessKey,
		SecretKey: assets.MinIO.SecretKey,
		Bucket:    assets.PositionsBucket,
		UseSSL:    assets.MinIO.UseSSL,
	})
	if err != nil {
		return fmt.Errorf("create minio client: %w", err)
	}

	ctx := context.Background()
	if err := store.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("ensure bucket %s: %w", assets.PositionsBucket, err)
	}

	cat := loadCatalog(catalogPath)
	if len(cat.Items) == 0 {
		return fmt.Errorf("catalog %s has no items; nothing to seed", catalogPath)
	}

	uploaded, skipped, err := seedCatalog(ctx, store, imagesDir, assets.PositionsPrefix, cat)
	if err != nil {
		return err
	}
	log.Printf("seed: uploaded %d objects, skipped %d already up to date (bucket=%s prefix=%s, %d catalog items)", uploaded, skipped, assets.PositionsBucket, assets.PositionsPrefix, len(cat.Items))
	return nil
}

// seedCatalog uploads every catalog item's already-downloaded local image
// to store, byte for byte: no decode, resize, re-encode, or watermark
// alteration, since attribution/watermark preservation is a product
// requirement. It is idempotent — an object whose remote bytes already
// match the local file is counted as skipped rather than re-uploaded, so a
// second run over an already-seeded bucket uploads nothing. It performs no
// network I/O beyond talking to store; it never contacts the source
// website.
func seedCatalog(ctx context.Context, store objectPutGetter, imagesDir, prefix string, cat catalog.Catalog) (uploaded, skipped int, err error) {
	for _, item := range cat.Items {
		if item.Media == nil || item.Media.Key == "" {
			continue
		}
		base := filepath.Base(item.Media.Key)
		localPath := filepath.Join(imagesDir, base)
		data, readErr := os.ReadFile(localPath)
		if readErr != nil {
			return uploaded, skipped, fmt.Errorf("read local image for item %s at %s: %w", item.ID, localPath, readErr)
		}

		objectKey := prefix + base
		if existing, getErr := store.Get(ctx, objectKey); getErr == nil && bytes.Equal(existing, data) {
			skipped++
			continue
		}
		if putErr := store.Put(ctx, objectKey, seedContentType(base), data); putErr != nil {
			return uploaded, skipped, fmt.Errorf("upload %s: %w", objectKey, putErr)
		}
		uploaded++
	}
	return uploaded, skipped, nil
}

// seedContentType maps a filename's extension to a Content-Type. It never
// inspects the file's bytes: doing so could invite "helpfully" re-encoding
// or otherwise touching the image, which the verbatim-upload requirement
// rules out.
func seedContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

// writeFileAtomic writes data to a temporary file in the same directory as
// path, then renames it over path. Same-directory placement matters: rename
// is only atomic within a single filesystem, so a temp file elsewhere (e.g.
// the system temp dir) could make the final step a cross-filesystem copy
// instead of an atomic rename. A reader can therefore only ever see the
// previous complete contents of path or the new complete contents, never a
// torn write. If anything fails before the rename, the temp file is removed
// so a killed or erroring run leaves no stray files behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	renamed = true
	return nil
}
