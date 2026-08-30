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
	headingPattern    = regexp.MustCompile(`^Sex position #(\d+)\.\s*(.+)$`)
	tagPathPattern    = regexp.MustCompile(`/tag/([^/?#"]+)`)
	whitespaceRunPtrn = regexp.MustCompile(`\s+`)
)

// descriptionHeaderText is the exact (trimmed) text of the <div> that
// introduces the full-length description block on a position page:
//
//	<div class='pos_headers'>Description:</div><hr>
//	        <p>...full text...</p>
//
// The site caps og:description at ~160 characters, cutting the real text
// mid-sentence, so the block above is preferred whenever it is present.
const descriptionHeaderText = "Description:"

// collapseWhitespace turns any run of whitespace (including newlines from
// multi-line source markup) into a single space and trims the ends.
func collapseWhitespace(s string) string {
	return strings.TrimSpace(whitespaceRunPtrn.ReplaceAllString(s, " "))
}

// nodeText concatenates the text of every descendant text node of n. Because
// golang.org/x/net/html already unescapes entities while tokenizing, and we
// only ever collect TextNode data (never tag markup), this simultaneously
// unescapes entities and strips inline tags.
func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// hasClass reports whether n carries the given class among (possibly
// several) space-separated classes in its class attribute. golang.org/x/net/html
// normalizes away the quote style (' vs ") used in the source markup, so this
// is tolerant of both by construction.
func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, c := range strings.Fields(attr.Val) {
			if c == class {
				return true
			}
		}
	}
	return false
}

// descriptionParagraph finds the <p> that follows a `Description:` header
// div, tolerating an optional <hr> and any amount of whitespace/comment
// nodes between the header, the optional <hr>, and the paragraph. It returns
// nil if the expected shape isn't there, so callers can fall back safely.
func descriptionParagraph(header *html.Node) *html.Node {
	sawHR := false
	for sib := header.NextSibling; sib != nil; sib = sib.NextSibling {
		switch sib.Type {
		case html.TextNode, html.CommentNode:
			continue
		case html.ElementNode:
			switch sib.Data {
			case "hr":
				if sawHR {
					return nil
				}
				sawHR = true
				continue
			case "p":
				return sib
			default:
				return nil
			}
		default:
			continue
		}
	}
	return nil
}

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
		heading      string
		metas        = map[string]string{}
		slugSet      = map[string]bool{}
		inH1         bool
		headText     strings.Builder
		fullDescBlk  string
		foundDescBlk bool
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
			case "div":
				if !foundDescBlk && hasClass(n, "pos_headers") &&
					collapseWhitespace(nodeText(n)) == descriptionHeaderText {
					if p := descriptionParagraph(n); p != nil {
						if text := collapseWhitespace(nodeText(p)); text != "" {
							fullDescBlk = text
							foundDescBlk = true
						}
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

	description := fullDescBlk
	if description == "" {
		description = strings.TrimSpace(metas["og:description"])
	}

	return ParsedPage{
		Number:      number,
		Name:        strings.TrimSpace(match[2]),
		Description: description,
		ImageURL:    image,
		TagSlugs:    slugs,
	}, nil
}
