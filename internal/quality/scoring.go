package quality

// ComputeQualityScore calculates an overall quality score from quality signals.
// The score is a weighted combination of resolution, color depth, and entropy.
// Returns a value clamped to [0.0, 1.0].
func ComputeQualityScore(signals QualitySignals) float32 {
	// Resolution factor: normalize by 4000x4000 (1.0 at 4K)
	resFactor := computeResolutionFactor(signals.Width, signals.Height)

	// Color depth factor: grayscale=0.33, RGB=0.66, RGBA=1.0
	colorFactor := computeColorFactor(signals.ColorDepth)

	// Entropy factor: use computed entropy score directly (already normalized 0.0-1.0)
	entropyFactor := signals.EntropyScore

	// Weighted combination: 40% resolution, 30% color, 30% entropy
	quality := (resFactor * 0.4) + (colorFactor * 0.3) + (entropyFactor * 0.3)

	// Clamp to [0.0, 1.0]
	if quality > 1.0 {
		quality = 1.0
	}
	if quality < 0.0 {
		quality = 0.0
	}

	return quality
}

// computeResolutionFactor normalizes image resolution by 4000x4000.
// Returns 1.0 for 4K (4000x4000) or larger, scales down for lower resolutions.
func computeResolutionFactor(width, height int) float32 {
	if width <= 0 || height <= 0 {
		return 0.0
	}

	// Total pixels
	totalPixels := width * height

	// Reference: 4000x4000 = 16,000,000 pixels
	refPixels := 4000 * 4000

	// Normalize: actual pixels / reference pixels
	factor := float32(totalPixels) / float32(refPixels)

	// Clamp to [0.0, 1.0]
	if factor > 1.0 {
		factor = 1.0
	}
	if factor < 0.0 {
		factor = 0.0
	}

	return factor
}

// computeColorFactor assigns a factor based on color depth.
// grayscale=0.33, RGB=0.66, RGBA=1.0.
func computeColorFactor(colorDepth string) float32 {
	switch colorDepth {
	case "grayscale":
		return 0.33
	case "rgb":
		return 0.66
	case "rgba":
		return 1.0
	default:
		// Default to RGB for unknown color depths
		return 0.66
	}
}

// GetResolutionFactor returns the normalized resolution factor for the given dimensions.
// This is a convenience function for external use.
func GetResolutionFactor(width, height int) float32 {
	return computeResolutionFactor(width, height)
}

// GetColorFactor returns the color factor for the given color depth string.
// This is a convenience function for external use.
func GetColorFactor(colorDepth string) float32 {
	return computeColorFactor(colorDepth)
}
