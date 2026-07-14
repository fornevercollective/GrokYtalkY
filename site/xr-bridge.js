/**
 * AR / VR / MR glasses & headsets — browser bridge
 *
 * - WebXR immersive session (when available)
 * - Stereo SBS/OU → equirect for Sphere HDRI
 * - Classify getUserMedia labels as headsets
 * - Push to media-queue as xr: / stereo: sources
 */
(function (global) {
  "use strict";

  function webxrSupported() {
    return !!(navigator.xr && navigator.xr.isSessionSupported);
  }

  async function webxrModes() {
    if (!webxrSupported()) return { immersive: false, inline: false, ar: false };
    var out = { immersive: false, inline: false, ar: false };
    try {
      out.immersive = await navigator.xr.isSessionSupported("immersive-vr");
    } catch (_) {}
    try {
      out.ar = await navigator.xr.isSessionSupported("immersive-ar");
    } catch (_) {}
    try {
      out.inline = await navigator.xr.isSessionSupported("inline");
    } catch (_) {}
    return out;
  }

  /**
   * Best-effort: open WebXR session and paint one stereo frame to canvas.
   * Many headsets still need cast URL / UVC for full passthrough video.
   */
  async function captureWebXRFrame(opts) {
    opts = opts || {};
    var modes = await webxrModes();
    var mode = opts.mode || (modes.ar ? "immersive-ar" : modes.immersive ? "immersive-vr" : "");
    if (!mode) {
      throw new Error("WebXR not supported in this browser — use GY_XR_CAST_URL or device: UVC");
    }
    var session = await navigator.xr.requestSession(mode, {
      requiredFeatures: mode.indexOf("ar") >= 0 ? ["local"] : ["local"],
      optionalFeatures: ["bounded-floor", "hand-tracking"],
    });
    var canvas = document.createElement("canvas");
    canvas.width = opts.width || 1920;
    canvas.height = opts.height || 1080;
    var gl = canvas.getContext("webgl", { xrCompatible: true });
    if (!gl) {
      session.end();
      throw new Error("WebGL unavailable for WebXR");
    }
    await gl.makeXRCompatible();
    var layer = new XRWebGLLayer(session, gl);
    session.updateRenderState({ baseLayer: layer });
    var refSpace = await session.requestReferenceSpace("local");

    return new Promise(function (resolve, reject) {
      var done = false;
      function onFrame(time, frame) {
        if (done) return;
        done = true;
        try {
          var pose = frame.getViewerPose(refSpace);
          // clear to passthrough-ish dark; true camera needs platform APIs
          gl.bindFramebuffer(gl.FRAMEBUFFER, layer.framebuffer);
          gl.clearColor(0.05, 0.06, 0.1, 1);
          gl.clear(gl.COLOR_BUFFER_BIT);
          var out = document.createElement("canvas");
          out.width = canvas.width;
          out.height = canvas.height;
          out.getContext("2d").drawImage(canvas, 0, 0);
          session.end().catch(function () {});
          resolve({
            canvas: out,
            pose: pose
              ? {
                  x: pose.transform.position.x,
                  y: pose.transform.position.y,
                  z: pose.transform.position.z,
                }
              : null,
            mode: mode,
          });
        } catch (e) {
          session.end().catch(function () {});
          reject(e);
        }
      }
      session.requestAnimationFrame(onFrame);
      setTimeout(function () {
        if (!done) {
          done = true;
          session.end().catch(function () {});
          reject(new Error("WebXR frame timeout"));
        }
      }, 4000);
    });
  }

  function classifyDeviceLabel(label) {
    var L = String(label || "").toLowerCase();
    var brands = [
      { id: "quest", m: /quest|oculus/, kind: "mr" },
      { id: "vision", m: /vision pro|apple vision/, kind: "mr" },
      { id: "hololens", m: /hololens/, kind: "mr" },
      { id: "magicleap", m: /magic leap/, kind: "ar" },
      { id: "pico", m: /pico/, kind: "vr" },
      { id: "vive", m: /vive|index|steamvr/, kind: "vr" },
      { id: "varjo", m: /varjo/, kind: "mr" },
      { id: "xreal", m: /xreal|nreal/, kind: "ar" },
      { id: "viture", m: /viture/, kind: "ar" },
      { id: "rokid", m: /rokid/, kind: "ar" },
      { id: "spectacles", m: /spectacles/, kind: "ar" },
      { id: "glass", m: /google glass|glass enterprise/, kind: "ar" },
    ];
    for (var i = 0; i < brands.length; i++) {
      if (brands[i].m.test(L)) return brands[i];
    }
    return null;
  }

  async function listXRCameras() {
    if (!navigator.mediaDevices) return [];
    try {
      var probe = await navigator.mediaDevices.getUserMedia({ video: true, audio: false });
      probe.getTracks().forEach(function (t) {
        t.stop();
      });
    } catch (_) {}
    var devices = await navigator.mediaDevices.enumerateDevices();
    return devices
      .filter(function (d) {
        return d.kind === "videoinput";
      })
      .map(function (d) {
        var c = classifyDeviceLabel(d.label);
        return {
          deviceId: d.deviceId,
          label: d.label || "camera",
          xr: c,
          isXR: !!c,
        };
      })
      .filter(function (d) {
        return d.isXR;
      });
  }

  /**
   * Open XR UVC device and convert stereo layout to equirect via GY_HDRI.
   */
  async function openXRDevice(deviceId, layout) {
    layout = layout || "sbs";
    var stream = await navigator.mediaDevices.getUserMedia({
      video: { deviceId: { ideal: deviceId }, width: { ideal: 1920 }, height: { ideal: 1080 } },
      audio: false,
    });
    var video = document.createElement("video");
    video.playsInline = true;
    video.muted = true;
    video.autoplay = true;
    video.srcObject = stream;
    await video.play().catch(function () {});
    for (var i = 0; i < 20 && !video.videoWidth; i++) {
      await new Promise(function (r) {
        setTimeout(r, 50);
      });
    }
    var frame = null;
    if (global.GY_HDRI && global.GY_HDRI.grabFrame) {
      frame = global.GY_HDRI.grabFrame(video, 1280);
    }
    var equirect = null;
    if (frame && global.GY_HDRI && global.GY_HDRI.stereoToEquirect) {
      equirect = global.GY_HDRI.stereoToEquirect(frame, layout, { width: 2048, height: 1024 });
    }
    return { stream: stream, video: video, frame: frame, equirect: equirect, layout: layout };
  }

  async function pushXRToQueue(q, opts) {
    opts = opts || {};
    if (!q) return [];
    var added = [];
    // catalog schemes
    var ids = opts.ids || ["xr:auto", "webxr:", "stereo:sbs:"];
    ids.forEach(function (id) {
      var it = q.addOne(id, { title: id });
      if (it) added.push(it);
    });
    // env-like cast: if user pasted URL
    if (opts.castUrl) {
      var c = q.addOne(opts.castUrl, { title: "XR cast" });
      if (c) {
        c.video = opts.castUrl;
        c.status = "ready";
        added.push(c);
      }
    }
    return added;
  }

  async function hdriFromXR(opts) {
    opts = opts || {};
    var layout = opts.layout || "sbs";
    var cams = await listXRCameras();
    if (cams.length) {
      var open = await openXRDevice(cams[0].deviceId, layout);
      if (open.equirect && global.GY_HDRI_VIEW) {
        global.GY_HDRI_VIEW.stashEquirect(open.equirect, {
          from: "xr-" + (cams[0].xr && cams[0].xr.id),
          slots: ["XR"],
        });
      }
      return open;
    }
    // WebXR fallback (pose + clear frame — real passthrough is platform-specific)
    if (opts.allowWebXR !== false) {
      var wx = await captureWebXRFrame(opts);
      if (wx.canvas && global.GY_HDRI && global.GY_HDRI.stereoToEquirect) {
        var eq = global.GY_HDRI.stereoToEquirect(wx.canvas, "mono", {
          width: 2048,
          height: 1024,
        });
        if (eq && global.GY_HDRI_VIEW) {
          global.GY_HDRI_VIEW.stashEquirect(eq, { from: "webxr", slots: ["XR"] });
        }
        return { canvas: wx.canvas, equirect: eq, pose: wx.pose, mode: wx.mode };
      }
    }
    throw new Error("No XR camera · set GY_XR_CAST_URL on hub or connect headset UVC/cast");
  }

  global.GY_XR_BRIDGE = {
    webxrSupported: webxrSupported,
    webxrModes: webxrModes,
    captureWebXRFrame: captureWebXRFrame,
    classifyDeviceLabel: classifyDeviceLabel,
    listXRCameras: listXRCameras,
    openXRDevice: openXRDevice,
    pushXRToQueue: pushXRToQueue,
    hdriFromXR: hdriFromXR,
  };
})(typeof window !== "undefined" ? window : globalThis);
