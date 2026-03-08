package crawl

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"

	"iris/internal/constants"
)

func NormalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("absolute url required")
	}
	if parsed.Scheme != constants.SchemeHTTP && parsed.Scheme != constants.SchemeHTTPS {
		return "", fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}
	return normalizeURL(parsed), nil
}

func normalizeURL(u *url.URL) string {
	normalized := *u
	normalized.Scheme = strings.ToLower(normalized.Scheme)
	normalized.Host = normalizeHost(normalized.Scheme, normalized.Host)
	normalized.User = nil
	normalized.Fragment = ""
	normalized.RawFragment = ""
	normalized.Path = normalizePath(normalized.Path)
	normalized.RawPath = ""
	normalized.RawQuery = normalizeQuery(normalized.Query())
	return normalized.String()
}

func normalizeHost(scheme, host string) string {
	hostname := strings.ToLower(host)
	port := ""
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		hostname = strings.ToLower(parsedHost)
		port = parsedPort
	} else if strings.Count(host, ":") >= 2 && strings.HasPrefix(host, "[") && strings.Contains(host, "]") {
		return strings.ToLower(host)
	}

	switch {
	case scheme == constants.SchemeHTTP && port == constants.HTTPPort:
		port = ""
	case scheme == constants.SchemeHTTPS && port == constants.HTTPSPort:
		port = ""
	}

	if port == "" {
		return hostname
	}
	return net.JoinHostPort(hostname, port)
}

func normalizePath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	cleaned := path.Clean(rawPath)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	if strings.HasSuffix(rawPath, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

func normalizeQuery(values url.Values) string {
	clean := make(url.Values)
	for key, items := range values {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "utm_") {
			continue
		}
		if _, drop := constants.DroppedQueryParams[lowerKey]; drop {
			continue
		}
		filtered := make([]string, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			continue
		}
		sort.Strings(filtered)
		clean[key] = filtered
	}
	return clean.Encode()
}
