// Package exif extracts metadata from image EXIF data.
package exif

import (
	"errors"
	"math"
	"os"
	"time"

	goexif "github.com/rwcarlsen/goexif/exif"
)

// Meta holds the EXIF metadata extracted from an image.
type Meta struct {
	TakenAt time.Time
	Lat     float64
	Lon     float64
	HasGPS  bool
}

// ErrNoEXIF is returned when a file has no readable EXIF data.
var ErrNoEXIF = errors.New("no EXIF data")

// Extract reads EXIF metadata from the file at path.
// Returns ErrNoEXIF for files without EXIF (e.g. HEIC, some PNGs).
func Extract(path string) (*Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	x, err := goexif.Decode(f)
	if err != nil {
		return nil, ErrNoEXIF
	}

	meta := &Meta{}

	if t, err := x.DateTime(); err == nil {
		meta.TakenAt = t
	}

	if lat, lon, err := x.LatLong(); err == nil && !isZeroCoord(lat, lon) {
		meta.Lat = lat
		meta.Lon = lon
		meta.HasGPS = true
	}

	return meta, nil
}

func isZeroCoord(lat, lon float64) bool {
	return math.Abs(lat) < 1e-6 && math.Abs(lon) < 1e-6
}
