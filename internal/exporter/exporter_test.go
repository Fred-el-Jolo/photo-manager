package exporter_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jolo/photo-manager/internal/exporter"
	"github.com/jolo/photo-manager/internal/session"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyGroup_OutputPath(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "photo.jpg"), "fake-jpeg")

	group := &session.Group{
		ID:   "g1",
		Name: "nice-trip",
		Photos: []session.SessionPhoto{
			{
				Path:    filepath.Join(src, "photo.jpg"),
				SHA256:  "abc",
				TakenAt: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := exporter.ApplyGroup(group, dst); err != nil {
		t.Fatalf("ApplyGroup: %v", err)
	}
	if !group.Applied {
		t.Error("expected group.Applied = true")
	}

	expected := filepath.Join(dst, "2024", "2024-06", "2024-06_nice-trip", "photo.jpg")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected output at %s: %v", expected, err)
	}
}

func TestApplyGroup_RemovedGoToREMOVED(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "del.jpg"), "data")

	group := &session.Group{
		ID:   "g1",
		Name: "trip",
		Photos: []session.SessionPhoto{
			{
				Path:      filepath.Join(src, "del.jpg"),
				TakenAt:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				IsRemoved: true,
			},
		},
	}

	if err := exporter.ApplyGroup(group, dst); err != nil {
		t.Fatalf("ApplyGroup: %v", err)
	}
	expected := filepath.Join(dst, "REMOVED", "del.jpg")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected removed file at %s: %v", expected, err)
	}
}

func TestApplyGroup_SkipIdentical(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	content := "identical-content"
	srcFile := filepath.Join(src, "a.jpg")
	writeFile(t, srcFile, content)

	// Pre-populate destination with the same content
	dstDir := filepath.Join(dst, "2024", "2024-05", "2024-05_g")
	dstFile := filepath.Join(dstDir, "a.jpg")
	writeFile(t, dstFile, content)

	// sha256("identical-content") — pre-computed so the skip-identical path triggers
	h := "5966da499924a682b29310c6452f2a754f8ab0b7a5e33ade75e76648da19d01e"
	group := &session.Group{
		ID:   "g1",
		Name: "g",
		Photos: []session.SessionPhoto{
			{
				Path:    srcFile,
				SHA256:  h,
				TakenAt: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	// We only care that ApplyGroup doesn't error and doesn't create a _1 variant
	if err := exporter.ApplyGroup(group, dst); err != nil {
		t.Fatalf("ApplyGroup: %v", err)
	}
	// _1 variant should NOT exist
	if _, err := os.Stat(filepath.Join(dstDir, "a_1.jpg")); !os.IsNotExist(err) {
		t.Error("unexpected _1 collision file created")
	}
}

func TestApplyGroup_CollisionSuffix(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	srcFile := filepath.Join(src, "x.jpg")
	writeFile(t, srcFile, "new-content")

	// Pre-populate destination with DIFFERENT content to force collision suffix
	dstDir := filepath.Join(dst, "2024", "2024-07", "2024-07_g")
	writeFile(t, filepath.Join(dstDir, "x.jpg"), "old-content")

	group := &session.Group{
		ID:   "g1",
		Name: "g",
		Photos: []session.SessionPhoto{
			{
				Path:    srcFile,
				SHA256:  "different-hash",
				TakenAt: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := exporter.ApplyGroup(group, dst); err != nil {
		t.Fatalf("ApplyGroup: %v", err)
	}
	// _1 variant should exist
	if _, err := os.Stat(filepath.Join(dstDir, "x_1.jpg")); err != nil {
		t.Errorf("expected _1 collision file: %v", err)
	}
}

func TestApplyGroup_UnknownDate(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	srcFile := filepath.Join(src, "nodate.jpg")
	writeFile(t, srcFile, "data")

	group := &session.Group{
		ID:   "g1",
		Name: "mystery",
		Photos: []session.SessionPhoto{
			{Path: srcFile}, // zero TakenAt
		},
	}

	if err := exporter.ApplyGroup(group, dst); err != nil {
		t.Fatalf("ApplyGroup: %v", err)
	}
	expected := filepath.Join(dst, "Unknown", "Unknown", "Unknown_mystery", "nodate.jpg")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected unknown-date path at %s: %v", expected, err)
	}
}
