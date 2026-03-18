package authority

import (
	"context"
	"sync"

	"iris/internal/constants"
)

// Config holds configuration for MemoryStore.
type Config struct {
	// MaxImageCountPerDomain is the maximum number of images a domain can have
	// for normalization purposes. Domains with this many images will have an
	// authority score of 1.0.
	MaxImageCountPerDomain int
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxImageCountPerDomain: constants.MaxImageCountPerDomain,
	}
}

// MemoryStore is an in-memory implementation of the Tracker interface.
// It tracks the number of images indexed from each domain and provides
// normalized authority scores.
//
// MemoryStore is safe for concurrent use.
type MemoryStore struct {
	mu      sync.RWMutex
	domains map[string]int
	config  *Config
}

// NewMemoryStore creates a new in-memory authority tracker.
func NewMemoryStore(config *Config) *MemoryStore {
	if config == nil {
		config = DefaultConfig()
	}
	return &MemoryStore{
		domains: make(map[string]int),
		config:  config,
	}
}

// RecordDomain records an image URL from a domain.
// It extracts the domain from the URL and increments the count for that domain.
func (m *MemoryStore) RecordDomain(ctx context.Context, url string) error {
	domain, err := ExtractDomain(url)
	if err != nil {
		// If we can't extract the domain, we just ignore this URL.
		// This is a design choice to avoid failing the entire indexing
		// process due to malformed URLs.
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.domains[domain]++
	return nil
}

// GetAuthority returns the authority score for a domain.
// The score is normalized between 0.0 and 1.0 based on the number of images
// indexed from that domain relative to MaxImageCountPerDomain.
//
// Formula: min(0.0, 1.0) = imageCount / maxImageCountPerDomain
//
// If the domain has not been seen before, it returns 0.0.
func (m *MemoryStore) GetAuthority(ctx context.Context, domain string) float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count, exists := m.domains[domain]
	if !exists {
		return 0.0
	}

	maxCount := m.config.MaxImageCountPerDomain
	if maxCount <= 0 {
		maxCount = 1
	}

	score := float32(count) / float32(maxCount)
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// GetDomainCount returns the raw count of images for a domain.
// This is useful for debugging and testing.
func (m *MemoryStore) GetDomainCount(ctx context.Context, domain string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.domains[domain]
}

// GetAllDomains returns a snapshot of all domains and their counts.
// This is useful for debugging and testing.
func (m *MemoryStore) GetAllDomains(ctx context.Context) map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]int, len(m.domains))
	for domain, count := range m.domains {
		result[domain] = count
	}
	return result
}

// Close releases any resources held by the tracker.
// For the in-memory implementation, this is a no-op.
func (m *MemoryStore) Close() error {
	return nil
}
