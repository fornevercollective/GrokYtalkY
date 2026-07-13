/**
 * gy-mid-lane Worker — public audience plane for GrokYtalkY.
 *
 * Ingest:  POST /mid  (from `gy mid-lane` on the DOJO host)
 * View:    WS  /ws?room=dojo&nick=viewer
 * State:   GET /state?room=dojo
 *
 * Interactive jam stays on gy hub / gy-sfu (16–32).
 * Ladder: glyph/hex (tiny) · mid (program JSON) · full (WHIP/playback URLs only — HD lives in CF Calls).
 */

export { MidLaneRoom } from "./room-do";

export interface Env {
  MID_ROOM: DurableObjectNamespace;
  SERVICE?: string;
  /** Shared secret; also accepted as EDGE_TOKEN binding / secret */
  EDGE_TOKEN?: string;
  /** Optional Cloudflare Calls / WHIP base for HD ladder documentation */
  CALLS_WHIP_URL?: string;
  CALLS_PLAYBACK_URL?: string;
}

function roomId(raw: string | null): string {
  const r = (raw || "global").trim().toLowerCase().replace(/[^a-z0-9._-]+/g, "-");
  return (r || "global").slice(0, 64);
}

function stubFor(env: Env, room: string): DurableObjectStub {
  const id = env.MID_ROOM.idFromName(room);
  return env.MID_ROOM.get(id);
}

function authorized(request: Request, env: Env): boolean {
  const need = (env.EDGE_TOKEN || "").trim();
  if (!need) return true; // open ingest for local dev
  const auth = request.headers.get("Authorization") || "";
  const bearer = auth.startsWith("Bearer ") ? auth.slice(7).trim() : "";
  const hdr = request.headers.get("X-GY-Edge-Token") || "";
  const q = new URL(request.url).searchParams.get("token") || "";
  return bearer === need || hdr === need || q === need;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
      return Response.json({
        ok: true,
        service: env.SERVICE || "gy-mid-lane",
        plane: "public-mid-lane",
        ladder: ["glyph", "mid", "full"],
        dojo: "use gy serve + rooms for interactive jam",
        auth: env.EDGE_TOKEN ? "token-required" : "open-dev",
        calls: {
          whip: env.CALLS_WHIP_URL || null,
          playback: env.CALLS_PLAYBACK_URL || null,
          note: "HD via WHIP/Calls — gy mid-lane only announces URLs, does not encode",
        },
      });
    }

    // gy mid-lane → POST /mid?room=dojo  (room also inside JSON body)
    if (url.pathname === "/mid" && request.method === "POST") {
      if (!authorized(request, env)) {
        return Response.json({ ok: false, error: "unauthorized" }, { status: 401 });
      }
      let room = roomId(url.searchParams.get("room"));
      // peek body room without consuming — clone
      try {
        const clone = request.clone();
        const body = (await clone.json()) as { room?: string };
        if (body?.room) room = roomId(body.room);
      } catch {
        /* use query/default */
      }
      return stubFor(env, room).fetch(
        new Request(new URL("/mid", request.url), {
          method: "POST",
          headers: request.headers,
          body: request.body,
        }),
      );
    }

    if (url.pathname === "/state") {
      const room = roomId(url.searchParams.get("room"));
      return stubFor(env, room).fetch(
        new Request(new URL("/state", request.url), request),
      );
    }

    if (url.pathname === "/ws") {
      if (request.headers.get("Upgrade") !== "websocket") {
        return new Response("expected websocket", { status: 426 });
      }
      const room = roomId(url.searchParams.get("room"));
      return stubFor(env, room).fetch(request);
    }

    if (url.pathname === "/" || url.pathname === "") {
      return new Response(
        [
          "gy-mid-lane — GrokYtalkY public mid-lane edge (CF)",
          "  GET  /health",
          "  POST /mid              ← gy mid-lane --edge (token if set)",
          "  GET  /state?room=dojo  ← last program + hexlum + full ladder",
          "  WS   /ws?room=dojo&nick=viewer",
          "",
          "Ladder: glyph (hexlum) · mid (program JSON) · full (WHIP/playback URLs)",
          "DOJO: gy serve · GY_ROOM=dojo gy",
          "Bridge: gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid \\",
          "          --whip $GY_CALLS_WHIP_URL --playback $GY_CALLS_PLAYBACK_URL",
        ].join("\n"),
        { headers: { "content-type": "text/plain; charset=utf-8" } },
      );
    }

    return new Response("not found", { status: 404 });
  },
};
