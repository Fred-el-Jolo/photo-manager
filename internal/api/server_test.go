package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolo/photo-manager/internal/session"
)

// newTestSession builds an in-memory session whose OutputDir is a temp dir so
// Save() can write without touching real files.
func newTestSession(t *testing.T) *session.Session {
	t.Helper()
	return &session.Session{
		InputDir:  "/in",
		OutputDir: t.TempDir(),
		Months: []session.MonthGroup{
			{
				Year:  2024,
				Month: 3,
				Groups: []session.Group{
					{
						ID:   "g1",
						Name: "paris",
						Photos: []session.SessionPhoto{
							{Path: "/in/a.jpg", SHA256: "aaa"},
							{Path: "/in/b.jpg", SHA256: "bbb"},
						},
					},
				},
			},
			{
				Year:  2024,
				Month: 4,
				Groups: []session.Group{
					{
						ID:   "g2",
						Name: "lyon",
						Photos: []session.SessionPhoto{
							{Path: "/in/c.jpg", SHA256: "ccc"},
						},
					},
				},
			},
		},
	}
}

func doRequest(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestGetSession(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodGet, "/api/session", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got session.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.InputDir != "/in" {
		t.Errorf("InputDir = %q, want %q", got.InputDir, "/in")
	}
}

func TestGetMonths(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodGet, "/api/months", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got []MonthSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(months) = %d, want 2", len(got))
	}
	if got[0].Year != 2024 || got[0].Month != 3 {
		t.Errorf("month[0] = %d-%d, want 2024-3", got[0].Year, got[0].Month)
	}
	if got[0].GroupCount != 1 || got[0].PhotoCount != 2 {
		t.Errorf("month[0] counts groups=%d photos=%d, want 1/2", got[0].GroupCount, got[0].PhotoCount)
	}
}

func TestPatchGroupName(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodPatch, "/api/groups/g1", `{"name":"nantes"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got session.Group
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "nantes" {
		t.Errorf("Name = %q, want %q", got.Name, "nantes")
	}
}

func TestPatchPhoto_Rotation(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodPatch, "/api/photos", `{"path":"/in/a.jpg","rotation":90}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got session.SessionPhoto
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Rotation != 90 {
		t.Errorf("Rotation = %d, want 90", got.Rotation)
	}
}

func TestPatchPhoto_Remove(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodPatch, "/api/photos", `{"path":"/in/b.jpg","is_removed":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got session.SessionPhoto
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !got.IsRemoved {
		t.Errorf("IsRemoved = false, want true")
	}
}

func TestNotFound(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodGet, "/api/groups/nonexistent", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["error"] == "" {
		t.Errorf("expected error message in body, got %v", got)
	}
}

func TestApplyGroup(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Write minimal content; rotation=0 so exporter uses copyFile, no decode needed.
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("fake-jpeg"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sess := &session.Session{
		InputDir:  srcDir,
		OutputDir: dstDir,
		Months: []session.MonthGroup{{
			Year: 2024, Month: 3,
			Groups: []session.Group{{
				ID: "g1", Name: "paris",
				Photos: []session.SessionPhoto{
					{Path: filepath.Join(srcDir, "a.jpg"), SHA256: "aaa",
						TakenAt: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
					{Path: filepath.Join(srcDir, "b.jpg"), SHA256: "bbb",
						TakenAt: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
				},
			}},
		}},
	}

	h := New(sess, "")
	rec := doRequest(t, h, http.MethodPost, "/api/groups/g1/apply", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got session.Group
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !got.Applied {
		t.Errorf("Applied = false, want true")
	}
	// Verify files were actually copied to the output structure.
	expected := filepath.Join(dstDir, "2024", "2024-03", "2024-03_paris", "a.jpg")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected exported file at %s: %v", expected, err)
	}
}

func TestPatchPhoto_MissingPath(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodPatch, "/api/photos", `{"rotation":90}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPatchPhoto_PartialUpdate(t *testing.T) {
	// Only rotation present must not clobber other fields (is_removed stays false).
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodPatch, "/api/photos", `{"path":"/in/a.jpg","new_name":"trip.jpg"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got session.SessionPhoto
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.NewName != "trip.jpg" {
		t.Errorf("NewName = %q, want %q", got.NewName, "trip.jpg")
	}
	if got.IsRemoved {
		t.Errorf("IsRemoved unexpectedly true after partial update")
	}
}

func TestCORSHeaders(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodGet, "/api/session", "")

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
		t.Errorf("CORS methods = %q, want to contain PATCH", got)
	}
}

func TestOptionsPreflight(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodOptions, "/api/session", "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", rec.Code)
	}
}

func TestThumbnail_InvalidSize(t *testing.T) {
	h := New(newTestSession(t), "")
	rec := doRequest(t, h, http.MethodGet, "/api/thumbnail?path=/x.jpg&size=BOGUS", "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServer_LiveViaHTTPTestServer(t *testing.T) {
	srv := httptest.NewServer(New(newTestSession(t), ""))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/session")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := newLRUCache(2)
	c.put("a", []byte("1"))
	c.put("b", []byte("2"))
	c.put("c", []byte("3")) // evicts "a" (least recent)

	if _, ok := c.get("a"); ok {
		t.Errorf("expected 'a' to be evicted")
	}
	if _, ok := c.get("b"); !ok {
		t.Errorf("expected 'b' to remain")
	}
	if _, ok := c.get("c"); !ok {
		t.Errorf("expected 'c' to remain")
	}
}

func TestLRUCache_TouchKeepsRecent(t *testing.T) {
	c := newLRUCache(2)
	c.put("a", []byte("1"))
	c.put("b", []byte("2"))
	c.get("a")             // touch 'a' -> most recent
	c.put("c", []byte("3")) // evicts 'b'

	if _, ok := c.get("a"); !ok {
		t.Errorf("expected 'a' to remain after touch")
	}
	if _, ok := c.get("b"); ok {
		t.Errorf("expected 'b' to be evicted")
	}
}
