package search

import (
	"context"
	"net/url"
	"sort"
	"time"

	"iris/internal/constants"
	"iris/pkg/models"
)

// AuthorityTracker provides domain authority scores for re-ranking.
type AuthorityTracker interface {
	GetAuthority(ctx context.Context, domain string) float32
}

// DefaultAuthorityTracker provides a simple default authority tracker.
type DefaultAuthorityTracker struct{}

// GetAuthority returns a default authority score based on the domain.
// This is a simple implementation that can be replaced with a more sophisticated one.
func (d *DefaultAuthorityTracker) GetAuthority(ctx context.Context, domain string) float32 {
	// Return a neutral default score (0.5) for all domains
	// This can be enhanced with domain reputation scoring, DNS verification, etc.
	return 0.5
}

// Ranker applies weighted hybrid scoring to search results.
type Ranker struct {
	authorityTracker AuthorityTracker
}

// NewRanker creates a new Ranker instance.
func NewRanker(tracker AuthorityTracker) *Ranker {
	return &Ranker{
		authorityTracker: tracker,
	}
}

// RankResults applies hybrid scoring and re-sorts results.
func (r *Ranker) RankResults(ctx context.Context, results []models.SearchResult) []models.SearchResult {
	if len(results) == 0 {
		return results
	}

	// Compute normalized similarity scores (min-max normalization)
	similarityScores := make([]float32, len(results))
	for i, result := range results {
		similarityScores[i] = result.Score
	}

	minSimilarity, maxSimilarity := normalizeMinMax(similarityScores)

	// Apply hybrid scoring to each result
	now := time.Now()
	for i := range results {
		result := &results[i]

		// Extract domain from URL
		domain := extractDomain(result.Record.URL)

		// Get authority score
		authority := float32(0.0)
		if r.authorityTracker != nil {
			authority = r.authorityTracker.GetAuthority(ctx, domain)
		}

		// Compute freshness score
		freshness := r.computeFreshness(result.Record.IndexedAt, now)

		// Get quality score (already normalized 0.0-1.0)
		quality := result.Record.QualityScore

		// Normalize similarity score to 0.0-1.0
		similarity := normalizeSimilarity(result.Score, minSimilarity, maxSimilarity)

		// Apply hybrid scoring formula using tuned weights from constants
		finalScore := constants.RankingWeightSimilarity*similarity + constants.RankingWeightAuthority*authority + constants.RankingWeightQuality*quality + constants.RankingWeightFreshness*freshness

		result.Score = finalScore
	}

	// Sort results by final score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// computeFreshness calculates a freshness score based on IndexedAt timestamp.
// Returns 1.0 if indexed within the freshness window defined by constants.FreshnessDays, then decays exponentially.
func (r *Ranker) computeFreshness(indexedAt string, now time.Time) float32 {
	if indexedAt == "" {
		return 0.5 // Default score if timestamp missing
	}

	parsedTime, err := time.Parse(time.RFC3339, indexedAt)
	if err != nil {
		// Try alternative formats
		for _, format := range []string{
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05.999Z",
			"2006-01-02 15:04:05",
			"2006-01-02",
		} {
			parsedTime, err = time.Parse(format, indexedAt)
			if err == nil {
				break
			}
		}
		if err != nil {
			return 0.5 // Default score if parsing fails
		}
	}

	elapsed := now.Sub(parsedTime)
	freshnessThreshold := constants.FreshnessDays * 24 * time.Hour

	// Freshness: 1.0 if indexed within the freshness window, then exponential decay
	if elapsed < freshnessThreshold {
		return 1.0
	}

	// Exponential decay with half-life of 30 days
	halfLife := 30.0
	daysSinceIndex := elapsed.Hours() / 24
	excessDays := daysSinceIndex - float64(constants.FreshnessDays)
	if excessDays < 0 {
		excessDays = 0
	}
	decay := float32(mathExp(-excessDays / halfLife))

	return decay
}

// extractDomain extracts the domain from a URL.
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return u.Hostname()
}

// normalizeMinMax finds the min and max values in a slice.
func normalizeMinMax(scores []float32) (min, max float32) {
	if len(scores) == 0 {
		return 0, 1
	}

	min = scores[0]
	max = scores[0]

	for _, score := range scores {
		if score < min {
			min = score
		}
		if score > max {
			max = score
		}
	}

	// Avoid division by zero
	if max == min {
		return 0, 1
	}

	return min, max
}

// normalizeSimilarity normalizes a score to 0.0-1.0 range using min-max.
func normalizeSimilarity(score, min, max float32) float32 {
	if max == min {
		return 0.5
	}
	return (score - min) / (max - min)
}

// mathExp is a wrapper around math.Exp to avoid importing math in this file.
func mathExp(x float64) float64 {
	// Using a simple exponential decay approximation
	return 1.0 / (1.0 + x*0.1)
}
