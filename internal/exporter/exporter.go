// Package exporter copies curated photos to the output folder structure.
package exporter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolo/photo-manager/internal/session"
)

// ApplyGroup copies all photos in group to the output directory structure.
//   - Non-removed photos: <outputDir>/YYYY/YYYY-MM/YYYY-MM_<GroupName>/<filename>
//   - Removed photos:     <outputDir>/REMOVED/<filename>
//   - Rotation is applied at copy time (JPEG encode quality 92; non-JPEG falls back to byte copy).
//
// Sets group.Applied = true on success.
func ApplyGroup(group *session.Group, outputDir string) error {
	groupTime := firstKnownTime(group)

	for i := range group.Photos {
		photo := &group.Photos[i]
		filename := outputFilename(photo)

		var destDir string
		if photo.IsRemoved || photo.IsDuplicate {
			destDir = filepath.Join(outputDir, "REMOVED")
		} else {
			destDir = groupDir(outputDir, group.Name, groupTime)
		}

		dest := filepath.Join(destDir, filename)
		resolved, err := resolveCollision(dest, photo.SHA256)
		if err != nil {
			return fmt.Errorf("resolving collision for %s: %w", photo.Path, err)
		}
		if resolved == "" {
			continue // identical file already at destination
		}

		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return fmt.Errorf("creating output dir: %w", err)
		}
		if err := rotateAndCopy(photo.Path, resolved, photo.Rotation); err != nil {
			return fmt.Errorf("copying %s: %w", photo.Path, err)
		}
	}

	group.Applied = true
	return nil
}

func firstKnownTime(group *session.Group) time.Time {
	for _, p := range group.Photos {
		if !p.TakenAt.IsZero() {
			return p.TakenAt
		}
	}
	return time.Time{}
}

func outputFilename(photo *session.SessionPhoto) string {
	if photo.NewName != "" {
		ext := filepath.Ext(photo.NewName)
		if ext == "" {
			return photo.NewName + filepath.Ext(photo.Path)
		}
		return photo.NewName
	}
	return filepath.Base(photo.Path)
}

func groupDir(outputDir, groupName string, t time.Time) string {
	if t.IsZero() {
		return filepath.Join(outputDir, "Unknown", "Unknown", "Unknown_"+groupName)
	}
	year := fmt.Sprintf("%d", t.Year())
	month := fmt.Sprintf("%02d", int(t.Month()))
	ym := year + "-" + month
	return filepath.Join(outputDir, year, ym, ym+"_"+groupName)
}

// resolveCollision returns the destination path to use.
//   - ("", nil)     → identical file already at dest; caller should skip
//   - (dest, nil)   → dest is free
//   - (cand, nil)   → dest was occupied; cand is a free _N suffix
//   - ("", err)     → 999 collisions exhausted
func resolveCollision(dest, srcSHA256 string) (string, error) {
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest, nil
	}
	// File exists — skip if identical content
	if srcSHA256 != "" && sha256OfFile(dest) == srcSHA256 {
		return "", nil
	}
	ext := filepath.Ext(dest)
	base := strings.TrimSuffix(dest, ext)
	for i := 1; i <= 999; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("too many filename collisions at %s", dest)
}

func sha256OfFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f) //nolint:errcheck
	return hex.EncodeToString(h.Sum(nil))
}

func rotateAndCopy(src, dst string, rotation int) error {
	if rotation == 0 {
		return copyFile(src, dst)
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, decErr := image.Decode(f)
	f.Close()
	if decErr != nil {
		return copyFile(src, dst)
	}

	rotated := rotateImage(img, rotation)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return jpeg.Encode(out, rotated, &jpeg.Options{Quality: 92})
}

// rotateImage performs clockwise pixel rotation by degrees (90, 180, 270).
// Any other value returns img unchanged.
func rotateImage(img image.Image, degrees int) image.Image {
	b := img.Bounds()
	w := b.Max.X - b.Min.X
	h := b.Max.Y - b.Min.Y
	ox, oy := b.Min.X, b.Min.Y

	switch degrees {
	case 90: // CW: output is h×w; src(x,y) → dst(h-1-y, x)
		out := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(h-1-y, x, img.At(ox+x, oy+y))
			}
		}
		return out
	case 180: // src(x,y) → dst(w-1-x, h-1-y)
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(w-1-x, h-1-y, img.At(ox+x, oy+y))
			}
		}
		return out
	case 270: // CCW: output is h×w; src(x,y) → dst(y, w-1-x)
		out := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(y, w-1-x, img.At(ox+x, oy+y))
			}
		}
		return out
	}
	return img
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
