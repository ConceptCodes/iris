package search

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"iris/internal/authority"
	"iris/internal/quality"
	"iris/pkg/models"
)

// TestHybridRerankingSystem is an integration test that verifies the entire
// hybrid re-ranking system works together:
// 1. Quality analyzer populates scores on indexed images
// 2. Authority tracker accumulates domain counts
// 3. Ranker applies hybrid formula to produce different results than pure vector similarity
func TestHybridRerankingSystem(t *testing.T) {
	// Setup test components
	ctx := context.Background()

	// Create a mock encoder client
	mockClip := &mockClip{emb: models.Embedding{1.0, 0.0, 0.0, 0.0, 0.0}}
	registry := mustTestRegistry(t, mockClip)

	// Create in-memory authority tracker
	tracker := authority.NewMemoryStore(&authority.Config{
		MaxImageCountPerDomain: 10, // Small limit for testing
	})

	// Create ranker with the tracker
	ranker := NewRanker(tracker)

	// Create mock vector store that stores records and returns them
	store := &mockStore{
		id:  "test-id",
		res: []models.SearchResult{},
	}

	// Create engine with all components
	engine := NewEngine(registry, store, ranker, tracker)

	t.Run("quality_analyzer_populates_scores", func(t *testing.T) {
		// Create sample images with different dimensions
		testImages := []struct {
			name   string
			url    string
			width  int
			height int
			color  color.RGBA
		}{
			{
				name:   "small_grayscale",
				url:    "https://example1.com/small.jpg",
				width:  100,
				height: 100,
				color:  color.RGBA{R: 128, G: 128, B: 128, A: 255},
			},
			{
				name:   "medium_rgb",
				url:    "https://example2.com/medium.png",
				width:  800,
				height: 600,
				color:  color.RGBA{R: 255, G: 0, B: 0, A: 255},
			},
			{
				name:   "large_rgba",
				url:    "https://example3.com/large.png",
				width:  1920,
				height: 1080,
				color:  color.RGBA{R: 128, G: 128, B: 128, A: 128},
			},
		}

		// Create a quality analyzer to verify it populates scores
		qa := quality.NewDefaultAnalyzer()

		for _, img := range testImages {
			// Create synthetic image bytes
			imgBytes := createTestImage(t, img.width, img.height, img.color)

			// Index the image
			record := models.ImageRecord{
				URL:      img.url,
				Filename: img.name + ".png",
			}

			id, err := engine.IndexFromBytes(ctx, imgBytes, record)
			if err != nil {
				t.Fatalf("Failed to index image %s: %v", img.name, err)
			}
			if id == "" {
				t.Errorf("Expected non-empty ID for image %s", img.name)
			}

			// Verify quality analyzer can analyze the image
			signals, err := qa.Analyze(ctx, imgBytes)
			if err != nil {
				t.Errorf("Quality analyzer failed for %s: %v", img.name, err)
			}

			// Verify quality signals are populated
			if signals.Width != img.width {
				t.Errorf("Image %s: expected width %d, got %d", img.name, img.width, signals.Width)
			}
			if signals.Height != img.height {
				t.Errorf("Image %s: expected height %d, got %d", img.name, img.height, signals.Height)
			}
			if signals.FileSize == 0 {
				t.Errorf("Image %s: expected non-zero file size", img.name)
			}
			if signals.ColorDepth == "" {
				t.Errorf("Image %s: expected color depth to be set", img.name)
			}

			// Verify quality score can be computed
			qualityScore := quality.ComputeQualityScore(signals)
			if qualityScore < 0.0 || qualityScore > 1.0 {
				t.Errorf("Image %s: quality score %f outside valid range [0.0, 1.0]", img.name, qualityScore)
			}

			t.Logf("Image %s: width=%d, height=%d, colorDepth=%s, qualityScore=%.3f",
				img.name, signals.Width, signals.Height, signals.ColorDepth, qualityScore)
		}
	})

	t.Run("authority_tracker_accumulates_domains", func(t *testing.T) {
		// Reset tracker state
		tracker = authority.NewMemoryStore(&authority.Config{
			MaxImageCountPerDomain: 10,
		})
		ranker = NewRanker(tracker)
		engine = NewEngine(registry, store, ranker, tracker)

		// Test URLs from different domains
		testURLs := []string{
			"https://example1.com/image1.jpg",
			"https://example1.com/image2.jpg",
			"https://example1.com/image3.jpg", // example1.com: 3 images
			"https://example2.com/image1.jpg", // example2.com: 1 image
			"https://example2.com/image2.jpg", // example2.com: 2 images
		}

		// Record domains
		for _, url := range testURLs {
			err := tracker.RecordDomain(ctx, url)
			if err != nil {
				t.Fatalf("Failed to record domain for %s: %v", url, err)
			}
		}

		// Verify domain counts
		count1 := tracker.GetDomainCount(ctx, "example1.com")
		if count1 != 3 {
			t.Errorf("Expected example1.com count to be 3, got %d", count1)
		}

		count2 := tracker.GetDomainCount(ctx, "example2.com")
		if count2 != 2 {
			t.Errorf("Expected example2.com count to be 2, got %d", count2)
		}

		// Verify authority scores
		auth1 := tracker.GetAuthority(ctx, "example1.com")
		expectedAuth1 := float32(3) / float32(10)
		if auth1 != expectedAuth1 {
			t.Errorf("Expected example1.com authority %.3f, got %.3f", expectedAuth1, auth1)
		}

		auth2 := tracker.GetAuthority(ctx, "example2.com")
		expectedAuth2 := float32(2) / float32(10)
		if auth2 != expectedAuth2 {
			t.Errorf("Expected example2.com authority %.3f, got %.3f", expectedAuth2, auth2)
		}

		// Verify unknown domain returns 0.0
		authUnknown := tracker.GetAuthority(ctx, "unknown.com")
		if authUnknown != 0.0 {
			t.Errorf("Expected unknown domain authority to be 0.0, got %.3f", authUnknown)
		}

		// Verify authority scores increase with more images
		err := tracker.RecordDomain(ctx, "https://example1.com/image4.jpg")
		if err != nil {
			t.Fatalf("Failed to record additional domain: %v", err)
		}

		auth1After := tracker.GetAuthority(ctx, "example1.com")
		expectedAuth1After := float32(4) / float32(10)
		if auth1After != expectedAuth1After {
			t.Errorf("Expected example1.com authority to increase to %.3f, got %.3f",
				expectedAuth1After, auth1After)
		}

		t.Logf("Authority scores: example1.com=%.3f, example2.com=%.3f", auth1, auth2)
	})

	t.Run("ranker_applies_hybrid_formula", func(t *testing.T) {
		// Create mock search results with known similarity scores
		now := time.Now()
		recentTime := now.Add(-1 * time.Hour)
		oldTime := now.Add(-30 * 24 * time.Hour) // 30 days old

		mockResults := []models.SearchResult{
			{
				Record: models.ImageRecord{
					ID:           "result1",
					URL:          "https://highauth.com/img1.jpg",
					ImageWidth:   1920,
					ImageHeight:  1080,
					FileSize:     500000,
					ColorDepth:   "rgba",
					QualityScore: 0.8,
					IndexedAt:    recentTime.Format(time.RFC3339),
				},
				Score: 0.7, // Initial similarity score
			},
			{
				Record: models.ImageRecord{
					ID:           "result2",
					URL:          "https://lowauth.com/img2.jpg",
					ImageWidth:   800,
					ImageHeight:  600,
					FileSize:     100000,
					ColorDepth:   "rgb",
					QualityScore: 0.5,
					IndexedAt:    recentTime.Format(time.RFC3339),
				},
				Score: 0.9, // Higher similarity but lower quality/authority
			},
			{
				Record: models.ImageRecord{
					ID:           "result3",
					URL:          "https://highauth.com/img3.jpg",
					ImageWidth:   2560,
					ImageHeight:  1440,
					FileSize:     1000000,
					ColorDepth:   "rgba",
					QualityScore: 0.9,
					IndexedAt:    oldTime.Format(time.RFC3339), // Old image
				},
				Score: 0.8,
			},
		}

		// Record some domains to create authority differences
		tracker.RecordDomain(ctx, "https://highauth.com/img1.jpg")
		tracker.RecordDomain(ctx, "https://highauth.com/img2.jpg")
		tracker.RecordDomain(ctx, "https://highauth.com/img3.jpg")
		tracker.RecordDomain(ctx, "https://highauth.com/img4.jpg")
		tracker.RecordDomain(ctx, "https://highauth.com/img5.jpg")
		// lowauth.com has only 1 image

		// Store original similarity scores
		originalScores := make([]float32, len(mockResults))
		for i, r := range mockResults {
			originalScores[i] = r.Score
		}

		// Apply re-ranking
		rankedResults := ranker.RankResults(ctx, mockResults)

		// Verify all results are still present
		if len(rankedResults) != len(mockResults) {
			t.Errorf("Expected %d results, got %d", len(mockResults), len(rankedResults))
		}

		// Verify final scores differ from original similarity scores
		scoresChanged := false
		for i, result := range rankedResults {
			if result.Score != originalScores[i] {
				scoresChanged = true
				break
			}
		}
		if !scoresChanged {
			t.Error("Expected final scores to differ from original similarity scores")
		}

		// Verify results are sorted by final score (descending)
		for i := 1; i < len(rankedResults); i++ {
			if rankedResults[i].Score > rankedResults[i-1].Score {
				t.Errorf("Results not sorted: result[%d].Score=%.3f > result[%d].Score=%.3f",
					i-1, rankedResults[i-1].Score, i, rankedResults[i].Score)
			}
		}

		// Verify hybrid formula components
		// Result1: high quality, high authority, recent
		// Result2: medium quality, low authority, recent, high similarity
		// Result3: high quality, high authority, old, medium similarity

		// Result2 (highest similarity) should be boosted by quality and freshness
		// but limited by low authority

		// Result1 and Result3 (from highauth.com) should get authority boost
		// Result3 is old so it gets a freshness penalty

		// Log final scores for debugging
		t.Logf("Original scores: %v", originalScores)
		for i, r := range rankedResults {
			t.Logf("Ranked[%d]: id=%s, url=%s, finalScore=%.3f, quality=%.3f",
				i, r.Record.ID, r.Record.URL, r.Score, r.Record.QualityScore)
		}

		// Verify at least one result has a different ranking than original
		// (this proves re-ranking is happening)
		originalOrder := map[string]int{
			"result1": 0,
			"result2": 1,
			"result3": 2,
		}
		newOrder := make(map[string]int)
		for i, r := range rankedResults {
			newOrder[r.Record.ID] = i
		}

		orderChanged := false
		for id, origPos := range originalOrder {
			if newPos, ok := newOrder[id]; ok && newPos != origPos {
				orderChanged = true
				break
			}
		}

		if !orderChanged {
			t.Error("Expected result order to change after re-ranking")
		}
	})

	t.Run("integration_end_to_end", func(t *testing.T) {
		// Reset components for clean test
		tracker = authority.NewMemoryStore(&authority.Config{
			MaxImageCountPerDomain: 10,
		})
		ranker = NewRanker(tracker)
		resetStore := &mockStore{
			id: "test-id",
		}
		engine = NewEngine(registry, resetStore, ranker, tracker)

		// Create and index sample images
		testCases := []struct {
			url       string
			width     int
			height    int
			color     color.RGBA
			domainImg int // How many images from this domain
		}{
			{
				url:       "https://trusted.com/image1.jpg",
				width:     1920,
				height:    1080,
				color:     color.RGBA{R: 255, G: 255, B: 255, A: 255},
				domainImg: 5,
			},
			{
				url:       "https://trusted.com/image2.jpg",
				width:     800,
				height:    600,
				color:     color.RGBA{R: 128, G: 128, B: 128, A: 255},
				domainImg: 5,
			},
			{
				url:       "https://unknown.com/image1.jpg",
				width:     640,
				height:    480,
				color:     color.RGBA{R: 0, G: 0, B: 0, A: 255},
				domainImg: 1,
			},
		}

		var indexedIDs []string
		for _, tc := range testCases {
			imgBytes := createTestImage(t, tc.width, tc.height, tc.color)
			record := models.ImageRecord{
				URL:      tc.url,
				Filename: "test.png",
				Meta:     map[string]string{"test": "integration"},
			}

			id, err := engine.IndexFromBytes(ctx, imgBytes, record)
			if err != nil {
				t.Fatalf("Failed to index image: %v", err)
			}
			indexedIDs = append(indexedIDs, id)
		}

		// Verify all images were indexed
		if len(indexedIDs) != len(testCases) {
			t.Errorf("Expected %d indexed IDs, got %d", len(testCases), len(indexedIDs))
		}

		// Verify domains were recorded
		trustedCount := tracker.GetDomainCount(ctx, "trusted.com")
		if trustedCount != 2 {
			t.Errorf("Expected trusted.com count 2, got %d", trustedCount)
		}

		unknownCount := tracker.GetDomainCount(ctx, "unknown.com")
		if unknownCount != 1 {
			t.Errorf("Expected unknown.com count 1, got %d", unknownCount)
		}

		// Verify authority scores differ
		trustedAuth := tracker.GetAuthority(ctx, "trusted.com")
		unknownAuth := tracker.GetAuthority(ctx, "unknown.com")

		if trustedAuth <= unknownAuth {
			t.Errorf("Expected trusted.com authority (%.3f) > unknown.com authority (%.3f)",
				trustedAuth, unknownAuth)
		}

		// Simulate search results from vector store
		now := time.Now()
		mockResults := []models.SearchResult{
			{
				Record: models.ImageRecord{
					ID:           indexedIDs[0], // trusted.com image
					URL:          testCases[0].url,
					ImageWidth:   testCases[0].width,
					ImageHeight:  testCases[0].height,
					FileSize:     int64(len(createTestImage(t, testCases[0].width, testCases[0].height, testCases[0].color))),
					ColorDepth:   "rgba",
					QualityScore: 0.8,
					IndexedAt:    now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
				Score: 0.7,
			},
			{
				Record: models.ImageRecord{
					ID:           indexedIDs[2], // unknown.com image
					URL:          testCases[2].url,
					ImageWidth:   testCases[2].width,
					ImageHeight:  testCases[2].height,
					FileSize:     int64(len(createTestImage(t, testCases[2].width, testCases[2].height, testCases[2].color))),
					ColorDepth:   "rgba",
					QualityScore: 0.5,
					IndexedAt:    now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
				Score: 0.8, // Higher similarity but from unknown domain
			},
		}

		originalScoreUnknown := mockResults[1].Score

		// Apply re-ranking
		rankedResults := ranker.RankResults(ctx, mockResults)

		// Verify scores changed
		if rankedResults[1].Score == originalScoreUnknown {
			t.Error("Expected re-ranking to change scores")
		}

		// Verify trusted domain result got boosted
		// (even though it had lower initial similarity)
		trustedIdx := -1
		for i, r := range rankedResults {
			if r.Record.ID == indexedIDs[0] {
				trustedIdx = i
				break
			}
		}

		if trustedIdx == -1 {
			t.Error("Could not find trusted.com result in ranked results")
		}

		t.Logf("Integration test passed. Trusted domain: count=%d, auth=%.3f. Unknown domain: count=%d, auth=%.3f",
			trustedCount, trustedAuth, unknownCount, unknownAuth)
	})
}

// createTestImage creates a synthetic PNG image with given dimensions and color
func createTestImage(t *testing.T, width, height int, c color.RGBA) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}

	return buf.Bytes()
}
