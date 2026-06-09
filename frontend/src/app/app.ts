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
}

const state: AppState = {
  session: null,
  currentMonthKey: null,
  thumbSize: "L",
  lightbox: null,
  cull: null,
  selected: new Set<string>(),
  filter: "",
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
    if (!best || p.blur_score > best.blur_score) best = p;
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
  const next = (photo.rotation + 90) % 360;
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

  const header = el("h1", "month-header", monthLabel(month.year, month.month));
  content.appendChild(header);

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
  nameInput.placeholder = "2024-03_event-name";
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
  img.style.transform = `rotate(${photo.rotation}deg)`;
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
  if (keeper && photo.path === keeper) badges.appendChild(el("span", "badge badge-star", "★"));
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
}

// ─── Lightbox ────────────────────────────────────────────────────────────────

function openLightbox(group: ApiGroup, index: number): void {
  state.lightbox = { group, index };
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

function renderLightbox(): void {
  const box = document.getElementById("lightbox");
  if (!box) return;

  if (!state.lightbox) {
    box.className = "hidden";
    box.innerHTML = "";
    return;
  }

  const { group, index } = state.lightbox;
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
  img.style.transform = `rotate(${photo.rotation}deg)`;
  box.appendChild(img);

  const next = el("button", "lightbox-arrow lightbox-next", "›");
  next.addEventListener("click", () => lightboxStep(1));
  box.appendChild(next);

  const bar = el("div", "lightbox-bar");
  const dims = `${photo.width}×${photo.height}`;
  const blur = photo.blur_score.toFixed(2);
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
