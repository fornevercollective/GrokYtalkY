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
    scrub: document.getElementById("mq-scrub"),
    timeLab: document.getElementById("mq-time"),
  });

  // wire remote timeline → video
  q.on(function (snap) {
    if (!els.video || snap.mediaTime == null) return;
    // when mesh seek applied, snap video if drift > 0.6s
    try {
      if (
        isFinite(els.video.duration) &&
        Math.abs((els.video.currentTime || 0) - snap.mediaTime) > 0.6
      ) {
        els.video.currentTime = snap.mediaTime;
      }
      if (snap.playing && els.video.paused) els.video.play().catch(function () {});
      if (!snap.playing && !els.video.paused) els.video.pause();
    } catch (_) {}
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

  // Facility ingest registry + PGM
  var ingestList = document.getElementById("mq-ingest-list");
  async function refreshIngest() {
    if (!ingestList || !q.fetchIngestList) return;
    try {
      var data = await q.fetchIngestList();
      ingestList.innerHTML = "";
      (data.sources || []).forEach(function (s) {
        var b = document.createElement("button");
        b.type = "button";
        b.className = "mq-ingest-chip" + (s.ready ? " is-ready" : "");
        b.title = (s.detail || "") + " · " + s.id;
        var tag = "";
        if (s.brand === "Blackmagic") tag = '<span class="bmd">BMD</span> ';
        else if (s.scheme === "xr" || s.kind === "ar" || s.kind === "vr" || s.kind === "mr")
          tag = '<span class="xr">XR</span> ';
        b.innerHTML = tag + escapeHtml((s.label || s.id).slice(0, 36));
        b.addEventListener("click", function () {
          if (!s.ready && s.scheme !== "pgm" && s.scheme !== "xr" && s.scheme !== "stereo") {
            setStatus((s.detail || "not ready on this host"), "err");
          }
          var addId = s.id;
          if (s.scheme === "stereo" && (!s.id || s.id.endsWith(":"))) {
            addId = s.id + "device:0";
          }
          q.addOne(addId || s.id, { title: s.label });
          setStatus("queued " + (addId || s.id), "live");
          renderList();
        });
        ingestList.appendChild(b);
      });
    } catch (e) {
      if (ingestList) {
        ingestList.innerHTML =
          "<span class='mq-hint'>ingest list offline · is gy serve up?</span>";
      }
    }
  }
  var ingestRefresh = document.getElementById("mq-ingest-refresh");
  if (ingestRefresh) ingestRefresh.addEventListener("click", refreshIngest);
  var pgmBtn = document.getElementById("mq-pgm");
  if (pgmBtn) {
    pgmBtn.addEventListener("click", function () {
      q.addOne("pgm:", { title: "PGM · program bus" });
      setStatus("PGM tile queued · set GY_PGM_PLAY_URL for live play", "live");
      renderList();
    });
  }

  var liveLanes = [];
  var threeBtn = document.getElementById("mq-threecam");
  if (threeBtn) {
    threeBtn.addEventListener("click", async function () {
      setStatus("3-cam · permission + open…");
      threeBtn.disabled = true;
      try {
        // 1) native hub pack (ffmpeg devices → HLS → shared timeline)
        var base = q.hubHTTP ? q.hubHTTP() : location.origin;
        var r = await fetch(base + "/api/media/ingest/three-cam", {
          headers: { Accept: "application/json" },
        });
        var data = await r.json().catch(function () {
          return {};
        });
        var n = 0;
        if (data && data.ok && Array.isArray(data.items)) {
          data.items.forEach(function (it) {
            if (!it.video && !it.src) return;
            var item = q.addOne(it.src || it.video, {
              title: (it.slot || "?") + " · " + (it.label || "cam"),
              video: it.video || "",
            });
            if (item && it.video) {
              item.video = it.video;
              item.status = "ready";
              item.live = true;
              item.slot = it.slot;
              n++;
            }
          });
        }
        // 2) browser getUserMedia bridge (labels + face MOCAP variance) for HDRI / multi-source
        if (window.GY_CAM_BRIDGE) {
          try {
            liveLanes = await window.GY_CAM_BRIDGE.openThreeCam({ max: 3 });
            liveLanes = window.GY_CAM_BRIDGE.applyFaceSlots(liveLanes);
            var multi = window.GY_CAM_BRIDGE.toMultiSource(liveLanes);
            // stamp multi-source face flags onto matching queue items by slot
            var snap = q.snapshot && q.snapshot();
            if (snap && Array.isArray(snap.items)) {
              snap.items.forEach(function (it) {
                if (!it || !it.slot) return;
                var src = multi.sources.find(function (s) {
                  return s.slot === it.slot;
                });
                if (!src) return;
                it.mocap = src.mocap;
                it.talking = src.talking;
                it.variance = src.variance;
                it.varianceN = src.varianceN;
                it.muteHint = src.muteHint;
                it.face = src.face;
                it.multiSource = multi;
              });
            }
            try {
              q._lastMultiSource = multi;
              sessionStorage.setItem("gy.multiSource.v1", JSON.stringify(multi));
            } catch (_) {}
            var faceN = liveLanes.filter(function (l) {
              return l.mocap || (l.face && l.face.confidence > 0.25);
            }).length;
            setStatus(
              "3-cam · queue " +
                n +
                " · browser " +
                liveLanes.length +
                (faceN ? " · face MOCAP " + (multi.faceSlot || "L1") : "") +
                (multi.talking
                  ? " · talking v" + Number(multi.varianceN || 0).toFixed(2)
                  : faceN
                    ? " · quiet"
                    : "") +
                " · muteHint=" +
                (multi.muteHint || "none"),
              "live"
            );
          } catch (e) {
            setStatus(
              "native " + n + " · browser cam: " + (e && e.message ? e.message : e),
              n ? "live" : "err"
            );
          }
        } else {
          setStatus(n ? "3-cam · " + n + " on timeline" : "3-cam failed", n ? "live" : "err");
        }
        q.setMode("multi");
        if (els.mode) els.mode.value = "multi";
        renderList();
        if (n) {
          q.setPlaying(true);
          player.apply();
        }
      } catch (e) {
        setStatus("3-cam error · " + (e && e.message ? e.message : e), "err");
      }
      threeBtn.disabled = false;
    });
  }

  var hdriBtn = document.getElementById("mq-hdri");
  if (hdriBtn) {
    hdriBtn.addEventListener("click", async function () {
      if (!window.GY_HDRI) {
        setStatus("hdri-stitch.js missing", "err");
        return;
      }
      var lanes = liveLanes;
      if (!lanes || !lanes.length) {
        if (window.GY_CAM_BRIDGE) {
          try {
            lanes = await window.GY_CAM_BRIDGE.openThreeCam({ max: 3 });
            lanes = window.GY_CAM_BRIDGE.applyFaceSlots(lanes);
            liveLanes = lanes;
          } catch (e) {
            setStatus("open cams first · " + (e && e.message ? e.message : e), "err");
            return;
          }
        }
      }
      setStatus("HDRI stitch…");
      var result = window.GY_HDRI.runProbe(window.GY_CAM_BRIDGE.toHdriLanes(lanes), {
        tonemap: true,
        sendMesh: function (obj) {
          if (q.meshTimeline) {
            /* cast via dual if linked */
          }
          // fan hdri-probe if dual/mesh
          try {
            if (typeof q.castSphereDome === "function") {
              /* dome from equirect below */
            }
          } catch (_) {}
          return false;
        },
      });
      if (!result || !result.ok) {
        setStatus((result && result.error) || "HDRI fail", "err");
        return;
      }
      if (window.GY_HDRI_VIEW && result.equirect) {
        window.GY_HDRI_VIEW.stashEquirect(result.equirect, {
          from: "queue-3cam",
          slots: result.slots,
          w: result.equirect.width,
          h: result.equirect.height,
        });
        window.GY_HDRI_VIEW.openViewerPage({ mode: "outside" });
      }
      // mesh cast equirect if dual connected
      try {
        if (result.equirect) {
          var jpeg = result.equirect.toDataURL("image/jpeg", 0.72);
          var b64 = jpeg.split(",")[1] || "";
          var msg = {
            type: "hdri-probe",
            from: "queue-hdri",
            slots: result.slots,
            b64: b64,
            w: result.equirect.width,
            h: result.equirect.height,
            cast: "sphere",
            t: Date.now(),
          };
          if (q.snapshot && false) {
            /* placeholder */
          }
          // use dual send if available via connectDual's sendMesh
          var send = null;
          try {
            // re-get through a hack: castSphereDome only sends media-dome
            // push via fetch to hub optional
            fetch((q.hubHTTP && q.hubHTTP()) + "/api/hdri/probe", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                slots: result.slots,
                w: result.equirect.width,
                h: result.equirect.height,
                jpeg: jpeg,
                t: Date.now(),
              }),
            }).catch(function () {});
          } catch (_) {}
          void msg;
          void send;
        }
      } catch (_) {}
      setStatus(
        "HDRI · " + (result.slots || []).join("·") + " · view 3D opened · sphere via GrokGlyph cast",
        "live"
      );
    });
  }

  var xrBtn = document.getElementById("mq-xr");
  if (xrBtn) {
    xrBtn.addEventListener("click", async function () {
      setStatus("XR · load headset pack…");
      xrBtn.disabled = true;
      try {
        // Queue catalog schemes
        [
          "xr:auto",
          "xr:quest",
          "xr:vision",
          "xr:hololens",
          "xr:pico",
          "xr:vive",
          "xr:xreal",
          "webxr:",
          "stereo:sbs:",
        ].forEach(function (id) {
          q.addOne(id, { title: id });
        });
        // Prefer live capture / equirect
        if (window.GY_XR_BRIDGE) {
          try {
            var xr = await window.GY_XR_BRIDGE.hdriFromXR({ layout: "sbs" });
            if (xr && xr.equirect && window.GY_HDRI_VIEW) {
              window.GY_HDRI_VIEW.openViewerPage({ mode: "inside" });
            }
            setStatus(
              "XR · equirect ready" + (xr.pose ? " · head pose" : "") + " · inside 360",
              "live"
            );
          } catch (e) {
            setStatus(
              "XR schemes queued · " +
                (e && e.message ? e.message : e) +
                " · set GY_XR_CAST_URL or connect UVC",
              "live"
            );
          }
        } else {
          setStatus("XR schemes queued · resolve cast URL on hub", "live");
        }
        renderList();
      } catch (e) {
        setStatus("XR error · " + (e && e.message ? e.message : e), "err");
      }
      xrBtn.disabled = false;
    });
  }

  refreshIngest();

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
  var qualityEl = document.getElementById("mq-quality");
  if (qualityEl) {
    qualityEl.addEventListener("change", function () {
      q.setQuality(qualityEl.value);
    });
  }
  function wireSeek(id, delta) {
    var b = document.getElementById(id);
    if (!b) return;
    b.addEventListener("click", function () {
      player.seekRel(delta);
    });
  }
  wireSeek("mq-seek-back", -10);
  wireSeek("mq-seek-back30", -30);
  wireSeek("mq-seek-fwd", 10);
  wireSeek("mq-seek-fwd30", 30);

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
      var cTv = q.snapshot().current;
      (cTv && !cTv.video ? q.resolveItem(cTv) : Promise.resolve()).then(function () {
        window.open(q.castTargets().queueTV, "_blank", "noopener");
      });
    } else if (kind === "present") {
      var url = q.castTargets().queueTV;
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
      window.open(targets.share + "&out=player&play=1", "_blank", "noopener");
    } else if (kind === "sphere") {
      var cur = q.snapshot().current;
      (cur && !cur.video ? q.resolveItem(cur) : Promise.resolve()).then(function () {
        q.castSphereDome();
        window.open(q.castTargets().sphere, "_blank", "noopener");
        setStatus("sphere HDRI · media-dome sent", "live");
      });
    } else if (kind === "dome") {
      var c2 = q.snapshot().current;
      (c2 && !c2.video ? q.resolveItem(c2) : Promise.resolve()).then(function () {
        var m = q.castSphereDome();
        setStatus(m ? "media-dome → mesh" : "resolve a clip first", m ? "live" : "err");
      });
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

  // Dual SFU path (hub + WebRTC DC / SFU WS)
  var dualStatus = document.getElementById("mq-dual-status");
  var dualBtn = document.getElementById("mq-dual");
  var sfuInput = document.getElementById("mq-sfu");
  var sfuToken = document.getElementById("mq-sfu-token");
  if (sfuInput && !sfuInput.value) {
    try {
      var h = location.hostname || "127.0.0.1";
      sfuInput.value = "ws://" + h + ":9880/ws";
    } catch (_) {
      sfuInput.value = "ws://127.0.0.1:9880/ws";
    }
  }
  if (sfuToken && !sfuToken.value) {
    try {
      sfuToken.value = localStorage.getItem("gy-sfu-token") || "";
    } catch (_) {}
  }
  if (dualBtn) {
    dualBtn.addEventListener("click", function () {
      if (els.hub) q.setHub(els.hub.value.trim());
      var tok = (sfuToken && sfuToken.value.trim()) || "";
      try {
        if (tok) localStorage.setItem("gy-sfu-token", tok);
      } catch (_) {}
      var res = q.connectDual({
        nick: "queue-" + Math.random().toString(36).slice(2, 5),
        sfuWs: (sfuInput && sfuInput.value.trim()) || undefined,
        token: tok,
        room: "media",
        webrtc: true,
        onStatus: function (t, c) {
          if (!dualStatus) return;
          dualStatus.textContent = t || "dual";
          dualStatus.classList.toggle("is-live", !!(c && (c.hub || c.sfu)));
          dualStatus.classList.toggle("is-err", false);
        },
      });
      if (dualStatus) {
        if (res && res.hubOnly) {
          dualStatus.textContent = "hub only · media-sfu.js missing?";
        } else {
          dualStatus.textContent = "dual connecting…";
          dualStatus.classList.add("is-live");
        }
      }
      setStatus("dual path · hub + sfu room media", "live");
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

  // keyboard — TV scrub like gy watch
  document.addEventListener("keydown", function (e) {
    if (e.target && (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA" || e.target.tagName === "SELECT"))
      return;
    if (e.key === " " || e.key === "k") {
      e.preventDefault();
      if (q.snapshot().playing) els.pause && els.pause.click();
      else els.play && els.play.click();
    }
    if (e.key === "n") els.next && els.next.click();
    if (e.key === "p" && !e.shiftKey) els.prev && els.prev.click();
    if (e.key === "j") {
      e.preventDefault();
      player.seekRel(-10);
    }
    if (e.key === "l" && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      player.seekRel(10);
    }
    if (e.key === "J") {
      e.preventDefault();
      player.seekRel(-30);
    }
    if (e.key === "L") {
      e.preventDefault();
      player.seekRel(30);
    }
    if (e.key === "0") {
      e.preventDefault();
      player.seekAbs(0);
    }
    if (e.key === "ArrowRight" && e.shiftKey) {
      e.preventDefault();
      player.seekRel(30);
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      player.seekRel(10);
    }
    if (e.key === "ArrowLeft" && e.shiftKey) {
      e.preventDefault();
      player.seekRel(-30);
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      player.seekRel(-10);
    }
    if (e.key === "f" && els.video && els.video.requestFullscreen) {
      els.video.requestFullscreen().catch(function () {});
    }
  });

  // one-tap cmd uses live LAN when possible
  try {
    var ot = document.getElementById("mq-onetap-cmd");
    if (ot && location.port === "9876") {
      ot.textContent =
        "pbpaste | python3 -c \"import sys,urllib.parse;print(sys.stdin.read().strip())\" | xargs -I{} open '" +
        location.origin +
        "/queue.html#add={}'";
    }
  } catch (_) {}

  setStatus(
    q.snapshot().items.length
      ? q.snapshot().items.length + " queued · press Play"
      : "paste links · @handles · share the set"
  );
})();
