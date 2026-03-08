package crawl

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

type htmlDiscovery struct {
	imageURLs    []string
	pageURLs     []string
	canonicalURL string
}

type HTMLDiscovery struct {
	ImageURLs    []string
	PageURLs     []string
	CanonicalURL string
}

func extractHTMLLinks(r io.Reader, base *url.URL, allowedDomains []string) (htmlDiscovery, error) {
	root, err := html.Parse(r)
	if err != nil {
		return htmlDiscovery{}, fmt.Errorf("parse html: %w", err)
	}

	seenImages := map[string]struct{}{}
	seenPages := map[string]struct{}{}
	var result htmlDiscovery

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "link":
				if attrContainsToken(node, "rel", "canonical") {
					if href := attr(node, "href"); href != "" && result.canonicalURL == "" {
						if resolved, ok := resolveHTTPURL(base, href, allowedDomains); ok {
							result.canonicalURL = resolved
						}
					}
				}
			case "img":
				if src := attr(node, "src"); src != "" {
					if resolved, ok := resolveHTTPURL(base, src, allowedDomains); ok {
						if _, exists := seenImages[resolved]; !exists {
							seenImages[resolved] = struct{}{}
							result.imageURLs = append(result.imageURLs, resolved)
						}
					}
				}
			case "a":
				if href := attr(node, "href"); href != "" {
					if resolved, ok := resolveHTTPURL(base, href, allowedDomains); ok {
						if looksLikeImageURL(resolved) {
							if _, exists := seenImages[resolved]; !exists {
								seenImages[resolved] = struct{}{}
								result.imageURLs = append(result.imageURLs, resolved)
							}
						} else if _, exists := seenPages[resolved]; !exists {
							seenPages[resolved] = struct{}{}
							result.pageURLs = append(result.pageURLs, resolved)
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)
	return result, nil
}

func ExtractHTMLLinks(r io.Reader, rawBase string, allowedDomains []string) (HTMLDiscovery, error) {
	base, err := url.Parse(rawBase)
	if err != nil {
		return HTMLDiscovery{}, err
	}
	result, err := extractHTMLLinks(r, base, allowedDomains)
	if err != nil {
		return HTMLDiscovery{}, err
	}
	return HTMLDiscovery{
		ImageURLs:    result.imageURLs,
		PageURLs:     result.pageURLs,
		CanonicalURL: result.canonicalURL,
	}, nil
}

func extractSitemapLocs(r io.Reader) ([]string, error) {
	var (
		urlset struct {
			URLs []struct {
				Loc string `xml:"loc"`
			} `xml:"url"`
		}
		index struct {
			Sitemaps []struct {
				Loc string `xml:"loc"`
			} `xml:"sitemap"`
		}
	)

	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	if err := xml.Unmarshal(body, &urlset); err == nil && len(urlset.URLs) > 0 {
		locs := make([]string, 0, len(urlset.URLs))
		for _, item := range urlset.URLs {
			if strings.TrimSpace(item.Loc) != "" {
				locs = append(locs, strings.TrimSpace(item.Loc))
			}
		}
		return locs, nil
	}

	if err := xml.Unmarshal(body, &index); err == nil && len(index.Sitemaps) > 0 {
		locs := make([]string, 0, len(index.Sitemaps))
		for _, item := range index.Sitemaps {
			if strings.TrimSpace(item.Loc) != "" {
				locs = append(locs, strings.TrimSpace(item.Loc))
			}
		}
		return locs, nil
	}

	return nil, fmt.Errorf("unsupported sitemap format")
}

func ExtractSitemapLocs(r io.Reader) ([]string, error) {
	return extractSitemapLocs(r)
}

func FetchSitemapLocs(ctx context.Context, sitemapURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch sitemap: status %d", resp.StatusCode)
	}
	return extractSitemapLocs(resp.Body)
}

func resolveHTTPURL(base *url.URL, raw string, allowedDomains []string) (string, bool) {
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	if len(allowedDomains) > 0 && !hostAllowed(resolved.Hostname(), allowedDomains) {
		return "", false
	}
	return normalizeURL(resolved), true
}

func hostAllowed(host string, allowedDomains []string) bool {
	host = strings.ToLower(host)
	for _, domain := range allowedDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func looksLikeImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func LooksLikeImageURL(raw string) bool {
	return looksLikeImageURL(raw)
}

func attr(node *html.Node, key string) string {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func attrContainsToken(node *html.Node, key, token string) bool {
	value := strings.ToLower(attr(node, key))
	for _, part := range strings.Fields(value) {
		if part == token {
			return true
		}
	}
	return false
}
