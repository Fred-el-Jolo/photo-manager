# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`photo-manager` is a Go CLI tool for importing, deduplicating, organizing, and curating personal photo libraries. It parses EXIF metadata to auto-organize photos by date and location, detects near-duplicate images using perceptual hashing, and launches an Electrobun desktop UI for the user to select keepers from groups of similar shots.

## Commands

```bash
# Build Go
go build ./...

# Run
go run ./cmd/photo-manager [flags]

# Test all
go test ./...

# Test single package
go test ./internal/importer/

# Test single test
go test ./internal/importer/ -run TestDeduplicate

# Lint (golangci-lint required)
golangci-lint run

# Build frontend only (from frontend/)
cd frontend && bun run build

# Launch curate UI standalone (from frontend/, requires PM_INPUT and PM_OUTPUT env vars)
cd frontend && PM_INPUT=/tmp/in.json PM_OUTPUT=/tmp/out.json bun run curate
```

## Architecture

```
cmd/photo-manager/     # CLI entry point, flag parsing, command dispatch
internal/
  importer/            # Import pipeline orchestrator
  dedup/               # Exact deduplication (SHA-256 hash index)
  exif/                # EXIF metadata extraction (date, GPS, camera)
  organizer/           # Folder structure builder (year/month/location)
  similarity/          # Perceptual hash computation and grouping
  frontend/            # Go-side launcher for Electrobun curate UI
  storage/             # Photo library index (JSON)
frontend/              # Electrobun desktop app (Bun + WebKitGTK)
  src/
    bun/index.ts       # Electrobun main process (image server, RPC, window)
    curate-ui/app.ts   # WebView UI (photo grid, keeper selection)
    curate-ui/index.html
    types.ts           # Shared types + RPC schema (both sides)
  electrobun.config.ts
```

### Import Pipeline

Photos flow through stages in sequence:

1. **Scan** — walk source directory, collect all image paths
2. **Deduplicate** — compute SHA-256; skip if hash already in library index
3. **Extract** — parse EXIF for `DateTimeOriginal`, GPS lat/lon, camera model
4. **Organize** — build destination path: `<library>/<year>/<month>/<location>/`
5. **Copy** — move/copy file to destination, update library index
6. **Similarity** — compute perceptual hash (pHash), cluster groups with distance ≤ threshold
7. **Curate** — launch Electrobun UI for any groups found; user selects keeper, rest queued for deletion

### Folder Structure

```
library/
  2024/
    03/
      48.85N_2.35E/   # raw GPS string when coordinates present
      Unknown/        # fallback when GPS absent
    04/
      48.85N_2.35E/
  .photo-manager/
    index.json        # library index: sha256, phash, EXIF cache, dest_path
```

GPS coordinates are stored as raw strings (e.g. `48.85N_2.35E`). Reverse-geocoding is not implemented.

### Similarity / Near-Duplicate Detection

- Perceptual hash (pHash) via `github.com/corona10/goimagehash`
- Hamming distance threshold (default: 10) groups similar photos; pass `--threshold` to adjust
- `similarity.Group.Paths` uses `json:"paths"` tag — required for Go→JS JSON serialization
- Union-Find connected components: transitive grouping (A~B, B~C → all three in one group)

### Curate UI (Electrobun Desktop)

Replaces the old Bubbletea TUI. Architecture:

- **Go** (`internal/frontend/frontend.go`) writes `CurateInput` JSON to a temp file, sets `PM_INPUT` and `PM_OUTPUT` env vars, then runs `bun run curate` from the `frontend/` directory
- **Bun main process** (`src/bun/index.ts`) reads input, starts a local HTTP image server on a random port, opens a `BrowserWindow`, and pushes data to the WebView via RPC after `dom-ready`
- **WebView** (`src/curate-ui/app.ts`) receives data via RPC `init` handler, renders photo grid, user picks keeper per group, calls `done` RPC which writes `CurateOutput` JSON and exits
- **IPC**: two temp JSON files (`PM_INPUT` / `PM_OUTPUT`) — Go writes input, Bun writes output

#### Electrobun Build Flow

```bash
# Must run build before dev — generates version.json and bundles scripts
electrobun build && electrobun dev
```

The `bun run curate` script in `package.json` runs both. Raw `bun run src/bun/index.ts` does NOT work — Electrobun requires its own build pipeline.

#### Image Server

Local `Bun.serve({ port: 0 })` HTTP server avoids WebKitGTK `file://` CORS restrictions. Path-traversal guard: `relative(libRoot, abs).startsWith("..")`. Serves `Cache-Control: public, max-age=3600`.

#### RPC Schema

Defined in `src/types.ts` as `PhotoManagerRPC`. Push model — bun pushes `init` to webview after `dom-ready` to avoid WebSocket race condition. Webview calls `done` when finished.

#### Known Pitfalls

- `::before` pseudo-elements on `<img>` are invalid CSS (replaced element) — don't use them
- Positioned elements (`position: absolute`) stack above normal-flow siblings; use `z-index` explicitly when layering badges/overlays over images
- WebKitGTK `IntersectionObserver` may not fire reliably on initial render — use eager `img.src` loading for local images instead of `data-src` lazy loading

### Key Libraries

| Purpose | Library |
|---------|---------|
| EXIF parsing | `github.com/rwcarlsen/goexif/exif` |
| Perceptual hashing | `github.com/corona10/goimagehash` |
| CLI flags | `github.com/spf13/cobra` |
| Image decoding | stdlib `image/jpeg`, `image/png` + `golang.org/x/image` |
| Desktop UI | `electrobun` (Bun + WebKitGTK) |

### Library Index

`<library>/.photo-manager/index.json` tracks:
- File path → SHA-256 hash (for dedup)
- File path → pHash (for similarity queries)
- File path → EXIF metadata cache

The index is **not** automatically cleaned when files are deleted externally. To force a re-import after deleting files, remove or edit the index manually.
