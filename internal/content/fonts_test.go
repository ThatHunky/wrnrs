package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"wrnrs/internal/content"
)

func TestLoadFontCatalogValidatesReferencedFiles(t *testing.T) {
	file, err := os.Open("../../content/fonts.v1.json")
	if err != nil {
		t.Fatalf("open font catalog: %v", err)
	}
	defer file.Close()

	catalog, err := content.LoadFontCatalog(file)
	if err != nil {
		t.Fatalf("LoadFontCatalog returned error: %v", err)
	}
	if err := catalog.Validate("../../"); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	font, ok := catalog.Font("nunito_regular")
	if !ok {
		t.Fatal("nunito_regular not found")
	}
	if filepath.Ext(font.Path) != ".ttf" {
		t.Fatalf("font path = %q, want .ttf", font.Path)
	}
}
