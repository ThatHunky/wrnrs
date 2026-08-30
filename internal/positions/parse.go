package positions

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ErrNotAPositionPage marks a page that is an article about a family of
// positions rather than a single catalog entry. Those pages carry the merged
// tags of every variation they show and must never enter the catalog.
var ErrNotAPositionPage = errors.New("not a single-position page")

var (
	headingPattern = regexp.MustCompile(`^Sex position #(\d+)\.\s*(.+)$`)
	tagPathPattern = regexp.MustCompile(`/tag/([^/?#"]+)`)
)

type ParsedPage struct {
	Number      int
	Name        string
	Description string
	ImageURL    string
	TagSlugs    []string
}

func ParsePage(r io.Reader) (ParsedPage, error) {
	root, err := html.Parse(r)
	if err != nil {
		return ParsedPage{}, fmt.Errorf("parse html: %w", err)
	}

	var (
		heading  string
		metas    = map[string]string{}
		slugSet  = map[string]bool{}
		inH1     bool
		headText strings.Builder
	)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1":
				if heading == "" {
					inH1 = true
					headText.Reset()
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
					inH1 = false
					heading = strings.TrimSpace(headText.String())
					return
				}
			case "meta":
				var property, content string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "property":
						property = attr.Val
					case "content":
						content = attr.Val
					}
				}
				if property != "" {
					metas[property] = content
				}
			case "a":
				for _, attr := range n.Attr {
					if attr.Key != "href" {
						continue
					}
					if m := tagPathPattern.FindStringSubmatch(attr.Val); m != nil {
						slugSet[m[1]] = true
					}
				}
			}
		}
		if n.Type == html.TextNode && inH1 {
			headText.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	match := headingPattern.FindStringSubmatch(heading)
	if match == nil {
		return ParsedPage{}, fmt.Errorf("heading %q: %w", heading, ErrNotAPositionPage)
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return ParsedPage{}, fmt.Errorf("heading %q: %w", heading, ErrNotAPositionPage)
	}

	image := strings.TrimSpace(metas["og:image"])
	if image == "" {
		return ParsedPage{}, errors.New("page has no og:image")
	}

	slugs := make([]string, 0, len(slugSet))
	for slug := range slugSet {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	return ParsedPage{
		Number:      number,
		Name:        strings.TrimSpace(match[2]),
		Description: strings.TrimSpace(metas["og:description"]),
		ImageURL:    image,
		TagSlugs:    slugs,
	}, nil
}
