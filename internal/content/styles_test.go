package content_test

import (
	"os"
	"testing"

	"wrnrs/internal/content"
)

func TestLoadStyleCatalog(t *testing.T) {
	file, err := os.Open("../../content/styles.v1.json")
	if err != nil {
		t.Fatalf("open style catalog: %v", err)
	}
	defer file.Close()

	catalog, err := content.LoadStyleCatalog(file)
	if err != nil {
		t.Fatalf("LoadStyleCatalog returned error: %v", err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	style, ok := catalog.Style("default_warm")
	if !ok {
		t.Fatal("default_warm not found")
	}
	if style.Tokens.BorderRadius != 30 {
		t.Fatalf("border radius = %v, want 30", style.Tokens.BorderRadius)
	}
}
