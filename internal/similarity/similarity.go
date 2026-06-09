// Package similarity clusters photos by perceptual and temporal proximity.
package similarity

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"os"
	"sort"
	"time"

	"github.com/corona10/goimagehash"
)

const DefaultThreshold = 10
const burstWindow = 30 * time.Second

// Photo is the input record for the clusterer.
type Photo struct {
	Path      string
	PHash     uint64
	TakenAt   time.Time
	BlurScore float64
}

// Group is a cluster of perceptually similar photos.
type Group struct {
	Paths           []string `json:"paths"`
	SuggestedKeeper string   `json:"suggested_keeper"`
}

type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (uf *unionFind) find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x]) // path compression
	}
	return uf.parent[x]
}

func (uf *unionFind) union(x, y int) {
	rx, ry := uf.find(x), uf.find(y)
	if rx == ry {
		return
	}
	switch {
	case uf.rank[rx] < uf.rank[ry]:
		uf.parent[rx] = ry
	case uf.rank[rx] > uf.rank[ry]:
		uf.parent[ry] = rx
	default:
		uf.parent[ry] = rx
		uf.rank[rx]++
	}
}

// Cluster groups photos by perceptual similarity using Union-Find connected components.
//
// Two photos join the same group when either:
//   - hammingDistance(a.PHash, b.PHash) <= threshold (visual similarity)
//   - both TakenAt are non-zero and within 30s of each other (burst detection)
//
// Returns groups (≥2 members) and singletons (ungrouped paths).
// SuggestedKeeper is the path with the highest BlurScore; defaults to Paths[0] when all are 0.
func Cluster(photos []Photo, threshold int) (groups []Group, singletons []string) {
	if len(photos) == 0 {
		return nil, nil
	}

	uf := newUnionFind(len(photos))

	for i := 0; i < len(photos); i++ {
		for j := i + 1; j < len(photos); j++ {
			if hammingDistance(photos[i].PHash, photos[j].PHash) <= threshold {
				uf.union(i, j)
				continue
			}
			if !photos[i].TakenAt.IsZero() && !photos[j].TakenAt.IsZero() {
				diff := photos[i].TakenAt.Sub(photos[j].TakenAt)
				if diff < 0 {
					diff = -diff
				}
				if diff <= burstWindow {
					uf.union(i, j)
				}
			}
		}
	}

	// collect components keyed by root index
	components := make(map[int][]int)
	for i := range photos {
		root := uf.find(i)
		components[root] = append(components[root], i)
	}

	// Sort for deterministic output: sort members within each component by index,
	// then sort components by the path of their first (lowest-index) member.
	type component struct{ members []int }
	comps := make([]component, 0, len(components))
	for _, members := range components {
		sort.Ints(members)
		comps = append(comps, component{members})
	}
	sort.Slice(comps, func(a, b int) bool {
		return photos[comps[a].members[0]].Path < photos[comps[b].members[0]].Path
	})

	for _, comp := range comps {
		members := comp.members
		if len(members) == 1 {
			singletons = append(singletons, photos[members[0]].Path)
			continue
		}

		keeperIdx := members[0]
		maxBlur := photos[members[0]].BlurScore
		for _, idx := range members[1:] {
			if photos[idx].BlurScore > maxBlur {
				maxBlur = photos[idx].BlurScore
				keeperIdx = idx
			}
		}

		paths := make([]string, len(members))
		for i, idx := range members {
			paths[i] = photos[idx].Path
		}
		groups = append(groups, Group{
			Paths:           paths,
			SuggestedKeeper: photos[keeperIdx].Path,
		})
	}

	return groups, singletons
}

// HashFile computes the perceptual hash of the image at path.
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

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
