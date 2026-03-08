package crawl

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"iris/internal/constants"
	"net/http"
	"net/url"
	"strings"
)

type RobotsClient struct {
	fetcher   *CachedFetcher
	userAgent string
}

type robotsPolicy struct {
	groups []robotsGroup
}

type robotsGroup struct {
	agents []string
	rules  []robotsRule
}

type robotsRule struct {
	allow       bool
	pattern     string
	specificity int
}

func NewRobotsClient(client *http.Client, userAgent string) *RobotsClient {
	return NewRobotsClientWithOptions(client, userAgent, FetcherOptions{
		DefaultTTL:      constants.DefaultTTL24h,
		HostConcurrency: constants.DefaultHostConcurrency,
	})
}

func NewRobotsClientWithOptions(client *http.Client, userAgent string, options FetcherOptions) *RobotsClient {
	userAgent = strings.TrimSpace(strings.ToLower(userAgent))
	if userAgent == "" {
		userAgent = constants.DefaultCrawlerUserAgent
	}
	if options.DefaultTTL <= 0 {
		options.DefaultTTL = constants.DefaultTTL24h
	}
	return &RobotsClient{
		fetcher:   NewCachedFetcher(client, userAgent, options),
		userAgent: userAgent,
	}
}

func (c *RobotsClient) Allowed(ctx context.Context, rawURL string) (bool, error) {
	normalizedURL, err := NormalizeURL(rawURL)
	if err != nil {
		return false, err
	}
	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return false, err
	}
	policy, err := c.policyFor(ctx, parsed)
	if err != nil {
		return false, err
	}
	return policy.allows(c.userAgent, robotsPathWithQuery(parsed)), nil
}

func (c *RobotsClient) policyFor(ctx context.Context, target *url.URL) (robotsPolicy, error) {
	origin := target.Scheme + "://" + target.Host
	result, err := c.fetcher.Fetch(ctx, origin+constants.PathRobotsTxt)
	if err != nil {
		normalizedErr := strings.ToLower(err.Error())
		if strings.Contains(normalizedErr, "status 404") || strings.Contains(normalizedErr, "status 410") {
			return robotsPolicy{}, nil
		}
		return robotsPolicy{}, err
	}
	return parseRobots(bytes.NewReader(result.Body))
}

func (p robotsPolicy) allows(userAgent, path string) bool {
	rules := p.matchingRules(userAgent)
	if len(rules) == 0 {
		return true
	}

	bestSpecificity := -1
	bestAllow := true
	for _, rule := range rules {
		if !ruleMatchesPath(rule.pattern, path) {
			continue
		}
		if rule.specificity > bestSpecificity {
			bestSpecificity = rule.specificity
			bestAllow = rule.allow
			continue
		}
		if rule.specificity == bestSpecificity && rule.allow {
			bestAllow = true
		}
	}
	return bestAllow
}

func (p robotsPolicy) matchingRules(userAgent string) []robotsRule {
	bestScore := 0
	for index := range p.groups {
		score := matchingAgentScore(p.groups[index].agents, userAgent)
		if score > bestScore {
			bestScore = score
		}
	}
	if bestScore == 0 {
		return nil
	}

	rules := make([]robotsRule, 0)
	for index := range p.groups {
		if matchingAgentScore(p.groups[index].agents, userAgent) == bestScore {
			rules = append(rules, p.groups[index].rules...)
		}
	}
	return rules
}

func parseRobots(r io.Reader) (robotsPolicy, error) {
	var (
		policy      robotsPolicy
		current     robotsGroup
		haveAgent   bool
		sawRule     bool
		lineScanner = bufio.NewScanner(r)
	)

	flush := func() {
		if !haveAgent {
			return
		}
		group := robotsGroup{
			agents: append([]string(nil), current.agents...),
			rules:  append([]robotsRule(nil), current.rules...),
		}
		policy.groups = append(policy.groups, group)
		current = robotsGroup{}
		haveAgent = false
		sawRule = false
	}

	for lineScanner.Scan() {
		line := stripRobotsComment(lineScanner.Text())
		if line == "" {
			if sawRule {
				flush()
			}
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case constants.RobotsDirectiveUserAgent:
			if sawRule {
				flush()
			}
			agent := strings.ToLower(value)
			if agent == "" {
				continue
			}
			current.agents = append(current.agents, agent)
			haveAgent = true
		case constants.RobotsDirectiveAllow, constants.RobotsDirectiveDisallow:
			if !haveAgent {
				continue
			}
			if value == "" {
				continue
			}
			current.rules = append(current.rules, robotsRule{
				allow:       key == constants.RobotsDirectiveAllow,
				pattern:     value,
				specificity: ruleSpecificity(value),
			})
			sawRule = true
		}
	}
	if err := lineScanner.Err(); err != nil {
		return robotsPolicy{}, err
	}
	flush()
	return policy, nil
}

func stripRobotsComment(line string) string {
	if index := strings.Index(line, constants.PatternComment); index >= 0 {
		line = line[:index]
	}
	return strings.TrimSpace(line)
}

func matchingAgentScore(agents []string, userAgent string) int {
	userAgent = strings.ToLower(userAgent)
	best := 0
	for _, agent := range agents {
		switch {
		case agent == constants.PatternWildcard:
			if best == 0 {
				best = 1
			}
		case strings.Contains(userAgent, agent):
			if len(agent)+1 > best {
				best = len(agent) + 1
			}
		}
	}
	return best
}

func robotsPathWithQuery(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path
}

func ruleSpecificity(pattern string) int {
	pattern = strings.ReplaceAll(pattern, constants.PatternWildcard, "")
	pattern = strings.ReplaceAll(pattern, constants.PatternAnchor, "")
	return len(pattern)
}

func ruleMatchesPath(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	anchored := strings.HasSuffix(pattern, constants.PatternAnchor)
	pattern = strings.TrimSuffix(pattern, constants.PatternAnchor)
	return wildcardMatch(pattern, path, anchored)
}

func wildcardMatch(pattern, value string, anchored bool) bool {
	if pattern == "" {
		return value == ""
	}

	parts := strings.Split(pattern, constants.PatternWildcard)
	position := 0
	for index, part := range parts {
		if part == "" {
			continue
		}
		found := strings.Index(value[position:], part)
		if found < 0 {
			return false
		}
		found += position
		if index == 0 && !strings.HasPrefix(pattern, constants.PatternWildcard) && found != 0 {
			return false
		}
		position = found + len(part)
	}

	if anchored && len(parts) > 0 {
		last := parts[len(parts)-1]
		return strings.HasSuffix(value, last)
	}
	if !strings.HasPrefix(pattern, constants.PatternWildcard) && parts[0] != "" && !strings.HasPrefix(value, parts[0]) {
		return false
	}
	return true
}
