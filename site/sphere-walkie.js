/**
 * Sphere walkie — Burst-style hold-to-talk on the glyph dome.
 * Hold orb → face+mic → vburst-frame paints seats on sphere.html.
 * Mesh chat like burst Spaces chat (not cloud AI).
 */
(function (global) {
  "use strict";

  var GLYPH_N = 25;

  function createSphereWalkie(opts) {
    opts = opts || {};
    var el = {
      orb: document.getElementById("sp-walkie-orb"),
      face: document.getElementById("sp-walkie-face"),
      ring: document.getElementById("sp-walkie-ring"),
      label: document.getElementById("sp-walkie-label"),
      meta: document.getElementById("sp-walkie-meta"),
      btnHold: document.getElementById("sp-walkie-hold"),
      btnCam: document.getElementById("sp-walkie-cam"),
      log: document.getElementById("sp-walkie-log"),
      input: document.getElementById("sp-walkie-input"),
      send: document.getElementById("sp-walkie-send"),
    };
    if (!el.orb || !el.face) {
      console.warn("[sphere-walkie] missing DOM");
      return null;
    }

    var faceCtx = el.face.getContext("2d", { willReadFrequently: true });
    var ringCtx = el.ring ? el.ring.getContext("2d") : null;
    faceCtx.imageSmoothingEnabled = false;

    var video = null;
    var stream = null;
    var nick =
      (opts.getNick && opts.getNick()) ||
      "walkie-" + Math.random().toString(36).slice(2, 6);
    var tx = false;
    var rxFrom = "";
    var level = 0;
    var bands = new Array(32).fill(0);
    var raf = 0;
    var audioCtx = null;
    var analyser = null;
    var micSource = null;
    var lastGlyph = new Array(GLYPH_N * GLYPH_N).fill(0);
    var remoteImg = null;
    var frameTimer = 0;

    function setMeta(t) {
      if (el.meta) el.meta.innerHTML = t;
      if (typeof opts.onStatus === "function") opts.onStatus(t);
    }
    function setLabel(t) {
      if (el.label) el.label.textContent = t;
    }
    function sendMesh(obj) {
      if (typeof opts.sendMesh === "function") return opts.sendMesh(obj);
      return false;
    }
    function mood(m) {
      if (typeof opts.onMood === "function") opts.onMood(m);
      el.orb.classList.toggle("tx", m === "tx");
      el.orb.classList.toggle("rx", m === "rx");
    }

    function appendChat(from, text, kind) {
      if (!el.log || !text) return;
      var line = document.createElement("div");
      line.className = "sp-walkie-line" + (kind ? " " + kind : "");
      var who = document.createElement("span");
      who.className = "who";
      who.textContent = from || "·";
      var body = document.createElement("span");
      body.textContent = " · " + text;
      line.appendChild(who);
      line.appendChild(body);
      el.log.appendChild(line);
      el.log.scrollTop = el.log.scrollHeight;
      while (el.log.children.length > 100) el.log.removeChild(el.log.firstChild);
    }

    function paintSimFace(t) {
      var n = GLYPH_N;
      var img = faceCtx.createImageData(n, n);
      var d = img.data;
      var cx = n * 0.5 + Math.sin(t * 0.001) * 1.5;
      var cy = n * 0.45;
      for (var y = 0; y < n; y++) {
        for (var x = 0; x < n; x++) {
          var i = (y * n + x) * 4;
          var dist = Math.hypot(x - cx, y - cy);
          var L = 20 + (y / n) * 40;
          if (dist < n * 0.28) L = 140 + (1 - dist / (n * 0.28)) * 80;
          if (Math.hypot(x - (cx - 3), y - (cy - 2)) < 1.4) L = 20;
          if (Math.hypot(x - (cx + 3), y - (cy - 2)) < 1.4) L = 20;
          d[i] = d[i + 1] = d[i + 2] = L;
          d[i + 3] = 255;
          lastGlyph[y * n + x] = L;
        }
      }
      faceCtx.putImageData(img, 0, 0);
    }

    function sampleVideoToGlyph() {
      if (!video || video.readyState < 2) {
        paintSimFace(performance.now());
        return;
      }
      var n = GLYPH_N;
      faceCtx.drawImage(video, 0, 0, n, n);
      var img = faceCtx.getImageData(0, 0, n, n);
      var d = img.data;
      for (var i = 0, g = 0; i < d.length; i += 4, g++) {
        var L = 0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2];
        lastGlyph[g] = L;
        var v = Math.min(255, Math.pow(L / 255, 0.85) * 255);
        d[i] = d[i + 1] = d[i + 2] = v;
      }
      faceCtx.putImageData(img, 0, 0);
    }

    function paintRing() {
      if (!ringCtx || !el.ring) return;
      var w = el.ring.width;
      var h = el.ring.height;
      var cx = w / 2;
      var cy = h / 2;
      var r0 = w * 0.42;
      ringCtx.clearRect(0, 0, w, h);
      var n = bands.length;
      for (var i = 0; i < n; i++) {
        var a0 = (i / n) * Math.PI * 2 - Math.PI / 2;
        var a1 = ((i + 1) / n) * Math.PI * 2 - Math.PI / 2;
        var lv = bands[i] || level * 0.5;
        var r1 = r0 + 4 + lv * 22;
        ringCtx.beginPath();
        ringCtx.arc(cx, cy, r0, a0, a1);
        ringCtx.arc(cx, cy, r1, a1, a0, true);
        ringCtx.closePath();
        if (tx) ringCtx.fillStyle = "rgba(248,113,113," + (0.35 + lv * 0.55) + ")";
        else if (rxFrom) ringCtx.fillStyle = "rgba(74,222,128," + (0.3 + lv * 0.5) + ")";
        else ringCtx.fillStyle = "rgba(125,211,252," + (0.15 + lv * 0.45) + ")";
        ringCtx.fill();
      }
    }

    function readMic() {
      if (!analyser) return;
      var data = new Uint8Array(analyser.frequencyBinCount);
      analyser.getByteFrequencyData(data);
      var sum = 0;
      for (var i = 0; i < bands.length; i++) {
        var idx = Math.floor((i / bands.length) * data.length);
        var v = (data[idx] || 0) / 255;
        bands[i] = Math.max(bands[i] * 0.5, v);
        sum += v;
      }
      level = sum / bands.length;
    }

    function sendBurstFrame() {
      var c = document.createElement("canvas");
      c.width = 120;
      c.height = 120;
      var ctx = c.getContext("2d");
      ctx.imageSmoothingEnabled = false;
      ctx.drawImage(el.face, 0, 0, 120, 120);
      var dataUrl = c.toDataURL("image/jpeg", 0.55);
      var b64 = dataUrl.split(",")[1] || "";
      var glyph = lastGlyph.map(function (v) {
        return Math.round(v);
      });
      sendMesh({
        type: "vburst-frame",
        from: nick,
        fmt: "jpeg",
        b64: b64,
        w: 120,
        h: 120,
        glyph: glyph,
        glyphN: GLYPH_N,
        t: Date.now(),
      });
      // local paint on sphere ball
      if (typeof opts.onLocalFrame === "function") {
        opts.onLocalFrame({
          type: "vburst-frame",
          from: nick,
          glyph: glyph,
          glyphN: GLYPH_N,
          t: Date.now(),
        });
      }
    }

    function startBurst() {
      if (tx) return;
      tx = true;
      mood("tx");
      setLabel("TX");
      setMeta("<em>bursting</em> · face + mic → sphere");
      sendMesh({ type: "vburst-start", from: nick, t: Date.now() });
      sendMesh({ type: "ptt", state: "down", from: nick });
    }

    function stopBurst() {
      if (!tx) return;
      tx = false;
      mood(rxFrom ? "rx" : "idle");
      setLabel("hold");
      setMeta("idle · hold orb to walkie");
      sendMesh({ type: "vburst-end", from: nick, t: Date.now() });
      sendMesh({ type: "ptt", state: "up", from: nick });
    }

    function loop(now) {
      raf = requestAnimationFrame(loop);
      level *= 0.9;
      for (var i = 0; i < bands.length; i++) bands[i] *= 0.88;
      if (tx) readMic();
      if (rxFrom && remoteImg) {
        faceCtx.drawImage(remoteImg, 0, 0, GLYPH_N, GLYPH_N);
      } else if (tx || stream) {
        sampleVideoToGlyph();
      } else {
        paintSimFace(now);
      }
      paintRing();
      if (tx && now - frameTimer > 160) {
        frameTimer = now;
        sendBurstFrame();
      }
    }

    async function enableCam() {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: "user", width: { ideal: 320 }, height: { ideal: 320 } },
          audio: true,
        });
        video = document.createElement("video");
        video.playsInline = true;
        video.muted = true;
        video.srcObject = stream;
        await video.play();
        audioCtx = new (global.AudioContext || global.webkitAudioContext)();
        analyser = audioCtx.createAnalyser();
        analyser.fftSize = 64;
        micSource = audioCtx.createMediaStreamSource(stream);
        micSource.connect(analyser);
        setMeta("cam ready · hold orb to burst");
        if (el.btnCam) el.btnCam.textContent = "Cam on";
        appendChat("system", "camera ready — hold the orb to walkie", "system");
      } catch (e) {
        setMeta("cam blocked — sim face");
        appendChat("system", "cam blocked — using sim face", "system");
      }
    }

    function sendChat() {
      if (!el.input) return;
      var text = el.input.value.trim();
      if (!text) return;
      el.input.value = "";
      appendChat(nick, text, "self");
      sendMesh({
        type: "chat",
        text: text,
        from: nick,
        role: "peer",
        t: Date.now(),
      });
    }

    /** mesh inbound from sphere hub */
    function onMesh(msg) {
      if (!msg || !msg.type) return;
      var from = msg.from || "";
      if (from === nick) return;
      var typ = msg.type;

      if (typ === "chat" && msg.text) {
        appendChat(from, msg.text, "peer");
        return;
      }
      if (typ === "vburst-start" || (typ === "ptt" && msg.state === "down")) {
        rxFrom = from;
        mood("rx");
        setLabel(from);
        setMeta("<em>" + from + "</em> bursting on sphere");
        return;
      }
      if (typ === "vburst-end" || (typ === "ptt" && msg.state === "up")) {
        if (from === rxFrom) {
          rxFrom = "";
          mood(tx ? "tx" : "idle");
          setLabel("hold");
          setMeta("idle");
        }
        return;
      }
      if (typ === "vburst-frame") {
        rxFrom = from;
        mood("rx");
        if (Array.isArray(msg.glyph) && msg.glyph.length) {
          lastGlyph = msg.glyph.map(Number);
          var img = faceCtx.createImageData(GLYPH_N, GLYPH_N);
          for (var i = 0; i < lastGlyph.length; i++) {
            var v = lastGlyph[i];
            img.data[i * 4] = img.data[i * 4 + 1] = img.data[i * 4 + 2] = v;
            img.data[i * 4 + 3] = 255;
          }
          faceCtx.putImageData(img, 0, 0);
        } else if (msg.b64) {
          var im = new Image();
          im.onload = function () {
            remoteImg = im;
            faceCtx.drawImage(im, 0, 0, GLYPH_N, GLYPH_N);
          };
          im.src = "data:image/jpeg;base64," + msg.b64;
        }
        for (var b = 0; b < bands.length; b++) bands[b] = 0.3 + Math.random() * 0.4;
        level = 0.5;
      }
    }

    function bindHold(target) {
      if (!target) return;
      var down = function (e) {
        e.preventDefault();
        e.stopPropagation();
        startBurst();
      };
      var up = function (e) {
        e.preventDefault();
        stopBurst();
      };
      target.addEventListener("pointerdown", down);
      target.addEventListener("pointerup", up);
      target.addEventListener("pointerleave", up);
      target.addEventListener("pointercancel", up);
    }

    bindHold(el.orb);
    bindHold(el.btnHold);
    if (el.btnCam) el.btnCam.addEventListener("click", enableCam);
    if (el.send) el.send.addEventListener("click", sendChat);
    if (el.input) {
      el.input.addEventListener("keydown", function (e) {
        if (e.key === "Enter") {
          e.preventDefault();
          sendChat();
        }
      });
    }

    // Space = PTT when not typing
    global.addEventListener("keydown", function (e) {
      if (e.code !== "Space" || e.repeat) return;
      var tag = (e.target && e.target.tagName) || "";
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      e.preventDefault();
      startBurst();
    });
    global.addEventListener("keyup", function (e) {
      if (e.code !== "Space") return;
      var tag = (e.target && e.target.tagName) || "";
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      e.preventDefault();
      stopBurst();
    });

    setMeta("enable cam · hold orb · space = PTT");
    setLabel("hold");
    appendChat("system", "Walkie burst · hold the ball orb · chat below", "system");
    paintSimFace(0);
    raf = requestAnimationFrame(loop);

    return {
      onMesh: onMesh,
      enableCam: enableCam,
      startBurst: startBurst,
      stopBurst: stopBurst,
      sendChat: sendChat,
      getNick: function () {
        return nick;
      },
      setNick: function (n) {
        if (n) nick = n;
      },
    };
  }

  global.GY_SPHERE_WALKIE = { create: createSphereWalkie };
})(typeof window !== "undefined" ? window : globalThis);
