package content

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type StyleCatalog struct {
	Version int     `json:"version"`
	Styles  []Style `json:"styles"`
}

type Style struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Premium bool              `json:"premium"`
	Tokens  StyleTokens       `json:"tokens"`
}

type StyleTokens struct {
	BorderRadius float64 `json:"border_radius"`
	GlassOpacity float64 `json:"glass_opacity"`
	DefaultColor string  `json:"default_color"`
}

func LoadStyleCatalog(r io.Reader) (*StyleCatalog, error) {
	var catalog StyleCatalog
	if err := json.NewDecoder(r).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode style catalog: %w", err)
	}
	return &catalog, nil
}

func (c *StyleCatalog) Validate() error {
	if c.Version <= 0 {
		return fmt.Errorf("style catalog version must be positive")
	}
	seen := map[string]bool{}
	for _, style := range c.Styles {
		if strings.TrimSpace(style.ID) == "" {
			return fmt.Errorf("style id is required")
		}
		if seen[style.ID] {
			return fmt.Errorf("style %s is duplicated", style.ID)
		}
		seen[style.ID] = true

		if len(style.Name) == 0 {
			return fmt.Errorf("style %s must have a name", style.ID)
		}
		if style.Tokens.BorderRadius < 0 {
			return fmt.Errorf("style %s border_radius must be non-negative", style.ID)
		}
		if style.Tokens.GlassOpacity < 0 || style.Tokens.GlassOpacity > 1 {
			return fmt.Errorf("style %s glass_opacity must be between 0 and 1", style.ID)
		}
		if !strings.HasPrefix(style.Tokens.DefaultColor, "#") || len(style.Tokens.DefaultColor) != 7 {
			return fmt.Errorf("style %s default_color must be in #RRGGBB format", style.ID)
		}
	}
	return nil
}

func (c *StyleCatalog) Style(id string) (Style, bool) {
	for _, style := range c.Styles {
		if style.ID == id {
			return style, true
		}
	}
	return Style{}, false
}
