/**
 * Optional bridge notes / helpers — DOJO hub ↔ CF Space chat.
 *
 * DOJO → CF (public captions):
 *   Connect to gy hub WS, filter type==="chat" from hosts, POST or WS into
 *   this worker's room (rate-limit; host role only).
 *
 * CF → DOJO (stage questions):
 *   Only moderated / approved lines; never dump 1k viewers into the terminal hub.
 *
 * This file is a stub for a future Node/Workers cron or small Go sidecar.
 * Prefer running the bridge out-of-band so JAX/FFmpeg and media SFUs stay clean.
 */

export interface BridgeConfig {
  hubWs: string; // ws://127.0.0.1:9876
  spaceRoom: string; // space:launch
  workerWs: string; // wss://gy-chat.../ws?room=space:launch&nick=bridge
  hostNicks: string[]; // only mirror these into CF
}

export function shouldMirrorToPublic(from: string, hosts: string[]): boolean {
  return hosts.length === 0 || hosts.includes(from);
}

export function hubChatToSpace(msg: {
  type: string;
  text?: string;
  from?: string;
  t?: number;
}): string | null {
  if (msg.type !== "chat" || !msg.text) return null;
  return JSON.stringify({
    type: "chat",
    text: msg.text,
    from: msg.from || "host",
    t: msg.t || Date.now(),
    role: "host",
    meta: { bridged: true },
  });
}
