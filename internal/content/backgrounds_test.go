package content_test

import (
	"os"
	"testing"

	"wrnrs/internal/content"
)

func TestLoadBackgroundCatalog(t *testing.T) {
	file, err := os.Open("../../content/backgrounds.v1.json")
	if err != nil {
		t.Fatalf("open background catalog: %v", err)
	}
	defer file.Close()

	catalog, err := content.LoadBackgroundCatalog(file)
	if err != nil {
		t.Fatalf("LoadBackgroundCatalog returned error: %v", err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	bg, ok := catalog.Background("builtin_blush_gradient")
	if !ok {
		t.Fatal("builtin_blush_gradient not found")
	}
	if bg.ObjectKey != "built-ins/blush-gradient.webp" {
		t.Fatalf("object key = %q, want built-ins/blush-gradient.webp", bg.ObjectKey)
	}
}
