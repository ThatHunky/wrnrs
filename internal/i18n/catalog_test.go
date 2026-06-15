package i18n_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wrnrs/internal/i18n"
)

func TestCatalogReturnsLocalizedBrandAndFallsBackToUkrainian(t *testing.T) {
	raw := `{
		"language": "uk",
		"brand": "між нами.",
		"strings": {
			"menu.start": "Почати"
		}
	}`

	catalog, err := i18n.LoadCatalog(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadCatalog returned error: %v", err)
	}

	bundle := i18n.NewBundle()
	bundle.Add(catalog)

	if got := bundle.Brand("uk"); got != "між нами." {
		t.Fatalf("Brand(uk) = %q", got)
	}
	if got := bundle.Text("en", "menu.start"); got != "Почати" {
		t.Fatalf("fallback text = %q, want Ukrainian string", got)
	}
}

func TestProductionCatalogsHaveMatchingKeys(t *testing.T) {
	root := filepath.Join("..", "..", "content", "i18n")
	ukFile, err := os.Open(filepath.Join(root, "uk.json"))
	if err != nil {
		t.Fatalf("open uk catalog: %v", err)
	}
	defer ukFile.Close()
	enFile, err := os.Open(filepath.Join(root, "en.json"))
	if err != nil {
		t.Fatalf("open en catalog: %v", err)
	}
	defer enFile.Close()

	uk, err := i18n.LoadCatalog(ukFile)
	if err != nil {
		t.Fatalf("load uk catalog: %v", err)
	}
	en, err := i18n.LoadCatalog(enFile)
	if err != nil {
		t.Fatalf("load en catalog: %v", err)
	}

	for key := range uk.Strings {
		if _, ok := en.Strings[key]; !ok {
			t.Fatalf("English catalog missing key %q", key)
		}
	}
	for key := range en.Strings {
		if _, ok := uk.Strings[key]; !ok {
			t.Fatalf("Ukrainian catalog missing key %q", key)
		}
	}
}
