import { Electroview } from "electrobun/view";
import type { Group, PhotoMeta, PhotoManagerRPC } from "../types";

// ─── State ────────────────────────────────────────────────────────────────────

let groups: Group[] = [];
let photos: Record<string, PhotoMeta> = {};
let imagePort = 0;
let groupIdx = 0;
let keeperPath = "";
const toDelete = new Set<string>();

// ─── DOM refs ────────────────────────────────────────────────────────────────

const loadingEl = document.getElementById("loading")!;
const headerEl = document.getElementById("header")!;
const gridEl = document.getElementById("photo-grid")!;
const doneMsgEl = document.getElementById("done-msg")!;
const counterEl = document.getElementById("group-counter")!;
const btnSkip = document.getElementById("btn-skip") as HTMLButtonElement;
const btnConfirm = document.getElementById("btn-confirm") as HTMLButtonElement;


// ─── Helpers ─────────────────────────────────────────────────────────────────

function imageUrl(path: string): string {
  return `http://localhost:${imagePort}/?path=${encodeURIComponent(path)}`;
}

function formatDate(iso?: string): string {
  if (!iso) return "Date unknown";
  const d = new Date(iso);
  return isNaN(d.getTime()) ? "Date unknown" : d.toLocaleString();
}

// ─── Render ───────────────────────────────────────────────────────────────────

function renderGroup(group: Group): void {
  gridEl.innerHTML = "";
  keeperPath = group.paths[0];
  btnConfirm.disabled = false;

  for (const path of group.paths) {
    const meta: PhotoMeta | undefined = photos[path];
    const filename = path.split("/").pop() ?? path;

    const card = document.createElement("div");
    card.className = "photo-card" + (path === keeperPath ? " keeper" : "");
    card.dataset.path = path;

    const imgWrap = document.createElement("div");
    imgWrap.className = "photo-img-wrap";

    const img = document.createElement("img");
    img.alt = filename;
    img.src = imageUrl(path);

    const badge = document.createElement("div");
    badge.className = "keep-badge";
    badge.textContent = "★ KEEP";

    imgWrap.appendChild(img);
    imgWrap.appendChild(badge);

    const metaEl = document.createElement("div");
    metaEl.className = "photo-meta";

    const nameEl = document.createElement("div");
    nameEl.className = "photo-name";
    nameEl.title = path;
    nameEl.textContent = filename;

    const detailEl = document.createElement("div");
    detailEl.className = "photo-details";
    detailEl.textContent = formatDate(meta?.taken_at);

    metaEl.appendChild(nameEl);
    metaEl.appendChild(detailEl);
    card.appendChild(imgWrap);
    card.appendChild(metaEl);

    card.addEventListener("click", () => selectKeeper(path));
    gridEl.appendChild(card);
  }
}

function selectKeeper(path: string): void {
  keeperPath = path;
  for (const card of gridEl.querySelectorAll<HTMLElement>(".photo-card")) {
    card.classList.toggle("keeper", card.dataset.path === path);
  }
}

function showGroup(idx: number): void {
  groupIdx = idx;
  counterEl.textContent = `Group ${idx + 1} / ${groups.length}`;
  renderGroup(groups[idx]);
  gridEl.scrollTop = 0;
}

async function advanceOrFinish(): Promise<void> {
  const next = groupIdx + 1;
  if (next >= groups.length) {
    await sendDone();
    return;
  }
  showGroup(next);
}

// ─── Buttons ─────────────────────────────────────────────────────────────────

btnConfirm.addEventListener("click", async () => {
  for (const path of groups[groupIdx].paths) {
    if (path !== keeperPath) toDelete.add(path);
  }
  await advanceOrFinish();
});

btnSkip.addEventListener("click", async () => { await advanceOrFinish(); });

// ─── Send result ──────────────────────────────────────────────────────────────

async function sendDone(): Promise<void> {
  headerEl.style.display = "none";
  gridEl.style.display = "none";
  doneMsgEl.style.display = "flex";
  await rpc.requestProxy.done({ toDelete: [...toDelete] });
}

// ─── RPC — receives data pushed by bun after dom-ready ───────────────────────

const rpc = Electroview.defineRPC<PhotoManagerRPC>({
  handlers: {
    requests: {
      init(data) {
        groups = data.groups;
        photos = data.photos;
        imagePort = data.imagePort;

        loadingEl.style.display = "none";

        if (groups.length === 0) {
          doneMsgEl.style.display = "flex";
        } else {
          headerEl.style.display = "flex";
          gridEl.style.display = "flex";
          showGroup(0);
        }
      },
    },
  },
});

new Electroview({ rpc });
