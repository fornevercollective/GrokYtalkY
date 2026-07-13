/**
 * gy-chat Worker — edge entry for Space / Creator Studio–style chat.
 *
 * 1k+ viewers: WebSocket → Durable Object room.
 * DOJO (16–32) stays on gy hub / gy-sfu — this path is public broadcast chat.
 */

export { ChatRoom } from "./room-do";

export interface Env {
  CHAT_ROOM: DurableObjectNamespace;
  SERVICE?: string;
  HUB_WS?: string;
}

function roomId(raw: string | null): string {
  const r = (raw || "space:demo").trim().slice(0, 128);
  return r || "space:demo";
}

function stubFor(env: Env, room: string): DurableObjectStub {
  const id = env.CHAT_ROOM.idFromName(room);
  return env.CHAT_ROOM.get(id);
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
      return Response.json({
        ok: true,
        service: env.SERVICE || "gy-chat",
        plane: "public-space-chat",
        dojo: "use gy serve / gy-sfu for 16–32 interactive",
      });
    }

    if (url.pathname === "/history") {
      const room = roomId(url.searchParams.get("room"));
      return stubFor(env, room).fetch(
        new Request(new URL("/history", request.url), request),
      );
    }

    if (url.pathname === "/ws") {
      if (request.headers.get("Upgrade") !== "websocket") {
        return new Response("expected websocket", { status: 426 });
      }
      const room = roomId(url.searchParams.get("room"));
      // Forward upgrade to the DO that owns this Space room.
      return stubFor(env, room).fetch(request);
    }

    if (url.pathname === "/" || url.pathname === "") {
      return new Response(
        [
          "gy-chat — GrokYtalkY Space-style public chat (CF)",
          "  GET /health",
          "  GET /history?room=space:demo",
          "  WS  /ws?room=space:demo&nick=viewer",
          "DOJO chat: gy serve / gy-sfu (not this worker)",
        ].join("\n"),
        { headers: { "content-type": "text/plain; charset=utf-8" } },
      );
    }

    return new Response("not found", { status: 404 });
  },
};
