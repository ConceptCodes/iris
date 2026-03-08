package crawl

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractHTMLLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/gallery")
	doc := `
	<html><body>
		<link rel="canonical" href="/gallery">
		<img src="/img/cat.jpg">
		<a href="/page-2">next</a>
		<a href="https://example.com/static/dog.png">dog</a>
		<a href="https://other.com/nope.jpg">outside</a>
	</body></html>`

	result, err := extractHTMLLinks(strings.NewReader(doc), base, []string{"example.com"})
	if err != nil {
		t.Fatalf("extract html links: %v", err)
	}
	if len(result.imageURLs) != 2 {
		t.Fatalf("expected 2 image urls, got %d", len(result.imageURLs))
	}
	if len(result.pageURLs) != 1 {
		t.Fatalf("expected 1 page url, got %d", len(result.pageURLs))
	}
	if result.canonicalURL != "https://example.com/gallery" {
		t.Fatalf("unexpected canonical url: %q", result.canonicalURL)
	}
}

func TestExtractSitemapLocs(t *testing.T) {
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
	<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
	  <url><loc>https://example.com/a</loc></url>
	  <url><loc>https://example.com/b.jpg</loc></url>
	</urlset>`

	locs, err := extractSitemapLocs(strings.NewReader(xmlBody))
	if err != nil {
		t.Fatalf("extract sitemap locs: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locs, got %d", len(locs))
	}
}
