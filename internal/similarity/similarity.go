// Package similarity detects near-duplicate photos using perceptual hashing.
// Phase 2 stub — not yet wired into the import pipeline.
package similarity

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/corona10/goimagehash"
)

const DefaultThreshold = 10 // Hamming distance ≤ 10 → considered similar

// Group is a set of photo paths that are perceptually similar to each other.
type Group struct {
	Paths []string `json:"paths"`
}

// HashFile computes the perceptual hash (pHash) of the image at path.
func HashFile(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}

	h, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return 0, err
	}
	return h.GetHash(), nil
}

// GroupByHash clusters paths into groups where pHash distance ≤ threshold.
// Phase 2 will call this after import to find curation candidates.
func GroupByHash(hashes map[string]uint64, threshold int) []Group {
	visited := make(map[string]bool)
	var groups []Group

	paths := make([]string, 0, len(hashes))
	for p := range hashes {
		paths = append(paths, p)
	}

	for i, a := range paths {
		if visited[a] {
			continue
		}
		group := Group{Paths: []string{a}}
		for _, b := range paths[i+1:] {
			if visited[b] {
				continue
			}
			dist := hammingDistance(hashes[a], hashes[b])
			if dist <= threshold {
				group.Paths = append(group.Paths, b)
				visited[b] = true
			}
		}
		if len(group.Paths) > 1 {
			visited[a] = true
			groups = append(groups, group)
		}
	}
	return groups
}

func hammingDistance(a, b uint64) int {
	x := a ^ b
	count := 0
	for x != 0 {
		count += int(x & 1)
		x >>= 1
	}
	return count
}
