package authority

import (
	"context"
	"sync"
	"testing"
)

// TestExtractDomain tests the ExtractDomain function with various URL formats.
func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{
			name:        "HTTPS URL with path",
			url:         "https://example.com/image.jpg",
			expected:    "example.com",
			expectError: false,
		},
		{
			name:        "HTTP URL with subdomain and port",
			url:         "http://sub.example.com:8080/path",
			expected:    "sub.example.com",
			expectError: false,
		},
		{
			name:        "Mixed case URL",
			url:         "HTTP://EXAMPLE.COM/PATH",
			expected:    "example.com",
			expectError: false,
		},
		{
			name:        "Simple domain without scheme",
			url:         "example.com",
			expected:    "example.com",
			expectError: false,
		},
		{
			name:        "Domain with query parameters",
			url:         "https://example.com/image.jpg?q=test",
			expected:    "example.com",
			expectError: false,
		},
		{
			name:        "Domain with fragment",
			url:         "https://example.com/page#section",
			expected:    "example.com",
			expectError: false,
		},
		{
			name:        "Localhost",
			url:         "http://localhost:8080",
			expected:    "localhost",
			expectError: false,
		},
		{
			name:        "Empty URL",
			url:         "",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Malformed URL with scheme",
			url:         "http:///path",
			expected:    "",
			expectError: true,
		},
		{
			name:        "URL without domain",
			url:         "file:///path/to/file.jpg",
			expected:    "",
			expectError: true,
		},
		{
			name:        "FTP URL",
			url:         "ftp://files.example.com/download.zip",
			expected:    "files.example.com",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractDomain(tt.url)

			if tt.expectError {
				if err == nil {
					t.Errorf("ExtractDomain(%q) expected error, got nil", tt.url)
				}
			} else {
				if err != nil {
					t.Errorf("ExtractDomain(%q) unexpected error: %v", tt.url, err)
				}
				if result != tt.expected {
					t.Errorf("ExtractDomain(%q) = %q, want %q", tt.url, result, tt.expected)
				}
			}
		})
	}
}

// TestMemoryStore_RecordDomain tests recording domains during indexing.
func TestMemoryStore_RecordDomain(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(DefaultConfig())

	tests := []struct {
		name        string
		url         string
		expectError bool
	}{
		{
			name:        "Valid URL",
			url:         "https://example.com/image.jpg",
			expectError: false,
		},
		{
			name:        "Another valid URL from same domain",
			url:         "https://example.com/image2.jpg",
			expectError: false,
		},
		{
			name:        "URL from different domain",
			url:         "https://other.com/image.jpg",
			expectError: false,
		},
		{
			name:        "Malformed URL",
			url:         "",
			expectError: false, // We ignore malformed URLs, not error
		},
		{
			name:        "URL with invalid format",
			url:         "http:///path",
			expectError: false, // We ignore malformed URLs, not error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.RecordDomain(ctx, tt.url)
			if tt.expectError && err == nil {
				t.Errorf("RecordDomain(%q) expected error, got nil", tt.url)
			}
			if !tt.expectError && err != nil {
				t.Errorf("RecordDomain(%q) unexpected error: %v", tt.url, err)
			}
		})
	}

	// Verify counts
	count1 := store.GetDomainCount(ctx, "example.com")
	if count1 != 2 {
		t.Errorf("GetDomainCount(example.com) = %d, want 2", count1)
	}

	count2 := store.GetDomainCount(ctx, "other.com")
	if count2 != 1 {
		t.Errorf("GetDomainCount(other.com) = %d, want 1", count2)
	}

	// Verify unknown domain
	countUnknown := store.GetDomainCount(ctx, "unknown.com")
	if countUnknown != 0 {
		t.Errorf("GetDomainCount(unknown.com) = %d, want 0", countUnknown)
	}
}

// TestMemoryStore_GetAuthority tests authority score computation and normalization.
func TestMemoryStore_GetAuthority(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		MaxImageCountPerDomain: 100,
	}
	store := NewMemoryStore(config)

	// Record some domains
	urls := []string{
		"https://high.com/1.jpg",
		"https://high.com/2.jpg",
		"https://high.com/3.jpg",
		"https://medium.com/1.jpg",
		"https://low.com/1.jpg",
	}

	for _, url := range urls {
		if err := store.RecordDomain(ctx, url); err != nil {
			t.Fatalf("RecordDomain failed: %v", err)
		}
	}

	tests := []struct {
		name          string
		domain        string
		expectedScore float32
		tolerance     float32
		explanation   string
	}{
		{
			name:          "High authority domain",
			domain:        "high.com",
			expectedScore: 0.03, // 3 / 100 = 0.03
			tolerance:     0.001,
			explanation:   "3 images / 100 max = 0.03",
		},
		{
			name:          "Medium authority domain",
			domain:        "medium.com",
			expectedScore: 0.01, // 1 / 100 = 0.01
			tolerance:     0.001,
			explanation:   "1 image / 100 max = 0.01",
		},
		{
			name:          "Low authority domain",
			domain:        "low.com",
			expectedScore: 0.01, // 1 / 100 = 0.01
			tolerance:     0.001,
			explanation:   "1 image / 100 max = 0.01",
		},
		{
			name:          "Unknown domain",
			domain:        "unknown.com",
			expectedScore: 0.0,
			tolerance:     0.0,
			explanation:   "0 images = 0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := store.GetAuthority(ctx, tt.domain)
			if score < tt.expectedScore-tt.tolerance || score > tt.expectedScore+tt.tolerance {
				t.Errorf("GetAuthority(%q) = %f, want %f (%s)", tt.domain, score, tt.expectedScore, tt.explanation)
			}
		})
	}
}

// TestMemoryStore_GetAuthority_Capping tests that scores are capped at 1.0.
func TestMemoryStore_GetAuthority_Capping(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		MaxImageCountPerDomain: 10,
	}
	store := NewMemoryStore(config)

	// Record more than max images for a domain
	for i := 0; i < 20; i++ {
		url := "https://example.com/image.jpg"
		if err := store.RecordDomain(ctx, url); err != nil {
			t.Fatalf("RecordDomain failed: %v", err)
		}
	}

	// Score should be capped at 1.0
	score := store.GetAuthority(ctx, "example.com")
	if score != 1.0 {
		t.Errorf("GetAuthority with 20 images and max 10 = %f, want 1.0", score)
	}
}

// TestMemoryStore_ConcurrentAccess tests concurrent safety.
func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		MaxImageCountPerDomain: 1000,
	}
	store := NewMemoryStore(config)

	var wg sync.WaitGroup
	numGoroutines := 100
	urlsPerGoroutine := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < urlsPerGoroutine; j++ {
				url := "https://example.com/path"
				if err := store.RecordDomain(ctx, url); err != nil {
					t.Errorf("RecordDomain failed: %v", err)
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < urlsPerGoroutine; j++ {
				_ = store.GetAuthority(ctx, "example.com")
			}
		}()
	}

	wg.Wait()

	// Verify final count
	expectedCount := numGoroutines * urlsPerGoroutine
	actualCount := store.GetDomainCount(ctx, "example.com")
	if actualCount != expectedCount {
		t.Errorf("GetDomainCount = %d, want %d (concurrent writes may have been lost)", actualCount, expectedCount)
	}

	// Verify score is within bounds
	score := store.GetAuthority(ctx, "example.com")
	if score < 0.0 || score > 1.0 {
		t.Errorf("GetAuthority = %f, want between 0.0 and 1.0", score)
	}
}

// TestMemoryStore_GetAllDomains tests retrieving all domains.
func TestMemoryStore_GetAllDomains(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(DefaultConfig())

	// Record some domains
	urls := []string{
		"https://example.com/1.jpg",
		"https://example.com/2.jpg",
		"https://other.com/1.jpg",
	}

	for _, url := range urls {
		if err := store.RecordDomain(ctx, url); err != nil {
			t.Fatalf("RecordDomain failed: %v", err)
		}
	}

	// Get all domains
	domains := store.GetAllDomains(ctx)

	// Verify count
	if len(domains) != 2 {
		t.Errorf("GetAllDomains returned %d domains, want 2", len(domains))
	}

	// Verify specific domains
	if domains["example.com"] != 2 {
		t.Errorf("domains[example.com] = %d, want 2", domains["example.com"])
	}

	if domains["other.com"] != 1 {
		t.Errorf("domains[other.com] = %d, want 1", domains["other.com"])
	}
}

// TestMemoryStore_Close tests closing the store.
func TestMemoryStore_Close(t *testing.T) {
	store := NewMemoryStore(DefaultConfig())

	err := store.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// TestMemoryStore_ZeroMaxCount tests behavior with zero max count.
func TestMemoryStore_ZeroMaxCount(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		MaxImageCountPerDomain: 0,
	}
	store := NewMemoryStore(config)

	// Record a domain
	url := "https://example.com/image.jpg"
	if err := store.RecordDomain(ctx, url); err != nil {
		t.Fatalf("RecordDomain failed: %v", err)
	}

	// Score should be 1.0 (division by zero should be handled)
	score := store.GetAuthority(ctx, "example.com")
	if score < 0.0 || score > 1.0 {
		t.Errorf("GetAuthority with zero max count = %f, want between 0.0 and 1.0", score)
	}
}

// TestMemoryStore_NilConfig tests behavior with nil config.
func TestMemoryStore_NilConfig(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(nil) // Should use default config

	// Record a domain
	url := "https://example.com/image.jpg"
	if err := store.RecordDomain(ctx, url); err != nil {
		t.Fatalf("RecordDomain failed: %v", err)
	}

	// Should work with default max count (10000)
	score := store.GetAuthority(ctx, "example.com")
	if score < 0.0 || score > 1.0 {
		t.Errorf("GetAuthority = %f, want between 0.0 and 1.0", score)
	}
}
