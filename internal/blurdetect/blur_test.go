package blurdetect_test

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/jolo/photo-manager/internal/blurdetect"
)

func TestSharpVsBlurry(t *testing.T) {
	sharp := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (x+y)%2 == 0 {
				sharp.Set(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				sharp.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}

	blurry := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			blurry.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}

	sharpScore := blurdetect.ScoreImage(sharp)
	blurryScore := blurdetect.ScoreImage(blurry)

	if sharpScore <= blurryScore*10 {
		t.Errorf("expected sharp score (%f) >> blurry score (%f)", sharpScore, blurryScore)
	}
}

func TestTooSmall(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if score := blurdetect.ScoreImage(img); score != 0 {
		t.Errorf("expected 0 for 2×2 image, got %f", score)
	}
}

func TestUniformGray(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 6, 6))
	gray := color.RGBA{128, 128, 128, 255}
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			img.Set(x, y, gray)
		}
	}
	if score := blurdetect.ScoreImage(img); score != 0 {
		t.Errorf("expected 0 for uniform gray, got %f", score)
	}
}

func TestScoreFile_BadPath(t *testing.T) {
	score, err := blurdetect.ScoreFile("/nonexistent/path/to/image.jpg")
	if err == nil {
		t.Error("expected error for bad path")
	}
	if score != 0 {
		t.Errorf("expected score 0 for bad path, got %f", score)
	}
}

func TestScoreFile_NotAnImage(t *testing.T) {
	f, err := os.CreateTemp("", "blurtest-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("this is not an image file")
	f.Close()

	score, err := blurdetect.ScoreFile(f.Name())
	if err != nil {
		t.Errorf("expected nil error for decode failure, got %v", err)
	}
	if score != 0 {
		t.Errorf("expected score 0 for non-image file, got %f", score)
	}
}
