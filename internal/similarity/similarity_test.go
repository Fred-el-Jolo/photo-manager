package similarity_test

import (
	"math/bits"
	"testing"
	"time"

	"github.com/jolo/photo-manager/internal/similarity"
)

var baseTime = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

func makePhoto(path string, phash uint64, secsOffset int, blur float64) similarity.Photo {
	return similarity.Photo{
		Path:      path,
		PHash:     phash,
		TakenAt:   baseTime.Add(time.Duration(secsOffset) * time.Second),
		BlurScore: blur,
	}
}

func TestCluster_Empty(t *testing.T) {
	groups, singletons := similarity.Cluster(nil, 10)
	if len(groups) != 0 || len(singletons) != 0 {
		t.Errorf("expected 0 groups 0 singletons, got %d/%d", len(groups), len(singletons))
	}
}

func TestCluster_Single(t *testing.T) {
	photos := []similarity.Photo{makePhoto("a.jpg", 0, 0, 1.0)}
	groups, singletons := similarity.Cluster(photos, 10)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
	if len(singletons) != 1 || singletons[0] != "a.jpg" {
		t.Errorf("expected singleton [a.jpg], got %v", singletons)
	}
}

func TestCluster_IdenticalHashes(t *testing.T) {
	// dist=0 ≤ threshold → group; timestamps 1000s apart to avoid temporal union
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 1.0),
		makePhoto("b.jpg", 0, 1000, 1.0),
	}
	groups, singletons := similarity.Cluster(photos, 10)
	if len(groups) != 1 || len(singletons) != 0 {
		t.Errorf("expected 1 group 0 singletons, got %d/%d", len(groups), len(singletons))
	}
	if len(groups[0].Paths) != 2 {
		t.Errorf("expected group of 2, got %d", len(groups[0].Paths))
	}
}

func TestCluster_AtThreshold(t *testing.T) {
	// bits.OnesCount64(1023) == 10 == threshold
	hash := uint64(1023)
	if bits.OnesCount64(hash) != 10 {
		t.Fatalf("test setup: 1023 has %d bits, expected 10", bits.OnesCount64(hash))
	}
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 1.0),
		makePhoto("b.jpg", hash, 1000, 1.0),
	}
	groups, _ := similarity.Cluster(photos, 10)
	if len(groups) != 1 {
		t.Errorf("expected 1 group at threshold, got %d", len(groups))
	}
}

func TestCluster_OverThreshold(t *testing.T) {
	// bits.OnesCount64(2047) == 11 > threshold; timestamps far apart → no temporal union
	hash := uint64(2047)
	if bits.OnesCount64(hash) != 11 {
		t.Fatalf("test setup: 2047 has %d bits, expected 11", bits.OnesCount64(hash))
	}
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 1.0),
		makePhoto("b.jpg", hash, 1000, 1.0),
	}
	groups, singletons := similarity.Cluster(photos, 10)
	if len(groups) != 0 || len(singletons) != 2 {
		t.Errorf("expected 0 groups 2 singletons, got %d/%d", len(groups), len(singletons))
	}
}

func TestCluster_BurstTemporal(t *testing.T) {
	// 25 bits set → distance 25 >> threshold, but 15s apart → burst window → group
	hashB := uint64(1<<25 - 1)
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 1.0),
		makePhoto("b.jpg", hashB, 15, 1.0),
	}
	groups, singletons := similarity.Cluster(photos, 10)
	if len(groups) != 1 || len(singletons) != 0 {
		t.Errorf("expected 1 group via temporal, got %d groups %d singletons", len(groups), len(singletons))
	}
}

func TestCluster_Transitivity(t *testing.T) {
	// dist(A,B)=10, dist(B,C)=1, dist(A,C)=11
	// Union-Find unions A+B and B+C → all three in one component
	hashB := uint64(1023)        // dist(0, 1023) = 10
	hashC := uint64(1023 | 1024) // dist(1023, 3071) = 1; dist(0, 3071) = 11
	if bits.OnesCount64(0^hashB) != 10 {
		t.Fatalf("dist(A,B) != 10: got %d", bits.OnesCount64(0^hashB))
	}
	if bits.OnesCount64(hashB^hashC) != 1 {
		t.Fatalf("dist(B,C) != 1: got %d", bits.OnesCount64(hashB^hashC))
	}
	if bits.OnesCount64(0^hashC) != 11 {
		t.Fatalf("dist(A,C) != 11: got %d", bits.OnesCount64(0^hashC))
	}
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 1.0),
		makePhoto("b.jpg", hashB, 1000, 1.0),
		makePhoto("c.jpg", hashC, 2000, 1.0),
	}
	groups, singletons := similarity.Cluster(photos, 10)
	if len(groups) != 1 || len(singletons) != 0 {
		t.Errorf("expected 1 group via transitivity, got %d groups %d singletons", len(groups), len(singletons))
	}
	if len(groups[0].Paths) != 3 {
		t.Errorf("expected group of 3, got %d", len(groups[0].Paths))
	}
}

func TestCluster_SuggestedKeeper(t *testing.T) {
	photos := []similarity.Photo{
		makePhoto("low.jpg", 0, 0, 5.0),
		makePhoto("high.jpg", 0, 0, 50.0),
		makePhoto("lowest.jpg", 0, 0, 1.0),
	}
	groups, _ := similarity.Cluster(photos, 10)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].SuggestedKeeper != "high.jpg" {
		t.Errorf("expected SuggestedKeeper=high.jpg, got %s", groups[0].SuggestedKeeper)
	}
}

func TestCluster_AllZeroBlur(t *testing.T) {
	photos := []similarity.Photo{
		makePhoto("a.jpg", 0, 0, 0),
		makePhoto("b.jpg", 0, 0, 0),
	}
	groups, _ := similarity.Cluster(photos, 10)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].SuggestedKeeper == "" {
		t.Error("SuggestedKeeper should not be empty when all BlurScores are 0")
	}
}
