/**
 * Play Queue page controller
 */
(function () {
  "use strict";

  var MQ = window.GY_MEDIA_QUEUE;
  if (!MQ) {
    console.error("media-queue.js missing");
    return;
  }

  var els = {
    input: document.getElementById("mq-input"),
    add: document.getElementById("mq-add"),
    addPage: document.getElementById("mq-add-page"),
    resolve: document.getElementById("mq-resolve"),
    clear: document.getElementById("mq-clear"),
    addHint: document.getElementById("mq-add-hint"),
    status: document.getElementById("mq-status"),
    video: document.getElementById("mq-video"),
    multi: document.getElementById("mq-multi"),
    audio: document.getElementById("mq-audio"),
    prev: document.getElementById("mq-prev"),
    play: document.getElementById("mq-play"),
    pause: document.getElementById("mq-pause"),
    next: document.getElementById("mq-next"),
    mode: document.getElementById("mq-mode"),
    list: document.getElementById("mq-list"),
    count: document.getElementById("mq-count"),
    empty: document.getElementById("mq-empty"),
    share: document.getElementById("mq-share"),
    hub: document.getElementById("mq-hub"),
    hubConnect: document.getElementById("mq-hub-connect"),
    hubStatus: document.getElementById("mq-hub-status"),
    bookmarklet: document.getElementById("mq-bookmarklet"),
    bookmarkCode: document.getElementById("mq-bookmark-code"),
    exportBtn: document.getElementById("mq-export"),
    importBtn: document.getElementById("mq-import"),
    importFile: document.getElementById("mq-import-file"),
  };

  var params = new URLSearchParams(location.search || "");
  var outMode = (params.get("out") || "").toLowerCase();
  if (outMode === "tv" || outMode === "player") {
    document.body.classList.add("mq-out-tv");
  }

  var q = MQ.create({
    hubWs: (els.hub && els.hub.value) || MQ.defaultHubWS(),
  });
  if (els.hub) els.hub.value = q.snapshot().hubWs || MQ.defaultHubWS();

  var player = MQ.bindPlayer(q, {
    videoMain: els.video,
    multiWrap: els.multi,
    audioOnly: els.audio,
    status: els.status,
  });

  function setStatus(t, kind) {
    if (!els.status) return;
    els.status.textContent = t || "";
    els.status.classList.toggle("is-live", kind === "live");
    els.status.classList.toggle("is-err", kind === "err");
  }

  function renderList() {
    var snap = q.snapshot();
    if (els.count) els.count.textContent = String(snap.items.length);
    if (els.empty) els.empty.hidden = snap.items.length > 0;
    if (els.mode) els.mode.value = snap.mode;
    if (!els.list) return;
    els.list.innerHTML = "";
    snap.items.forEach(function (it, i) {
      var li = document.createElement("li");
      li.className = "mq-item";
      if (i === snap.index) li.classList.add("is-on");
      if (it.status === "playing") li.classList.add("is-playing");
      if (it.status === "error") li.classList.add("is-err");
      li.dataset.id = it.id;

      var main = document.createElement("div");
      var title = document.createElement("div");
      title.className = "mq-item-title";
      title.textContent = it.title || it.input;
      var meta = document.createElement("div");
      meta.className = "mq-item-meta";
      meta.innerHTML =
        (it.live ? '<span class="live">LIVE</span> · ' : "") +
        (it.status || "queued") +
        (it.via ? " · " + it.via : "") +
        (it.platform ? " · " + it.platform : "") +
        (it.error ? " · " + it.error.slice(0, 40) : "") +
        "<br/>" +
        escapeHtml(it.input).slice(0, 80);
      main.appendChild(title);
      main.appendChild(meta);

      var actions = document.createElement("div");
      actions.className = "mq-item-actions";
      actions.innerHTML =
        '<button type="button" data-act="play">play</button>' +
        '<button type="button" data-act="resolve">resolve</button>' +
        '<button type="button" data-act="up">↑</button>' +
        '<button type="button" data-act="down">↓</button>' +
        '<button type="button" data-act="rm">✕</button>';

      li.appendChild(main);
      li.appendChild(actions);
      li.addEventListener("click", function (e) {
        var btn = e.target.closest("button[data-act]");
        if (btn) {
          e.stopPropagation();
          var act = btn.getAttribute("data-act");
          if (act === "rm") q.remove(it.id);
          else if (act === "up") q.move(it.id, -1);
          else if (act === "down") q.move(it.id, 1);
          else if (act === "resolve") {
            q.resolveItem(it).then(function () {
              renderList();
            });
          } else if (act === "play") {
            q.select(it.id);
            q.setPlaying(true);
            player.apply();
          }
          renderList();
          return;
        }
        q.select(it.id);
        renderList();
      });
      els.list.appendChild(li);
    });
  }

  function escapeHtml(t) {
    return String(t)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  q.on(function () {
    renderList();
  });

  // boot: URL set / #add
  q.loadFromLocation();
  renderList();

  // auto-play TV mode
  if (outMode === "tv" || outMode === "player" || params.get("play") === "1") {
    q.setMode(params.get("mode") === "multi" ? "multi" : params.get("mode") === "audio" ? "audio" : "seq");
    q.resolveAll(true).then(function () {
      q.setPlaying(true);
      return player.apply();
    });
  }

  if (els.add) {
    els.add.addEventListener("click", function () {
      var raw = (els.input && els.input.value) || "";
      var added = q.addMany(raw);
      if (els.input) els.input.value = "";
      setStatus("added " + added.length, "live");
      if (els.addHint) {
        els.addHint.textContent =
          added.length + " in queue · resolve when ready · share link ships the whole set";
      }
    });
  }
  if (els.input) {
    els.input.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        if (els.add) els.add.click();
      }
    });
  }
  if (els.addPage) {
    els.addPage.addEventListener("click", async function () {
      try {
        var text = await navigator.clipboard.readText();
        if (text) {
          q.addMany(text);
          setStatus("from clipboard", "live");
        } else setStatus("clipboard empty", "err");
      } catch (_) {
        setStatus("clipboard blocked — paste into the box", "err");
      }
    });
  }
  if (els.resolve) {
    els.resolve.addEventListener("click", async function () {
      setStatus("resolving all…");
      els.resolve.disabled = true;
      try {
        await q.resolveAll(false);
        setStatus("resolved", "live");
      } catch (e) {
        setStatus("resolve error", "err");
      }
      els.resolve.disabled = false;
      renderList();
    });
  }
  if (els.clear) {
    els.clear.addEventListener("click", function () {
      if (confirm("Clear the whole queue?")) {
        q.clear();
        player.apply();
        setStatus("cleared");
      }
    });
  }

  if (els.play) {
    els.play.addEventListener("click", async function () {
      var snap = q.snapshot();
      if (!snap.items.length) {
        setStatus("queue empty", "err");
        return;
      }
      // resolve current if needed
      var cur = snap.current;
      if (cur && !cur.video) await q.resolveItem(cur);
      if (snap.mode === "multi") await q.resolveAll(true);
      q.setPlaying(true);
      await player.apply();
    });
  }
  if (els.pause) {
    els.pause.addEventListener("click", function () {
      q.setPlaying(false);
      try {
        if (els.video) els.video.pause();
        if (els.audio) els.audio.pause();
      } catch (_) {}
      setStatus("paused");
    });
  }
  if (els.next) {
    els.next.addEventListener("click", async function () {
      q.next();
      q.setPlaying(true);
      await player.apply();
    });
  }
  if (els.prev) {
    els.prev.addEventListener("click", async function () {
      q.prev();
      q.setPlaying(true);
      await player.apply();
    });
  }
  if (els.mode) {
    els.mode.addEventListener("change", function () {
      q.setMode(els.mode.value);
      if (q.snapshot().playing) player.apply();
    });
  }

  async function copyText(t) {
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(t);
        return true;
      }
    } catch (_) {}
    try {
      var ta = document.createElement("textarea");
      ta.value = t;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
      return true;
    } catch (_) {
      return false;
    }
  }

  if (els.share) {
    els.share.addEventListener("click", async function () {
      var url = q.shareURL();
      var ok = await copyText(url);
      setStatus(ok ? "share link copied" : url, ok ? "live" : "");
    });
  }

  // outputs
  function openOut(kind) {
    var targets = q.castTargets();
    if (kind === "tv") {
      window.open(targets.queueTV + "&play=1", "_blank", "noopener");
    } else if (kind === "present") {
      // Presentation API or fallback window
      var url = targets.queueTV + "&play=1";
      if (navigator.presentation && navigator.presentation.defaultRequest) {
        /* limited support — fallback */
      }
      if (window.PresentationRequest) {
        try {
          var req = new PresentationRequest([url]);
          req.start().catch(function () {
            window.open(url, "_blank", "noopener");
          });
          return;
        } catch (_) {}
      }
      window.open(url, "gy-tv", "noopener,width=1280,height=720");
    } else if (kind === "glyph") {
      window.open(targets.glyphCast, "_blank", "noopener");
    } else if (kind === "phone") {
      window.open(targets.share + "&out=player", "_blank", "noopener");
    } else if (kind === "sphere") {
      window.open(targets.sphere, "_blank", "noopener");
    } else if (kind === "speakers") {
      q.setMode("audio");
      if (els.mode) els.mode.value = "audio";
      q.setPlaying(true);
      player.apply();
      setStatus("speakers mode", "live");
    }
  }

  document.querySelectorAll("[data-out]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      openOut(btn.getAttribute("data-out"));
    });
  });

  if (els.hub) {
    els.hub.addEventListener("change", function () {
      q.setHub(els.hub.value.trim());
    });
  }
  if (els.hubConnect) {
    els.hubConnect.addEventListener("click", function () {
      if (els.hub) q.setHub(els.hub.value.trim());
      var sock = q.connectMesh("queue-" + Math.random().toString(36).slice(2, 5));
      if (els.hubStatus) {
        if (!sock) {
          els.hubStatus.textContent = "mesh fail";
          els.hubStatus.classList.add("is-err");
          return;
        }
        els.hubStatus.textContent = "mesh…";
        sock.addEventListener("open", function () {
          els.hubStatus.textContent = "mesh on · room media";
          els.hubStatus.classList.add("is-live");
          els.hubStatus.classList.remove("is-err");
        });
        sock.addEventListener("close", function () {
          els.hubStatus.textContent = "mesh off";
          els.hubStatus.classList.remove("is-live");
        });
      }
    });
  }

  // bookmarklet
  function buildBookmarklet() {
    var base = "";
    try {
      base = location.origin + location.pathname.replace(/[^/]+$/, "queue.html");
    } catch (_) {
      base = "http://127.0.0.1:9876/queue.html";
    }
    var code =
      "javascript:(function(){var u=location.href;var b=" +
      JSON.stringify(base) +
      ";window.open(b+'#add='+encodeURIComponent(u),'_blank')})();";
    if (els.bookmarklet) els.bookmarklet.href = code;
    if (els.bookmarkCode) els.bookmarkCode.textContent = code;
  }
  buildBookmarklet();

  if (els.exportBtn) {
    els.exportBtn.addEventListener("click", function () {
      var blob = new Blob([JSON.stringify(q.exportSet(), null, 2)], {
        type: "application/json",
      });
      var a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = "gy-queue.json";
      a.click();
    });
  }
  if (els.importBtn && els.importFile) {
    els.importBtn.addEventListener("click", function () {
      els.importFile.click();
    });
    els.importFile.addEventListener("change", function () {
      var f = els.importFile.files && els.importFile.files[0];
      if (!f) return;
      var reader = new FileReader();
      reader.onload = function () {
        try {
          q.importSet(String(reader.result || ""));
          setStatus("imported", "live");
          renderList();
        } catch (e) {
          setStatus("import fail", "err");
        }
      };
      reader.readAsText(f);
    });
  }

  // keyboard
  document.addEventListener("keydown", function (e) {
    if (e.target && (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA")) return;
    if (e.key === " " || e.key === "k") {
      e.preventDefault();
      if (q.snapshot().playing) els.pause && els.pause.click();
      else els.play && els.play.click();
    }
    if (e.key === "n" || e.key === "ArrowRight") els.next && els.next.click();
    if (e.key === "p" || e.key === "ArrowLeft") els.prev && els.prev.click();
    if (e.key === "f" && els.video && els.video.requestFullscreen) {
      els.video.requestFullscreen().catch(function () {});
    }
  });

  setStatus(
    q.snapshot().items.length
      ? q.snapshot().items.length + " queued · press Play"
      : "paste links · @handles · share the set"
  );
})();
