// Package importer orchestrates the full photo import pipeline.
package importer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jolo/photo-manager/internal/dedup"
	"github.com/jolo/photo-manager/internal/exif"
	"github.com/jolo/photo-manager/internal/organizer"
	"github.com/jolo/photo-manager/internal/similarity"
	"github.com/jolo/photo-manager/internal/storage"
)

// imageExtensions lists the file extensions treated as photos.
var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".tiff": true,
	".tif":  true,
	".raw":  true,
	".cr2":  true,
	".nef":  true,
	".arw":  true,
	".webp": true,
}

// Options configures an import run.
type Options struct {
	SourceDir string
	LibRoot   string
	Move      bool // if true, delete source after successful copy
	Verbose   bool
}

// Result summarises the outcome of an import run.
type Result struct {
	Imported   int
	Duplicates int
	Errors     int
}

// Run executes the full import pipeline for all photos found in opts.SourceDir.
func Run(opts Options) (*Result, error) {
	idx, err := storage.Load(opts.LibRoot)
	if err != nil {
		return nil, fmt.Errorf("loading library index: %w", err)
	}

	result := &Result{}

	err = filepath.WalkDir(opts.SourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isImage(path) {
			return nil
		}

		if importErr := importFile(path, opts, idx, result); importErr != nil {
			fmt.Fprintf(os.Stderr, "error importing %s: %v\n", path, importErr)
			result.Errors++
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	if err := idx.Save(); err != nil {
		return result, fmt.Errorf("saving library index: %w", err)
	}

	return result, nil
}

func importFile(srcPath string, opts Options, idx *storage.LibraryIndex, result *Result) error {
	// Step 1: hash for dedup
	hash, err := dedup.HashFile(srcPath)
	if err != nil {
		return fmt.Errorf("hashing: %w", err)
	}
	if idx.HasHash(hash) {
		if opts.Verbose {
			fmt.Printf("  skip (duplicate): %s\n", srcPath)
		}
		result.Duplicates++
		return nil
	}

	// Step 2: extract EXIF (best-effort; HEIC and others may lack it)
	meta, err := exif.Extract(srcPath)
	if err != nil {
		if !errors.Is(err, exif.ErrNoEXIF) {
			return fmt.Errorf("exif: %w", err)
		}
		// No EXIF: fall back to file mod time
		info, statErr := os.Stat(srcPath)
		if statErr != nil {
			return statErr
		}
		meta = organizer.FallbackTime(info.ModTime())
	}

	// Step 3: build destination path
	dest := organizer.DestPath(opts.LibRoot, filepath.Base(srcPath), meta)

	// Handle filename collisions
	dest = resolveCollision(dest)

	// Step 4: copy (or move) file
	if err := copyFile(srcPath, dest); err != nil {
		return fmt.Errorf("copying: %w", err)
	}
	if opts.Move {
		_ = os.Remove(srcPath)
	}

	// Step 5: compute perceptual hash (best-effort — skip on error)
	pHash, _ := similarity.HashFile(dest)

	// Step 6: update index
	entry := &storage.PhotoMeta{
		SHA256:   hash,
		PHash:    pHash,
		TakenAt:  meta.TakenAt,
		Lat:      meta.Lat,
		Lon:      meta.Lon,
		DestPath: dest,
	}
	if meta.HasGPS {
		entry.Location = "gps"
	}
	idx.Add(entry)

	if opts.Verbose {
		fmt.Printf("  imported: %s → %s\n", srcPath, dest)
	}
	result.Imported++
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
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

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// resolveCollision appends a counter suffix if dest already exists.
func resolveCollision(dest string) string {
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	ext := filepath.Ext(dest)
	base := strings.TrimSuffix(dest, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return dest
}

func isImage(path string) bool {
	return imageExtensions[strings.ToLower(filepath.Ext(path))]
}
