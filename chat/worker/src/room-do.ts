/**
 * ChatRoom Durable Object — one instance per Space / public room.
 * In-memory ring buffer + WebSocket fanout (Creator Studio–style).
 */

export interface ChatMsg {
  type: "chat" | "reaction" | "chat_pin" | "system" | "welcome" | "peer_joined" | "peer_left" | "error";
  text?: string;
  from?: string;
  id?: string;
  t?: number;
  room?: string;
  role?: string;
  meta?: Record<string, unknown>;
  peer_id?: string;
  nicks?: string[];
  message?: string;
}

const MAX_HISTORY = 200;
const MAX_TEXT = 2000;

interface Session {
  nick: string;
  role: string;
}

export class ChatRoom implements DurableObject {
  private sessions = new Map<WebSocket, Session>();
  private history: ChatMsg[] = [];
  private roomName = "space:demo";

  constructor(
    private state: DurableObjectState,
    _env: unknown,
  ) {
    // Restore hibernated WebSockets (CF DO hibernation API when enabled).
    this.state.getWebSockets().forEach((ws) => {
      const meta = ws.deserializeAttachment() as Session | null;
      if (meta) this.sessions.set(ws, meta);
    });
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname.endsWith("/history") || url.pathname === "/history") {
      return Response.json({
        room: this.roomName,
        messages: this.history.slice(-100),
      });
    }

    if (request.headers.get("Upgrade") !== "websocket") {
      return new Response("expected websocket", { status: 426 });
    }

    const nick = (url.searchParams.get("nick") || "anon").slice(0, 64);
    const room = (url.searchParams.get("room") || this.roomName).slice(0, 128);
    this.roomName = room;
    const role = (url.searchParams.get("role") || "listener").slice(0, 32);

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair) as [WebSocket, WebSocket];

    this.state.acceptWebSocket(server);
    const session: Session = { nick, role };
    server.serializeAttachment(session);
    this.sessions.set(server, session);

    this.send(server, {
      type: "welcome",
      room: this.roomName,
      from: "system",
      text: `joined ${this.roomName}`,
      t: Date.now(),
      nicks: this.nickList(),
    });

    this.broadcast(
      {
        type: "peer_joined",
        from: nick,
        room: this.roomName,
        t: Date.now(),
        text: `${nick} joined`,
      },
      server,
    );

    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer) {
    const session = this.sessions.get(ws);
    if (!session) return;

    let raw: string;
    if (typeof message === "string") raw = message;
    else raw = new TextDecoder().decode(message);

    let body: Record<string, unknown>;
    try {
      body = JSON.parse(raw) as Record<string, unknown>;
    } catch {
      this.send(ws, { type: "error", message: "bad json" });
      return;
    }

    const typ = String(body.type || "chat");

    if (typ === "chat") {
      const text = String(body.text || "").trim().slice(0, MAX_TEXT);
      if (!text) return;
      const msg: ChatMsg = {
        type: "chat",
        text,
        from: session.nick,
        id: String(body.id || ""),
        t: Date.now(),
        room: this.roomName,
        role: session.role,
        meta: (body.meta as Record<string, unknown>) || undefined,
      };
      this.pushHistory(msg);
      this.broadcast(msg);
      return;
    }

    if (typ === "reaction") {
      const msg: ChatMsg = {
        type: "reaction",
        text: String(body.text || body.meta && (body.meta as { reaction?: string }).reaction || ""),
        from: session.nick,
        t: Date.now(),
        room: this.roomName,
        meta: (body.meta as Record<string, unknown>) || { reaction: body.text },
      };
      this.broadcast(msg);
      return;
    }

    if (typ === "leave") {
      ws.close(1000, "leave");
      return;
    }

    this.send(ws, { type: "error", message: `unknown type ${typ}` });
  }

  async webSocketClose(ws: WebSocket) {
    const session = this.sessions.get(ws);
    this.sessions.delete(ws);
    if (session) {
      this.broadcast({
        type: "peer_left",
        from: session.nick,
        room: this.roomName,
        t: Date.now(),
        text: `${session.nick} left`,
      });
    }
  }

  async webSocketError(ws: WebSocket) {
    this.sessions.delete(ws);
  }

  private nickList(): string[] {
    return [...this.sessions.values()].map((s) => s.nick);
  }

  private pushHistory(msg: ChatMsg) {
    this.history.push(msg);
    if (this.history.length > MAX_HISTORY) {
      this.history = this.history.slice(-MAX_HISTORY);
    }
  }

  private send(ws: WebSocket, msg: ChatMsg) {
    try {
      ws.send(JSON.stringify(msg));
    } catch {
      /* closed */
    }
  }

  private broadcast(msg: ChatMsg, except?: WebSocket) {
    const data = JSON.stringify(msg);
    for (const ws of this.sessions.keys()) {
      if (except && ws === except) continue;
      try {
        ws.send(data);
      } catch {
        this.sessions.delete(ws);
      }
    }
  }
}
