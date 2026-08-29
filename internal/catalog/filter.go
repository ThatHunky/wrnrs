package catalog

import (
	"sort"
	"strings"
)

// Filter narrows a catalog. Values inside one facet are OR-ed, facets are
// AND-ed, Exclude removes any match, and Tags requires all listed tags.
type Filter struct {
	Include map[string][]string
	Exclude map[string][]string
	Tags    []string
}

func (c *Catalog) Filtered(f Filter) []Item {
	out := make([]Item, 0, len(c.Items))
	for _, item := range c.Items {
		if item.Matches(f) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (i Item) Matches(f Filter) bool {
	for facet, allowed := range f.Include {
		if len(allowed) == 0 {
			continue
		}
		if !anyValue(i.Facets[facet], allowed) {
			return false
		}
	}
	for facet, banned := range f.Exclude {
		if anyValue(i.Facets[facet], banned) {
			return false
		}
	}
	for _, tag := range f.Tags {
		if !hasValue(i.Tags, tag) {
			return false
		}
	}
	return true
}

func anyValue(have, want []string) bool {
	for _, w := range want {
		if hasValue(have, w) {
			return true
		}
	}
	return false
}

func hasValue(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
