import { BrowserView, BrowserWindow } from "electrobun/bun";
import { resolve, relative } from "path";
import { existsSync } from "fs";
import type { CurateInput, CurateOutput, InitPayload, PhotoManagerRPC } from "../types";

// ─── IPC paths (passed via env vars by the Go parent process) ────────────────

const inputPath = process.env.PM_INPUT;
const outputPath = process.env.PM_OUTPUT;

if (!inputPath || !outputPath) {
  console.error("PM_INPUT and PM_OUTPUT env vars must be set by the Go parent process.");
  process.exit(1);
}

// ─── Load input ───────────────────────────────────────────────────────────────

const input: CurateInput = await Bun.file(inputPath).json();
const libRoot = resolve(input.lib_root);

// ─── Image HTTP server ────────────────────────────────────────────────────────
// Serves local images so WebKitGTK (Linux) avoids file:// CORS restrictions.

// Hoisted so the map is allocated once, not per-request.
const MIME_TYPES: Record<string, string> = {
  jpg: "image/jpeg", jpeg: "image/jpeg",
  png: "image/png", webp: "image/webp",
  tiff: "image/tiff", tif: "image/tiff",
  gif: "image/gif",
  avif: "image/avif",
  heic: "image/heic", heif: "image/heif",
};

function mimeFor(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return MIME_TYPES[ext] ?? "application/octet-stream";
}

const imageServer = Bun.serve({
  port: 0, // OS assigns a free port
  fetch(req) {
    const rawPath = new URL(req.url).searchParams.get("path");
    if (!rawPath) return new Response("missing path", { status: 400 });

    // Prevent path traversal: resolved path must be inside libRoot.
    const abs = resolve(rawPath);
    const rel = relative(libRoot, abs);
    if (rel.startsWith("..") || rel === "") {
      return new Response("forbidden", { status: 403 });
    }
    if (!existsSync(abs)) return new Response("not found", { status: 404 });

    return new Response(Bun.file(abs), {
      headers: { "Content-Type": mimeFor(abs), "Cache-Control": "public, max-age=3600" },
    });
  },
});

if (!imageServer.port) throw new Error("Image server failed to start on a free port");
const imagePort = imageServer.port;

// ─── RPC ──────────────────────────────────────────────────────────────────────

const rpc = BrowserView.defineRPC<PhotoManagerRPC>({
  handlers: {
    requests: {
      async done({ toDelete }: { toDelete: string[] }) {
        const out: CurateOutput = { to_delete: toDelete };
        await Bun.write(outputPath, JSON.stringify(out));
        imageServer.stop(true);
        process.exit(0);
      },
    },
  },
});

// ─── Window ───────────────────────────────────────────────────────────────────

const win = new BrowserWindow({
  title: "Photo Manager — Curate",
  url: "views://curate-ui/index.html",
  frame: { x: 0, y: 0, width: 1280, height: 820 },
  rpc,
});

// Push data to the webview only after dom-ready — the WebSocket RPC channel
// is guaranteed to be open at that point, avoiding a connection race.
win.webview.on("dom-ready", () => {
  win.webview.rpc!.requestProxy.init({
    groups: input.groups,
    photos: input.photos,
    imagePort,
  });
});
