// Package scanner walks an input directory, extracts metadata for every image
// file, groups the photos by calendar month, runs similarity clustering within
// each month, and assembles a populated *session.Session ready for curation.
package scanner

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jolo/photo-manager/internal/blurdetect"
	"github.com/jolo/photo-manager/internal/dedup"
	"github.com/jolo/photo-manager/internal/exif"
	"github.com/jolo/photo-manager/internal/session"
	"github.com/jolo/photo-manager/internal/similarity"
)

// imageExtensions is the set of accepted file extensions (lower-case).
var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".tiff": true, ".tif": true,
	".raw": true, ".cr2": true, ".nef": true, ".arw": true, ".webp": true,
}

// scannedPhoto is the intermediate record accumulated for each image during
// the walk, before grouping and clustering.
type scannedPhoto struct {
	path      string
	sha256    string
	phash     uint64
	takenAt   time.Time
	width     int
	height    int
	fileSize  int64
	blurScore float64
}

// monthKey identifies a calendar month.
type monthKey struct {
	year  int
	month int
}

// Scan walks inputDir recursively, extracts metadata for every image file,
// groups photos by calendar month, runs similarity clustering within each
// month, and returns a populated *session.Session.
//
// threshold is the Hamming distance used by similarity.Cluster; pass
// similarity.DefaultThreshold for the standard value.
// progress(done, total) is called after each file is processed (may be nil).
// The returned session is not persisted to disk — the caller is responsible
// for saving it.
func Scan(inputDir, outputDir string, threshold int, progress func(done, total int)) (*session.Session, error) {
	sess := session.New(inputDir, outputDir)

	// 1. Collect all image paths.
	var paths []string
	walkErr := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if imageExtensions[ext] {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scanning %s: %w", inputDir, walkErr)
	}

	// 2. Initial progress callback.
	if progress != nil {
		progress(0, len(paths))
	}

	// 3. Process each image.
	var scanned []scannedPhoto
	errCount := 0
	for i, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scanner: stat %s: %v\n", path, err)
			errCount++
			if progress != nil {
				progress(i+1, len(paths))
			}
			continue
		}
		modTime := fi.ModTime()

		sha, err := dedup.HashFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scanner: hash %s: %v\n", path, err)
			errCount++
			if progress != nil {
				progress(i+1, len(paths))
			}
			continue
		}

		var takenAt time.Time
		meta, err := exif.Extract(path)
		switch {
		case errors.Is(err, exif.ErrNoEXIF):
			takenAt = modTime
		case err != nil:
			fmt.Fprintf(os.Stderr, "scanner: exif %s: %v\n", path, err)
			takenAt = modTime
		default:
			takenAt = meta.TakenAt
			if takenAt.IsZero() {
				takenAt = modTime
			}
		}

		phash, phashErr := similarity.HashFile(path)
		if phashErr != nil {
			// Use sentinel so a failed hash never spuriously clusters with anything.
			phash = math.MaxUint64
		}
		blur, _ := blurdetect.ScoreFile(path) // best-effort, 0 on error
		width, height := imageDimensions(path)

		if progress != nil {
			progress(i+1, len(paths))
		}

		scanned = append(scanned, scannedPhoto{
			path:      path,
			sha256:    sha,
			phash:     phash,
			takenAt:   takenAt,
			width:     width,
			height:    height,
			fileSize:  fi.Size(),
			blurScore: blur,
		})
	}
	if errCount > 0 {
		fmt.Fprintf(os.Stderr, "scanner: %d file(s) failed to process\n", errCount)
	}

	// 4. Group scanned photos by (year, month), preserving walk order within.
	byMonth := make(map[monthKey][]scannedPhoto)
	for _, sp := range scanned {
		key := monthKey{year: sp.takenAt.Year(), month: int(sp.takenAt.Month())}
		byMonth[key] = append(byMonth[key], sp)
	}

	// 5. Build MonthGroups in deterministic order (year ASC, then month ASC).
	keys := make([]monthKey, 0, len(byMonth))
	for k := range byMonth {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].year != keys[b].year {
			return keys[a].year < keys[b].year
		}
		return keys[a].month < keys[b].month
	})

	for _, key := range keys {
		monthPhotos := byMonth[key]

		// Build similarity input, preserving order.
		simPhotos := make([]similarity.Photo, len(monthPhotos))
		for i, sp := range monthPhotos {
			simPhotos[i] = similarity.Photo{
				Path:      sp.path,
				PHash:     sp.phash,
				TakenAt:   sp.takenAt,
				BlurScore: sp.blurScore,
			}
		}

		groups, singletons := similarity.Cluster(simPhotos, threshold)

		// Index scanned photos by path for fast lookup when building groups.
		byPath := make(map[string]scannedPhoto, len(monthPhotos))
		for _, sp := range monthPhotos {
			byPath[sp.path] = sp
		}

		var sessionGroups []session.Group
		for _, g := range groups {
			sessionGroups = append(sessionGroups, session.Group{
				ID:              session.NewGroupID(),
				Name:            "",
				Photos:          buildPhotos(g.Paths, byPath),
				SuggestedKeeper: g.SuggestedKeeper,
			})
		}
		// Each singleton becomes its own group — merging them all into one creates
		// an unworkable blob when a month has many unique photos.
		for _, s := range singletons {
			sessionGroups = append(sessionGroups, session.Group{
				ID:     session.NewGroupID(),
				Name:   "",
				Photos: buildPhotos([]string{s}, byPath),
			})
		}

		sess.Months = append(sess.Months, session.MonthGroup{
			Year:   key.year,
			Month:  key.month,
			Groups: sessionGroups,
		})
	}

	// 6. Stamp scan time.
	sess.ScannedAt = time.Now()

	return sess, nil
}

// buildPhotos converts a slice of paths into SessionPhotos, looking up the
// scanned metadata for each. Walk/cluster order is preserved.
func buildPhotos(paths []string, byPath map[string]scannedPhoto) []session.SessionPhoto {
	photos := make([]session.SessionPhoto, 0, len(paths))
	for _, p := range paths {
		sp, ok := byPath[p]
		if !ok {
			// Defensive: cluster returned a path we never scanned.
			photos = append(photos, session.SessionPhoto{Path: p})
			continue
		}
		photos = append(photos, session.SessionPhoto{
			Path:      sp.path,
			SHA256:    sp.sha256,
			PHash:     sp.phash,
			TakenAt:   sp.takenAt,
			Width:     sp.width,
			Height:    sp.height,
			FileSize:  sp.fileSize,
			BlurScore: sp.blurScore,
		})
	}
	return photos
}

// imageDimensions returns the pixel width and height of the image at path, or
// 0,0 on any error (best-effort).
func imageDimensions(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
