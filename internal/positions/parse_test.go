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
