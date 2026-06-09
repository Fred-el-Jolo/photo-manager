package scanner

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeJPEG returns the bytes of a 1x1 RGBA JPEG.
func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// writeJPEG writes a fresh 1x1 JPEG to dir/name and returns its full path.
func writeJPEG(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestScan_Empty(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()

	sess, err := Scan(in, out, 10, nil)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if sess == nil {
		t.Fatal("Scan returned nil session")
	}
	if len(sess.Months) != 0 {
		t.Errorf("expected 0 months, got %d", len(sess.Months))
	}
	if sess.InputDir != in || sess.OutputDir != out {
		t.Errorf("dirs not set: in=%q out=%q", sess.InputDir, sess.OutputDir)
	}
	if sess.ScannedAt.IsZero() {
		t.Error("ScannedAt should be set")
	}
}

func TestScan_SinglePhoto(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	data := makeJPEG(t)
	writeJPEG(t, in, "photo.jpg", data)

	sess, err := Scan(in, out, 10, nil)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(sess.Months) != 1 {
		t.Fatalf("expected 1 month, got %d", len(sess.Months))
	}
	mg := sess.Months[0]
	if len(mg.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(mg.Groups))
	}
	g := mg.Groups[0]
	if len(g.Photos) != 1 {
		t.Fatalf("expected 1 photo, got %d", len(g.Photos))
	}
	ph := g.Photos[0]
	if ph.FileSize != int64(len(data)) {
		t.Errorf("FileSize: got %d want %d", ph.FileSize, len(data))
	}
	if ph.SHA256 == "" {
		t.Error("SHA256 should be populated")
	}
}

func TestScan_Progress(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	data := makeJPEG(t)
	for _, n := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		writeJPEG(t, in, n, data)
	}

	var calls [][2]int
	progress := func(done, total int) {
		calls = append(calls, [2]int{done, total})
	}

	_, err := Scan(in, out, 10, progress)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	// Expect: initial (0,3) plus one call per file (1,3),(2,3),(3,3).
	if len(calls) != 4 {
		t.Fatalf("expected 4 progress calls, got %d: %v", len(calls), calls)
	}
	if calls[0] != [2]int{0, 3} {
		t.Errorf("first call: got %v want [0 3]", calls[0])
	}
	last := calls[len(calls)-1]
	if last != [2]int{3, 3} {
		t.Errorf("last call: got %v want [3 3]", last)
	}
	// Every call must report total == 3 and done monotonic increasing.
	for i, c := range calls {
		if c[1] != 3 {
			t.Errorf("call %d total: got %d want 3", i, c[1])
		}
		if i > 0 && c[0] < calls[i-1][0] {
			t.Errorf("progress not monotonic at %d: %v then %v", i, calls[i-1], c)
		}
	}
}

func TestScan_GroupsByMonth(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	data := makeJPEG(t)

	// Synthetic JPEGs carry no EXIF, so takenAt falls back to mod time.
	// Set the two files to mod times one month apart.
	p1 := writeJPEG(t, in, "march.jpg", data)
	p2 := writeJPEG(t, in, "april.jpg", data)

	march := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	april := time.Date(2024, 4, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p1, march, march); err != nil {
		t.Fatalf("chtimes p1: %v", err)
	}
	if err := os.Chtimes(p2, april, april); err != nil {
		t.Fatalf("chtimes p2: %v", err)
	}

	sess, err := Scan(in, out, 10, nil)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(sess.Months) != 2 {
		t.Fatalf("expected 2 month groups, got %d", len(sess.Months))
	}
	// Sorted year ASC then month ASC: March (3) before April (4).
	if sess.Months[0].Month != 3 || sess.Months[1].Month != 4 {
		t.Errorf("months out of order: got %d then %d", sess.Months[0].Month, sess.Months[1].Month)
	}
	if sess.Months[0].Year != 2024 || sess.Months[1].Year != 2024 {
		t.Errorf("years wrong: got %d, %d", sess.Months[0].Year, sess.Months[1].Year)
	}
}

func TestScan_SimilarityGrouping(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	data := makeJPEG(t)

	// Three identical files -> identical pHash -> one similarity cluster.
	mod := time.Date(2024, 5, 10, 9, 0, 0, 0, time.UTC)
	for _, n := range []string{"x.jpg", "y.jpg", "z.jpg"} {
		p := writeJPEG(t, in, n, data)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}

	sess, err := Scan(in, out, 10, nil)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(sess.Months) != 1 {
		t.Fatalf("expected 1 month, got %d", len(sess.Months))
	}
	mg := sess.Months[0]
	if len(mg.Groups) != 1 {
		t.Fatalf("expected 1 group (all clustered), got %d", len(mg.Groups))
	}
	if len(mg.Groups[0].Photos) != 3 {
		t.Fatalf("expected 3 photos in the cluster, got %d", len(mg.Groups[0].Photos))
	}
}
