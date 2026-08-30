package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"wrnrs/internal/catalog"
)

// fakeObjectStore is an in-memory stand-in for *objectstore.MinIOStore that
// satisfies objectPutGetter. It lets seedCatalog be exercised without a
// real MinIO server and, crucially, without any network access at all —
// proving the seeding path makes no request to the source website (there
// is no source website reachable from this test).
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}}
}

func (f *fakeObjectStore) Get(_ context.Context, objectKey string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[objectKey]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", objectKey)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (f *fakeObjectStore) Put(_ context.Context, objectKey, _ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := make([]byte, len(data))
	copy(stored, data)
	f.objects[objectKey] = stored
	f.puts++
	return nil
}

func TestSeedCatalogUploadsLocalImagesVerbatim(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "519.png"), []byte("fake-png-bytes-519"), 0o644); err != nil {
		t.Fatalf("write fixture image: %v", err)
	}

	cat := catalog.Catalog{Items: []catalog.Item{
		{ID: "519", Media: &catalog.MediaRef{Key: "positions/519.png"}},
	}}

	store := newFakeObjectStore()
	uploaded, skipped, err := seedCatalog(context.Background(), store, dir, "positions/", cat)
	if err != nil {
		t.Fatalf("seedCatalog: %v", err)
	}
	if uploaded != 1 || skipped != 0 {
		t.Fatalf("uploaded=%d skipped=%d, want uploaded=1 skipped=0", uploaded, skipped)
	}

	got, ok := store.objects["positions/519.png"]
	if !ok {
		t.Fatal("store does not contain positions/519.png after seeding")
	}
	if !bytes.Equal(got, []byte("fake-png-bytes-519")) {
		t.Fatalf("uploaded bytes = %q, want exact verbatim source bytes (no re-encode)", got)
	}
}

func TestSeedCatalogIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001.png"), []byte("bytes-one"), 0o644); err != nil {
		t.Fatalf("write fixture image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "002.png"), []byte("bytes-two"), 0o644); err != nil {
		t.Fatalf("write fixture image: %v", err)
	}

	cat := catalog.Catalog{Items: []catalog.Item{
		{ID: "001", Media: &catalog.MediaRef{Key: "positions/001.png"}},
		{ID: "002", Media: &catalog.MediaRef{Key: "positions/002.png"}},
	}}

	store := newFakeObjectStore()
	ctx := context.Background()

	uploaded1, skipped1, err := seedCatalog(ctx, store, dir, "positions/", cat)
	if err != nil {
		t.Fatalf("first seedCatalog: %v", err)
	}
	if uploaded1 != 2 || skipped1 != 0 {
		t.Fatalf("first run uploaded=%d skipped=%d, want uploaded=2 skipped=0", uploaded1, skipped1)
	}
	if store.puts != 2 {
		t.Fatalf("store.puts = %d after first run, want 2", store.puts)
	}

	// Re-running against an already-seeded store must be a no-op: every
	// object already matches its local file byte for byte, so nothing new
	// should be uploaded.
	uploaded2, skipped2, err := seedCatalog(ctx, store, dir, "positions/", cat)
	if err != nil {
		t.Fatalf("second seedCatalog: %v", err)
	}
	if uploaded2 != 0 || skipped2 != 2 {
		t.Fatalf("second run uploaded=%d skipped=%d, want uploaded=0 skipped=2 (idempotent re-run)", uploaded2, skipped2)
	}
	if store.puts != 2 {
		t.Fatalf("store.puts = %d after second run, want still 2 (no re-upload)", store.puts)
	}
}

func TestSeedCatalogUploadsWhenRemoteBytesDiffer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "003.png"), []byte("fresh-local-bytes"), 0o644); err != nil {
		t.Fatalf("write fixture image: %v", err)
	}

	cat := catalog.Catalog{Items: []catalog.Item{
		{ID: "003", Media: &catalog.MediaRef{Key: "positions/003.png"}},
	}}

	store := newFakeObjectStore()
	store.objects["positions/003.png"] = []byte("stale-remote-bytes")

	uploaded, skipped, err := seedCatalog(context.Background(), store, dir, "positions/", cat)
	if err != nil {
		t.Fatalf("seedCatalog: %v", err)
	}
	if uploaded != 1 || skipped != 0 {
		t.Fatalf("uploaded=%d skipped=%d, want uploaded=1 skipped=0 when remote bytes are stale", uploaded, skipped)
	}
	if got := string(store.objects["positions/003.png"]); got != "fresh-local-bytes" {
		t.Fatalf("stored bytes = %q, want the local file's bytes to win", got)
	}
}

func TestSeedCatalogFailsClearlyWhenLocalImageIsMissing(t *testing.T) {
	dir := t.TempDir() // deliberately empty

	cat := catalog.Catalog{Items: []catalog.Item{
		{ID: "007", Media: &catalog.MediaRef{Key: "positions/007.png"}},
	}}

	store := newFakeObjectStore()
	_, _, err := seedCatalog(context.Background(), store, dir, "positions/", cat)
	if err == nil {
		t.Fatal("seedCatalog succeeded despite a missing local image; want an error naming the item")
	}
	if !strings.Contains(err.Error(), "007") || !strings.Contains(err.Error(), "007.png") {
		t.Fatalf("seedCatalog error = %q, want it to name the item id and the local path", err.Error())
	}
}

func TestSeedCatalogSkipsItemsWithoutMedia(t *testing.T) {
	dir := t.TempDir()
	cat := catalog.Catalog{Items: []catalog.Item{
		{ID: "008", Media: nil},
	}}

	store := newFakeObjectStore()
	uploaded, skipped, err := seedCatalog(context.Background(), store, dir, "positions/", cat)
	if err != nil {
		t.Fatalf("seedCatalog: %v", err)
	}
	if uploaded != 0 || skipped != 0 {
		t.Fatalf("uploaded=%d skipped=%d, want both 0 for a medialess item", uploaded, skipped)
	}
}

func TestSeedContentTypeByExtension(t *testing.T) {
	cases := map[string]string{
		"519.png":     "image/png",
		"519.PNG":     "image/png",
		"519.webp":    "image/webp",
		"519.jpg":     "image/jpeg",
		"519.jpeg":    "image/jpeg",
		"519.gif":     "image/gif",
		"519.unknown": "application/octet-stream",
	}
	for name, want := range cases {
		if got := seedContentType(name); got != want {
			t.Errorf("seedContentType(%q) = %q, want %q", name, got, want)
		}
	}
}
