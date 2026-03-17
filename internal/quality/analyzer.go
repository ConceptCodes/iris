// Package quality provides image quality analysis for ranking signals.
// It extracts dimensions, color depth, and entropy scores to evaluate
// image quality for search result relevance.
package quality

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

// QualityAnalyzer defines the interface for analyzing image quality.
type QualityAnalyzer interface {
	Analyze(ctx context.Context, imageBytes []byte) (QualitySignals, error)
}

// QualitySignals contains extracted quality metrics from an image.
type QualitySignals struct {
	Width        int     // Image width in pixels
	Height       int     // Image height in pixels
	FileSize     int64   // File size in bytes
	ColorDepth   string  // Color mode: "grayscale", "rgb", "rgba"
	EntropyScore float32 // Normalized entropy score (0.0-1.0)
}

// DefaultAnalyzer implements QualityAnalyzer using only standard library.
type DefaultAnalyzer struct{}

// NewDefaultAnalyzer creates a new DefaultAnalyzer instance.
func NewDefaultAnalyzer() *DefaultAnalyzer {
	return &DefaultAnalyzer{}
}

// Analyze decodes the image bytes and extracts quality signals.
func (a *DefaultAnalyzer) Analyze(ctx context.Context, imageBytes []byte) (QualitySignals, error) {
	// Return empty signals on decode errors (graceful fallback)
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return QualitySignals{}, nil
	}

	// Get image bounds
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Detect color mode
	colorDepth := detectColorMode(img)

	// Compute entropy
	entropyScore := computeEntropy(img, width, height)

	return QualitySignals{
		Width:        width,
		Height:       height,
		FileSize:     int64(len(imageBytes)),
		ColorDepth:   colorDepth,
		EntropyScore: entropyScore,
	}, nil
}

// detectColorMode determines if the image is grayscale, RGB, or RGBA.
func detectColorMode(img image.Image) string {
	switch img.(type) {
	case *image.Gray:
		return "grayscale"
	case *image.RGBA:
		return "rgba"
	case *image.NRGBA:
		return "rgba"
	case *image.RGBA64:
		return "rgba"
	case *image.NRGBA64:
		return "rgba"
	case *image.YCbCr:
		// JPEG YCbCr is typically RGB-equivalent for our purposes
		return "rgb"
	case *image.Paletted:
		// Palettes can contain any colors, treat as RGB for simplicity
		return "rgb"
	default:
		// Default to RGB for unknown image types
		return "rgb"
	}
}

// computeEntropy calculates a normalized entropy score based on pixel value distribution.
// Returns a value between 0.0 (low entropy/uniform) and 1.0 (high entropy/diverse).
func computeEntropy(img image.Image, width, height int) float32 {
	if width == 0 || height == 0 {
		return 0.0
	}

	// Build histogram of pixel intensities
	histogram := make(map[float32]int)

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()

			// Convert to normalized grayscale intensity (0.0-1.0)
			// Using standard luminance formula: 0.299*R + 0.587*G + 0.114*B
			intensity := (0.299*float32(r>>8) + 0.587*float32(g>>8) + 0.114*float32(b>>8)) / 255.0

			// Bucket into 256 levels for histogram
			bucket := float32(math.Floor(float64(intensity*255))) / 255.0
			histogram[bucket]++
		}
	}

	if len(histogram) == 0 {
		return 0.0
	}

	// Calculate Shannon entropy
	totalPixels := width * height
	var entropy float64

	for _, count := range histogram {
		probability := float64(count) / float64(totalPixels)
		if probability > 0 {
			entropy -= probability * math.Log2(probability)
		}
	}

	// Normalize entropy: maximum entropy for 256 levels is log2(256) = 8
	maxEntropy := 8.0
	normalizedEntropy := entropy / maxEntropy

	// Clamp to [0.0, 1.0]
	if normalizedEntropy > 1.0 {
		normalizedEntropy = 1.0
	}
	if normalizedEntropy < 0.0 {
		normalizedEntropy = 0.0
	}

	return float32(normalizedEntropy)
}
