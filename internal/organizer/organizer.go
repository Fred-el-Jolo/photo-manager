// Package organizer builds destination paths for imported photos.
package organizer

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/jolo/photo-manager/internal/exif"
)

// DestPath returns the destination path for a photo given its metadata.
//
// Structure: <libRoot>/<year>/<month_num>-<month_name>/<location>/<basename>
// Example:   library/2024/03-March/Paris/IMG_0001.jpg
//
// When EXIF is nil or TakenAt is zero, falls back to "Unknown" for date parts.
// When GPS is absent, location is "Unknown".
func DestPath(libRoot, basename string, meta *exif.Meta) string {
	year, month, location := "Unknown", "Unknown", "Unknown"

	if meta != nil && !meta.TakenAt.IsZero() {
		year = fmt.Sprintf("%d", meta.TakenAt.Year())
		month = fmt.Sprintf("%02d-%s", meta.TakenAt.Month(), meta.TakenAt.Month().String())
	}

	if meta != nil && meta.HasGPS {
		location = formatLocation(meta.Lat, meta.Lon)
	}

	return filepath.Join(libRoot, year, month, location, basename)
}

// formatLocation returns a folder-safe location string from GPS coordinates.
// Phase 1: uses a lat/lon label. Phase 2 will reverse-geocode to city name.
func formatLocation(lat, lon float64) string {
	latDir := "N"
	if lat < 0 {
		latDir = "S"
		lat = -lat
	}
	lonDir := "E"
	if lon < 0 {
		lonDir = "W"
		lon = -lon
	}
	return fmt.Sprintf("%.2f%s_%.2f%s", lat, latDir, lon, lonDir)
}

// FallbackTime returns a best-effort time from the file's mod time.
func FallbackTime(modTime time.Time) *exif.Meta {
	return &exif.Meta{TakenAt: modTime}
}
