
// All-canvas, WYSIWYG version of fit_frame.mjs
// - Live preview and export are rendered by the same drawing pipeline
// - No CSS transforms are used for the photo or frame
// - Fixes Safari rounding/skew mismatches between CSS and Canvas

Telegram.WebApp.ready();
Telegram.WebApp.expand();

const ETransform = [[1, 0, 0], [0, 1, 0]];

// DOM
const help_div = document.querySelector(".help");
const screen_size_source = document.querySelector(".photo");
const photoEl = document.querySelector(".photo img");
const frameSourceEl = document.querySelector(".frame_source");
// const flareSourceEl = document.querySelector(".flare_source"); // optional
const overlayEl = document.querySelector(".overlay");

// State (sizes in CSS pixels unless noted)
let W = 0, H = 0, Vmin = 0;
let frame_size = 0;          // CSS px
let f_top = 0, f_left = 0;   // CSS px
let ph = 0, pw = 0;          // photo intrinsic pixels

// Transforms are expressed in "real frame units" (0..real_frame_size)
let transformationMatrix = ETransform;

// Compensations for iOS alignment (also in real frame units)
const alignment = { x: 0, y: 0, changed: false, scale: {x:1, y:1} };

// Canvas: on-screen viewer and offscreen square for the frame area
const DPR = Math.max(1, window.devicePixelRatio || 1);
const viewer = document.createElement("canvas");
viewer.className = "viewer-canvas";
Object.assign(viewer.style, {
  position: "fixed",
  inset: "0",
  width: "100vw",
  height: "100vh",
  zIndex: "0",          // below overlay/help
});
document.body.appendChild(viewer);
const vctx = viewer.getContext("2d");

let offscreen = document.createElement("canvas");
let offctx = offscreen.getContext("2d");

// Helpers: matrices are [[a,c,e],[b,d,f]]
function M(A, B) {
  return [
    [
      A[0][0] * B[0][0] + A[0][1] * B[1][0],
      A[0][0] * B[0][1] + A[0][1] * B[1][1],
      A[0][0] * B[0][2] + A[0][1] * B[1][2] + A[0][2],
    ],
    [
      A[1][0] * B[0][0] + A[1][1] * B[1][0],
      A[1][0] * B[0][1] + A[1][1] * B[1][1],
      A[1][0] * B[0][2] + A[1][1] * B[1][2] + A[1][2],
    ]
  ];
}
function MM(...matrices) {
  if (matrices.length < 1) return ETransform;
  let result = matrices[0];
  for (let i = 1; i < matrices.length; i++) result = M(result, matrices[i]);
  return result;
}
function translate2matrix(x, y) {
  return [[1,0,x],[0,1,y]];
}
function scale2matrix(sx, sy) {
  return [[sx,0,0],[0,sy,0]];
}
function rotate2matrix(theta) {
  const c = Math.cos(theta), s = Math.sin(theta);
  return [[c,-s,0],[s,c,0]];
}
function decomposeTransformMatrix(T) {
  const [a,c,e] = T[0];
  const [b,d,f] = T[1];
  const scalingX = Math.hypot(a,c);
  const scalingY = Math.hypot(b,d);
  const rotation = Math.atan2(b,a);
  return { scaling: {x: scalingX, y: scalingY}, rotation, translation: {x:e, y:f} };
}

// Coords conversions
function viewportDeltaToReal(dx_css, dy_css) {
  // CSS px delta -> real frame units
  const factor = real_frame_size / frame_size;
  return [dx_css * factor, dy_css * factor];
}
function viewportPointToReal(x_css, y_css) {
  // CSS px point -> real frame units, relative to frame center
  const cx = f_left + frame_size/2;
  const cy = f_top + frame_size/2;
  const [dx, dy] = [x_css - cx, y_css - cy];
  const factor = real_frame_size / frame_size;
  return [dx * factor, dy * factor];
}

// Sizing + initial fit
function recalcLayout() {
  W = screen_size_source.clientWidth;
  H = screen_size_source.clientHeight;
  Vmin = Math.min(W,H);
  frame_size = Vmin; // full vmin
  f_left = (W - frame_size)/2;
  f_top = (H - frame_size)/2;

  // Resize canvases
  viewer.width = Math.max(1, Math.round(W * DPR));
  viewer.height = Math.max(1, Math.round(H * DPR));
  viewer.style.width = W + "px";
  viewer.style.height = H + "px";

  const squarePx = Math.max(1, Math.round(frame_size * DPR));
  offscreen.width = squarePx;
  offscreen.height = squarePx;

  // Hide CSS overlayed frame; we render it ourselves.
  if (overlayEl) overlayEl.style.backgroundImage = "none";

  // Initial photo fit (if not set yet)
  recalcPhoto();
  draw();
}

function recalcPhoto() {
  if (!photoEl.naturalWidth || !photoEl.naturalHeight) return;
  if (pw !== photoEl.naturalWidth || ph !== photoEl.naturalHeight) {
    pw = photoEl.naturalWidth;
    ph = photoEl.naturalHeight;
    const smaller = Math.min(pw, ph);
    const F = real_frame_size / smaller; // scale to cover frame at export size
    transformationMatrix = [[F,0,0],[0,F,0]];
  }
}

// Unified renderer used for both preview and export
function renderToSquareCanvas(ctx, targetPx) {
  // targetPx: canvas width/height in pixels (already set on ctx.canvas)
  ctx.clearRect(0,0,targetPx,targetPx);

  // Compose photo under frame
  ctx.save();
  // Center of frame
  ctx.translate(targetPx/2, targetPx/2);

  // Map real-frame units to target pixels
  const S = targetPx / real_frame_size;
  ctx.scale(S, S);

  // Apply user transform + optional alignment
  const { scaling, rotation, translation } = decomposeTransformMatrix(transformationMatrix);
  ctx.translate(translation.x + alignment.x, translation.y + alignment.y);
  ctx.scale(scaling.x, scaling.y);
  ctx.rotate(rotation);

  // Draw the photo
  ctx.drawImage(photoEl, -pw/2, -ph/2, pw, ph);
  ctx.restore();

  // Frame on top (sized to the square)
  if (frameSourceEl && frameSourceEl.complete) {
    ctx.drawImage(frameSourceEl, 0, 0, targetPx, targetPx);
  }
  // Optional flare:
  // if (flareSourceEl && flareSourceEl.complete) {
  //   ctx.globalCompositeOperation = "screen";
  //   ctx.drawImage(flareSourceEl, 0, 0, targetPx, targetPx);
  //   ctx.globalCompositeOperation = "source-over";
  // }
}

function draw() {
  // Draw frame square into offscreen, then blit to viewer at correct place
  renderToSquareCanvas(offctx, offscreen.width);

  vctx.clearRect(0,0,viewer.width, viewer.height);
  // Account for device pixel ratio
  const dx = Math.round(f_left * DPR);
  const dy = Math.round(f_top * DPR);
  vctx.drawImage(offscreen, dx, dy);
}

// ----- Interaction (always update transform in REAL frame units) -----
let isMouseDown = false, lastMouseX = 0, lastMouseY = 0;

function movePhoto(dx_css, dy_css) {
  const [dx, dy] = viewportDeltaToReal(dx_css, dy_css);
  transformationMatrix = M(translate2matrix(dx, dy), transformationMatrix);
  draw();
}

function rotatePhoto(angle, pivotX_css, pivotY_css) {
  const [px, py] = viewportPointToReal(pivotX_css, pivotY_css);
  const R = MM(translate2matrix(px,py), rotate2matrix(angle), translate2matrix(-px,-py));
  transformationMatrix = M(R, transformationMatrix);
  draw();
}

function scalePhoto(scaleFactor, pivotX_css, pivotY_css) {
  const [px, py] = viewportPointToReal(pivotX_css, pivotY_css);
  const S = MM(translate2matrix(px,py), scale2matrix(scaleFactor, scaleFactor), translate2matrix(-px,-py));
  transformationMatrix = M(S, transformationMatrix);
  draw();
}

// Mouse
function onMouseDown(e) {
  e.preventDefault();
  isMouseDown = true;
  lastMouseX = e.clientX; lastMouseY = e.clientY;
}
function onMouseMove(e) {
  if (!isMouseDown) return;
  e.preventDefault();
  if (e.shiftKey) {
    const angle = Math.atan2(e.clientY - lastMouseY, e.clientX - lastMouseX);
    rotatePhoto(angle, e.clientX, e.clientY);
  } else {
    movePhoto(e.clientX - lastMouseX, e.clientY - lastMouseY);
    lastMouseX = e.clientX; lastMouseY = e.clientY;
  }
}
function onMouseUp(e){ e.preventDefault(); isMouseDown=false; }

function onMouseWheel(e) {
  e.preventDefault();
  const step = e.shiftKey ? 0.005 : 0.1;
  const k = (e.deltaY < 0) ? (1 + step) : (1 - step);
  scalePhoto(k, e.clientX, e.clientY);
}

// Touch
let initialTouchDist = 0, initialTouchAngle = 0;

function getTouches(e) {
  const t = e.touches;
  return Array.from(t).map(ti => ({x: ti.clientX, y: ti.clientY}));
}
function onTouchStart(e) {
  e.preventDefault();
  if (e.touches.length === 1) {
    const t = e.touches[0];
    lastMouseX = t.clientX; lastMouseY = t.clientY;
  } else if (e.touches.length === 2) {
    const [t1, t2] = getTouches(e);
    initialTouchDist = Math.hypot(t2.x - t1.x, t2.y - t1.y);
    initialTouchAngle = Math.atan2(t2.y - t1.y, t2.x - t1.x);
  }
}
function onTouchMove(e) {
  e.preventDefault();
  if (e.touches.length === 1) {
    const t = e.touches[0];
    movePhoto(t.clientX - lastMouseX, t.clientY - lastMouseY);
    lastMouseX = t.clientX; lastMouseY = t.clientY;
  } else if (e.touches.length === 2) {
    const [t1, t2] = getTouches(e);
    const dist = Math.hypot(t2.x - t1.x, t2.y - t1.y);
    const angle = Math.atan2(t2.y - t1.y, t2.x - t1.x);

    const k = dist / Math.max(1e-6, initialTouchDist);
    const cx = (t1.x + t2.x)/2, cy = (t1.y + t2.y)/2;

    // scale around center, then rotate around same center
    scalePhoto(k, cx, cy);
    rotatePhoto(angle - initialTouchAngle, cx, cy);

    initialTouchDist = dist;
    initialTouchAngle = angle;
  }
}
function onTouchEnd(e){ e.preventDefault(); /* noop */ }

// ----- Realign flow -----
function startRealign() {
  document.body.classList.add("realign");
  // Allow CSS to show the marker overlay by clearing inline override:
  if (overlayEl) overlayEl.style.backgroundImage = "";
}
function stopRealign() {
  const d = decomposeTransformMatrix(transformationMatrix);
  alignment.x = d.translation.x;
  alignment.y = d.translation.y;
  alignment.changed = true;
  alignment.scale = d.scaling;
  document.body.classList.remove("realign");
  if (overlayEl) overlayEl.style.backgroundImage = "none";
}

// ----- Export (same renderer => WYSIWYG) -----
function generateCroppedImage(returnCanvas) {
  const out = document.createElement("canvas");
  out.width = real_frame_size;
  out.height = real_frame_size;
  const ctx = out.getContext("2d");
  renderToSquareCanvas(ctx, out.width);
  return returnCanvas ? out : out.toDataURL("image/png", 0.95);
}

// ----- Debug toggles (kept mostly as-is) -----
(function debugInit(){
  window.DEBUG = false;
  let debug_countdown = 10;
  function remove_from_dom(el){ el?.parentElement?.removeChild(el); }

  function init_debug(){
    if (DEBUG) return;
    DEBUG = true;
    document.body.classList.add("debug");
    const dbg = document.querySelector(".debug-layer");
    dbg.querySelector("button.canvas").addEventListener("click", () => {
      const img = new Image();
      img.src = generateCroppedImage(false);
      img.onload = () => {
        remove_from_dom(dbg.querySelector("img.debug-export"));
        img.className = "debug-export";
        img.style.position = "fixed";
        img.style.top = (f_top + frame_size + 10) + "px";
        img.style.left = (f_left) + "px";
        img.style.width = frame_size + "px";
        document.body.appendChild(img);
      };
    });
    dbg.querySelector("button.realign").addEventListener("click", () => {
      if (document.body.classList.contains("realign")) stopRealign();
      else startRealign();
    });
  }

  document.addEventListener("contextmenu", (e) => {
    if (e.altKey && !DEBUG) {
      if (--debug_countdown <= 0) init_debug();
      e.preventDefault();
    }
  }, {capture:true});
  document.body.addEventListener("touchstart", (e) => {
    if (!DEBUG && e.touches.length === 5) {
      if (--debug_countdown <= 0) init_debug();
    }
  }, {capture:true});
})();

// ----- Wire up -----
function IDQ(){ return "initData=" + encodeURIComponent(Telegram.WebApp.initData); }
function AlignmentSave(){
  if (!alignment.changed) return "";
  return `&x=${encodeURIComponent(alignment.x)}&y=${encodeURIComponent(alignment.y)}`;
}
function send_error(err){
  return fetch("error?" + IDQ(), { method:"POST", body: err });
}

Telegram.WebApp.MainButton.setText(finish_button_text);
Telegram.WebApp.MainButton.show();
Telegram.WebApp.MainButton.onClick(() => {
  try {
    if (document.body.classList.contains("realign")) { stopRealign(); return; }
    generateCroppedImage(true).toBlob(function(blob){
      fetch('fit_frame?' + IDQ() + AlignmentSave(), {
        method: 'PUT',
        headers: { 'Content-Type': 'image/jpeg' },
        body: blob
      }).then(resp => {
        if (!resp.ok) throw new Error(`Network response was not ok...: ${resp.status} ${resp.statusText}\n${resp.body}`);
      }).catch(err => {
        console.error('Error:', err);
        return send_error(err);
      }).finally(() => {
        Telegram.WebApp.close();
      });
    }, 'image/jpeg', quality / 100.);
  } catch (error) {
    send_error(error);
  };
});
Telegram.WebApp.MainButton.enable();
Telegram.WebApp.MainButton.show();

Telegram.WebApp.BackButton.onClick(() => Telegram.WebApp.close());
Telegram.WebApp.BackButton.show();

// Event listeners for interaction
viewer.addEventListener("mousedown", onMouseDown);
window.addEventListener("mousemove", onMouseMove);
window.addEventListener("mouseup", onMouseUp);
viewer.addEventListener("wheel", onMouseWheel, {passive:false});
viewer.addEventListener("touchstart", onTouchStart, {passive:false});
viewer.addEventListener("touchmove", onTouchMove, {passive:false});
viewer.addEventListener("touchend", onTouchEnd, {passive:false});

// Resize / orientation
window.addEventListener("resize", () => { recalcLayout(); });

// Wait for assets then init
function whenReady(){
  if (photoEl.complete && frameSourceEl.complete) {
    recalcLayout();
    draw();
    return;
  }
  setTimeout(whenReady, 50);
}
whenReady();
