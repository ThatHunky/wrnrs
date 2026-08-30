package positions_test

import (
	"errors"
	"strings"
	"testing"

	"wrnrs/internal/positions"
)

const singlePositionPage = `<!DOCTYPE html><html><head>
<meta property="og:description" content="Sex position #519 - Revelation (on the couch). Kamasutra.">
<meta property="og:image" content="https://example.test/uploads/2019/06/18_55.png">
</head><body>
<h1 class="entry-title" itemprop="headline">Sex position #519. Revelation</h1>
<a href="https://example.test/tag/medium-level">medium</a>
<a href="https://example.test/tag/sofa">sofa</a>
<a href="https://example.test/tag/cowgirl">cowgirl</a>
<a href="https://example.test/positions/518.html">previous</a>
</body></html>`

const articlePage = `<!DOCTYPE html><html><head>
<meta property="og:description" content="42 awesome ways to switch it up.">
<meta property="og:image" content="https://example.test/uploads/2016/03/3_8.png">
</head><body>
<h1 class="entry-title" itemprop="headline">Missionary Sex Position - 42 Variations + Tips</h1>
<a href="https://example.test/tag/easy-level">easy</a>
<a href="https://example.test/tag/crazy">crazy</a>
</body></html>`

func TestParsePageExtractsEveryField(t *testing.T) {
	page, err := positions.ParsePage(strings.NewReader(singlePositionPage))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	if page.Number != 519 {
		t.Fatalf("Number = %d, want 519", page.Number)
	}
	if page.Name != "Revelation" {
		t.Fatalf("Name = %q, want Revelation", page.Name)
	}
	if !strings.HasPrefix(page.Description, "Sex position #519 - Revelation") {
		t.Fatalf("Description = %q, want the og:description text", page.Description)
	}
	if page.ImageURL != "https://example.test/uploads/2019/06/18_55.png" {
		t.Fatalf("ImageURL = %q, want the og:image url", page.ImageURL)
	}
	if got := strings.Join(page.TagSlugs, ","); got != "cowgirl,medium-level,sofa" {
		t.Fatalf("TagSlugs = %q, want cowgirl,medium-level,sofa (sorted, links only from /tag/)", got)
	}
}

func TestParsePageRejectsArticleStylePages(t *testing.T) {
	_, err := positions.ParsePage(strings.NewReader(articlePage))
	if !errors.Is(err, positions.ErrNotAPositionPage) {
		t.Fatalf("ParsePage on an article page returned %v, want ErrNotAPositionPage", err)
	}
}

func TestParsePageRejectsPageWithoutImage(t *testing.T) {
	noImage := strings.Replace(singlePositionPage,
		`<meta property="og:image" content="https://example.test/uploads/2019/06/18_55.png">`, "", 1)

	_, err := positions.ParsePage(strings.NewReader(noImage))
	if err == nil {
		t.Fatal("ParsePage without og:image succeeded, want an error")
	}
	if errors.Is(err, positions.ErrNotAPositionPage) {
		t.Fatal("missing image reported as ErrNotAPositionPage; it is a different failure")
	}
}

const fullDescriptionPage = `<!DOCTYPE html><html><head>
<meta property="og:description" content="Sex position #519 - Revelation (on the couch). Kamasutra.">
<meta property="og:image" content="https://example.test/uploads/2019/06/18_55.png">
</head><body>
<h1 class="entry-title" itemprop="headline">Sex position #519. Revelation</h1>
<a href="https://example.test/tag/medium-level">medium</a>
<a href="https://example.test/tag/sofa">sofa</a>
<a href="https://example.test/tag/cowgirl">cowgirl</a>

<div class='pos_headers'>Description:</div><hr>
        <p>If the lovers don&#8217;t know what sex-shyness means, it&#8217;s an obvious advantage. The man lies with his upper body on the sofa &amp; his straight legs hang off the edge, so the couple can find their own rhythm together.</p>
<div class='pos_headers'>Most popular positions</div><hr>
</body></html>`

func TestParsePagePrefersFullDescriptionBlockOverMetaTag(t *testing.T) {
	page, err := positions.ParsePage(strings.NewReader(fullDescriptionPage))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	want := "If the lovers don’t know what sex-shyness means, it’s an obvious advantage. The man lies with his upper body on the sofa & his straight legs hang off the edge, so the couple can find their own rhythm together."
	if page.Description != want {
		t.Fatalf("Description = %q, want %q", page.Description, want)
	}
	if strings.Contains(page.Description, "Kamasutra") {
		t.Fatalf("Description = %q, want the full block text, not the truncated og:description", page.Description)
	}
}

func TestParsePageFallsBackToMetaDescriptionWithoutBlock(t *testing.T) {
	page, err := positions.ParsePage(strings.NewReader(singlePositionPage))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if !strings.HasPrefix(page.Description, "Sex position #519 - Revelation") {
		t.Fatalf("Description = %q, want the og:description fallback text", page.Description)
	}
}

func TestParsePageDescriptionBlockToleratesDoubleQuotedClass(t *testing.T) {
	doubleQuoted := strings.Replace(fullDescriptionPage,
		`<div class='pos_headers'>Description:</div><hr>`,
		`<div class="pos_headers">Description:</div><hr>`, 1)

	page, err := positions.ParsePage(strings.NewReader(doubleQuoted))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if !strings.HasPrefix(page.Description, "If the lovers don’t know") {
		t.Fatalf("Description = %q, want the full block text with double-quoted class", page.Description)
	}
}

func TestParsePageDescriptionBlockToleratesMissingHR(t *testing.T) {
	noHR := strings.Replace(fullDescriptionPage,
		`<div class='pos_headers'>Description:</div><hr>
        <p>`,
		`<div class='pos_headers'>Description:</div>
        <p>`, 1)

	page, err := positions.ParsePage(strings.NewReader(noHR))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if !strings.HasPrefix(page.Description, "If the lovers don’t know") {
		t.Fatalf("Description = %q, want the full block text with no <hr> after the header", page.Description)
	}
}

func TestParsePageDescriptionBlockUnescapesEntitiesAndKeepsApostrophes(t *testing.T) {
	page, err := positions.ParsePage(strings.NewReader(fullDescriptionPage))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if strings.Contains(page.Description, "&#8217;") || strings.Contains(page.Description, "&amp;") {
		t.Fatalf("Description = %q, want entities unescaped", page.Description)
	}
	if !strings.Contains(page.Description, "don’t") || !strings.Contains(page.Description, "it’s") {
		t.Fatalf("Description = %q, want typographic apostrophes intact", page.Description)
	}
	if !strings.Contains(page.Description, "sofa & his") {
		t.Fatalf("Description = %q, want &amp; unescaped to a literal &", page.Description)
	}
}

func TestParsePageDescriptionBlockCollapsesMultilineWhitespace(t *testing.T) {
	multiline := strings.Replace(fullDescriptionPage,
		`<p>If the lovers don&#8217;t know what sex-shyness means, it&#8217;s an obvious advantage. The man lies with his upper body on the sofa &amp; his straight legs hang off the edge, so the couple can find their own rhythm together.</p>`,
		"<p>If the lovers don&#8217;t know\n   what sex-shyness means,\n\t it&#8217;s an obvious   advantage.\n   The man lies with his upper body\n   on the sofa &amp; his straight legs hang off the edge,\n   so the couple can find their own rhythm together.</p>",
		1)

	page, err := positions.ParsePage(strings.NewReader(multiline))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	want := "If the lovers don’t know what sex-shyness means, it’s an obvious advantage. The man lies with his upper body on the sofa & his straight legs hang off the edge, so the couple can find their own rhythm together."
	if page.Description != want {
		t.Fatalf("Description = %q, want collapsed single-line text %q", page.Description, want)
	}
}

func TestParsePageDeduplicatesTagLinks(t *testing.T) {
	doubled := strings.Replace(singlePositionPage,
		`<a href="https://example.test/tag/sofa">sofa</a>`,
		`<a href="https://example.test/tag/sofa">sofa</a><a href="https://example.test/tag/sofa">sofa again</a>`, 1)

	page, err := positions.ParsePage(strings.NewReader(doubled))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if got := strings.Join(page.TagSlugs, ","); got != "cowgirl,medium-level,sofa" {
		t.Fatalf("TagSlugs = %q, want each slug once", got)
	}
}
