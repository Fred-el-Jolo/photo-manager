// Package storage manages the photo library index persisted as JSON.
package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const indexFileName = "index.json"
const indexDir = ".photo-manager"

// PhotoMeta holds cached metadata for an imported photo.
type PhotoMeta struct {
	SHA256   string    `json:"sha256"`
	PHash    uint64    `json:"phash,omitempty"`
	TakenAt  time.Time `json:"taken_at,omitempty"`
	Lat      float64   `json:"lat,omitempty"`
	Lon      float64   `json:"lon,omitempty"`
	Location string    `json:"location,omitempty"` // folder-safe location name
	DestPath string    `json:"dest_path"`
}

// LibraryIndex is the in-memory + on-disk index of the photo library.
type LibraryIndex struct {
	mu      sync.Mutex
	Photos  map[string]*PhotoMeta `json:"photos"`  // dest path → meta
	Hashes  map[string]string     `json:"hashes"`  // SHA256 → dest path (dedup lookup)
	libRoot string
}

// Load reads or creates the library index from disk.
func Load(libRoot string) (*LibraryIndex, error) {
	idx := &LibraryIndex{
		Photos:  make(map[string]*PhotoMeta),
		Hashes:  make(map[string]string),
		libRoot: libRoot,
	}
	path := idx.indexPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, idx); err != nil {
		return nil, err
	}
	return idx, nil
}

// Save writes the index to disk atomically.
func (idx *LibraryIndex) Save() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := idx.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, idx.indexPath())
}

// HasHash returns true if a file with the given SHA-256 is already in the library.
func (idx *LibraryIndex) HasHash(sha256 string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, ok := idx.Hashes[sha256]
	return ok
}

// Add registers a photo in the index (does not save to disk).
func (idx *LibraryIndex) Add(meta *PhotoMeta) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.Photos[meta.DestPath] = meta
	idx.Hashes[meta.SHA256] = meta.DestPath
}

// AllPHashes returns a map of dest path → pHash for all photos with a computed pHash.
func (idx *LibraryIndex) AllPHashes() map[string]uint64 {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	out := make(map[string]uint64, len(idx.Photos))
	for path, meta := range idx.Photos {
		if meta.PHash != 0 {
			out[path] = meta.PHash
		}
	}
	return out
}

// Remove deletes a photo entry from the index (does not save to disk).
func (idx *LibraryIndex) Remove(path string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if meta, ok := idx.Photos[path]; ok {
		delete(idx.Hashes, meta.SHA256)
		delete(idx.Photos, path)
	}
}

func (idx *LibraryIndex) indexPath() string {
	return filepath.Join(idx.libRoot, indexDir, indexFileName)
}
