package crawl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRobotsLongestMatchWins(t *testing.T) {
	policy, err := parseRobots(strings.NewReader(`
User-agent: *
Disallow: /images/
Allow: /images/public/
Disallow: /tmp/*.jpg$
`))
	if err != nil {
		t.Fatalf("parse robots: %v", err)
	}

	if !policy.allows("iris", "/images/public/cat.jpg") {
		t.Fatalf("expected allow for more specific public path")
	}
	if policy.allows("iris", "/images/private/cat.jpg") {
		t.Fatalf("expected disallow for private image path")
	}
	if policy.allows("iris", "/tmp/test.jpg") {
		t.Fatalf("expected anchored wildcard disallow")
	}
	if !policy.allows("iris", "/tmp/test.jpg?size=large.png") {
		t.Fatalf("expected querystring path not to match jpg anchor")
	}
}

func TestParseRobotsSpecificUserAgentWins(t *testing.T) {
	policy, err := parseRobots(strings.NewReader(`
User-agent: *
Disallow: /

User-agent: iris
Allow: /
Disallow: /private/
`))
	if err != nil {
		t.Fatalf("parse robots: %v", err)
	}

	if !policy.allows("iris", "/public/page") {
		t.Fatalf("expected iris-specific allow group to win")
	}
	if policy.allows("iris", "/private/page") {
		t.Fatalf("expected private path to be disallowed")
	}
	if policy.allows("other-bot", "/public/page") {
		t.Fatalf("expected wildcard group to disallow other bots")
	}
}

func TestParseRobotsMergesMatchingGroups(t *testing.T) {
	policy, err := parseRobots(strings.NewReader(`
User-agent: iris
Disallow: /private/

User-agent: iris
Disallow: /tmp/
Allow: /tmp/public/
`))
	if err != nil {
		t.Fatalf("parse robots: %v", err)
	}

	if policy.allows("iris", "/private/page") {
		t.Fatalf("expected first matching group to apply")
	}
	if policy.allows("iris", "/tmp/page") {
		t.Fatalf("expected second matching group to apply")
	}
	if !policy.allows("iris", "/tmp/public/page") {
		t.Fatalf("expected more specific allow to win after merge")
	}
}

func TestRobotsClientAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /blocked/\n"))
		case "/open/image.jpg":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewRobotsClient(server.Client(), "iris")

	allowed, err := client.Allowed(context.Background(), server.URL+"/open/image.jpg")
	if err != nil {
		t.Fatalf("allowed open url: %v", err)
	}
	if !allowed {
		t.Fatalf("expected open path to be allowed")
	}

	allowed, err = client.Allowed(context.Background(), server.URL+"/blocked/image.jpg")
	if err != nil {
		t.Fatalf("allowed blocked url: %v", err)
	}
	if allowed {
		t.Fatalf("expected blocked path to be disallowed")
	}
}
