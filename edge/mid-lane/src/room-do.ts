/**
 * MidLaneRoom Durable Object — one instance per hub mesh room.
 * Ingest: POST /mid (from gy mid-lane). Fan-out: WS viewers + last-state GET.
 * Public audience plane — not DOJO interactive mesh.
 */

export interface MidLaneEnvelope {
  type?: string;
  room?: string;
  lane?: string;
  from?: string;
  t?: number;
  seq?: number;
  program?: Record<string, unknown>;
  gyst?: Record<string, unknown>;
  caption?: string;
  mark?: string;
  mode?: string;
  ladder?: string; // glyph | mid | full
  whip_url?: string;
  playback_url?: string;
  via?: string;
}

interface Viewer {
  nick: string;
  role: string;
}

const MAX_RING = 32;

export class MidLaneRoom implements DurableObject {
  private viewers = new Map<WebSocket, Viewer>();
  private roomName = "global";
  private lastProgram: MidLaneEnvelope | null = null;
  private lastHex: MidLaneEnvelope | null = null;
  private lastFull: MidLaneEnvelope | null = null; // HD ladder metadata (WHIP/playback)
  private ring: MidLaneEnvelope[] = [];

  constructor(
    private state: DurableObjectState,
    _env: unknown,
  ) {
    this.state.getWebSockets().forEach((ws) => {
      const meta = ws.deserializeAttachment() as Viewer | null;
      if (meta) this.viewers.set(ws, meta);
    });
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    // last program + last hexlum snapshot
    if (url.pathname.endsWith("/state") || url.pathname === "/state") {
      return Response.json({
        room: this.roomName,
        program: this.lastProgram,
        hexlum: this.lastHex,
        full: this.lastFull,
        ladder: {
          glyph: !!this.lastHex,
          mid: !!this.lastProgram,
          full: !!this.lastFull,
          whip_url: this.lastFull?.whip_url || this.lastProgram?.whip_url || null,
          playback_url:
            this.lastFull?.playback_url || this.lastProgram?.playback_url || null,
        },
        viewers: this.viewers.size,
        recent: this.ring.slice(-16),
      });
    }

    // ingest from gy mid-lane
    if (
      (url.pathname.endsWith("/mid") || url.pathname === "/mid") &&
      request.method === "POST"
    ) {
      let body: MidLaneEnvelope;
      try {
        body = (await request.json()) as MidLaneEnvelope;
      } catch {
        return Response.json({ ok: false, error: "invalid json" }, { status: 400 });
      }
      return this.ingest(body);
    }

    // viewer WebSocket
    if (request.headers.get("Upgrade") === "websocket") {
      return this.acceptViewer(request, url);
    }

    return new Response("mid-lane room: POST /mid · GET /state · WS /ws", {
      status: 200,
      headers: { "content-type": "text/plain; charset=utf-8" },
    });
  }

  private ingest(body: MidLaneEnvelope): Response {
    if (!body || typeof body !== "object") {
      return Response.json({ ok: false, error: "empty body" }, { status: 400 });
    }
    const lane = String(body.lane || body.type || "mid");
    const env: MidLaneEnvelope = {
      ...body,
      type: "mid-lane",
      room: body.room || this.roomName,
      lane,
      t: body.t || Date.now(),
      via: body.via || "edge-mid-lane",
    };
    if (env.room) this.roomName = String(env.room).slice(0, 64);

    if (lane === "program") {
      this.lastProgram = env;
      if (env.ladder === "full" || env.whip_url || env.playback_url) {
        this.lastFull = { ...env, lane: "full", ladder: "full" };
      }
    } else if (lane === "hex" || lane === "hexlum") {
      this.lastHex = env;
    } else if (lane === "full" || lane === "mid") {
      if (lane === "full") this.lastFull = env;
      if (lane === "mid" && !this.lastProgram) this.lastProgram = env;
    }

    this.ring.push(env);
    if (this.ring.length > MAX_RING) this.ring = this.ring.slice(-MAX_RING);

    // fan-out to public viewers
    this.broadcast({
      type: "mid-lane",
      room: this.roomName,
      lane: env.lane,
      ladder: env.ladder || (lane === "hex" || lane === "hexlum" ? "glyph" : "mid"),
      from: env.from,
      t: env.t,
      seq: env.seq,
      caption: env.caption,
      mark: env.mark,
      mode: env.mode,
      program: env.program,
      gyst: env.gyst,
      whip_url: env.whip_url,
      playback_url: env.playback_url,
      via: env.via,
    });

    return Response.json({
      ok: true,
      room: this.roomName,
      lane: env.lane,
      ladder: env.ladder || null,
      viewers: this.viewers.size,
      seq: env.seq,
    });
  }

  private acceptViewer(request: Request, url: URL): Response {
    const nick = (url.searchParams.get("nick") || "viewer").slice(0, 64);
    const room = (url.searchParams.get("room") || this.roomName).slice(0, 64);
    this.roomName = room;
    const role = (url.searchParams.get("role") || "viewer").slice(0, 32);

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair) as [WebSocket, WebSocket];

    this.state.acceptWebSocket(server);
    const session: Viewer = { nick, role };
    server.serializeAttachment(session);
    this.viewers.set(server, session);

    this.send(server, {
      type: "welcome",
      room: this.roomName,
      plane: "mid-lane-public",
      text: `joined mid-lane ${this.roomName}`,
      t: Date.now(),
      viewers: this.viewers.size,
    });
    // catch-up: last program, hex, then full-ladder hints
    if (this.lastProgram) this.send(server, this.lastProgram);
    if (this.lastHex) this.send(server, this.lastHex);
    if (this.lastFull) this.send(server, this.lastFull);

    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, _message: string | ArrayBuffer) {
    // viewers are receive-only for mid-lane (no chat here — use gy-chat worker)
    this.send(ws, {
      type: "system",
      text: "mid-lane is receive-only · use Space chat worker for chat",
      t: Date.now(),
    });
  }

  async webSocketClose(ws: WebSocket) {
    this.viewers.delete(ws);
  }

  async webSocketError(ws: WebSocket) {
    this.viewers.delete(ws);
  }

  private send(ws: WebSocket, msg: unknown) {
    try {
      ws.send(JSON.stringify(msg));
    } catch {
      this.viewers.delete(ws);
    }
  }

  private broadcast(msg: unknown, except?: WebSocket) {
    const raw = JSON.stringify(msg);
    for (const [ws] of this.viewers) {
      if (ws === except) continue;
      try {
        ws.send(raw);
      } catch {
        this.viewers.delete(ws);
      }
    }
  }
}
