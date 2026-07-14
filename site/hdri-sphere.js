/**
 * HDRI equirect → Three.js sphere / 360 view.
 *
 * - Outside ball (map on sphere surface) · inside panosphere (sky)
 * - Stash via sessionStorage for GrokGlyph → viewer handoff
 * - Optional CDN Three.js; pure WebGL fallback if offline
 */
(function (global) {
  "use strict";

  var STORE_KEY = "gy.hdri.equirect";
  var META_KEY = "gy.hdri.meta";
  var BC_NAME = "gy-hdri";
  var THREE_CDN =
    "https://cdn.jsdelivr.net/npm/three@0.170.0/build/three.min.js";

  function now() {
    return Date.now();
  }

  function canvasToDataURL(src, quality) {
    if (!src) return "";
    if (typeof src === "string") return src;
    try {
      if (src.toDataURL) {
        return src.toDataURL("image/jpeg", quality != null ? quality : 0.82);
      }
    } catch (_) {}
    return "";
  }

  /** Persist equirect for cross-page open (GrokGlyph → hdri-view / sphere). */
  function stashEquirect(src, meta) {
    meta = meta || {};
    var dataUrl = canvasToDataURL(src, meta.quality != null ? meta.quality : 0.82);
    if (!dataUrl) return false;
    var payload = {
      dataUrl: dataUrl,
      w: meta.w || (src && src.width) || 0,
      h: meta.h || (src && src.height) || 0,
      slots: meta.slots || [],
      from: meta.from || "hdri",
      t: meta.t || now(),
    };
    try {
      sessionStorage.setItem(STORE_KEY, dataUrl);
      sessionStorage.setItem(META_KEY, JSON.stringify({
        w: payload.w,
        h: payload.h,
        slots: payload.slots,
        from: payload.from,
        t: payload.t,
      }));
    } catch (e) {
      console.warn("[hdri-sphere] stash failed (size?)", e);
      return false;
    }
    try {
      if (typeof BroadcastChannel !== "undefined") {
        var bc = new BroadcastChannel(BC_NAME);
        bc.postMessage({ type: "hdri-update", meta: payload });
        bc.close();
      }
    } catch (_) {}
    return true;
  }

  function loadStashed() {
    try {
      var dataUrl = sessionStorage.getItem(STORE_KEY);
      if (!dataUrl) return null;
      var meta = {};
      try {
        meta = JSON.parse(sessionStorage.getItem(META_KEY) || "{}");
      } catch (_) {}
      return { dataUrl: dataUrl, meta: meta };
    } catch (_) {
      return null;
    }
  }

  function clearStash() {
    try {
      sessionStorage.removeItem(STORE_KEY);
      sessionStorage.removeItem(META_KEY);
    } catch (_) {}
  }

  function loadImage(src) {
    return new Promise(function (resolve, reject) {
      if (!src) return reject(new Error("no image"));
      if (src instanceof HTMLImageElement) {
        if (src.complete && src.naturalWidth) return resolve(src);
        src.onload = function () {
          resolve(src);
        };
        src.onerror = reject;
        return;
      }
      if (src instanceof HTMLCanvasElement) {
        var im = new Image();
        im.onload = function () {
          resolve(im);
        };
        im.onerror = reject;
        im.src = src.toDataURL("image/png");
        return;
      }
      var img = new Image();
      img.crossOrigin = "anonymous";
      img.onload = function () {
        resolve(img);
      };
      img.onerror = function () {
        reject(new Error("image load failed"));
      };
      if (typeof src === "string") {
        if (src.indexOf("data:") === 0 || src.indexOf("blob:") === 0 || src.indexOf("http") === 0) {
          img.src = src;
        } else {
          img.src = "data:image/jpeg;base64," + src;
        }
      } else {
        reject(new Error("bad image src"));
      }
    });
  }

  function loadThree() {
    if (global.THREE) return Promise.resolve(global.THREE);
    return new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.src = THREE_CDN;
      s.async = true;
      s.onload = function () {
        if (global.THREE) resolve(global.THREE);
        else reject(new Error("THREE missing after load"));
      };
      s.onerror = function () {
        reject(new Error("Three.js CDN failed"));
      };
      document.head.appendChild(s);
    });
  }

  /**
   * Pure WebGL equirect ball (no Three) — works offline.
   * mode: "outside" | "inside"
   */
  function createWebGLViewer(container, opts) {
    opts = opts || {};
    var mode = opts.mode || "outside"; // outside ball | inside 360
    var canvas = document.createElement("canvas");
    canvas.className = "hdri-view-canvas";
    canvas.style.cssText = "display:block;width:100%;height:100%;touch-action:none;";
    container.appendChild(canvas);
    var gl =
      canvas.getContext("webgl", { antialias: true, alpha: false }) ||
      canvas.getContext("experimental-webgl", { antialias: true, alpha: false });
    if (!gl) throw new Error("WebGL unavailable");

    var VS =
      "attribute vec3 aPos;attribute vec2 aUv;uniform mat4 uMvp;varying vec2 vUv;" +
      "void main(){vUv=aUv;gl_Position=uMvp*vec4(aPos,1.0);}";
    var FS =
      "precision mediump float;varying vec2 vUv;uniform sampler2D uTex;" +
      "void main(){gl_FragColor=texture2D(uTex,vUv);}";

    function compile(type, src) {
      var s = gl.createShader(type);
      gl.shaderSource(s, src);
      gl.compileShader(s);
      return s;
    }
    var prog = gl.createProgram();
    gl.attachShader(prog, compile(gl.VERTEX_SHADER, VS));
    gl.attachShader(prog, compile(gl.FRAGMENT_SHADER, FS));
    gl.linkProgram(prog);
    gl.useProgram(prog);
    var aPos = gl.getAttribLocation(prog, "aPos");
    var aUv = gl.getAttribLocation(prog, "aUv");
    var uMvp = gl.getUniformLocation(prog, "uMvp");
    var uTex = gl.getUniformLocation(prog, "uTex");

    // UV sphere
    var segW = 64,
      segH = 32;
    var verts = [];
    var uvs = [];
    var idx = [];
    for (var y = 0; y <= segH; y++) {
      var v = y / segH;
      var th = v * Math.PI;
      for (var x = 0; x <= segW; x++) {
        var u = x / segW;
        var ph = u * Math.PI * 2;
        var sx = Math.sin(th) * Math.cos(ph);
        var sy = Math.cos(th);
        var sz = Math.sin(th) * Math.sin(ph);
        verts.push(sx, sy, sz);
        // equirect: flip U for outside-looking maps often needed
        uvs.push(1 - u, 1 - v);
      }
    }
    for (var j = 0; j < segH; j++) {
      for (var i = 0; i < segW; i++) {
        var a = j * (segW + 1) + i;
        var b = a + segW + 1;
        idx.push(a, b, a + 1, b, b + 1, a + 1);
      }
    }
    var vBuf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, vBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(verts), gl.STATIC_DRAW);
    var uvBuf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, uvBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(uvs), gl.STATIC_DRAW);
    var iBuf = gl.createBuffer();
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, iBuf);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, new Uint16Array(idx), gl.STATIC_DRAW);
    var indexCount = idx.length;

    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    // placeholder
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.RGBA,
      1,
      1,
      0,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      new Uint8Array([20, 24, 40, 255])
    );

    gl.enable(gl.DEPTH_TEST);
    gl.clearColor(0.03, 0.03, 0.05, 1);

    var yaw = 0.4,
      pitch = 0.15,
      dist = mode === "inside" ? 0.01 : 2.6;
    var dragging = false,
      lx = 0,
      ly = 0;
    var raf = 0;
    var alive = true;

    function m4P(fovy, aspect, near, far) {
      var f = 1 / Math.tan(fovy / 2);
      var nf = 1 / (near - far);
      var m = new Float32Array(16);
      m[0] = f / aspect;
      m[5] = f;
      m[10] = (far + near) * nf;
      m[11] = -1;
      m[14] = 2 * far * near * nf;
      return m;
    }
    function m4Look(eye, target, up) {
      var zx = eye[0] - target[0],
        zy = eye[1] - target[1],
        zz = eye[2] - target[2];
      var zl = Math.hypot(zx, zy, zz) || 1;
      zx /= zl;
      zy /= zl;
      zz /= zl;
      var xx = up[1] * zz - up[2] * zy;
      var xy = up[2] * zx - up[0] * zz;
      var xz = up[0] * zy - up[1] * zx;
      var xl = Math.hypot(xx, xy, xz) || 1;
      xx /= xl;
      xy /= xl;
      xz /= xl;
      var yx = zy * xz - zz * xy;
      var yy = zz * xx - zx * xz;
      var yz = zx * xy - zy * xx;
      var m = new Float32Array(16);
      m[0] = xx;
      m[1] = yx;
      m[2] = zx;
      m[3] = 0;
      m[4] = xy;
      m[5] = yy;
      m[6] = zy;
      m[7] = 0;
      m[8] = xz;
      m[9] = yz;
      m[10] = zz;
      m[11] = 0;
      m[12] = -(xx * eye[0] + xy * eye[1] + xz * eye[2]);
      m[13] = -(yx * eye[0] + yy * eye[1] + yz * eye[2]);
      m[14] = -(zx * eye[0] + zy * eye[1] + zz * eye[2]);
      m[15] = 1;
      return m;
    }
    function m4Mul(a, b) {
      var o = new Float32Array(16);
      for (var c = 0; c < 4; c++)
        for (var r = 0; r < 4; r++)
          o[c * 4 + r] =
            a[r] * b[c * 4] +
            a[4 + r] * b[c * 4 + 1] +
            a[8 + r] * b[c * 4 + 2] +
            a[12 + r] * b[c * 4 + 3];
      return o;
    }

    function resize() {
      var rect = container.getBoundingClientRect();
      var dpr = Math.min(2, window.devicePixelRatio || 1);
      var w = Math.max(1, Math.floor(rect.width * dpr));
      var h = Math.max(1, Math.floor(rect.height * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      gl.viewport(0, 0, w, h);
    }

    function eyePos() {
      if (mode === "inside") {
        return [0, 0, 0];
      }
      var cp = Math.cos(pitch);
      return [
        Math.sin(yaw) * cp * dist,
        Math.sin(pitch) * dist,
        Math.cos(yaw) * cp * dist,
      ];
    }

    function frame() {
      if (!alive) return;
      raf = requestAnimationFrame(frame);
      resize();
      var aspect = canvas.width / Math.max(1, canvas.height);
      var proj = m4P(mode === "inside" ? 1.2 : 0.9, aspect, 0.05, 100);
      var eye = eyePos();
      var target =
        mode === "inside"
          ? [
              Math.sin(yaw) * Math.cos(pitch),
              Math.sin(pitch),
              Math.cos(yaw) * Math.cos(pitch),
            ]
          : [0, 0, 0];
      var view = m4Look(eye, target, [0, 1, 0]);
      var mvp = m4Mul(proj, view);

      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
      gl.useProgram(prog);
      gl.uniformMatrix4fv(uMvp, false, mvp);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.uniform1i(uTex, 0);

      gl.bindBuffer(gl.ARRAY_BUFFER, vBuf);
      gl.enableVertexAttribArray(aPos);
      gl.vertexAttribPointer(aPos, 3, gl.FLOAT, false, 0, 0);
      gl.bindBuffer(gl.ARRAY_BUFFER, uvBuf);
      gl.enableVertexAttribArray(aUv);
      gl.vertexAttribPointer(aUv, 2, gl.FLOAT, false, 0, 0);
      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, iBuf);

      if (mode === "inside") {
        gl.disable(gl.CULL_FACE);
        gl.frontFace(gl.CW);
      } else {
        gl.enable(gl.CULL_FACE);
        gl.cullFace(gl.BACK);
        gl.frontFace(gl.CCW);
      }
      gl.drawElements(gl.TRIANGLES, indexCount, gl.UNSIGNED_SHORT, 0);
    }

    function onDown(e) {
      dragging = true;
      lx = e.clientX != null ? e.clientX : e.touches[0].clientX;
      ly = e.clientY != null ? e.clientY : e.touches[0].clientY;
    }
    function onMove(e) {
      if (!dragging) return;
      var x = e.clientX != null ? e.clientX : e.touches[0].clientX;
      var y = e.clientY != null ? e.clientY : e.touches[0].clientY;
      var dx = x - lx,
        dy = y - ly;
      lx = x;
      ly = y;
      var sens = mode === "inside" ? 0.005 : 0.006;
      yaw -= dx * sens;
      pitch += dy * sens * (mode === "inside" ? 1 : 1);
      pitch = Math.max(-1.2, Math.min(1.2, pitch));
    }
    function onUp() {
      dragging = false;
    }
    function onWheel(e) {
      if (mode === "inside") return;
      e.preventDefault();
      dist *= e.deltaY > 0 ? 1.08 : 0.92;
      dist = Math.max(1.35, Math.min(6, dist));
    }

    canvas.addEventListener("pointerdown", onDown);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    canvas.addEventListener("wheel", onWheel, { passive: false });
    canvas.addEventListener(
      "touchmove",
      function (e) {
        if (dragging) e.preventDefault();
      },
      { passive: false }
    );

    function setMap(src) {
      return loadImage(src).then(function (img) {
        gl.bindTexture(gl.TEXTURE_2D, tex);
        gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, 1);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
        return { w: img.naturalWidth || img.width, h: img.naturalHeight || img.height };
      });
    }

    function setMode(m) {
      mode = m === "inside" ? "inside" : "outside";
      dist = mode === "inside" ? 0.01 : 2.6;
    }

    function dispose() {
      alive = false;
      cancelAnimationFrame(raf);
      canvas.removeEventListener("pointerdown", onDown);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("wheel", onWheel);
      if (canvas.parentNode) canvas.parentNode.removeChild(canvas);
      try {
        gl.getExtension("WEBGL_lose_context") &&
          gl.getExtension("WEBGL_lose_context").loseContext();
      } catch (_) {}
    }

    raf = requestAnimationFrame(frame);

    return {
      kind: "webgl",
      canvas: canvas,
      setMap: setMap,
      setMode: setMode,
      getMode: function () {
        return mode;
      },
      resize: resize,
      dispose: dispose,
    };
  }

  /**
   * Three.js equirect sphere (preferred when CDN available).
   */
  function createThreeViewer(container, THREE, opts) {
    opts = opts || {};
    var mode = opts.mode || "outside";
    var canvas = document.createElement("canvas");
    canvas.className = "hdri-view-canvas";
    canvas.style.cssText = "display:block;width:100%;height:100%;touch-action:none;";
    container.appendChild(canvas);

    var renderer = new THREE.WebGLRenderer({
      canvas: canvas,
      antialias: true,
      alpha: false,
      powerPreference: "high-performance",
    });
    renderer.setClearColor(0x08080c, 1);
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));

    var scene = new THREE.Scene();
    var camera = new THREE.PerspectiveCamera(70, 1, 0.05, 100);
    camera.position.set(0, 0.2, 2.6);

    var geo = new THREE.SphereGeometry(1.4, 64, 32);
    var mat = new THREE.MeshBasicMaterial({
      color: 0x1a1a28,
      side: THREE.FrontSide,
    });
    var mesh = new THREE.Mesh(geo, mat);
    // flip U so equirect looks natural
    mesh.scale.x = -1;
    scene.add(mesh);

    // soft fill so empty map still reads as a ball
    var amb = new THREE.AmbientLight(0xffffff, 0.35);
    scene.add(amb);

    var yaw = 0.35,
      pitch = 0.12,
      dist = 2.6;
    var dragging = false,
      lx = 0,
      ly = 0;
    var raf = 0;
    var alive = true;

    function applyMode() {
      if (mode === "inside") {
        mat.side = THREE.BackSide;
        mesh.scale.set(1, 1, 1);
        camera.fov = 85;
        camera.position.set(0, 0, 0.01);
        dist = 0.01;
      } else {
        mat.side = THREE.FrontSide;
        mesh.scale.set(-1, 1, 1);
        camera.fov = 55;
        dist = Math.max(2.0, dist);
      }
      camera.updateProjectionMatrix();
    }
    applyMode();

    function resize() {
      var rect = container.getBoundingClientRect();
      var w = Math.max(1, Math.floor(rect.width));
      var h = Math.max(1, Math.floor(rect.height));
      renderer.setSize(w, h, false);
      camera.aspect = w / h;
      camera.updateProjectionMatrix();
    }

    function updateCam() {
      if (mode === "inside") {
        camera.position.set(0, 0, 0);
        var tx = Math.sin(yaw) * Math.cos(pitch);
        var ty = Math.sin(pitch);
        var tz = Math.cos(yaw) * Math.cos(pitch);
        camera.lookAt(tx, ty, tz);
      } else {
        var cp = Math.cos(pitch);
        camera.position.set(
          Math.sin(yaw) * cp * dist,
          Math.sin(pitch) * dist,
          Math.cos(yaw) * cp * dist
        );
        camera.lookAt(0, 0, 0);
      }
    }

    function frame() {
      if (!alive) return;
      raf = requestAnimationFrame(frame);
      resize();
      updateCam();
      renderer.render(scene, camera);
    }

    function onDown(e) {
      dragging = true;
      lx = e.clientX != null ? e.clientX : e.touches[0].clientX;
      ly = e.clientY != null ? e.clientY : e.touches[0].clientY;
      try {
        canvas.setPointerCapture(e.pointerId);
      } catch (_) {}
    }
    function onMove(e) {
      if (!dragging) return;
      var x = e.clientX != null ? e.clientX : e.touches[0].clientX;
      var y = e.clientY != null ? e.clientY : e.touches[0].clientY;
      var dx = x - lx,
        dy = y - ly;
      lx = x;
      ly = y;
      yaw -= dx * 0.0055;
      pitch += dy * 0.005;
      pitch = Math.max(-1.25, Math.min(1.25, pitch));
    }
    function onUp() {
      dragging = false;
    }
    function onWheel(e) {
      if (mode === "inside") return;
      e.preventDefault();
      dist *= e.deltaY > 0 ? 1.08 : 0.92;
      dist = Math.max(1.7, Math.min(7, dist));
    }

    canvas.addEventListener("pointerdown", onDown);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    canvas.addEventListener("wheel", onWheel, { passive: false });

    function setMap(src) {
      return loadImage(src).then(function (img) {
        var tex = new THREE.Texture(img);
        tex.colorSpace = THREE.SRGBColorSpace || THREE.sRGBEncoding;
        tex.needsUpdate = true;
        tex.wrapS = THREE.ClampToEdgeWrapping;
        tex.wrapT = THREE.ClampToEdgeWrapping;
        tex.minFilter = THREE.LinearFilter;
        tex.magFilter = THREE.LinearFilter;
        if (mat.map) mat.map.dispose();
        mat.map = tex;
        mat.color.set(0xffffff);
        mat.needsUpdate = true;
        return { w: img.naturalWidth || img.width, h: img.naturalHeight || img.height };
      });
    }

    function setMode(m) {
      mode = m === "inside" ? "inside" : "outside";
      applyMode();
    }

    function dispose() {
      alive = false;
      cancelAnimationFrame(raf);
      canvas.removeEventListener("pointerdown", onDown);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      canvas.removeEventListener("wheel", onWheel);
      if (mat.map) mat.map.dispose();
      geo.dispose();
      mat.dispose();
      renderer.dispose();
      if (canvas.parentNode) canvas.parentNode.removeChild(canvas);
    }

    raf = requestAnimationFrame(frame);

    return {
      kind: "three",
      canvas: canvas,
      setMap: setMap,
      setMode: setMode,
      getMode: function () {
        return mode;
      },
      resize: resize,
      dispose: dispose,
      THREE: THREE,
      scene: scene,
      mesh: mesh,
    };
  }

  /**
   * Mount viewer into container. Prefers Three.js, falls back to WebGL.
   * @returns {Promise<Viewer>}
   */
  function createViewer(container, opts) {
    opts = opts || {};
    if (!container) return Promise.reject(new Error("no container"));
    container.innerHTML = "";
    return loadThree()
      .then(function (THREE) {
        return createThreeViewer(container, THREE, opts);
      })
      .catch(function (err) {
        console.warn("[hdri-sphere] Three unavailable, WebGL fallback:", err && err.message);
        return createWebGLViewer(container, opts);
      });
  }

  /** Open dedicated viewer page with optional stash already set. */
  function openViewerPage(opts) {
    opts = opts || {};
    var q = [];
    if (opts.mode) q.push("mode=" + encodeURIComponent(opts.mode));
    if (opts.mesh) q.push("mesh=1");
    var url = "hdri-view.html" + (q.length ? "?" + q.join("&") : "");
    if (opts.sameTab) location.href = url;
    else window.open(url, "_blank", "noopener");
    return url;
  }

  global.GY_HDRI_VIEW = {
    STORE_KEY: STORE_KEY,
    META_KEY: META_KEY,
    BC_NAME: BC_NAME,
    stashEquirect: stashEquirect,
    loadStashed: loadStashed,
    clearStash: clearStash,
    loadImage: loadImage,
    createViewer: createViewer,
    openViewerPage: openViewerPage,
    canvasToDataURL: canvasToDataURL,
  };
})(typeof window !== "undefined" ? window : globalThis);
