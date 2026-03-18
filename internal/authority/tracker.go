// Package authority provides domain authority tracking for image search ranking.
// It tracks how many images come from each domain during indexing and provides
// authority scores at search time for re-ranking.
package authority

import "context"

// Tracker defines the interface for tracking domain authority.
// Implementations should be safe for concurrent use.
type Tracker interface {
	// RecordDomain records an image URL from a domain during indexing.
	// This should be called for each indexed image URL.
	RecordDomain(ctx context.Context, url string) error

	// GetAuthority returns the authority score for a domain.
	// Scores are normalized between 0.0 and 1.0, where higher values indicate
	// domains with more indexed images (higher authority).
	GetAuthority(ctx context.Context, domain string) float32

	// Close releases any resources held by the tracker.
	Close() error
}
