package content

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type BackgroundCatalog struct {
	Version     int          `json:"version"`
	Backgrounds []Background `json:"backgrounds"`
}

type Background struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Premium   bool              `json:"premium"`
	Name      map[string]string `json:"name"`
	ObjectKey string            `json:"object_key"`
}

func LoadBackgroundCatalog(r io.Reader) (*BackgroundCatalog, error) {
	var catalog BackgroundCatalog
	if err := json.NewDecoder(r).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode background catalog: %w", err)
	}
	return &catalog, nil
}

func (c *BackgroundCatalog) Validate() error {
	if c.Version <= 0 {
		return fmt.Errorf("background catalog version must be positive")
	}
	seen := map[string]bool{}
	for _, bg := range c.Backgrounds {
		if strings.TrimSpace(bg.ID) == "" {
			return fmt.Errorf("background id is required")
		}
		if seen[bg.ID] {
			return fmt.Errorf("background %s is duplicated", bg.ID)
		}
		seen[bg.ID] = true

		if bg.Kind != "built_in" && bg.Kind != "user_upload" {
			return fmt.Errorf("background %s has invalid kind: %s", bg.ID, bg.Kind)
		}
		if len(bg.Name) == 0 {
			return fmt.Errorf("background %s must have a name", bg.ID)
		}
		if strings.TrimSpace(bg.ObjectKey) == "" {
			return fmt.Errorf("background %s object_key is required", bg.ID)
		}
	}
	return nil
}

func (c *BackgroundCatalog) Background(id string) (Background, bool) {
	for _, bg := range c.Backgrounds {
		if bg.ID == id {
			return bg, true
		}
	}
	return Background{}, false
}
