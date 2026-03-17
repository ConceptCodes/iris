package quality

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestGrayscaleDetection(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Gray{Y: 128})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Failed to encode grayscale image: %v", err)
	}

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), buf.Bytes())

	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if signals.ColorDepth != "grayscale" {
		t.Errorf("Expected color depth 'grayscale', got '%s'", signals.ColorDepth)
	}

	if signals.Width != 1 || signals.Height != 1 {
		t.Errorf("Expected dimensions 1x1, got %dx%d", signals.Width, signals.Height)
	}
}

func TestRGBDetection(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Failed to encode RGB image: %v", err)
	}

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), buf.Bytes())

	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if signals.ColorDepth != "rgba" {
		t.Errorf("Expected color depth 'rgba', got '%s'", signals.ColorDepth)
	}

	if signals.Width != 1 || signals.Height != 1 {
		t.Errorf("Expected dimensions 1x1, got %dx%d", signals.Width, signals.Height)
	}
}

func TestRGBADetection(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 128, G: 128, B: 128, A: 128})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Failed to encode RGBA image: %v", err)
	}

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), buf.Bytes())

	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if signals.ColorDepth != "rgba" {
		t.Errorf("Expected color depth 'rgba', got '%s'", signals.ColorDepth)
	}
}

func TestScoreNormalization(t *testing.T) {
	testCases := []struct {
		name     string
		signals  QualitySignals
		minScore float32
		maxScore float32
	}{
		{
			name: "High quality 4K RGBA",
			signals: QualitySignals{
				Width:        4000,
				Height:       4000,
				FileSize:     10000000,
				ColorDepth:   "rgba",
				EntropyScore: 0.9,
			},
			minScore: 0.0,
			maxScore: 1.0,
		},
		{
			name: "Low quality small grayscale",
			signals: QualitySignals{
				Width:        100,
				Height:       100,
				FileSize:     1000,
				ColorDepth:   "grayscale",
				EntropyScore: 0.1,
			},
			minScore: 0.0,
			maxScore: 1.0,
		},
		{
			name: "Medium quality HD RGB",
			signals: QualitySignals{
				Width:        1920,
				Height:       1080,
				FileSize:     500000,
				ColorDepth:   "rgb",
				EntropyScore: 0.5,
			},
			minScore: 0.0,
			maxScore: 1.0,
		},
		{
			name: "Zero dimensions",
			signals: QualitySignals{
				Width:        0,
				Height:       0,
				FileSize:     0,
				ColorDepth:   "rgb",
				EntropyScore: 0.0,
			},
			minScore: 0.0,
			maxScore: 1.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			score := ComputeQualityScore(tc.signals)

			if score < tc.minScore || score > tc.maxScore {
				t.Errorf("Score %f outside expected range [%f, %f]", score, tc.minScore, tc.maxScore)
			}
		})
	}
}

func TestResolutionFactor(t *testing.T) {
	testCases := []struct {
		name        string
		width       int
		height      int
		expectedMax float32
		expectedMin float32
	}{
		{
			name:        "4K resolution",
			width:       4000,
			height:      4000,
			expectedMax: 1.0,
			expectedMin: 1.0,
		},
		{
			name:        "HD resolution",
			width:       1920,
			height:      1080,
			expectedMax: 0.13,
			expectedMin: 0.12,
		},
		{
			name:        "Small resolution",
			width:       100,
			height:      100,
			expectedMax: 0.001,
			expectedMin: 0.000,
		},
		{
			name:        "Zero dimensions",
			width:       0,
			height:      0,
			expectedMax: 0.0,
			expectedMin: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			factor := computeResolutionFactor(tc.width, tc.height)

			if factor < tc.expectedMin || factor > tc.expectedMax {
				t.Errorf("Resolution factor %f outside expected range [%f, %f]",
					factor, tc.expectedMin, tc.expectedMax)
			}
		})
	}
}

func TestColorFactor(t *testing.T) {
	testCases := []struct {
		name           string
		colorDepth     string
		expectedFactor float32
	}{
		{
			name:           "Grayscale",
			colorDepth:     "grayscale",
			expectedFactor: 0.33,
		},
		{
			name:           "RGB",
			colorDepth:     "rgb",
			expectedFactor: 0.66,
		},
		{
			name:           "RGBA",
			colorDepth:     "rgba",
			expectedFactor: 1.0,
		},
		{
			name:           "Unknown defaults to RGB",
			colorDepth:     "unknown",
			expectedFactor: 0.66,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			factor := computeColorFactor(tc.colorDepth)

			if factor != tc.expectedFactor {
				t.Errorf("Expected color factor %f, got %f", tc.expectedFactor, factor)
			}
		})
	}
}

func TestGracefulDecodeError(t *testing.T) {
	invalidBytes := []byte{0x00, 0x01, 0x02, 0x03}

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), invalidBytes)

	if err != nil {
		t.Errorf("Expected no error on decode failure, got: %v", err)
	}

	if signals.Width != 0 || signals.Height != 0 {
		t.Errorf("Expected zero dimensions on decode failure, got %dx%d",
			signals.Width, signals.Height)
	}
}

func TestEntropyRange(t *testing.T) {
	uniformImg := image.NewGray(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			uniformImg.Set(x, y, color.Gray{Y: 128})
		}
	}

	var buf1 bytes.Buffer
	if err := png.Encode(&buf1, uniformImg); err != nil {
		t.Fatalf("Failed to encode uniform image: %v", err)
	}

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), buf1.Bytes())

	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if signals.EntropyScore < 0.0 || signals.EntropyScore > 1.0 {
		t.Errorf("Entropy score %f outside valid range [0.0, 1.0]", signals.EntropyScore)
	}
}

func TestFileSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("Failed to encode image: %v", err)
	}

	expectedSize := int64(buf.Len())

	analyzer := NewDefaultAnalyzer()
	signals, err := analyzer.Analyze(context.Background(), buf.Bytes())

	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if signals.FileSize != expectedSize {
		t.Errorf("Expected file size %d, got %d", expectedSize, signals.FileSize)
	}
}

func TestGetResolutionFactor(t *testing.T) {
	factor := GetResolutionFactor(2000, 2000)

	if factor < 0.0 || factor > 1.0 {
		t.Errorf("GetResolutionFactor returned %f outside valid range [0.0, 1.0]", factor)
	}
}

func TestGetColorFactor(t *testing.T) {
	testCases := []struct {
		colorDepth     string
		expectedFactor float32
	}{
		{"grayscale", 0.33},
		{"rgb", 0.66},
		{"rgba", 1.0},
		{"unknown", 0.66},
	}

	for _, tc := range testCases {
		factor := GetColorFactor(tc.colorDepth)
		if factor != tc.expectedFactor {
			t.Errorf("GetColorFactor(%s) = %f, expected %f",
				tc.colorDepth, factor, tc.expectedFactor)
		}
	}
}
