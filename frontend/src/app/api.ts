// Typed API client for the photo-manager Go HTTP server.
// All functions throw on non-2xx responses.

// ─── Server response types (match Go JSON exactly) ───────────────────────────

export interface ApiSession {
  input_dir: string;
  output_dir: string;
  scanned_at: string; // ISO 8601
  months: ApiMonthGroup[];
}

export interface ApiMonthGroup {
  year: number;
  month: number; // 1–12
  groups: ApiGroup[];
}

export interface ApiGroup {
  id: string;
  name: string; // empty string = unnamed
  photos: ApiPhoto[];
  applied: boolean;
  suggested_keeper: string; // path of sharpest photo; empty when group has 1 photo
}

export interface ApiPhoto {
  path: string;
  sha256: string;
  phash: number;
  taken_at: string; // ISO 8601, may be empty
  width: number;
  height: number;
  file_size: number;
  blur_score: number;
  is_duplicate: boolean;
  is_removed: boolean;
  rotation: number; // 0, 90, 180, 270
  new_name: string;
}

export interface MonthSummary {
  year: number;
  month: number;
  group_count: number;
  photo_count: number;
}

export type ThumbSize = "L" | "XL" | "XXL";

export interface PhotoPatch {
  path: string;
  rotation?: number;
  is_removed?: boolean;
  is_duplicate?: boolean;
  new_name?: string;
}

// ─── Internal helpers ────────────────────────────────────────────────────────

// Relative origin — works when the SPA is served by the Go server on any port.
const BASE = "";

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init);
  if (!res.ok) {
    let detail = "";
    try {
      detail = await res.text();
    } catch {
      detail = "";
    }
    throw new Error(
      `${init?.method ?? "GET"} ${path} failed: ${res.status} ${res.statusText}${
        detail ? ` — ${detail}` : ""
      }`,
    );
  }
  // Some endpoints (apply) may return an empty body; guard against that.
  const text = await res.text();
  if (!text) {
    return undefined as unknown as T;
  }
  return JSON.parse(text) as T;
}

function jsonInit(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

// ─── Public API ──────────────────────────────────────────────────────────────

export const api = {
  getSession(): Promise<ApiSession> {
    return request<ApiSession>("/api/session");
  },

  getMonths(): Promise<MonthSummary[]> {
    return request<MonthSummary[]>("/api/months");
  },

  getGroup(id: string): Promise<ApiGroup> {
    return request<ApiGroup>(`/api/groups/${encodeURIComponent(id)}`);
  },

  patchGroup(id: string, name: string): Promise<ApiGroup> {
    return request<ApiGroup>(
      `/api/groups/${encodeURIComponent(id)}`,
      jsonInit("PATCH", { name }),
    );
  },

  applyGroup(id: string): Promise<ApiGroup> {
    return request<ApiGroup>(
      `/api/groups/${encodeURIComponent(id)}/apply`,
      { method: "POST" },
    );
  },

  patchPhoto(patch: PhotoPatch): Promise<ApiPhoto> {
    return request<ApiPhoto>("/api/photos", jsonInit("PATCH", patch));
  },

  thumbnailUrl(path: string, size: ThumbSize): string {
    return `/api/thumbnail?path=${encodeURIComponent(path)}&size=${size}`;
  },

  rawUrl(path: string): string {
    return `/api/raw?path=${encodeURIComponent(path)}`;
  },
};
