// Package blurdetect measures image sharpness via Laplacian variance.
package blurdetect

import (
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// ScoreFile opens the image at path and returns its sharpness score.
// Returns (0, err) for OS errors. Returns (0, nil) for decode errors (best-effort).
func ScoreFile(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, nil // unknown format or corrupt — not fatal
	}
	return ScoreImage(img), nil
}

// ScoreImage computes the Laplacian variance sharpness score of an image.
// The Laplacian kernel [0,1,0; 1,-4,1; 0,1,0] amplifies edges; higher variance = sharper.
// Returns 0 for images smaller than 3×3 pixels.
func ScoreImage(img image.Image) float64 {
	bounds := img.Bounds()
	w := bounds.Max.X - bounds.Min.X
	h := bounds.Max.Y - bounds.Min.Y
	if w < 3 || h < 3 {
		return 0
	}

	// Build grayscale float64 grid using luminance conversion.
	gray := make([][]float64, h)
	for y := range gray {
		gray[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			c := color.GrayModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
			gray[y][x] = float64(c.Y) / 255.0
		}
	}

	// Apply Laplacian to interior pixels; skip the 1-px border.
	lap := make([]float64, 0, (h-2)*(w-2))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			v := -4*gray[y][x] + gray[y-1][x] + gray[y+1][x] + gray[y][x-1] + gray[y][x+1]
			lap = append(lap, v)
		}
	}

	// Variance of the Laplacian output.
	var sum float64
	for _, v := range lap {
		sum += v
	}
	mean := sum / float64(len(lap))
	var variance float64
	for _, v := range lap {
		d := v - mean
		variance += d * d
	}
	return variance / float64(len(lap))
}
