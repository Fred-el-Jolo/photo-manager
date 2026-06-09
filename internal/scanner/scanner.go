// Package scanner walks an input directory, extracts metadata for every image
// file, groups the photos by calendar month, runs similarity clustering within
// each month, and assembles a populated *session.Session ready for curation.
package scanner

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jolo/photo-manager/internal/blurdetect"
	"github.com/jolo/photo-manager/internal/dedup"
	"github.com/jolo/photo-manager/internal/exif"
	"github.com/jolo/photo-manager/internal/session"
	"github.com/jolo/photo-manager/internal/similarity"
)

var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".tiff": true, ".tif": true,
	".raw": true, ".cr2": true, ".nef": true, ".arw": true, ".webp": true,
}

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

type monthKey struct {
	year  int
	month int
}

type workResult struct {
	photo  scannedPhoto
	errMsg string // non-empty on failure
}

// processOne handles all per-photo work with a single image decode.
func processOne(path string) (scannedPhoto, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return scannedPhoto{}, err
	}
	modTime := fi.ModTime()

	sha, err := dedup.HashFile(path)
	if err != nil {
		return scannedPhoto{}, err
	}

	var takenAt time.Time
	meta, exifErr := exif.Extract(path)
	switch {
	case errors.Is(exifErr, exif.ErrNoEXIF):
		takenAt = modTime
	case exifErr != nil:
		takenAt = modTime
	default:
		takenAt = meta.TakenAt
		if takenAt.IsZero() {
			takenAt = modTime
		}
	}

	// Decode once; use for pHash, blur score, and dimensions.
	var phash uint64 = math.MaxUint64
	var blur float64
	var width, height int

	if f, openErr := os.Open(path); openErr == nil {
		img, _, decErr := image.Decode(f)
		f.Close()
		if decErr == nil {
			if h, hashErr := similarity.HashImage(img); hashErr == nil {
				phash = h
			}
			blur = blurdetect.ScoreImage(img)
			b := img.Bounds()
			width = b.Max.X - b.Min.X
			height = b.Max.Y - b.Min.Y
		}
	}

	return scannedPhoto{
		path:      path,
		sha256:    sha,
		phash:     phash,
		takenAt:   takenAt,
		width:     width,
		height:    height,
		fileSize:  fi.Size(),
		blurScore: blur,
	}, nil
}

func Scan(inputDir, outputDir string, threshold int, progress func(done, total int)) (*session.Session, error) {
	sess := session.New(inputDir, outputDir)

	var paths []string
	walkErr := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if imageExtensions[strings.ToLower(filepath.Ext(path))] {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scanning %s: %w", inputDir, walkErr)
	}

	if progress != nil {
		progress(0, len(paths))
	}

	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}

	jobs := make(chan string, len(paths))
	results := make(chan workResult, len(paths))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				sp, err := processOne(path)
				if err != nil {
					results <- workResult{errMsg: fmt.Sprintf("%s: %v", path, err)}
				} else {
					results <- workResult{photo: sp}
				}
			}
		}()
	}

	for _, p := range paths {
		jobs <- p
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var scanned []scannedPhoto
	errCount := 0
	done := 0
	for r := range results {
		done++
		if r.errMsg != "" {
			fmt.Fprintf(os.Stderr, "scanner: %s\n", r.errMsg)
			errCount++
		} else {
			scanned = append(scanned, r.photo)
		}
		if progress != nil {
			progress(done, len(paths))
		}
	}

	if errCount > 0 {
		fmt.Fprintf(os.Stderr, "scanner: %d file(s) failed to process\n", errCount)
	}

	// Sort by path so group order is deterministic across runs.
	sort.Slice(scanned, func(i, j int) bool {
		return scanned[i].path < scanned[j].path
	})

	byMonth := make(map[monthKey][]scannedPhoto)
	for _, sp := range scanned {
		key := monthKey{year: sp.takenAt.Year(), month: int(sp.takenAt.Month())}
		byMonth[key] = append(byMonth[key], sp)
	}

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

	sess.ScannedAt = time.Now()
	return sess, nil
}

func buildPhotos(paths []string, byPath map[string]scannedPhoto) []session.SessionPhoto {
	photos := make([]session.SessionPhoto, 0, len(paths))
	for _, p := range paths {
		sp, ok := byPath[p]
		if !ok {
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
