// Photo Manager SPA — vanilla TypeScript, no framework.

import {
  api,
  type ApiGroup,
  type ApiPhoto,
  type ApiSession,
  type ThumbSize,
} from "./api";

// ─── Constants ───────────────────────────────────────────────────────────────

const THUMB_PX: Record<ThumbSize, string> = {
  L: "160px",
  XL: "280px",
  XXL: "420px",
};

const MONTH_NAMES = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

// ─── State ───────────────────────────────────────────────────────────────────

interface LightboxState {
  group: ApiGroup;
  index: number;
  showMovePicker: boolean;
  moveQuery: string;
}

interface CullState {
  group: ApiGroup;
  index: number;
}

interface AppState {
  session: ApiSession | null;
  currentMonthKey: string | null; // "2024-3"
  thumbSize: ThumbSize;
  lightbox: LightboxState | null;
  cull: CullState | null;
  selected: Set<string>; // photo paths
  filter: string; // group-name filter from command palette
  selectionMove: { open: boolean; query: string } | null;
}

const state: AppState = {
  session: null,
  currentMonthKey: null,
  thumbSize: "L",
  lightbox: null,
  cull: null,
  selected: new Set<string>(),
  filter: "",
  selectionMove: null,
};

// ─── Helpers ─────────────────────────────────────────────────────────────────

function monthKey(year: number, month: number): string {
  return `${year}-${month}`;
}

function monthLabel(year: number, month: number): string {
  return `${MONTH_NAMES[month - 1]} ${year}`;
}

function shortMonth(year: number, month: number): string {
  return `${MONTH_NAMES[month - 1].slice(0, 3)} ${year}`;
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] ?? p;
}

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// Returns the suggested keeper path: uses the server-computed value when available
// (set during scan), falling back to client-side max blur_score for legacy sessions.
function keeperPath(group: ApiGroup): string | null {
  if (group.suggested_keeper) return group.suggested_keeper;
  let best: ApiPhoto | null = null;
  for (const p of group.photos) {
    if (!best || (p.blur_score ?? 0) > (best.blur_score ?? 0)) best = p;
  }
  return best ? best.path : null;
}

// Health score: 🟢 named & clean · 🟡 unnamed or flagged dupes · 🔴 pending purge.
function healthDot(group: ApiGroup): string {
  const hasRemovedPending = group.photos.some((p) => p.is_removed) && !group.applied;
  if (hasRemovedPending) return "🔴";
  const unnamed = group.name.trim() === "";
  const flaggedDupes = group.photos.some((p) => p.is_duplicate);
  if (unnamed || flaggedDupes) return "🟡";
  return "🟢";
}

function currentMonthGroups(): ApiGroup[] {
  if (!state.session || !state.currentMonthKey) return [];
  for (const m of state.session.months) {
    if (monthKey(m.year, m.month) === state.currentMonthKey) return m.groups;
  }
  return [];
}

function findGroup(id: string): ApiGroup | null {
  if (!state.session) return null;
  for (const m of state.session.months) {
    for (const g of m.groups) {
      if (g.id === id) return g;
    }
  }
  return null;
}

// Replace a photo in local state after a successful API patch.
function mergePhoto(updated: ApiPhoto): void {
  if (!state.session) return;
  for (const m of state.session.months) {
    for (const g of m.groups) {
      const i = g.photos.findIndex((p) => p.path === updated.path);
      if (i !== -1) g.photos[i] = updated;
    }
  }
}

// Return all ApiPhoto objects whose paths are in the given set.
function photosByPaths(paths: Set<string>): ApiPhoto[] {
  if (!state.session) return [];
  const out: ApiPhoto[] = [];
  for (const m of state.session.months) {
    for (const g of m.groups) {
      for (const p of g.photos) {
        if (paths.has(p.path)) out.push(p);
      }
    }
  }
  return out;
}

// ─── Toast notifications ─────────────────────────────────────────────────────

function toast(message: string, kind: "info" | "error" = "info"): void {
  const container = document.getElementById("toast-container");
  if (!container) return;
  const node = el("div", `toast toast-${kind}`, message);
  container.appendChild(node);
  // Trigger enter transition on next frame.
  requestAnimationFrame(() => node.classList.add("toast-show"));
  setTimeout(() => {
    node.classList.remove("toast-show");
    setTimeout(() => node.remove(), 300);
  }, 2500);
}

// ─── Thumbnail size ──────────────────────────────────────────────────────────

function setThumbSize(size: ThumbSize): void {
  state.thumbSize = size;
  document.documentElement.style.setProperty("--thumb-size", THUMB_PX[size]);
  render();
}

// ─── Photo actions (API-first, then local merge) ─────────────────────────────

async function patchPhoto(
  path: string,
  patch: { rotation?: number; is_removed?: boolean; is_duplicate?: boolean; new_name?: string },
): Promise<void> {
  try {
    const updated = await api.patchPhoto({ path, ...patch });
    mergePhoto(updated);
  } catch (err) {
    toast(`Save failed: ${(err as Error).message}`, "error");
    throw err;
  }
}

async function rotatePhoto(photo: ApiPhoto): Promise<void> {
  const next = ((photo.rotation ?? 0) + 90) % 360;
  await patchPhoto(photo.path, { rotation: next });
  toast("Saved");
  rerenderDynamic();
}

async function toggleRemoved(photo: ApiPhoto): Promise<void> {
  await patchPhoto(photo.path, { is_removed: !photo.is_removed });
  toast(photo.is_removed ? "Restored" : "Removed");
  rerenderDynamic();
}

async function toggleDuplicate(photo: ApiPhoto): Promise<void> {
  await patchPhoto(photo.path, { is_duplicate: !photo.is_duplicate });
  toast("Saved");
  rerenderDynamic();
}

async function renamePhoto(photo: ApiPhoto): Promise<void> {
  const next = window.prompt("New filename", photo.new_name || baseName(photo.path));
  if (next === null) return;
  await patchPhoto(photo.path, { new_name: next });
  toast("Saved");
  rerenderDynamic();
}

// ─── Group actions ───────────────────────────────────────────────────────────

async function saveGroupName(group: ApiGroup, name: string): Promise<void> {
  if (name === group.name) return;
  try {
    const updated = await api.patchGroup(group.id, name);
    group.name = updated.name;
    toast("Saved");
    renderSidebar();
  } catch (err) {
    toast(`Save failed: ${(err as Error).message}`, "error");
  }
}

async function applyGroup(group: ApiGroup): Promise<void> {
  try {
    const updated = await api.applyGroup(group.id);
    group.applied = updated.applied;
    group.name = updated.name;
    group.photos = updated.photos;
    toast("Applied ✓");
    render();
  } catch (err) {
    toast(`Apply failed: ${(err as Error).message}`, "error");
  }
}

// Apply every unapplied group in the current month in sequence, one render at end.
async function applyAllGroups(): Promise<void> {
  const groups = currentMonthGroups().filter((g) => !g.applied);
  if (groups.length === 0) {
    toast("All groups already applied");
    return;
  }
  let done = 0;
  for (const g of groups) {
    try {
      const updated = await api.applyGroup(g.id);
      g.applied = updated.applied;
      g.name = updated.name;
      g.photos = updated.photos;
      done++;
    } catch (err) {
      toast(`Apply failed for "${g.name || g.id}": ${(err as Error).message}`, "error");
    }
  }
  if (done) toast(`Applied ${done} group${done === 1 ? "" : "s"} ✓`);
  render();
}

// Purge dupes: mark every is_duplicate photo as is_removed, except the keeper.
async function purgeDupes(group: ApiGroup): Promise<void> {
  const keeper = keeperPath(group);
  const targets = group.photos.filter(
    (p) => p.is_duplicate && !p.is_removed && p.path !== keeper,
  );
  if (targets.length === 0) {
    toast("No dupes to queue");
    return;
  }
  let queued = 0;
  for (const p of targets) {
    try {
      await patchPhoto(p.path, { is_removed: true });
      queued += 1;
    } catch {
      // patchPhoto already toasted the error; keep going.
    }
  }
  toast(`${queued} photo${queued === 1 ? "" : "s"} queued for removal`);
  render();
}

// ─── Selection ───────────────────────────────────────────────────────────────

function toggleSelect(path: string): void {
  if (state.selected.has(path)) state.selected.delete(path);
  else state.selected.add(path);
  rerenderDynamic();
}

function groupHasSelection(group: ApiGroup): boolean {
  return group.photos.some((p) => state.selected.has(p.path));
}

// ─── Rendering ───────────────────────────────────────────────────────────────

function render(): void {
  const app = document.getElementById("app");
  if (!app) return;
  app.innerHTML = "";

  const sidebar = el("aside", "sidebar");
  sidebar.id = "sidebar";
  app.appendChild(sidebar);

  const content = el("main", "content");
  content.id = "content";
  app.appendChild(content);

  renderSidebar();
  renderContent();
  renderLightbox();
  renderCullHud();
  renderSelectionHud();
}

function renderSidebar(): void {
  const sidebar = document.getElementById("sidebar");
  if (!sidebar || !state.session) return;
  sidebar.innerHTML = "";

  const title = el("div", "sidebar-title", "Photo Manager");
  sidebar.appendChild(title);

  const palette = el("input", "palette") as HTMLInputElement;
  palette.id = "palette";
  palette.type = "text";
  palette.placeholder = "/ filter groups…";
  palette.value = state.filter;
  palette.addEventListener("input", () => {
    state.filter = palette.value;
    renderContent();
  });
  sidebar.appendChild(palette);

  const divider = el("div", "sidebar-divider");
  sidebar.appendChild(divider);

  const list = el("nav", "month-list");
  for (const m of state.session.months) {
    const key = monthKey(m.year, m.month);
    const item = el("button", "month-item");
    if (key === state.currentMonthKey) item.classList.add("active");

    const groupCount = m.groups.length;
    const photoCount = m.groups.reduce((sum, g) => sum + g.photos.length, 0);

    // Worst health across the month's groups drives the dot.
    let dot = "🟢";
    for (const g of m.groups) {
      const d = healthDot(g);
      if (d === "🔴") { dot = "🔴"; break; }
      if (d === "🟡") dot = "🟡";
    }

    const dotSpan = el("span", "month-dot", dot);
    const nameSpan = el("span", "month-name", shortMonth(m.year, m.month));
    const countSpan = el("span", "month-count", `${groupCount}/${photoCount}`);

    item.append(dotSpan, nameSpan, countSpan);
    item.addEventListener("click", () => {
      state.currentMonthKey = key;
      state.selected.clear();
      render();
    });
    list.appendChild(item);
  }
  sidebar.appendChild(list);

  const sizeWrap = el("div", "size-controls");
  sizeWrap.appendChild(el("div", "size-label", "Thumb size:"));
  const sizeRow = el("div", "size-row");
  (["L", "XL", "XXL"] as ThumbSize[]).forEach((s) => {
    const btn = el("button", "size-btn", s);
    if (state.thumbSize === s) btn.classList.add("active");
    btn.addEventListener("click", () => setThumbSize(s));
    sizeRow.appendChild(btn);
  });
  sizeWrap.appendChild(sizeRow);
  sidebar.appendChild(sizeWrap);
}

function renderContent(): void {
  const content = document.getElementById("content");
  if (!content || !state.session) return;
  content.innerHTML = "";

  if (!state.currentMonthKey) {
    content.appendChild(el("div", "empty", "Select a month from the sidebar."));
    return;
  }

  const month = state.session.months.find(
    (m) => monthKey(m.year, m.month) === state.currentMonthKey,
  );
  if (!month) {
    content.appendChild(el("div", "empty", "Month not found."));
    return;
  }

  const toolbar = el("div", "content-toolbar");
  toolbar.appendChild(el("h1", "month-header", monthLabel(month.year, month.month)));

  const unapplied = month.groups.filter((g) => !g.applied).length;
  const applyAllBtn = el("button", "btn btn-apply-all",
    unapplied > 0 ? `Apply all (${unapplied})` : "All applied ✓");
  applyAllBtn.disabled = unapplied === 0;
  applyAllBtn.addEventListener("click", () => { void applyAllGroups(); });
  toolbar.appendChild(applyAllBtn);

  content.appendChild(toolbar);

  const filter = state.filter.trim().toLowerCase();
  const groups = filter
    ? month.groups.filter((g) => g.name.toLowerCase().includes(filter))
    : month.groups;

  if (groups.length === 0) {
    content.appendChild(el("div", "empty", "No groups match."));
    return;
  }

  for (const g of groups) {
    content.appendChild(renderGroupCard(g));
  }
}

function renderGroupCard(group: ApiGroup): HTMLElement {
  const card = el("section", "group-card");
  card.dataset.groupId = group.id;
  if (group.applied) card.classList.add("applied");

  const head = el("div", "group-head");

  const nameInput = el("input", "group-name") as HTMLInputElement;
  nameInput.type = "text";
  nameInput.placeholder = "group name";
  nameInput.value = group.name;
  const commit = () => { void saveGroupName(group, nameInput.value); };
  nameInput.addEventListener("blur", commit);
  nameInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      nameInput.blur();
    }
  });
  head.appendChild(nameInput);

  const applyBtn = el("button", "btn btn-apply");
  if (group.applied) {
    applyBtn.textContent = "Applied ✓";
    applyBtn.disabled = true;
  } else {
    applyBtn.textContent = "Apply";
    applyBtn.addEventListener("click", () => { void applyGroup(group); });
  }
  head.appendChild(applyBtn);

  const purgeBtn = el("button", "btn btn-purge", "Purge Dupes");
  purgeBtn.title = "Queue duplicates for removal (keeps sharpest)";
  purgeBtn.addEventListener("click", () => { void purgeDupes(group); });
  head.appendChild(purgeBtn);

  if (group.applied) {
    head.appendChild(el("span", "applied-badge", "Applied ✓"));
  }

  card.appendChild(head);

  const grid = el("div", "grid");
  const keeper = keeperPath(group);
  group.photos.forEach((photo, idx) => {
    grid.appendChild(renderThumb(group, photo, idx, keeper));
  });
  card.appendChild(grid);

  return card;
}

function renderThumb(
  group: ApiGroup,
  photo: ApiPhoto,
  index: number,
  keeper: string | null,
): HTMLElement {
  const cell = el("div", "thumb");
  cell.dataset.path = photo.path;
  if (state.selected.has(photo.path)) cell.classList.add("selected");
  if (photo.is_removed) cell.classList.add("is-removed");

  // Culling cursor highlight.
  if (state.cull && state.cull.group.id === group.id && state.cull.index === index) {
    cell.classList.add("cull-cursor");
  }
  if (state.cull) cell.classList.add("cull-dim");

  const img = el("img", "thumb-img") as HTMLImageElement;
  img.src = api.thumbnailUrl(photo.path, state.thumbSize);
  img.alt = baseName(photo.path);
  img.loading = "eager";
  img.style.transform = `rotate(${photo.rotation ?? 0}deg)`;
  img.addEventListener("click", () => openLightbox(group, index));
  cell.appendChild(img);

  // Multi-select checkbox (top-left).
  const checkbox = el("input", "thumb-check") as HTMLInputElement;
  checkbox.type = "checkbox";
  checkbox.checked = state.selected.has(photo.path);
  if (groupHasSelection(group)) cell.classList.add("show-check");
  checkbox.addEventListener("click", (e) => {
    e.stopPropagation();
    toggleSelect(photo.path);
  });
  cell.appendChild(checkbox);

  // Badges (top-right).
  const badges = el("div", "badges");
  if (photo.is_removed) badges.appendChild(el("span", "badge badge-removed", "🗑"));
  if (photo.is_duplicate) badges.appendChild(el("span", "badge badge-dup", "⚠"));
  if (keeper && photo.path === keeper) {
    const starBadge = el("span", "badge badge-star", "★");
    starBadge.title = "Suggested keeper — sharpest photo in this group";
    badges.appendChild(starBadge);
  }
  cell.appendChild(badges);

  // Hover action bar.
  const actions = el("div", "thumb-actions");
  const mkAction = (label: string, title: string, fn: () => void) => {
    const b = el("button", "thumb-action", label);
    b.title = title;
    b.addEventListener("click", (e) => {
      e.stopPropagation();
      fn();
    });
    return b;
  };
  actions.append(
    mkAction("↻", "Rotate", () => { void rotatePhoto(photo); }),
    mkAction(photo.is_removed ? "↺" : "🗑", photo.is_removed ? "Restore" : "Remove", () => { void toggleRemoved(photo); }),
    mkAction("⚑", "Flag duplicate", () => { void toggleDuplicate(photo); }),
    mkAction("✎", "Rename", () => { void renamePhoto(photo); }),
  );
  cell.appendChild(actions);

  return cell;
}

// Re-render the active content + sidebar without rebuilding the whole shell.
// Cheap enough for this app size; keeps state changes simple and correct.
function rerenderDynamic(): void {
  renderSidebar();
  renderContent();
  renderLightbox();
  renderSelectionHud();
}

// ─── Lightbox ────────────────────────────────────────────────────────────────

function openLightbox(group: ApiGroup, index: number): void {
  state.lightbox = { group, index, showMovePicker: false, moveQuery: "" };
  renderLightbox();
}

function closeLightbox(): void {
  state.lightbox = null;
  renderLightbox();
}

function lightboxStep(delta: number): void {
  if (!state.lightbox) return;
  const total = state.lightbox.group.photos.length;
  state.lightbox.index = (state.lightbox.index + delta + total) % total;
  renderLightbox();
}

// allGroups returns every group in the session with its parent month, sorted
// chronologically then by name — used for the move picker.
function allGroups(): Array<{ group: ApiGroup; year: number; month: number }> {
  if (!state.session) return [];
  const out: Array<{ group: ApiGroup; year: number; month: number }> = [];
  for (const m of state.session.months) {
    for (const g of m.groups) {
      out.push({ group: g, year: m.year, month: m.month });
    }
  }
  return out;
}

// groupFullId returns the display identifier for a group: "YYYY-MM_name" or
// "YYYY-MM (unnamed)" when name is empty.
function groupFullId(year: number, month: number, name: string): string {
  const ym = `${year}-${String(month).padStart(2, "0")}`;
  return name ? `${ym}_${name}` : `${ym} (unnamed)`;
}

// parseGroupId parses "YYYY-MM_name" into {year, month, name}.
// Returns null if the format doesn't match.
function parseGroupId(s: string): { year: number; month: number; name: string } | null {
  const m = s.match(/^(\d{4})-(\d{2})_(.+)$/);
  if (!m) return null;
  return { year: parseInt(m[1], 10), month: parseInt(m[2], 10), name: m[3] };
}

async function movePhotoToGroup(photo: ApiPhoto, targetId: string): Promise<void> {
  try {
    const updated = await api.movePhoto(photo.path, targetId);
    if (!state.session) return;
    // Remove from source, update in target.
    for (const m of state.session.months) {
      for (const g of m.groups) {
        const i = g.photos.findIndex((p) => p.path === photo.path);
        if (i !== -1) g.photos.splice(i, 1);
      }
    }
    for (const m of state.session.months) {
      for (const g of m.groups) {
        if (g.id === targetId) g.photos.push(updated);
      }
    }
    toast("Moved ✓");
    closeLightbox();
    render();
  } catch (err) {
    toast(`Move failed: ${(err as Error).message}`, "error");
  }
}

async function movePhotoByLabel(photo: ApiPhoto, label: string): Promise<void> {
  // Try to find existing group by full id label first.
  const parsed = parseGroupId(label);
  if (!parsed) {
    toast("Use format YYYY-MM_name (e.g. 2024-10_paris)", "error");
    return;
  }
  // Check if a group with this label already exists.
  const existing = allGroups().find(
    ({ group, year, month }) =>
      year === parsed.year && month === parsed.month && group.name === parsed.name,
  );
  if (existing) {
    await movePhotoToGroup(photo, existing.group.id);
    return;
  }
  // Create the group then move.
  try {
    const newGroup = await api.createGroup(parsed.year, parsed.month, parsed.name);
    if (!state.session) return;
    // Insert into local state.
    let inserted = false;
    for (const m of state.session.months) {
      if (m.year === parsed.year && m.month === parsed.month) {
        m.groups.push(newGroup);
        inserted = true;
        break;
      }
    }
    if (!inserted) {
      state.session.months.push({
        year: parsed.year,
        month: parsed.month,
        groups: [newGroup],
      });
      state.session.months.sort((a, b) =>
        a.year !== b.year ? a.year - b.year : a.month - b.month,
      );
    }
    await movePhotoToGroup(photo, newGroup.id);
  } catch (err) {
    toast(`Failed to create group: ${(err as Error).message}`, "error");
  }
}

function renderLightbox(): void {
  const box = document.getElementById("lightbox");
  if (!box) return;

  if (!state.lightbox) {
    box.className = "hidden";
    box.innerHTML = "";
    return;
  }

  const { group, index, showMovePicker, moveQuery } = state.lightbox;
  const photo = group.photos[index];
  if (!photo) {
    closeLightbox();
    return;
  }

  box.className = "lightbox-overlay";
  box.innerHTML = "";

  const closeBtn = el("button", "lightbox-close", "✕");
  closeBtn.addEventListener("click", closeLightbox);
  box.appendChild(closeBtn);

  const prev = el("button", "lightbox-arrow lightbox-prev", "‹");
  prev.addEventListener("click", () => lightboxStep(-1));
  box.appendChild(prev);

  const img = el("img", "lightbox-img") as HTMLImageElement;
  img.src = api.rawUrl(photo.path);
  img.alt = baseName(photo.path);
  img.style.transform = `rotate(${photo.rotation ?? 0}deg)`;
  box.appendChild(img);

  const next = el("button", "lightbox-arrow lightbox-next", "›");
  next.addEventListener("click", () => lightboxStep(1));
  box.appendChild(next);

  // Action bar
  const actions = el("div", "lightbox-actions");

  const rotBtn = el("button", "lightbox-action", "↻ Rotate");
  rotBtn.addEventListener("click", async () => {
    await rotatePhoto(photo);
    renderLightbox();
  });
  actions.appendChild(rotBtn);

  const removeBtn = el("button", "lightbox-action" + (photo.is_removed ? " active" : ""),
    photo.is_removed ? "↺ Restore" : "🗑 Remove");
  removeBtn.addEventListener("click", async () => {
    await toggleRemoved(photo);
    renderLightbox();
  });
  actions.appendChild(removeBtn);

  const dupBtn = el("button", "lightbox-action" + (photo.is_duplicate ? " active" : ""), "⚑ Dup");
  dupBtn.addEventListener("click", async () => {
    await toggleDuplicate(photo);
    renderLightbox();
  });
  actions.appendChild(dupBtn);

  const moveBtn = el("button", "lightbox-action" + (showMovePicker ? " active" : ""), "→ Move…");
  moveBtn.addEventListener("click", () => {
    if (state.lightbox) {
      state.lightbox.showMovePicker = !state.lightbox.showMovePicker;
      state.lightbox.moveQuery = "";
    }
    renderLightbox();
  });
  actions.appendChild(moveBtn);

  box.appendChild(actions);

  // Move picker (shown when → Move… is active)
  if (showMovePicker) {
    const picker = el("div", "lightbox-picker");

    const input = el("input", "lightbox-picker-input") as HTMLInputElement;
    input.type = "text";
    input.placeholder = "2024-10_paris or select below";
    input.value = moveQuery;
    input.addEventListener("input", () => {
      if (state.lightbox) state.lightbox.moveQuery = input.value;
      renderLightbox();
    });
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        void movePhotoByLabel(photo, input.value.trim());
      }
    });
    picker.appendChild(input);

    const list = el("div", "lightbox-picker-list");
    const query = moveQuery.toLowerCase();
    const groups = allGroups().filter(({ group: g, year, month }) => {
      if (g.id === group.id) return false; // exclude current group
      const label = groupFullId(year, month, g.name).toLowerCase();
      return !query || label.includes(query);
    });
    for (const { group: g, year, month } of groups.slice(0, 12)) {
      const label = groupFullId(year, month, g.name);
      const item = el("button", "lightbox-picker-item", label);
      item.addEventListener("click", () => { void movePhotoToGroup(photo, g.id); });
      list.appendChild(item);
    }
    if (groups.length === 0) {
      list.appendChild(el("div", "lightbox-picker-empty", "No matching groups"));
    }
    picker.appendChild(list);

    const hint = el("div", "lightbox-picker-hint",
      "Type YYYY-MM_name + Enter to create a new group and move");
    picker.appendChild(hint);

    box.appendChild(picker);
  }

  // Info bar
  const bar = el("div", "lightbox-bar");
  const dims = `${photo.width ?? 0}×${photo.height ?? 0}`;
  const blur = (photo.blur_score ?? 0).toFixed(2);
  const date = photo.taken_at ? photo.taken_at : "no date";
  bar.textContent =
    `${baseName(photo.path)}  |  ${dims}  |  blur:${blur}  |  ${index + 1}/${group.photos.length}  |  ${date}`;
  box.appendChild(bar);
}

// ─── Culling mode ────────────────────────────────────────────────────────────

function enterCull(): void {
  if (state.lightbox) return;
  const groups = currentMonthGroups();
  if (groups.length === 0 || groups[0].photos.length === 0) {
    toast("Nothing to cull");
    return;
  }
  state.cull = { group: groups[0], index: 0 };
  render();
}

function exitCull(): void {
  state.cull = null;
  render();
}

// Advance the cursor across the current month's groups, in order.
function cullAdvance(): void {
  if (!state.cull) return;
  const groups = currentMonthGroups();
  let gi = groups.findIndex((g) => g.id === state.cull!.group.id);
  if (gi === -1) gi = 0;
  let idx = state.cull.index + 1;

  while (gi < groups.length) {
    if (idx < groups[gi].photos.length) {
      state.cull = { group: groups[gi], index: idx };
      render();
      scrollCursorIntoView();
      return;
    }
    gi += 1;
    idx = 0;
  }
  // Reached the end.
  toast("Culling complete");
  exitCull();
}

function scrollCursorIntoView(): void {
  const node = document.querySelector(".cull-cursor");
  if (node) (node as HTMLElement).scrollIntoView({ block: "center", behavior: "smooth" });
}

function cullCurrentPhoto(): ApiPhoto | null {
  if (!state.cull) return null;
  return state.cull.group.photos[state.cull.index] ?? null;
}

async function cullKeep(): Promise<void> {
  const photo = cullCurrentPhoto();
  if (photo && (photo.is_removed || photo.is_duplicate)) {
    await patchPhoto(photo.path, { is_removed: false, is_duplicate: false });
  }
  cullAdvance();
}

async function cullRemove(): Promise<void> {
  const photo = cullCurrentPhoto();
  if (photo && !photo.is_removed) {
    await patchPhoto(photo.path, { is_removed: true });
  }
  cullAdvance();
}

function cullSkip(): void {
  cullAdvance();
}

// ─── Selection actions ───────────────────────────────────────────────────────

async function batchPatch(
  paths: string[],
  patch: { is_removed?: boolean; is_duplicate?: boolean },
): Promise<void> {
  let failed = 0;
  await Promise.all(paths.map(async (p) => {
    try {
      const updated = await api.patchPhoto({ path: p, ...patch });
      mergePhoto(updated);
    } catch {
      failed++;
    }
  }));
  if (failed) toast(`${failed} operation${failed === 1 ? "" : "s"} failed`, "error");
}

async function moveSelectedToGroup(targetGroupId: string): Promise<void> {
  const paths = [...state.selected];
  let moved = 0;
  for (const path of paths) {
    try {
      const updated = await api.movePhoto(path, targetGroupId);
      if (state.session) {
        for (const m of state.session.months) {
          for (const g of m.groups) {
            const i = g.photos.findIndex((p) => p.path === path);
            if (i !== -1) g.photos.splice(i, 1);
          }
        }
        for (const m of state.session.months) {
          for (const g of m.groups) {
            if (g.id === targetGroupId) g.photos.push(updated);
          }
        }
        moved++;
      }
    } catch { /* skip failed moves */ }
  }
  state.selected.clear();
  state.selectionMove = null;
  if (moved) toast(`Moved ${moved} photo${moved === 1 ? "" : "s"} ✓`);
  render();
}

async function bulkMoveSelected(label: string): Promise<void> {
  const parsed = parseGroupId(label);
  if (!parsed) {
    toast("Use format YYYY-MM_name (e.g. 2024-10_paris)", "error");
    return;
  }
  const existing = allGroups().find(
    ({ group, year, month }) =>
      year === parsed.year && month === parsed.month && group.name === parsed.name,
  );
  if (existing) {
    await moveSelectedToGroup(existing.group.id);
    return;
  }
  try {
    const newGroup = await api.createGroup(parsed.year, parsed.month, parsed.name);
    if (!state.session) return;
    let inserted = false;
    for (const m of state.session.months) {
      if (m.year === parsed.year && m.month === parsed.month) {
        m.groups.push(newGroup);
        inserted = true;
        break;
      }
    }
    if (!inserted) {
      state.session.months.push({ year: parsed.year, month: parsed.month, groups: [newGroup] });
      state.session.months.sort((a, b) =>
        a.year !== b.year ? a.year - b.year : a.month - b.month,
      );
    }
    await moveSelectedToGroup(newGroup.id);
  } catch (err) {
    toast(`Failed: ${(err as Error).message}`, "error");
  }
}

function renderSelectionHud(): void {
  document.getElementById("selection-hud")?.remove();
  if (state.selected.size === 0) return;

  const hud = el("div", "selection-hud");
  hud.id = "selection-hud";

  // Move picker shown above the action row (flex column, rendered first)
  if (state.selectionMove?.open) {
    const picker = el("div", "selection-move-picker");

    const input = el("input", "lightbox-picker-input") as HTMLInputElement;
    input.type = "text";
    input.placeholder = "2024-10_paris or select below";
    input.value = state.selectionMove.query;
    input.addEventListener("input", () => {
      if (state.selectionMove) state.selectionMove.query = input.value;
      renderSelectionHud();
    });
    input.addEventListener("keydown", async (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        await bulkMoveSelected(input.value.trim());
      }
    });
    picker.appendChild(input);

    const list = el("div", "lightbox-picker-list");
    const query = state.selectionMove.query.toLowerCase();
    const groups = allGroups().filter(({ group: g, year, month }) => {
      const label = groupFullId(year, month, g.name).toLowerCase();
      return !query || label.includes(query);
    });
    for (const { group: g, year, month } of groups.slice(0, 12)) {
      const label = groupFullId(year, month, g.name);
      const item = el("button", "lightbox-picker-item", label);
      item.addEventListener("click", () => { void moveSelectedToGroup(g.id); });
      list.appendChild(item);
    }
    if (groups.length === 0) {
      list.appendChild(el("div", "lightbox-picker-empty", "No matching groups"));
    }
    picker.appendChild(list);
    picker.appendChild(el("div", "lightbox-picker-hint",
      "Type YYYY-MM_name + Enter to create a new group"));
    hud.appendChild(picker);
  }

  // Action row
  const row = el("div", "selection-hud-row");
  row.appendChild(el("span", "selection-count",
    `${state.selected.size} photo${state.selected.size === 1 ? "" : "s"}`));

  const removeBtn = el("button", "btn selection-action", "🗑 Remove");
  removeBtn.addEventListener("click", async () => {
    await batchPatch([...state.selected], { is_removed: true });
    rerenderDynamic();
    renderSelectionHud();
  });
  row.appendChild(removeBtn);

  const selectedPhotos = photosByPaths(state.selected);
  const allFlagged = selectedPhotos.length > 0 && selectedPhotos.every((p) => p.is_duplicate);
  const dupBtn = el("button",
    "btn selection-action" + (allFlagged ? " active" : ""),
    allFlagged ? "⚑ Unflag dupe" : "⚑ Flag dupe");
  dupBtn.addEventListener("click", async () => {
    await batchPatch([...state.selected], { is_duplicate: !allFlagged });
    rerenderDynamic();
    renderSelectionHud();
  });
  row.appendChild(dupBtn);

  const moveBtn = el("button",
    "btn selection-action" + (state.selectionMove?.open ? " active" : ""),
    "→ Move to…");
  moveBtn.addEventListener("click", () => {
    state.selectionMove = state.selectionMove?.open ? null : { open: true, query: "" };
    renderSelectionHud();
  });
  row.appendChild(moveBtn);

  const clearBtn = el("button", "btn selection-action", "✕ Clear");
  clearBtn.addEventListener("click", () => {
    state.selected.clear();
    state.selectionMove = null;
    rerenderDynamic();
    renderSelectionHud();
  });
  row.appendChild(clearBtn);

  hud.appendChild(row);
  document.body.appendChild(hud);
}

function renderCullHud(): void {
  // The HUD lives inside #app; render() rebuilds #app so we append here.
  if (!state.cull) return;
  const app = document.getElementById("app");
  if (!app) return;
  const hud = el(
    "div",
    "cull-hud",
    "Culling mode — K keep · D remove · Space skip · ESC exit",
  );
  app.appendChild(hud);
}

// ─── Keyboard shortcuts ──────────────────────────────────────────────────────

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || target.isContentEditable;
}

function onKeyDown(e: KeyboardEvent): void {
  // Culling mode captures most keys.
  if (state.cull) {
    switch (e.key.toLowerCase()) {
      case "k":
        e.preventDefault();
        void cullKeep();
        return;
      case "d":
        e.preventDefault();
        void cullRemove();
        return;
      case " ":
        e.preventDefault();
        cullSkip();
        return;
      case "escape":
        e.preventDefault();
        exitCull();
        return;
      default:
        return;
    }
  }

  // Lightbox navigation.
  if (state.lightbox) {
    if (e.key === "Escape") {
      e.preventDefault();
      closeLightbox();
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      lightboxStep(-1);
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      lightboxStep(1);
    }
    return;
  }

  // Command palette focus on "/".
  if (e.key === "/" && !isTypingTarget(e.target)) {
    e.preventDefault();
    document.getElementById("palette")?.focus();
    return;
  }

  // Don't fire single-letter shortcuts while typing.
  if (isTypingTarget(e.target)) return;

  switch (e.key.toLowerCase()) {
    case "l":
      setThumbSize("L");
      break;
    case "x":
      setThumbSize("XL");
      break;
    case "z":
      setThumbSize("XXL");
      break;
    case "c":
      enterCull();
      break;
    default:
      break;
  }
}

// ─── Bootstrap ───────────────────────────────────────────────────────────────

export async function init(): Promise<void> {
  document.documentElement.style.setProperty("--thumb-size", THUMB_PX[state.thumbSize]);
  document.addEventListener("keydown", onKeyDown);

  fetch("/version.json")
    .then((r) => r.json())
    .then(({ v }: { v: string }) => {
      const badge = document.getElementById("version-badge");
      if (badge) badge.textContent = v;
    })
    .catch(() => { /* version badge is cosmetic, ignore errors */ });

  try {
    const session = await api.getSession();
    state.session = session;
    if (session.months.length > 0) {
      const first = session.months[0];
      state.currentMonthKey = monthKey(first.year, first.month);
    }
    render();
  } catch (err) {
    const app = document.getElementById("app");
    if (app) {
      app.innerHTML = "";
      const fail = el(
        "div",
        "load-error",
        `Could not reach server at ${window.location.origin} — ${(err as Error).message}`,
      );
      app.appendChild(fail);
    }
    toast("Failed to connect to server", "error");
  }
}
