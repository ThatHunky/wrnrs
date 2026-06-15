package i18n

import (
	"encoding/json"
	"fmt"
	"io"
)

const DefaultLanguage = "uk"

type Catalog struct {
	Language string            `json:"language"`
	Brand    string            `json:"brand"`
	Strings  map[string]string `json:"strings"`
}

type Bundle struct {
	catalogs map[string]Catalog
}

func LoadCatalog(r io.Reader) (Catalog, error) {
	var catalog Catalog
	if err := json.NewDecoder(r).Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	if catalog.Language == "" {
		return Catalog{}, fmt.Errorf("catalog language is required")
	}
	if catalog.Strings == nil {
		catalog.Strings = map[string]string{}
	}
	return catalog, nil
}

func NewBundle() *Bundle {
	return &Bundle{catalogs: map[string]Catalog{}}
}

func (b *Bundle) Add(catalog Catalog) {
	b.catalogs[catalog.Language] = catalog
}

func (b *Bundle) Brand(language string) string {
	if catalog, ok := b.catalogs[language]; ok && catalog.Brand != "" {
		return catalog.Brand
	}
	if catalog, ok := b.catalogs[DefaultLanguage]; ok && catalog.Brand != "" {
		return catalog.Brand
	}
	if language == "en" {
		return "WRNRS"
	}
	return "між нами."
}

func (b *Bundle) Text(language, key string) string {
	if catalog, ok := b.catalogs[language]; ok {
		if text, ok := catalog.Strings[key]; ok {
			return text
		}
	}
	if catalog, ok := b.catalogs[DefaultLanguage]; ok {
		if text, ok := catalog.Strings[key]; ok {
			return text
		}
	}
	return key
}
