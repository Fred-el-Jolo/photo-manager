// src/app/api.ts
var BASE = "";
async function request(path, init) {
  const res = await fetch(`${BASE}${path}`, init);
  if (!res.ok) {
    let detail = "";
    try {
      detail = await res.text();
    } catch {
      detail = "";
    }
    throw new Error(`${init?.method ?? "GET"} ${path} failed: ${res.status} ${res.statusText}${detail ? ` — ${detail}` : ""}`);
  }
  const text = await res.text();
  if (!text) {
    return;
  }
  return JSON.parse(text);
}
function jsonInit(method, body) {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  };
}
var api = {
  getSession() {
    return request("/api/session");
  },
  getMonths() {
    return request("/api/months");
  },
  getGroup(id) {
    return request(`/api/groups/${encodeURIComponent(id)}`);
  },
  patchGroup(id, name) {
    return request(`/api/groups/${encodeURIComponent(id)}`, jsonInit("PATCH", { name }));
  },
  applyGroup(id) {
    return request(`/api/groups/${encodeURIComponent(id)}/apply`, { method: "POST" });
  },
  patchPhoto(patch) {
    return request("/api/photos", jsonInit("PATCH", patch));
  },
  createGroup(year, month, name) {
    return request("/api/groups", jsonInit("POST", { year, month, name }));
  },
  movePhoto(path, targetGroupId) {
    return request("/api/photos", jsonInit("PATCH", { path, target_group_id: targetGroupId }));
  },
  thumbnailUrl(path, size) {
    return `/api/thumbnail?path=${encodeURIComponent(path)}&size=${size}`;
  },
  rawUrl(path) {
    return `/api/raw?path=${encodeURIComponent(path)}`;
  }
};

// src/app/app.ts
var THUMB_PX = {
  L: "160px",
  XL: "280px",
  XXL: "420px"
};
var MONTH_NAMES = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December"
];
var state = {
  session: null,
  currentMonthKey: null,
  thumbSize: "L",
  lightbox: null,
  cull: null,
  selected: new Set,
  filter: "",
  selectionMove: null
};
function monthKey(year, month) {
  return `${year}-${month}`;
}
function monthLabel(year, month) {
  return `${MONTH_NAMES[month - 1]} ${year}`;
}
function shortMonth(year, month) {
  return `${MONTH_NAMES[month - 1].slice(0, 3)} ${year}`;
}
function baseName(p) {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] ?? p;
}
function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className)
    node.className = className;
  if (text !== undefined)
    node.textContent = text;
  return node;
}
function keeperPath(group) {
  if (group.suggested_keeper)
    return group.suggested_keeper;
  let best = null;
  for (const p of group.photos) {
    if (!best || (p.blur_score ?? 0) > (best.blur_score ?? 0))
      best = p;
  }
  return best ? best.path : null;
}
function healthDot(group) {
  const hasRemovedPending = group.photos.some((p) => p.is_removed) && !group.applied;
  if (hasRemovedPending)
    return "\uD83D\uDD34";
  const unnamed = group.name.trim() === "";
  const flaggedDupes = group.photos.some((p) => p.is_duplicate);
  if (unnamed || flaggedDupes)
    return "\uD83D\uDFE1";
  return "\uD83D\uDFE2";
}
function currentMonthGroups() {
  if (!state.session || !state.currentMonthKey)
    return [];
  for (const m of state.session.months) {
    if (monthKey(m.year, m.month) === state.currentMonthKey)
      return m.groups;
  }
  return [];
}
function mergePhoto(updated) {
  if (!state.session)
    return;
  for (const m of state.session.months) {
    for (const g of m.groups) {
      const i = g.photos.findIndex((p) => p.path === updated.path);
      if (i !== -1)
        g.photos[i] = updated;
    }
  }
}
function toast(message, kind = "info") {
  const container = document.getElementById("toast-container");
  if (!container)
    return;
  const node = el("div", `toast toast-${kind}`, message);
  container.appendChild(node);
  requestAnimationFrame(() => node.classList.add("toast-show"));
  setTimeout(() => {
    node.classList.remove("toast-show");
    setTimeout(() => node.remove(), 300);
  }, 2500);
}
function setThumbSize(size) {
  state.thumbSize = size;
  document.documentElement.style.setProperty("--thumb-size", THUMB_PX[size]);
  render();
}
async function patchPhoto(path, patch) {
  try {
    const updated = await api.patchPhoto({ path, ...patch });
    mergePhoto(updated);
  } catch (err) {
    toast(`Save failed: ${err.message}`, "error");
    throw err;
  }
}
async function rotatePhoto(photo) {
  const next = ((photo.rotation ?? 0) + 90) % 360;
  await patchPhoto(photo.path, { rotation: next });
  toast("Saved");
  rerenderDynamic();
}
async function toggleRemoved(photo) {
  await patchPhoto(photo.path, { is_removed: !photo.is_removed });
  toast(photo.is_removed ? "Restored" : "Removed");
  rerenderDynamic();
}
async function toggleDuplicate(photo) {
  await patchPhoto(photo.path, { is_duplicate: !photo.is_duplicate });
  toast("Saved");
  rerenderDynamic();
}
async function renamePhoto(photo) {
  const next = window.prompt("New filename", photo.new_name || baseName(photo.path));
  if (next === null)
    return;
  await patchPhoto(photo.path, { new_name: next });
  toast("Saved");
  rerenderDynamic();
}
async function saveGroupName(group, name) {
  if (name === group.name)
    return;
  try {
    const updated = await api.patchGroup(group.id, name);
    group.name = updated.name;
    toast("Saved");
    renderSidebar();
  } catch (err) {
    toast(`Save failed: ${err.message}`, "error");
  }
}
async function applyGroup(group) {
  try {
    const updated = await api.applyGroup(group.id);
    group.applied = updated.applied;
    group.name = updated.name;
    group.photos = updated.photos;
    toast("Applied ✓");
    render();
  } catch (err) {
    toast(`Apply failed: ${err.message}`, "error");
  }
}
async function purgeDupes(group) {
  const keeper = keeperPath(group);
  const targets = group.photos.filter((p) => p.is_duplicate && !p.is_removed && p.path !== keeper);
  if (targets.length === 0) {
    toast("No dupes to queue");
    return;
  }
  let queued = 0;
  for (const p of targets) {
    try {
      await patchPhoto(p.path, { is_removed: true });
      queued += 1;
    } catch {}
  }
  toast(`${queued} photo${queued === 1 ? "" : "s"} queued for removal`);
  render();
}
function toggleSelect(path) {
  if (state.selected.has(path))
    state.selected.delete(path);
  else
    state.selected.add(path);
  rerenderDynamic();
}
function groupHasSelection(group) {
  return group.photos.some((p) => state.selected.has(p.path));
}
function render() {
  const app = document.getElementById("app");
  if (!app)
    return;
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
function renderSidebar() {
  const sidebar = document.getElementById("sidebar");
  if (!sidebar || !state.session)
    return;
  sidebar.innerHTML = "";
  const title = el("div", "sidebar-title", "Photo Manager");
  sidebar.appendChild(title);
  const palette = el("input", "palette");
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
    if (key === state.currentMonthKey)
      item.classList.add("active");
    const groupCount = m.groups.length;
    const photoCount = m.groups.reduce((sum, g) => sum + g.photos.length, 0);
    let dot = "\uD83D\uDFE2";
    for (const g of m.groups) {
      const d = healthDot(g);
      if (d === "\uD83D\uDD34") {
        dot = "\uD83D\uDD34";
        break;
      }
      if (d === "\uD83D\uDFE1")
        dot = "\uD83D\uDFE1";
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
  ["L", "XL", "XXL"].forEach((s) => {
    const btn = el("button", "size-btn", s);
    if (state.thumbSize === s)
      btn.classList.add("active");
    btn.addEventListener("click", () => setThumbSize(s));
    sizeRow.appendChild(btn);
  });
  sizeWrap.appendChild(sizeRow);
  sidebar.appendChild(sizeWrap);
}
function renderContent() {
  const content = document.getElementById("content");
  if (!content || !state.session)
    return;
  content.innerHTML = "";
  if (!state.currentMonthKey) {
    content.appendChild(el("div", "empty", "Select a month from the sidebar."));
    return;
  }
  const month = state.session.months.find((m) => monthKey(m.year, m.month) === state.currentMonthKey);
  if (!month) {
    content.appendChild(el("div", "empty", "Month not found."));
    return;
  }
  const header = el("h1", "month-header", monthLabel(month.year, month.month));
  content.appendChild(header);
  const filter = state.filter.trim().toLowerCase();
  const groups = filter ? month.groups.filter((g) => g.name.toLowerCase().includes(filter)) : month.groups;
  if (groups.length === 0) {
    content.appendChild(el("div", "empty", "No groups match."));
    return;
  }
  for (const g of groups) {
    content.appendChild(renderGroupCard(g));
  }
}
function renderGroupCard(group) {
  const card = el("section", "group-card");
  card.dataset.groupId = group.id;
  if (group.applied)
    card.classList.add("applied");
  const head = el("div", "group-head");
  const nameInput = el("input", "group-name");
  nameInput.type = "text";
  nameInput.placeholder = "group name";
  nameInput.value = group.name;
  const commit = () => {
    saveGroupName(group, nameInput.value);
  };
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
    applyBtn.addEventListener("click", () => {
      applyGroup(group);
    });
  }
  head.appendChild(applyBtn);
  const purgeBtn = el("button", "btn btn-purge", "Purge Dupes");
  purgeBtn.title = "Queue duplicates for removal (keeps sharpest)";
  purgeBtn.addEventListener("click", () => {
    purgeDupes(group);
  });
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
function renderThumb(group, photo, index, keeper) {
  const cell = el("div", "thumb");
  cell.dataset.path = photo.path;
  if (state.selected.has(photo.path))
    cell.classList.add("selected");
  if (photo.is_removed)
    cell.classList.add("is-removed");
  if (state.cull && state.cull.group.id === group.id && state.cull.index === index) {
    cell.classList.add("cull-cursor");
  }
  if (state.cull)
    cell.classList.add("cull-dim");
  const img = el("img", "thumb-img");
  img.src = api.thumbnailUrl(photo.path, state.thumbSize);
  img.alt = baseName(photo.path);
  img.loading = "eager";
  img.style.transform = `rotate(${photo.rotation ?? 0}deg)`;
  img.addEventListener("click", () => openLightbox(group, index));
  cell.appendChild(img);
  const checkbox = el("input", "thumb-check");
  checkbox.type = "checkbox";
  checkbox.checked = state.selected.has(photo.path);
  if (groupHasSelection(group))
    cell.classList.add("show-check");
  checkbox.addEventListener("click", (e) => {
    e.stopPropagation();
    toggleSelect(photo.path);
  });
  cell.appendChild(checkbox);
  const badges = el("div", "badges");
  if (photo.is_removed)
    badges.appendChild(el("span", "badge badge-removed", "\uD83D\uDDD1"));
  if (photo.is_duplicate)
    badges.appendChild(el("span", "badge badge-dup", "⚠"));
  if (keeper && photo.path === keeper)
    badges.appendChild(el("span", "badge badge-star", "★"));
  cell.appendChild(badges);
  const actions = el("div", "thumb-actions");
  const mkAction = (label, title, fn) => {
    const b = el("button", "thumb-action", label);
    b.title = title;
    b.addEventListener("click", (e) => {
      e.stopPropagation();
      fn();
    });
    return b;
  };
  actions.append(mkAction("↻", "Rotate", () => {
    rotatePhoto(photo);
  }), mkAction(photo.is_removed ? "↺" : "\uD83D\uDDD1", photo.is_removed ? "Restore" : "Remove", () => {
    toggleRemoved(photo);
  }), mkAction("⚑", "Flag duplicate", () => {
    toggleDuplicate(photo);
  }), mkAction("✎", "Rename", () => {
    renamePhoto(photo);
  }));
  cell.appendChild(actions);
  return cell;
}
function rerenderDynamic() {
  renderSidebar();
  renderContent();
  renderLightbox();
  renderSelectionHud();
}
function openLightbox(group, index) {
  state.lightbox = { group, index, showMovePicker: false, moveQuery: "" };
  renderLightbox();
}
function closeLightbox() {
  state.lightbox = null;
  renderLightbox();
}
function lightboxStep(delta) {
  if (!state.lightbox)
    return;
  const total = state.lightbox.group.photos.length;
  state.lightbox.index = (state.lightbox.index + delta + total) % total;
  renderLightbox();
}
function allGroups() {
  if (!state.session)
    return [];
  const out = [];
  for (const m of state.session.months) {
    for (const g of m.groups) {
      out.push({ group: g, year: m.year, month: m.month });
    }
  }
  return out;
}
function groupFullId(year, month, name) {
  const ym = `${year}-${String(month).padStart(2, "0")}`;
  return name ? `${ym}_${name}` : `${ym} (unnamed)`;
}
function parseGroupId(s) {
  const m = s.match(/^(\d{4})-(\d{2})_(.+)$/);
  if (!m)
    return null;
  return { year: parseInt(m[1], 10), month: parseInt(m[2], 10), name: m[3] };
}
async function movePhotoToGroup(photo, targetId) {
  try {
    const updated = await api.movePhoto(photo.path, targetId);
    if (!state.session)
      return;
    for (const m of state.session.months) {
      for (const g of m.groups) {
        const i = g.photos.findIndex((p) => p.path === photo.path);
        if (i !== -1)
          g.photos.splice(i, 1);
      }
    }
    for (const m of state.session.months) {
      for (const g of m.groups) {
        if (g.id === targetId)
          g.photos.push(updated);
      }
    }
    toast("Moved ✓");
    closeLightbox();
    render();
  } catch (err) {
    toast(`Move failed: ${err.message}`, "error");
  }
}
async function movePhotoByLabel(photo, label) {
  const parsed = parseGroupId(label);
  if (!parsed) {
    toast("Use format YYYY-MM_name (e.g. 2024-10_paris)", "error");
    return;
  }
  const existing = allGroups().find(({ group, year, month }) => year === parsed.year && month === parsed.month && group.name === parsed.name);
  if (existing) {
    await movePhotoToGroup(photo, existing.group.id);
    return;
  }
  try {
    const newGroup = await api.createGroup(parsed.year, parsed.month, parsed.name);
    if (!state.session)
      return;
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
        groups: [newGroup]
      });
      state.session.months.sort((a, b) => a.year !== b.year ? a.year - b.year : a.month - b.month);
    }
    await movePhotoToGroup(photo, newGroup.id);
  } catch (err) {
    toast(`Failed to create group: ${err.message}`, "error");
  }
}
function renderLightbox() {
  const box = document.getElementById("lightbox");
  if (!box)
    return;
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
  const img = el("img", "lightbox-img");
  img.src = api.rawUrl(photo.path);
  img.alt = baseName(photo.path);
  img.style.transform = `rotate(${photo.rotation ?? 0}deg)`;
  box.appendChild(img);
  const next = el("button", "lightbox-arrow lightbox-next", "›");
  next.addEventListener("click", () => lightboxStep(1));
  box.appendChild(next);
  const actions = el("div", "lightbox-actions");
  const rotBtn = el("button", "lightbox-action", "↻ Rotate");
  rotBtn.addEventListener("click", async () => {
    await rotatePhoto(photo);
    renderLightbox();
  });
  actions.appendChild(rotBtn);
  const removeBtn = el("button", "lightbox-action" + (photo.is_removed ? " active" : ""), photo.is_removed ? "↺ Restore" : "\uD83D\uDDD1 Remove");
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
  if (showMovePicker) {
    const picker = el("div", "lightbox-picker");
    const input = el("input", "lightbox-picker-input");
    input.type = "text";
    input.placeholder = "2024-10_paris or select below";
    input.value = moveQuery;
    input.addEventListener("input", () => {
      if (state.lightbox)
        state.lightbox.moveQuery = input.value;
      renderLightbox();
    });
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        movePhotoByLabel(photo, input.value.trim());
      }
    });
    picker.appendChild(input);
    const list = el("div", "lightbox-picker-list");
    const query = moveQuery.toLowerCase();
    const groups = allGroups().filter(({ group: g, year, month }) => {
      if (g.id === group.id)
        return false;
      const label = groupFullId(year, month, g.name).toLowerCase();
      return !query || label.includes(query);
    });
    for (const { group: g, year, month } of groups.slice(0, 12)) {
      const label = groupFullId(year, month, g.name);
      const item = el("button", "lightbox-picker-item", label);
      item.addEventListener("click", () => {
        movePhotoToGroup(photo, g.id);
      });
      list.appendChild(item);
    }
    if (groups.length === 0) {
      list.appendChild(el("div", "lightbox-picker-empty", "No matching groups"));
    }
    picker.appendChild(list);
    const hint = el("div", "lightbox-picker-hint", "Type YYYY-MM_name + Enter to create a new group and move");
    picker.appendChild(hint);
    box.appendChild(picker);
  }
  const bar = el("div", "lightbox-bar");
  const dims = `${photo.width ?? 0}×${photo.height ?? 0}`;
  const blur = (photo.blur_score ?? 0).toFixed(2);
  const date = photo.taken_at ? photo.taken_at : "no date";
  bar.textContent = `${baseName(photo.path)}  |  ${dims}  |  blur:${blur}  |  ${index + 1}/${group.photos.length}  |  ${date}`;
  box.appendChild(bar);
}
function enterCull() {
  if (state.lightbox)
    return;
  const groups = currentMonthGroups();
  if (groups.length === 0 || groups[0].photos.length === 0) {
    toast("Nothing to cull");
    return;
  }
  state.cull = { group: groups[0], index: 0 };
  render();
}
function exitCull() {
  state.cull = null;
  render();
}
function cullAdvance() {
  if (!state.cull)
    return;
  const groups = currentMonthGroups();
  let gi = groups.findIndex((g) => g.id === state.cull.group.id);
  if (gi === -1)
    gi = 0;
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
  toast("Culling complete");
  exitCull();
}
function scrollCursorIntoView() {
  const node = document.querySelector(".cull-cursor");
  if (node)
    node.scrollIntoView({ block: "center", behavior: "smooth" });
}
function cullCurrentPhoto() {
  if (!state.cull)
    return null;
  return state.cull.group.photos[state.cull.index] ?? null;
}
async function cullKeep() {
  const photo = cullCurrentPhoto();
  if (photo && (photo.is_removed || photo.is_duplicate)) {
    await patchPhoto(photo.path, { is_removed: false, is_duplicate: false });
  }
  cullAdvance();
}
async function cullRemove() {
  const photo = cullCurrentPhoto();
  if (photo && !photo.is_removed) {
    await patchPhoto(photo.path, { is_removed: true });
  }
  cullAdvance();
}
function cullSkip() {
  cullAdvance();
}
async function batchPatch(paths, patch) {
  let failed = 0;
  await Promise.all(paths.map(async (p) => {
    try {
      const updated = await api.patchPhoto({ path: p, ...patch });
      mergePhoto(updated);
    } catch {
      failed++;
    }
  }));
  if (failed)
    toast(`${failed} operation${failed === 1 ? "" : "s"} failed`, "error");
}
async function moveSelectedToGroup(targetGroupId) {
  const paths = [...state.selected];
  let moved = 0;
  for (const path of paths) {
    try {
      const updated = await api.movePhoto(path, targetGroupId);
      if (state.session) {
        for (const m of state.session.months) {
          for (const g of m.groups) {
            const i = g.photos.findIndex((p) => p.path === path);
            if (i !== -1)
              g.photos.splice(i, 1);
          }
        }
        for (const m of state.session.months) {
          for (const g of m.groups) {
            if (g.id === targetGroupId)
              g.photos.push(updated);
          }
        }
        moved++;
      }
    } catch {}
  }
  state.selected.clear();
  state.selectionMove = null;
  if (moved)
    toast(`Moved ${moved} photo${moved === 1 ? "" : "s"} ✓`);
  render();
}
async function bulkMoveSelected(label) {
  const parsed = parseGroupId(label);
  if (!parsed) {
    toast("Use format YYYY-MM_name (e.g. 2024-10_paris)", "error");
    return;
  }
  const existing = allGroups().find(({ group, year, month }) => year === parsed.year && month === parsed.month && group.name === parsed.name);
  if (existing) {
    await moveSelectedToGroup(existing.group.id);
    return;
  }
  try {
    const newGroup = await api.createGroup(parsed.year, parsed.month, parsed.name);
    if (!state.session)
      return;
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
      state.session.months.sort((a, b) => a.year !== b.year ? a.year - b.year : a.month - b.month);
    }
    await moveSelectedToGroup(newGroup.id);
  } catch (err) {
    toast(`Failed: ${err.message}`, "error");
  }
}
function renderSelectionHud() {
  document.getElementById("selection-hud")?.remove();
  if (state.selected.size === 0)
    return;
  const hud = el("div", "selection-hud");
  hud.id = "selection-hud";
  if (state.selectionMove?.open) {
    const picker = el("div", "selection-move-picker");
    const input = el("input", "lightbox-picker-input");
    input.type = "text";
    input.placeholder = "2024-10_paris or select below";
    input.value = state.selectionMove.query;
    input.addEventListener("input", () => {
      if (state.selectionMove)
        state.selectionMove.query = input.value;
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
      item.addEventListener("click", () => {
        moveSelectedToGroup(g.id);
      });
      list.appendChild(item);
    }
    if (groups.length === 0) {
      list.appendChild(el("div", "lightbox-picker-empty", "No matching groups"));
    }
    picker.appendChild(list);
    picker.appendChild(el("div", "lightbox-picker-hint", "Type YYYY-MM_name + Enter to create a new group"));
    hud.appendChild(picker);
  }
  const row = el("div", "selection-hud-row");
  row.appendChild(el("span", "selection-count", `${state.selected.size} photo${state.selected.size === 1 ? "" : "s"}`));
  const removeBtn = el("button", "btn selection-action", "\uD83D\uDDD1 Remove");
  removeBtn.addEventListener("click", async () => {
    const paths = [...state.selected];
    await batchPatch(paths, { is_removed: true });
    state.selected.clear();
    state.selectionMove = null;
    rerenderDynamic();
    renderSelectionHud();
  });
  row.appendChild(removeBtn);
  const dupBtn = el("button", "btn selection-action", "⚑ Flag dupe");
  dupBtn.addEventListener("click", async () => {
    const paths = [...state.selected];
    await batchPatch(paths, { is_duplicate: true });
    state.selected.clear();
    state.selectionMove = null;
    rerenderDynamic();
    renderSelectionHud();
  });
  row.appendChild(dupBtn);
  const moveBtn = el("button", "btn selection-action" + (state.selectionMove?.open ? " active" : ""), "→ Move to…");
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
function renderCullHud() {
  if (!state.cull)
    return;
  const app = document.getElementById("app");
  if (!app)
    return;
  const hud = el("div", "cull-hud", "Culling mode — K keep · D remove · Space skip · ESC exit");
  app.appendChild(hud);
}
function isTypingTarget(target) {
  if (!(target instanceof HTMLElement))
    return false;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || target.isContentEditable;
}
function onKeyDown(e) {
  if (state.cull) {
    switch (e.key.toLowerCase()) {
      case "k":
        e.preventDefault();
        cullKeep();
        return;
      case "d":
        e.preventDefault();
        cullRemove();
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
  if (e.key === "/" && !isTypingTarget(e.target)) {
    e.preventDefault();
    document.getElementById("palette")?.focus();
    return;
  }
  if (isTypingTarget(e.target))
    return;
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
async function init() {
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
      const fail = el("div", "load-error", `Could not reach server at ${window.location.origin} — ${err.message}`);
      app.appendChild(fail);
    }
    toast("Failed to connect to server", "error");
  }
}

// src/app/main.ts
init();
