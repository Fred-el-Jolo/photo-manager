// Shared types between the Bun process and the WebView.
// This file must not import from electrobun/bun (Bun-only runtime).

export interface PhotoMeta {
  sha256: string;
  phash?: number;
  taken_at?: string; // ISO 8601
  lat?: number;
  lon?: number;
  location?: string;
  dest_path: string;
}

export interface Group {
  paths: string[];
}

export interface CurateInput {
  groups: Group[];
  photos: Record<string, PhotoMeta>;
  lib_root: string;
}

export interface CurateOutput {
  to_delete: string[];
}

// ─── RPC schema ──────────────────────────────────────────────────────────────
// Bun pushes data to the webview after dom-ready (avoids WebSocket race).
// Webview calls back to bun when the user is done.

export interface InitPayload {
  groups: Group[];
  photos: Record<string, PhotoMeta>;
  imagePort: number;
}

// Structural equivalent of ElectrobunRPCSchema for our app.
export interface PhotoManagerRPC {
  bun: {
    requests: {
      done: { params: { toDelete: string[] }; response: void };
    };
    messages: Record<never, never>;
  };
  webview: {
    requests: {
      init: { params: InitPayload; response: void };
    };
    messages: Record<never, never>;
  };
}
