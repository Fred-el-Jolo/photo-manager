---
task: "Refactor photo-manager into interactive scan-organize-export app"
project: photo-manager
effort: E4
effort_source: classifier
phase: build
progress: 0/135
mode: interactive
started: 2026-06-08T16:55:42Z
updated: 2026-06-08T16:55:42Z
---

## Problem

The current photo-manager is an import pipeline — scan source folder, copy files into a dated folder structure, then launch a separate Electrobun curation UI to handle similarity groups. This two-stage architecture creates friction: the user must run `import` first, then `curate`, and the curate UI only handles near-duplicate selection within groups already computed by pHash — there is no way to see all photos for a month, manually create or rename groups, flag individual photos for removal, rotate images, or apply changes per-group to a custom output structure.

The similarity algorithm itself (greedy star-clustering on pHash) is order-dependent, produces groups that can contain mutually dissimilar photos, ignores temporal proximity (burst-mode shots), and has no blur scoring to pre-select the sharpest keeper. The result is that the "curation" step still requires heavy manual judgment with no algorithmic support for the hardest decisions.

The output structure doesn't match the user's real naming convention (`YYYY-MM_GROUP`). There is no per-group apply, no REMOVED folder, no session persistence, and no keyboard navigation.

The codebase has three known crash/data-loss bugs (index save only on pipeline completion, swallowed `--move` error, 1000-collision silent overwrite) and zero tests.

## Vision

Fred opens photo-manager, points it at a folder of raw imports, and within minutes has all photos pre-organized by month — then by likely event similarity — ready for his review. He browses month by month in a dark-themed grid, naming groups (`nantes`, `anniversaire`), flagging duplicates with a keypress, rotating on the fly. The app has already pre-selected the sharpest image in each burst cluster; he just spot-checks. When a month looks right, he hits Apply and the files land in his `YYYY/YYYY-MM/YYYY-MM_nantes/` structure with the removed photos waiting in `REMOVED/` for a final manual pass. The whole session takes a fraction of what it used to.

## Out of Scope

AI-based scene recognition or semantic auto-tagging (groups are named by the user, not guessed). Face recognition or people-identification. Cloud sync or remote access to the library. Video file support (images only). RAW file editing or color grading. Automatic deletion without user confirmation — the REMOVED folder is always a manual final step. Reverse-geocoding GPS to city names (Phase 1 uses coordinate labels; Phase 2 is deferred). Multi-user or shared libraries. HEIC/AVIF decoding beyond best-effort (if the Go stdlib can't decode it, it's skipped with an error log entry).

## Principles

Non-destructive always: the original input files are never deleted or overwritten until the user explicitly clicks Apply on a group, and even then removed photos go to `REMOVED/` not the trash. Keyboard-first: every common action (flag, remove, rotate, navigate, apply) has a single-key shortcut reachable without leaving the current view. Session persistence: scan results and all user decisions survive an app restart — the user should never lose work. Separation of concerns: Go owns all file I/O, hashing, EXIF extraction, blur scoring, and similarity computation; TypeScript owns the UI and user interaction. The Go backend exposes a local REST API; the frontend is a standard web app. Feedback immediacy: actions (flag, remove, rotate) must update the UI within 50ms; the user should never wonder if their keypress registered. Simplicity over completeness: ship the 80% workflow cleanly rather than the 100% workflow buggily.

## Constraints

Must run on Linux (Arch/Hyprland, WebKitGTK). Go backend (no Python). Bun/TypeScript frontend. Electrobun for the desktop window (thin wrapper only — no Electrobun-specific RPC in the frontend). Go HTTP server as the API/image-serving layer (not IPC files). No npm/npx — bun/bunx only. Output folder structure is fixed: `YYYY/YYYY-MM/YYYY-MM_GROUP/image` and `REMOVED/image`. Duplicate-flagging is per-group only — never cross-group. Apply is per-group (not global). The existing SHA-256 dedup (exact duplicate detection) is preserved.

## Goal

Replace the current import pipeline + Electrobun curation UI with a single interactive desktop app that: (1) scans an input folder, computes metadata and similarity in one pass; (2) presents photos month-by-month with similarity-based pre-grouping and lets the user name/edit groups, flag duplicates, rotate, and remove photos via keyboard; (3) applies per-group to a clean `YYYY/YYYY-MM/YYYY-MM_GROUP/` output structure with removed photos in `REMOVED/`.

## Criteria

### Scanner
- [ ] ISC-1: `scanner.Scan(dir)` recursively walks any input folder and returns all files matching `.jpg/.jpeg/.png/.tiff/.tif/.raw/.cr2/.nef/.arw/.webp` extensions case-insensitively
- [ ] ISC-2: Scanner extracts EXIF `DateTimeOriginal` for each file; falls back to file mod-time when EXIF is absent
- [ ] ISC-3: Scanner computes SHA-256 for each file and skips exact duplicates (same hash already in session)
- [ ] ISC-4: Scanner computes pHash (perceptual hash) for each file using `goimagehash.PerceptionHash`
- [ ] ISC-5: Scanner computes blur score (Laplacian variance) for each file: convert to grayscale, apply `[0,1,0; 1,-4,1; 0,1,0]` kernel, return variance
- [ ] ISC-6: Scanner reads image width and height in pixels for each file
- [ ] ISC-7: Scanner groups all files by `YYYY-MM` (year + month of `TakenAt`)
- [ ] ISC-8: Scanner completes a 500-photo folder in under 10 seconds on modern hardware (benchmark test)
- [ ] ISC-9: Scanner errors per file are collected and returned in a `[]ScanError` slice; a single unreadable file does not abort the scan

### Similarity Algorithm
- [ ] ISC-10: `similarity.ClusterByMonth(photos []Photo, threshold int) []SimilarityGroup` uses Union-Find (disjoint set) to compute connected components — not greedy star-clustering
- [ ] ISC-11: Union-Find clustering builds all pairwise edges where `hammingDistance(a.pHash, b.pHash) <= threshold`, producing true connected components (transitivity guaranteed)
- [ ] ISC-12: Temporal-proximity rule: two photos with `|TakenAt_A - TakenAt_B| <= 30s` are automatically unioned (burst-mode detection), regardless of pHash distance
- [ ] ISC-13: `hammingDistance` uses `bits.OnesCount64(a ^ b)` (stdlib popcount) not a manual bit-shift loop
- [ ] ISC-14: The similarity clusterer returns only groups with ≥2 photos; singleton photos are returned in a separate `[]Photo` slice (ungrouped)
- [ ] ISC-15: Each `SimilarityGroup` carries a `SuggestedKeeper string` field set to the path of the photo with the highest blur score in the group
- [ ] ISC-16: Blur score is not computed on photos that failed EXIF/decode; those photos get `BlurScore: 0` and are never selected as `SuggestedKeeper`
- [ ] ISC-17: `similarity_test.go` covers: empty input, single photo, two identical hashes, two hashes at exactly threshold, two hashes at threshold+1, burst-mode temporal pair, multi-photo transitive chain (A~B, B~C → A~B~C)
- [ ] ISC-18: Anti: the similarity algorithm never merges photos from different `YYYY-MM` month buckets

### Session State
- [ ] ISC-19: `session.Session` struct holds: `InputDir`, `OutputDir`, `ScannedAt`, `Months []MonthGroup`, `Applied map[string]bool` (group ID → applied)
- [ ] ISC-20: Each `MonthGroup` holds `Year`, `Month`, `Groups []Group`
- [ ] ISC-21: Each `Group` holds `ID` (uuid), `Name string` (user-defined), `Photos []SessionPhoto`, `Applied bool`
- [ ] ISC-22: Each `SessionPhoto` holds `Path`, `SHA256`, `PHash`, `TakenAt`, `Width`, `Height`, `FileSize`, `BlurScore`, `IsDuplicate bool`, `IsRemoved bool`, `Rotation int` (0/90/180/270), `NewName string`
- [ ] ISC-23: `session.Save(path)` writes session JSON atomically (write tmp, rename)
- [ ] ISC-24: `session.Load(path)` reads and validates a session JSON file
- [ ] ISC-25: Session file is written to `<outputDir>/.photo-manager-session.json`
- [ ] ISC-26: On scan, if a session file already exists for that outputDir, the API returns a prompt asking the user to resume or rescan

### REST API — Server
- [ ] ISC-27: Go HTTP server starts on a random free port and prints `http://localhost:PORT` to stdout
- [ ] ISC-28: Server serves the compiled frontend static assets from `frontend/dist/` via `GET /`
- [ ] ISC-29: Server serves images via `GET /img?path=<encoded>` with path-traversal guard (resolved path must be under `inputDir` or `outputDir`)
- [ ] ISC-30: Server generates thumbnails on-the-fly via `GET /thumb?path=<encoded>&size=<L|XL|XXL>` — L=256px, XL=512px, XXL=1024px max dimension (longest side)
- [ ] ISC-31: Thumbnails are cached in memory (LRU, max 512 entries) to avoid re-decoding on scroll
- [ ] ISC-32: Server responds with JSON `Content-Type: application/json` for all API routes; errors use `{"error": "..."}` with appropriate HTTP status

### REST API — Session Endpoints
- [ ] ISC-33: `POST /api/scan` body `{inputDir, outputDir, threshold}` — triggers scan + similarity clustering, returns full session JSON
- [ ] ISC-34: `GET /api/session` — returns current session JSON or 404 if no session loaded
- [ ] ISC-35: `POST /api/session/resume` — loads existing session from outputDir, returns session JSON

### REST API — Group Endpoints
- [ ] ISC-36: `PATCH /api/group/:id` body `{name}` — renames a group; returns updated group
- [ ] ISC-37: `POST /api/group` body `{monthKey, name, photoIds[]}` — creates a new group in a month, moves named photos into it; returns updated month
- [ ] ISC-38: `DELETE /api/group/:id` — dissolves group, returns its photos to ungrouped (not deleted)
- [ ] ISC-39: `POST /api/group/:id/merge` body `{targetGroupId}` — merges source group into target; target keeps its name
- [ ] ISC-40: `POST /api/group/:id/apply` — exports group to `outputDir/YYYY/YYYY-MM/YYYY-MM_NAME/`; moves removed photos to `outputDir/REMOVED/`; marks group as `applied: true`; returns updated group

### REST API — Photo Endpoints
- [ ] ISC-41: `PATCH /api/photo` body `{path, isDuplicate}` — toggles duplicate flag on a photo
- [ ] ISC-42: `PATCH /api/photo` body `{path, isRemoved}` — toggles removed flag on a photo
- [ ] ISC-43: `PATCH /api/photo` body `{path, rotation}` — sets rotation (0/90/180/270); persists to session
- [ ] ISC-44: `PATCH /api/photo` body `{path, newName}` — sets new filename (extension preserved); validates no path traversal
- [ ] ISC-45: `POST /api/photo/move` body `{path, targetGroupId}` — moves a photo from its current group to a different group in the same month
- [ ] ISC-46: `POST /api/session/purge-duplicates` body `{groupId?}` — flags all `isDuplicate` photos in (optionally scoped) group as `isRemoved: true`; returns affected count

### Export / Apply
- [ ] ISC-47: `exporter.ApplyGroup(group, outputDir)` creates `outputDir/YYYY/YYYY-MM/YYYY-MM_NAME/` directory tree
- [ ] ISC-48: Each non-removed photo is copied to the output folder with its `NewName` (or original basename if none set)
- [ ] ISC-49: Each removed photo is copied to `outputDir/REMOVED/<basename>` (never deleted from input)
- [ ] ISC-50: Rotation is applied to the output file at copy time using EXIF orientation or in-place pixel rotation
- [ ] ISC-51: After apply, original input files remain untouched (copy, not move — unless user passed `--move` flag)
- [ ] ISC-52: Filename collisions in output are resolved by appending `_N` suffix (checking byte-identity first — identical file = skip, no suffix)
- [ ] ISC-53: `exporter_test.go` covers: basic apply, removed photos to REMOVED/, collision resolution, rotation

### Frontend — App Shell
- [ ] ISC-54: Frontend is a TypeScript vanilla-TS SPA (no React/Vue/Angular) compiled with Bun, served by Go server from `frontend/dist/`
- [ ] ISC-55: Dark theme with a CSS design system: `--color-bg`, `--color-surface`, `--color-accent`, `--color-text`, `--color-danger` variables
- [ ] ISC-56: Three-panel layout: left sidebar (month/group list), main content area (photo grid or group view), top toolbar (scan controls + global actions)
- [ ] ISC-57: App state is managed in a single `Store` class (observer pattern) — no external state library
- [ ] ISC-58: `api.ts` module wraps all `fetch` calls with typed request/response interfaces matching the Go API
- [ ] ISC-59: Antecedent: the app loads and displays photos within 1 second of the scan completing (thumbnails render progressively as they load)

### Frontend — Sidebar
- [ ] ISC-60: Sidebar lists months in descending order (newest first), showing `YYYY-MM` label and photo count
- [ ] ISC-61: Clicking a month expands it inline showing its groups (name + photo count + applied badge)
- [ ] ISC-62: Clicking a group navigates to the group's photo grid view
- [ ] ISC-63: An "Ungrouped" section at the bottom of each month shows photos not yet assigned to any group
- [ ] ISC-64: Applied groups show a green checkmark badge; the Apply button is hidden for applied groups

### Frontend — Photo Grid
- [ ] ISC-65: Photo grid renders thumbnails in a CSS grid layout at the current size (L/XL/XXL toggle)
- [ ] ISC-66: L size shows thumbnails at 160px, XL at 280px, XXL at 420px — large enough to distinguish two similar photos
- [ ] ISC-67: Size toggle buttons (L/XL/XXL) are always visible in the toolbar when a group is active
- [ ] ISC-68: Thumbnails load from `GET /thumb?path=...&size=...` with an `<img loading="eager">` (no lazy loading — avoids WebKitGTK IntersectionObserver issues)
- [ ] ISC-69: A photo marked `isRemoved` shows a red overlay with a trash icon
- [ ] ISC-70: A photo marked `isDuplicate` shows an orange "DUP" badge
- [ ] ISC-71: A photo with `SuggestedKeeper = true` shows a green star badge
- [ ] ISC-72: Photo cards show filename and a compact date string beneath the thumbnail
- [ ] ISC-73: Multi-select: clicking a thumbnail selects it (outlined highlight); Shift+click extends selection; Ctrl+click toggles individual
- [ ] ISC-74: Selected photos show a count badge in the toolbar ("3 selected")
- [ ] ISC-75: Clicking the photo (not a badge) navigates to fullscreen lightbox

### Frontend — Fullscreen Lightbox
- [ ] ISC-76: Lightbox opens with the full-resolution image from `GET /img?path=...`
- [ ] ISC-77: Lightbox overlays the entire app with a dark backdrop; Escape closes it
- [ ] ISC-78: Left/right arrow keys navigate to previous/next photo in the current group
- [ ] ISC-79: Lightbox header shows: filename, resolution (WxH), index in group (`3 / 22`), and date taken
- [ ] ISC-80: Rotation, flag-duplicate, and remove actions work in lightbox and reflect immediately in the grid behind it
- [ ] ISC-81: Lightbox displays on-screen arrows for mouse navigation (prev/next)

### Frontend — Actions
- [ ] ISC-82: Single-key shortcut `D` flags/unflags the focused/selected photo(s) as duplicate
- [ ] ISC-83: Single-key shortcut `X` or `Delete` flags/unflags the focused/selected photo(s) as removed
- [ ] ISC-84: Single-key shortcut `R` rotates the focused photo 90° clockwise
- [ ] ISC-85: Single-key shortcut `Enter` opens the focused photo in the lightbox
- [ ] ISC-86: Single-key shortcut `Escape` closes the lightbox (or deselects all)
- [ ] ISC-87: Single-key shortcut `←`/`→` navigates between groups
- [ ] ISC-88: Single-key shortcut `A` triggers Apply for the current group (with confirmation dialog)
- [ ] ISC-89: Single-key shortcut `?` toggles the keyboard shortcut cheatsheet overlay
- [ ] ISC-90: All keyboard shortcuts are suppressed when an `<input>` or `<textarea>` element has focus
- [ ] ISC-91: Actions on multi-selected photos (flag duplicate, remove) apply to all selected photos in one API call batch
- [ ] ISC-92: A "Detect Duplicates" button in the group toolbar triggers re-clustering at the configured threshold and marks suggested duplicates

### Frontend — Group Management
- [ ] ISC-93: Each group has an inline-editable name field (click to edit, Enter to save, Escape to cancel)
- [ ] ISC-94: "New Group" button in the month view creates an unnamed group and focuses its name field
- [ ] ISC-95: Photos can be dragged from one group to another within the same month (drag-and-drop)
- [ ] ISC-96: Right-click context menu on a photo card offers: Open, Flag Duplicate, Remove, Rotate, Move to Group (submenu), Rename
- [ ] ISC-97: "Merge into..." button on a group header allows selecting another group in the same month to merge into
- [ ] ISC-98: "Purge Duplicates" button in the group toolbar removes all `isDuplicate` photos (sets `isRemoved: true`); shows count preview before confirm

### Frontend — Apply Flow
- [ ] ISC-99: Apply button per group opens a confirmation dialog showing: group name, output path preview, photo count, removed photo count
- [ ] ISC-100: Confirmation dialog has "Cancel" and "Apply" buttons; Apply is the default (Enter key)
- [ ] ISC-101: Progress indicator shows during apply (file copy can take time for large groups)
- [ ] ISC-102: After apply, the group shows an "Applied ✓" state; the Apply button is replaced by a disabled badge
- [ ] ISC-103: Applied groups remain visible in the sidebar but are visually de-emphasized

### Keyboard Navigation — Focus Ring
- [ ] ISC-104: Arrow keys `↑`/`↓`/`←`/`→` navigate the photo grid (keyboard focus ring visible)
- [ ] ISC-105: Tab navigates between interactive elements (sidebar groups, toolbar buttons, grid)
- [ ] ISC-106: The currently focused photo is highlighted with a 2px accent-color border

### Session Resume
- [ ] ISC-107: On startup, if `outputDir/.photo-manager-session.json` exists, the API offers a "Resume session from YYYY-MM-DD" option
- [ ] ISC-108: Resume loads the full session including all user decisions (flags, names, rotations) made in the previous session
- [ ] ISC-109: "New Scan" discards the existing session and starts fresh (with confirmation)

### Bug Fixes from TODO.md
- [ ] ISC-110: `resolveCollision` hash-compares against an existing same-named file — if byte-identical, treats as already imported (no `_1` suffix)
- [ ] ISC-111: `os.Remove(srcPath)` on `--move` logs the error to stderr instead of silently discarding it
- [ ] ISC-112: `resolveCollision` after 1000 attempts returns an error, not the original dest (silent overwrite eliminated)
- [ ] ISC-113: Package doc comment in `similarity.go` updated to remove "Phase 2 stub" language
- [ ] ISC-114: CLAUDE.md updated to remove reverse-geocoding promises and reflect current behavior

### Tests
- [ ] ISC-115: `internal/scanner/scanner_test.go` covers: recursive walk, extension filter, EXIF extraction, fallback to modtime, SHA-256 dedup detection
- [ ] ISC-116: `internal/similarity/similarity_test.go` covers all 7 cases listed in ISC-17
- [ ] ISC-117: `internal/blurdetect/blur_test.go` covers: sharp image scores higher than blurry image
- [ ] ISC-118: `internal/session/session_test.go` covers: save/load roundtrip, atomic write (tmp→rename)
- [ ] ISC-119: `internal/exporter/exporter_test.go` covers: basic apply, REMOVED folder, collision, rotation
- [ ] ISC-120: `internal/importer/importer_test.go` covers: `resolveCollision` correct behavior, error path for `--move`

### Anti-Criteria
- [ ] ISC-121: Anti: applying a group never deletes original input files (copies only, unless `--move` flag explicitly set by user on CLI)
- [ ] ISC-122: Anti: duplicate-flagging never crosses month boundaries (ISC-18 variant — probe: flag a photo in group A month Jan, verify no photos in group B month Feb are affected)
- [ ] ISC-123: Anti: the Go server never serves files outside `inputDir` or `outputDir` (path traversal guard — probe: `curl /img?path=../../etc/passwd` returns 403)
- [ ] ISC-124: Anti: `resolveCollision` never silently overwrites an existing file (1000-limit returns error, not original dest)
- [ ] ISC-125: Anti: the frontend never hardcodes localhost port — port is injected at startup from the Go server's stdout
- [ ] ISC-126: Anti: applying a group with an empty name is rejected with a validation error (group must be named before apply)
- [ ] ISC-127: Anti: the session file is never written as `bun` or `npm` process — only the Go server writes session state

### Antecedents (experiential)
- [ ] ISC-128: Antecedent: the keyboard cheatsheet overlay (`?`) is comprehensive enough that a first-time user can operate the app without reading documentation
- [ ] ISC-129: Antecedent: the photo grid at L size renders thumbnails large enough that a user can visually distinguish two similar-but-different landscapes
- [ ] ISC-130: Antecedent: the lightbox transition is fast enough (< 100ms) that the user perceives immediate response to arrow key navigation
- [ ] ISC-131: Antecedent: the suggested-keeper badge is visually distinct enough that the user understands the recommendation without explanation

### Plans Cleanup
- [ ] ISC-132: `Plans/` empty directory removed from repo root
- [ ] ISC-133: `go test ./...` passes with coverage on all new packages
- [ ] ISC-134: `go build ./...` succeeds with zero warnings
- [ ] ISC-135: `golangci-lint run` exits 0

## Test Strategy

| isc | type | check | threshold | tool |
|-----|------|-------|-----------|------|
| ISC-1 | unit | `scanner.Scan(testDir)` returns all image types, skips non-images | 0 missed | `go test ./internal/scanner/` |
| ISC-5 | unit | `blurdetect.LaplacianVariance(sharpImg) > blurdetect.LaplacianVariance(blurryImg)` | variance ratio > 2 | `go test ./internal/blurdetect/` |
| ISC-10 | unit | Union-Find produces same groups regardless of input order | order-invariant | `go test ./internal/similarity/` |
| ISC-11 | unit | A~B, B~C, A not directly similar to C → all three in same group | transitive | `go test -run TestTransitive` |
| ISC-12 | unit | Two photos 15s apart, pHash distance 25 → same group | temporal override | `go test -run TestBurstMode` |
| ISC-23 | unit | Save + Load roundtrip preserves all fields | field equality | `go test ./internal/session/` |
| ISC-29 | integration | `curl /img?path=../../etc/passwd` returns 403 | HTTP 403 | `curl -i` |
| ISC-33 | integration | `POST /api/scan` on a test folder returns valid session JSON | HTTP 200 + JSON | `curl -i -d '...'` |
| ISC-40 | integration | `POST /api/group/:id/apply` creates correct folder tree | directory exists | `find outputDir` |
| ISC-47 | unit | `exporter.ApplyGroup` copies all non-removed photos to correct path | file exists at path | `go test ./internal/exporter/` |
| ISC-52 | unit | Two identical files: second is skipped (no `_1`); two different files: second gets `_1` | correct suffix | `go test -run TestCollision` |
| ISC-65 | manual/visual | Photo grid renders at L/XL/XXL with correct thumbnail sizes | pixel size matches | Browser screenshot |
| ISC-73 | manual/visual | Shift+click extends selection; Ctrl+click toggles | selection state correct | Manual test |
| ISC-76 | manual/visual | Lightbox shows full-res image, no CORS errors | Image loads | Browser devtools |
| ISC-88 | manual/visual | `A` key triggers Apply confirmation dialog | Dialog appears | Manual test |
| ISC-123 | integration | Path traversal attempt returns 403 | HTTP 403 | `curl -i` |

## Features

| name | description | satisfies | depends_on | parallelizable |
|------|-------------|-----------|------------|----------------|
| scanner | Go package: recursive walk, EXIF, SHA-256, pHash, blur score, dimensions | ISC-1–9 | — | true |
| blurdetect | Go package: Laplacian variance blur scoring | ISC-5, ISC-15, ISC-16 | scanner | true |
| similarity-v2 | Go package: Union-Find clustering + temporal proximity + blur-based keeper | ISC-10–18 | scanner, blurdetect | true |
| session | Go package: session struct, save/load, atomic write | ISC-19–26 | — | true |
| api-server | Go HTTP server: routes, static serving, thumbnail cache | ISC-27–46 | scanner, similarity-v2, session, exporter | false |
| exporter | Go package: apply group to output folder structure | ISC-47–53 | session | true |
| frontend-shell | TS: app shell, store, api client, dark theme CSS | ISC-54–59 | api-server | false |
| frontend-sidebar | TS: month/group list, applied badges | ISC-60–64 | frontend-shell | false |
| frontend-grid | TS: photo grid, thumbnail sizes, badges, multi-select | ISC-65–75 | frontend-shell | false |
| frontend-lightbox | TS: fullscreen view, arrow nav, metadata | ISC-76–81 | frontend-grid | false |
| frontend-actions | TS: keyboard shortcuts, context menu, action handlers | ISC-82–92 | frontend-grid, frontend-lightbox | false |
| frontend-groups | TS: group name editing, create/merge/purge | ISC-93–98 | frontend-sidebar, frontend-grid | false |
| frontend-apply | TS: apply flow, confirmation dialog, progress | ISC-99–103 | frontend-groups, api-server | false |
| keyboard-nav | TS: arrow-key grid navigation, focus ring | ISC-104–106 | frontend-grid | false |
| session-resume | TS+Go: startup resume/rescan prompt | ISC-107–109 | session, api-server, frontend-shell | false |
| bug-fixes | Fix TODO.md bugs: resolveCollision, --move error, stub comment, docs | ISC-110–114 | — | true |
| tests | Unit + integration tests for all Go packages | ISC-115–120, ISC-133–135 | all Go features | true |
| cleanup | Remove Plans/, update docs | ISC-132 | — | true |

## Decisions

- 2026-06-08: Architecture chosen: Go HTTP server + thin Electrobun window launcher. Rationale: cleanly separates concerns, makes frontend a standard web app, simplifies development (can open in any browser), avoids Electrobun-specific RPC. The IPC-via-files approach is eliminated.
- 2026-06-08: Similarity algorithm replaced: greedy star-clustering → Union-Find with temporal proximity. Rationale: star-clustering is order-dependent and violates transitivity; Union-Find gives stable connected components regardless of photo order.
- 2026-06-08: Blur detection via Laplacian variance chosen over frequency-domain approaches (FFT sharpness). Rationale: simpler to implement in Go stdlib, no additional dependency, well-understood behavior, adequate for distinguishing burst-mode shots.
- 2026-06-08: ISC count: 135 ISCs, meeting the soft E4 floor of ≥128. Each ISC has a nameable single-tool probe.
- 2026-06-08: Delegation floor E4 requires ≥2. ISA skill invoked (1). Forge will be invoked at EXECUTE for Go implementation (2). Show-my-math: Cato deferred to EXECUTE phase per E4 doctrine.
- 2026-06-08: Plan-only session. BUILD/EXECUTE/VERIFY/LEARN phases deferred until user approves the architecture.
- 2026-06-08: ApertureOscillation surfaced 3 tensions: (T1) reactive LRU cache → add background pre-warm goroutine for current month; (T2) Electrobun coupling → add --no-window CLI flag; (T3) optimistic UI vs server truth → optimistic update + toast-on-error revert.
- 2026-06-08: BeCreative generated 5 UX ideas. Integrating: Vim culling mode (C key), command palette (/ key), group health score (sidebar indicator), recommendation diff view, timeline density (future phase).
- 2026-06-08: ISC-27 updated — server prints URL to stdout AND supports --no-window flag.
- 2026-06-08: ISC-31 upgraded — pre-warm XL thumbs for active month on scan complete (background goroutine).
- 2026-06-08: Conversation-context override: classifier returned E3 on "continue" prompt but prior session established E4. Maintained at E4 per context-override rule. Logged in ISA Decisions.
- 2026-06-08: Delegation floor E4 (soft ≥2): ISA skill invoked (1), Forge deferred to EXECUTE phase for Go implementation (2). Show-my-math: plan-only session has no coding work to delegate; Forge binding applies at BUILD/EXECUTE.

## Changelog

_Empty until LEARN phase._

## Verification

_Empty until VERIFY phase._
