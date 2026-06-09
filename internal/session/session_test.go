package session_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jolo/photo-manager/internal/session"
)

func makeTestSession(t *testing.T) (*session.Session, string) {
	t.Helper()
	dir := t.TempDir()
	s := session.New("/input", dir)
	s.ScannedAt = time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	s.Months = []session.MonthGroup{
		{
			Year:  2024,
			Month: 3,
			Groups: []session.Group{
				{
					ID:   "g1",
					Name: "nice-trip",
					Photos: []session.SessionPhoto{
						{Path: "/input/a.jpg", SHA256: "aaa"},
						{Path: "/input/b.jpg", SHA256: "bbb", IsRemoved: true},
					},
				},
			},
		},
	}
	return s, dir
}

func TestNew(t *testing.T) {
	s := session.New("/in", "/out")
	if s.InputDir != "/in" || s.OutputDir != "/out" {
		t.Errorf("unexpected New result: %+v", s)
	}
	if len(s.Months) != 0 {
		t.Error("expected empty Months")
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	s, _ := makeTestSession(t)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := session.Load(s.OutputDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.InputDir != s.InputDir {
		t.Errorf("InputDir: got %q want %q", loaded.InputDir, s.InputDir)
	}
	if len(loaded.Months) != 1 || len(loaded.Months[0].Groups) != 1 {
		t.Errorf("unexpected month/group count: %+v", loaded.Months)
	}
	if len(loaded.Months[0].Groups[0].Photos) != 2 {
		t.Errorf("expected 2 photos, got %d", len(loaded.Months[0].Groups[0].Photos))
	}
	if !loaded.Months[0].Groups[0].Photos[1].IsRemoved {
		t.Error("IsRemoved not preserved after roundtrip")
	}
}

func TestSave_Atomic(t *testing.T) {
	s, dir := makeTestSession(t)
	if err := s.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	// Mutate and save again — tmp file should not persist
	s.Months[0].Groups[0].Name = "updated"
	if err := s.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(s.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after successful Save")
	}
	loaded, _ := session.Load(dir)
	if loaded.Months[0].Groups[0].Name != "updated" {
		t.Errorf("expected updated name, got %q", loaded.Months[0].Groups[0].Name)
	}
}

func TestLoad_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := session.Load(dir)
	if err == nil {
		t.Fatal("expected error for missing session file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist in chain, got %v", err)
	}
}

func TestFindGroup(t *testing.T) {
	s, _ := makeTestSession(t)
	g := s.FindGroup("g1")
	if g == nil {
		t.Fatal("expected to find group g1")
	}
	if g.Name != "nice-trip" {
		t.Errorf("unexpected group name: %q", g.Name)
	}
	if s.FindGroup("nope") != nil {
		t.Error("expected nil for missing group")
	}
}

func TestFindPhoto(t *testing.T) {
	s, _ := makeTestSession(t)
	p, g := s.FindPhoto("/input/a.jpg")
	if p == nil || g == nil {
		t.Fatal("expected to find photo")
	}
	if p.SHA256 != "aaa" {
		t.Errorf("unexpected SHA256: %q", p.SHA256)
	}
	if g.ID != "g1" {
		t.Errorf("unexpected group ID: %q", g.ID)
	}
	pp, gg := s.FindPhoto("/input/missing.jpg")
	if pp != nil || gg != nil {
		t.Error("expected (nil, nil) for missing photo")
	}
}

func TestNewGroupID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := session.NewGroupID()
		if seen[id] {
			t.Fatalf("duplicate ID at iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}
