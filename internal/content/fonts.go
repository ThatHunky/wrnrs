package content

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FontCatalog struct {
	Version int    `json:"version"`
	Fonts   []Font `json:"fonts"`
}

type Font struct {
	ID       string            `json:"id"`
	Name     map[string]string `json:"name"`
	Path     string            `json:"path"`
	Premium  bool              `json:"premium"`
	Category string            `json:"category,omitempty"`
}

func LoadFontCatalog(r io.Reader) (*FontCatalog, error) {
	var catalog FontCatalog
	if err := json.NewDecoder(r).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode font catalog: %w", err)
	}
	return &catalog, nil
}

func (c *FontCatalog) Validate(root string) error {
	if c.Version <= 0 {
		return fmt.Errorf("font catalog version must be positive")
	}
	seen := map[string]bool{}
	for _, font := range c.Fonts {
		if strings.TrimSpace(font.ID) == "" {
			return fmt.Errorf("font id is required")
		}
		if seen[font.ID] {
			return fmt.Errorf("font %s is duplicated", font.ID)
		}
		seen[font.ID] = true
		if strings.TrimSpace(font.Path) == "" {
			return fmt.Errorf("font %s path is required", font.ID)
		}
		path := font.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if info, err := os.Stat(path); err != nil {
			return fmt.Errorf("font %s file %s: %w", font.ID, font.Path, err)
		} else if info.IsDir() {
			return fmt.Errorf("font %s path %s is a directory", font.ID, font.Path)
		}
	}
	return nil
}

func (c *FontCatalog) Font(id string) (Font, bool) {
	for _, font := range c.Fonts {
		if font.ID == id {
			return font, true
		}
	}
	return Font{}, false
}
