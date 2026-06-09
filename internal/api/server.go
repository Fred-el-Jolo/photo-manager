// Package api serves the photo-manager HTTP API and static frontend assets.
package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jolo/photo-manager/internal/exporter"
	"github.com/jolo/photo-manager/internal/session"
	"github.com/nfnt/resize"
)

// thumbSizes maps size labels to the max dimension in pixels (aspect-ratio preserved).
var thumbSizes = map[string]uint{
	"L":   160,
	"XL":  280,
	"XXL": 420,
}

const (
	thumbnailCacheCap = 512
	jpegQuality       = 85
)

// server holds shared state for the API handler.
type server struct {
	sess      *session.Session
	staticDir string
	cache     *lruCache
	mu        sync.Mutex // guards mutations to sess + Save
}

// New creates an http.Handler that serves the photo-manager API and static files.
// sess is mutated and saved on every write request. staticDir is the path to the
// compiled frontend assets (served at /).
func New(sess *session.Session, staticDir string) http.Handler {
	s := &server{
		sess:      sess,
		staticDir: staticDir,
		cache:     newLRUCache(thumbnailCacheCap),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("GET /api/months", s.handleGetMonths)
	mux.HandleFunc("GET /api/groups/{id}", s.handleGetGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", s.handlePatchGroup)
	mux.HandleFunc("POST /api/groups/{id}/apply", s.handleApplyGroup)
	mux.HandleFunc("POST /api/groups", s.handleCreateGroup)
	mux.HandleFunc("PATCH /api/photos", s.handlePatchPhoto)
	mux.HandleFunc("GET /api/thumbnail", s.handleThumbnail)
	mux.HandleFunc("GET /api/raw", s.handleRaw)
	mux.HandleFunc("/", s.handleStatic)

	return corsMiddleware(mux)
}

// corsMiddleware adds permissive CORS headers and short-circuits OPTIONS preflight.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- JSON helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ---- API types ----

// MonthSummary is a lightweight view of one month's groups for the months list.
type MonthSummary struct {
	Year       int `json:"year"`
	Month      int `json:"month"`
	GroupCount int `json:"group_count"`
	PhotoCount int `json:"photo_count"`
}

// ---- Handlers ----

func (s *server) handleGetSession(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.sess)
}

func (s *server) handleGetMonths(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	summaries := make([]MonthSummary, 0, len(s.sess.Months))
	for _, m := range s.sess.Months {
		photos := 0
		for _, g := range m.Groups {
			photos += len(g.Photos)
		}
		summaries = append(summaries, MonthSummary{
			Year:       m.Year,
			Month:      m.Month,
			GroupCount: len(m.Groups),
			PhotoCount: photos,
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()

	g := s.sess.FindGroup(id)
	if g == nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *server) handlePatchGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Name *string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	g := s.sess.FindGroup(id)
	if g == nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	if body.Name != nil {
		g.Name = *body.Name
	}
	if err := s.sess.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *server) handleApplyGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.Lock()
	defer s.mu.Unlock()

	g := s.sess.FindGroup(id)
	if g == nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	if err := exporter.ApplyGroup(g, s.sess.OutputDir); err != nil {
		writeError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}
	if err := s.sess.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Year  int    `json:"year"`
		Month int    `json:"month"`
		Name  string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Year == 0 || body.Month < 1 || body.Month > 12 {
		writeError(w, http.StatusBadRequest, "year and month (1-12) are required")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	g := s.sess.CreateGroup(body.Year, body.Month, body.Name)
	if err := s.sess.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *server) handlePatchPhoto(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if err := readJSON(r, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	rawPath, ok := patch["path"]
	if !ok {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	var path string
	if err := json.Unmarshal(rawPath, &path); err != nil {
		writeError(w, http.StatusBadRequest, "path must be a string")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	photo, _ := s.sess.FindPhoto(path)
	if photo == nil {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}

	if raw, ok := patch["rotation"]; ok {
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "rotation must be an integer")
			return
		}
		if v != 0 && v != 90 && v != 180 && v != 270 {
			writeError(w, http.StatusBadRequest, "rotation must be 0, 90, 180, or 270")
			return
		}
		photo.Rotation = v
	}
	if raw, ok := patch["is_removed"]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "is_removed must be a boolean")
			return
		}
		photo.IsRemoved = v
	}
	if raw, ok := patch["is_duplicate"]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "is_duplicate must be a boolean")
			return
		}
		photo.IsDuplicate = v
	}
	if raw, ok := patch["new_name"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "new_name must be a string")
			return
		}
		photo.NewName = v
	}
	if raw, ok := patch["target_group_id"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "target_group_id must be a string")
			return
		}
		if err := s.sess.MovePhoto(path, v); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// photo pointer is stale after move — re-fetch from new group
		photo, _ = s.sess.FindPhoto(path)
	}

	if err := s.sess.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}
	writeJSON(w, http.StatusOK, photo)
}

// allowedPath reports whether absPath is inside the session's input or output directory.
// Guards thumbnail and raw endpoints against path traversal.
func (s *server) allowedPath(absPath string) bool {
	absIn, _ := filepath.Abs(s.sess.InputDir)
	absOut, _ := filepath.Abs(s.sess.OutputDir)
	return hasPathPrefix(absPath, absIn) || hasPathPrefix(absPath, absOut)
}

func hasPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))
}

func (s *server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	sizeKey := r.URL.Query().Get("size")
	if sizeKey == "" {
		sizeKey = "L"
	}
	dim, ok := thumbSizes[sizeKey]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid size (use L, XL, or XXL)")
		return
	}

	absPath, err := filepath.Abs(path)
	if err != nil || !s.allowedPath(absPath) {
		writeError(w, http.StatusForbidden, "path not allowed")
		return
	}

	key := absPath + ":" + sizeKey
	if data, found := s.cache.get(key); found {
		writeThumbnail(w, data)
		return
	}

	data, err := renderThumbnail(absPath, dim)
	if err != nil {
		writeError(w, http.StatusNotFound, "could not render thumbnail: "+err.Error())
		return
	}
	s.cache.put(key, data)
	writeThumbnail(w, data)
}

// handleRaw serves the original image file for the lightbox full-resolution view.
func (s *server) handleRaw(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	absPath, err := filepath.Abs(path)
	if err != nil || !s.allowedPath(absPath) {
		writeError(w, http.StatusForbidden, "path not allowed")
		return
	}
	http.ServeFile(w, r, absPath)
}

func writeThumbnail(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// renderThumbnail opens, decodes, resizes (preserving aspect ratio so the max
// dimension equals dim) and JPEG-encodes the image at quality 85.
func renderThumbnail(path string, dim uint) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	iw, ih := uint(bounds.Dx()), uint(bounds.Dy())

	var tw, th uint
	if iw >= ih {
		tw, th = dim, 0
	} else {
		tw, th = 0, dim
	}
	resized := resize.Resize(tw, th, img, resize.Lanczos3)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// handleStatic serves files from staticDir, falling back to index.html for
// extension-less paths (SPA client-side routing).
// Cache-Control: no-cache forces browsers to always revalidate — prevents
// stale JS/CSS after a rebuild.
func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean("/" + r.URL.Path)
	target := filepath.Join(s.staticDir, clean)

	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, target)
		return
	}

	// SPA fallback: serve index.html for paths without a file extension.
	if filepath.Ext(clean) == "" {
		index := filepath.Join(s.staticDir, "index.html")
		if _, err := os.Stat(index); err == nil {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, index)
			return
		}
	}

	http.NotFound(w, r)
}

// ---- LRU cache ----

type lruEntry struct {
	key  string
	data []byte
}

type lruCache struct {
	mu    sync.Mutex
	cap   int
	items map[string]*lruEntry
	order []string // front=most-recent; evict from back
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		cap:   capacity,
		items: make(map[string]*lruEntry, capacity),
		order: make([]string, 0, capacity),
	}
}

func (c *lruCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.touch(key)
	return e.data, true
}

func (c *lruCache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.items[key]; ok {
		existing.data = data
		c.touch(key)
		return
	}

	c.items[key] = &lruEntry{key: key, data: data}
	c.order = append([]string{key}, c.order...)

	for len(c.order) > c.cap {
		last := c.order[len(c.order)-1]
		c.order = c.order[:len(c.order)-1]
		delete(c.items, last)
	}
}

// touch moves key to the front of the order slice. Caller must hold c.mu.
func (c *lruCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append([]string{key}, c.order...)
}
