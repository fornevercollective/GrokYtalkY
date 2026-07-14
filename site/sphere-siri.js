/**
 * Glyph Sphere · Siri-like conversation
 * - Web Speech STT / TTS (browser)
 * - Hub POST /api/chat (SpaceXAI / xAI, key on server)
 * - Mesh fan-out: sphere-chat · sphere-ask (multi-person)
 * - Mood hooks for sphere wave (idle · listen · think · speak)
 */
(function (global) {
  "use strict";

  var MAX_HISTORY = 24;
  var SESSION_KEY = "gy.sphere.siri.session";

  function hubHTTPBase() {
    try {
      if (location.protocol === "http:" || location.protocol === "https:") {
        return location.origin;
      }
    } catch (_) {}
    return "http://127.0.0.1:9876";
  }

  function sessionId() {
    try {
      var id = localStorage.getItem(SESSION_KEY);
      if (!id) {
        id = "sp-" + Math.random().toString(36).slice(2, 10);
        localStorage.setItem(SESSION_KEY, id);
      }
      return id;
    } catch (_) {
      return "sp-anon";
    }
  }

  function SpeechRec() {
    return global.SpeechRecognition || global.webkitSpeechRecognition || null;
  }

  /**
   * @param {object} opts
   * @param {function} opts.onMood  (mood: string, detail?)
   * @param {function} opts.onLine  ({role, text, from, via?})
   * @param {function} opts.onStatus (text, kind?)
   * @param {function} [opts.sendMesh] (obj) → bool
   * @param {function} [opts.getNick] () → string
   */
  function createSphereSiri(opts) {
    opts = opts || {};
    var state = {
      mood: "idle", // idle | listen | think | speak
      history: [],
      busy: false,
      listening: false,
      speaking: false,
      continuous: false, // always listen for next turn after reply
      roomListen: true, // answer mesh sphere-ask / chat @sphere
      voiceOn: true,
      lastReply: "",
      available: null,
      model: "",
      rec: null,
    };

    function setMood(m, detail) {
      state.mood = m || "idle";
      if (typeof opts.onMood === "function") opts.onMood(state.mood, detail || {});
    }

    function status(t, kind) {
      if (typeof opts.onStatus === "function") opts.onStatus(t, kind);
    }

    function line(role, text, extra) {
      extra = extra || {};
      var row = {
        role: role,
        text: text,
        from: extra.from || (role === "assistant" ? "Sphere" : nick()),
        via: extra.via || "",
        t: Date.now(),
      };
      if (typeof opts.onLine === "function") opts.onLine(row);
      return row;
    }

    function nick() {
      if (typeof opts.getNick === "function") {
        try {
          return opts.getNick() || "you";
        } catch (_) {}
      }
      return "you";
    }

    function mesh(obj) {
      if (typeof opts.sendMesh === "function") {
        try {
          return opts.sendMesh(obj);
        } catch (_) {}
      }
      return false;
    }

    async function probe() {
      try {
        var r = await fetch(hubHTTPBase() + "/api/chat", {
          headers: { Accept: "application/json" },
        });
        var j = await r.json();
        state.available = !!j.available;
        state.model = j.model || "";
        return j;
      } catch (e) {
        state.available = false;
        return { ok: false, error: String(e) };
      }
    }

    function pushHistory(role, content) {
      state.history.push({ role: role, content: content });
      if (state.history.length > MAX_HISTORY * 2) {
        state.history = state.history.slice(-MAX_HISTORY * 2);
      }
    }

    async function ask(text, fromWho) {
      text = String(text || "").trim();
      if (!text) return null;
      if (state.busy) {
        status("still thinking…", "live");
        return null;
      }
      state.busy = true;
      setMood("think", { text: text });
      status("thinking…", "live");
      line("user", text, { from: fromWho || nick() });
      mesh({
        type: "sphere-chat",
        role: "user",
        text: text,
        from: fromWho || nick(),
        mood: "think",
      });

      var hist = state.history.slice();
      // AskGrok appends the current user message; history should not include it
      try {
        var res = await fetch(hubHTTPBase() + "/api/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          body: JSON.stringify({
            message: text,
            from: fromWho || nick(),
            session: sessionId(),
            history: hist,
          }),
        });
        var j = await res.json();
        if (!j.ok) {
          var err = j.error || "chat failed";
          status(err, "err");
          line("system", err);
          setMood("idle");
          state.busy = false;
          return null;
        }
        var reply = String(j.reply || "").trim();
        pushHistory("user", text);
        pushHistory("assistant", reply);
        state.lastReply = reply;
        line("assistant", reply, { via: j.via || j.model });
        mesh({
          type: "sphere-chat",
          role: "assistant",
          text: reply,
          from: "Sphere",
          mood: "speak",
        });
        status(
          (j.via || j.model || "Sphere") + (j.ms ? " · " + j.ms + "ms" : ""),
          "live"
        );
        if (state.voiceOn) {
          await speak(reply);
        } else {
          setMood("idle");
        }
        if (state.continuous && !state.listening) {
          setTimeout(function () {
            startListen();
          }, 400);
        }
        state.busy = false;
        return reply;
      } catch (e) {
        status(String(e.message || e), "err");
        line("system", String(e.message || e));
        setMood("idle");
        state.busy = false;
        return null;
      }
    }

    function speak(text) {
      return new Promise(function (resolve) {
        if (!state.voiceOn || !global.speechSynthesis || !text) {
          setMood("idle");
          resolve();
          return;
        }
        try {
          global.speechSynthesis.cancel();
        } catch (_) {}
        var u = new SpeechSynthesisUtterance(text);
        u.rate = 1.05;
        u.pitch = 1.0;
        u.lang = navigator.language || "en-US";
        // prefer a clear English voice if present
        try {
          var voices = global.speechSynthesis.getVoices() || [];
          var pick =
            voices.find(function (v) {
              return /samantha|karen|moira|google us english|Samantha/i.test(v.name);
            }) ||
            voices.find(function (v) {
              return /^en/i.test(v.lang) && v.localService;
            }) ||
            voices.find(function (v) {
              return /^en/i.test(v.lang);
            });
          if (pick) u.voice = pick;
        } catch (_) {}
        state.speaking = true;
        setMood("speak", { text: text });
        u.onend = function () {
          state.speaking = false;
          setMood("idle");
          resolve();
        };
        u.onerror = function () {
          state.speaking = false;
          setMood("idle");
          resolve();
        };
        global.speechSynthesis.speak(u);
      });
    }

    function stopSpeak() {
      try {
        if (global.speechSynthesis) global.speechSynthesis.cancel();
      } catch (_) {}
      state.speaking = false;
      if (state.mood === "speak") setMood("idle");
    }

    function startListen() {
      var SR = SpeechRec();
      if (!SR) {
        status("speech recognition not supported — type below", "err");
        return false;
      }
      if (state.listening) return true;
      stopSpeak();
      var rec = new SR();
      rec.lang = navigator.language || "en-US";
      rec.interimResults = true;
      rec.continuous = false;
      rec.maxAlternatives = 1;
      state.rec = rec;
      state.listening = true;
      setMood("listen");
      status("listening… tap mic again to cancel", "live");

      var finalText = "";
      rec.onresult = function (ev) {
        var interim = "";
        for (var i = ev.resultIndex; i < ev.results.length; i++) {
          var t = ev.results[i][0].transcript;
          if (ev.results[i].isFinal) finalText += t;
          else interim += t;
        }
        if (interim) status("… " + interim, "live");
        if (finalText) status("you: " + finalText, "live");
      };
      rec.onerror = function (ev) {
        state.listening = false;
        setMood("idle");
        var err = (ev && ev.error) || "mic error";
        if (err === "not-allowed") {
          status("mic permission denied — allow microphone or type", "err");
        } else if (err !== "aborted") {
          status("listen: " + err, "err");
        } else {
          status("listen cancelled", "live");
        }
      };
      rec.onend = function () {
        state.listening = false;
        state.rec = null;
        var text = String(finalText || "").trim();
        if (text) {
          ask(text);
        } else if (state.mood === "listen") {
          setMood("idle");
          status("didn't catch that — try again", "live");
        }
      };
      try {
        rec.start();
      } catch (e) {
        state.listening = false;
        setMood("idle");
        status("mic start failed · " + (e.message || e), "err");
        return false;
      }
      return true;
    }

    function stopListen() {
      if (state.rec) {
        try {
          state.rec.abort();
        } catch (_) {
          try {
            state.rec.stop();
          } catch (_) {}
        }
      }
      state.listening = false;
      state.rec = null;
      if (state.mood === "listen") setMood("idle");
    }

    function toggleListen() {
      if (state.listening) {
        stopListen();
        return false;
      }
      return startListen();
    }

    /** mesh inbound from other people */
    function onMesh(msg) {
      if (!msg || !msg.type) return;
      if (msg.type === "sphere-chat") {
        // show others' dialogue; don't re-answer assistant echoes
        if (msg.role === "assistant" && msg.from === "Sphere") {
          if (msg.text && msg.text !== state.lastReply) {
            line("assistant", msg.text, { from: "Sphere", via: "mesh" });
            if (msg.mood) setMood(String(msg.mood));
          }
          return;
        }
        if (msg.role === "user" && msg.from && msg.from !== nick()) {
          line("user", msg.text, { from: msg.from, via: "mesh" });
        }
        return;
      }
      if (!state.roomListen) return;
      if (msg.type === "sphere-ask" && msg.text) {
        if (state.busy || state.listening) return;
        ask(String(msg.text), msg.from || "peer");
        return;
      }
      // natural chat: "@sphere …" or "hey sphere …"
      if (msg.type === "chat" && msg.text) {
        var t = String(msg.text);
        var low = t.toLowerCase();
        var addressed =
          /(^|\s)@?sphere\b/i.test(t) ||
          /\bhey sphere\b/i.test(low) ||
          /\bok sphere\b/i.test(low) ||
          /\bhi sphere\b/i.test(low);
        if (!addressed) return;
        if (msg.from === nick() || msg.from === "Sphere") return;
        if (state.busy || state.listening) return;
        var cleaned = t
          .replace(/@?sphere[,:]?\s*/i, "")
          .replace(/\b(hey|ok|hi)\s+sphere[,:]?\s*/i, "")
          .trim();
        if (!cleaned) cleaned = t;
        ask(cleaned, msg.from || "peer");
      }
    }

    async function clear() {
      state.history = [];
      state.lastReply = "";
      try {
        await fetch(hubHTTPBase() + "/api/chat/clear", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ session: sessionId() }),
        });
      } catch (_) {}
      line("system", "conversation cleared");
      setMood("idle");
      status("cleared · say hi", "live");
    }

    // warm voices list (Chrome loads async)
    if (global.speechSynthesis) {
      try {
        global.speechSynthesis.getVoices();
        global.speechSynthesis.onvoiceschanged = function () {
          global.speechSynthesis.getVoices();
        };
      } catch (_) {}
    }

    probe().then(function (j) {
      if (j && j.available) {
        status("Sphere ready · " + (j.mode || j.model || "chat"), "live");
      } else if (j && j.ok === false) {
        status("chat offline · set XAI_API_KEY on hub", "err");
      } else {
        status("Sphere chat · probe hub /api/chat", "live");
      }
    });

    return {
      ask: ask,
      speak: speak,
      stopSpeak: stopSpeak,
      startListen: startListen,
      stopListen: stopListen,
      toggleListen: toggleListen,
      onMesh: onMesh,
      clear: clear,
      probe: probe,
      getState: function () {
        return {
          mood: state.mood,
          busy: state.busy,
          listening: state.listening,
          speaking: state.speaking,
          continuous: state.continuous,
          voiceOn: state.voiceOn,
          roomListen: state.roomListen,
          available: state.available,
          model: state.model,
          historyLen: state.history.length,
        };
      },
      setContinuous: function (v) {
        state.continuous = !!v;
      },
      setVoice: function (v) {
        state.voiceOn = !!v;
        if (!v) stopSpeak();
      },
      setRoomListen: function (v) {
        state.roomListen = !!v;
      },
      setMood: setMood,
    };
  }

  global.GY_SPHERE_SIRI = {
    create: createSphereSiri,
    speechSupported: function () {
      return !!SpeechRec();
    },
    ttsSupported: function () {
      return !!global.speechSynthesis;
    },
  };
})(typeof window !== "undefined" ? window : globalThis);
