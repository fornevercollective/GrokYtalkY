/**
 * Dual-path chat demo: Public Space (CF Worker) or DOJO gy hub.
 * Envelope: { type: "chat", text, from, t, role?, meta? }
 */
(function () {
  const $ = (id) => document.getElementById(id);

  const state = {
    mode: "space", // space | dojo
    ws: null,
    nick: "viewer",
  };

  const defaults = {
    space: () => {
      const u = new URL(location.href);
      // wrangler dev default; override for deployed worker
      const host = localStorage.getItem("gy-chat-space-ws") || "ws://127.0.0.1:8787/ws";
      return host;
    },
    dojo: () => localStorage.getItem("gy-chat-dojo-ws") || "ws://127.0.0.1:9876/",
  };

  function applyMode() {
    const space = state.mode === "space";
    $("mode-space").classList.toggle("active", space);
    $("mode-dojo").classList.toggle("active", !space);
    $("room").disabled = !space;
    if (space) {
      $("ws-url").value = defaults.space();
      if (!$("room").value) $("room").value = "space:demo";
    } else {
      $("ws-url").value = defaults.dojo();
    }
  }

  function setStatus(text, cls) {
    const el = $("status");
    el.textContent = text;
    el.className = "chat-status" + (cls ? " " + cls : "");
  }

  function appendLine(msg) {
    const log = $("log");
    const div = document.createElement("div");
    div.className = "chat-line";
    const from = msg.from || msg.nick || "·";
    const role = msg.role || "";
    const bridged = msg.meta && (msg.meta.bridged || msg.meta.source === "dojo-hub");
    if (bridged) div.classList.add("bridged");

    const who = document.createElement("span");
    who.className = "who";
    if (role === "host" || bridged) who.classList.add("host");
    if (msg.type === "system" || from === "system") who.classList.add("system");
    who.textContent = from;

    const text = document.createElement("span");
    const body =
      msg.text != null
        ? msg.text
        : msg.message != null
          ? msg.message
          : msg.type === "peer_joined"
            ? "joined"
            : msg.type === "peer_left"
              ? "left"
              : msg.type === "welcome"
                ? msg.text || "connected"
                : JSON.stringify(msg);
    text.textContent = " · " + body;

    div.appendChild(who);
    div.appendChild(text);
    log.appendChild(div);
    log.scrollTop = log.scrollHeight;
  }

  function buildURL() {
    let base = $("ws-url").value.trim();
    const nick = ($("nick").value || "viewer").trim().slice(0, 64);
    state.nick = nick;
    if (state.mode === "space") {
      const room = ($("room").value || "space:demo").trim();
      const u = new URL(base.replace(/^http/, "ws"));
      u.searchParams.set("room", room);
      u.searchParams.set("nick", nick);
      if (!u.searchParams.get("role")) u.searchParams.set("role", "listener");
      return u.toString();
    }
    // DOJO hub
    const u = new URL(base.replace(/^http/, "ws"));
    u.searchParams.set("role", "peer");
    u.searchParams.set("nick", nick);
    return u.toString();
  }

  function disconnect() {
    if (state.ws) {
      try {
        state.ws.close();
      } catch (_) {}
      state.ws = null;
    }
    setStatus("disconnected");
  }

  function connect() {
    disconnect();
    const url = buildURL();
    setStatus("connecting… " + url);
    let ws;
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setStatus("bad url: " + e.message, "err");
      return;
    }
    state.ws = ws;
    ws.onopen = () => {
      setStatus("live · " + (state.mode === "space" ? "public Space" : "DOJO hub"), "live");
      if (state.mode === "dojo") {
        ws.send(JSON.stringify({ type: "join", nick: state.nick, role: "term" }));
      }
      localStorage.setItem(
        state.mode === "space" ? "gy-chat-space-ws" : "gy-chat-dojo-ws",
        $("ws-url").value.trim(),
      );
    };
    ws.onclose = () => {
      state.ws = null;
      setStatus("closed", "err");
    };
    ws.onerror = () => setStatus("socket error", "err");
    ws.onmessage = (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        appendLine({ from: "raw", text: String(ev.data).slice(0, 200) });
        return;
      }
      // hub roster noise — skip
      if (msg.type === "roster") return;
      if (msg.type === "chat" || msg.type === "welcome" || msg.type === "peer_joined" ||
          msg.type === "peer_left" || msg.type === "system" || msg.type === "error" ||
          msg.type === "reaction") {
        // SFU chat shape: nick field
        if (msg.nick && !msg.from) msg.from = msg.nick;
        appendLine(msg);
      }
    };
  }

  function sendChat(text) {
    text = (text || "").trim();
    if (!text || !state.ws || state.ws.readyState !== WebSocket.OPEN) return;
    const payload = {
      type: "chat",
      text,
      from: state.nick,
      t: Date.now(),
    };
    state.ws.send(JSON.stringify(payload));
    // hub echoes via broadcast (may not include sender depending on hub) — show local
    if (state.mode === "dojo") {
      appendLine({ ...payload, role: "listener" });
    }
  }

  $("mode-space").onclick = () => {
    state.mode = "space";
    applyMode();
  };
  $("mode-dojo").onclick = () => {
    state.mode = "dojo";
    applyMode();
  };
  $("btn-connect").onclick = connect;
  $("btn-disconnect").onclick = disconnect;
  $("btn-clear").onclick = () => {
    $("log").innerHTML = "";
  };
  $("compose").onsubmit = (e) => {
    e.preventDefault();
    const input = $("input");
    sendChat(input.value);
    input.value = "";
  };

  applyMode();
})();
