package authority

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ExtractDomain extracts the domain from a URL.
// It removes the scheme, path, query, fragment, and port components,
// returning only the domain in lowercase.
//
// Examples:
//   - "https://example.com/image.jpg" -> "example.com"
//   - "http://sub.example.com:8080/path" -> "sub.example.com"
//   - "HTTP://EXAMPLE.COM" -> "example.com"
func ExtractDomain(rawURL string) (string, error) {
	// Handle empty URLs
	if rawURL == "" {
		return "", errors.New("empty URL")
	}

	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Extract host (includes domain and port)
	host := parsedURL.Host

	// Handle case where host is empty (e.g., malformed URL like "example.com")
	if host == "" {
		// Try to extract from the URL string directly if it's missing scheme
		if strings.Contains(rawURL, "://") {
			return "", errors.New("URL has scheme but no host")
		}
		// The URL might be a hostname without scheme
		host = rawURL
	}

	// Remove port number if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Remove trailing slashes and convert to lowercase
	host = strings.ToLower(strings.TrimSpace(host))

	// Validate that we have a domain
	if host == "" {
		return "", errors.New("no domain found in URL")
	}

	// Check if it looks like a domain (at least one dot)
	if !strings.Contains(host, ".") {
		// Could be a localhost or local domain name
		if host != "localhost" {
			return "", fmt.Errorf("invalid domain format: %s", host)
		}
	}

	return host, nil
}
