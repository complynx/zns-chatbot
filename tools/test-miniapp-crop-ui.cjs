"use strict";

const assert = require("node:assert/strict");
const { webcrypto, createHash, randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/crop.js"), "utf8");
const owner = randomUUID(), draftID = randomUUID(), photo = new Blob(["photo"]), frame = new Blob(["frame"]);
const hash = x => createHash("sha256").update(x).digest("hex");
const identity = () => ({ offset_x_ppm: 0, offset_y_ppm: 0, zoom_ppm: 1000000, angle_millidegrees: 0 });
const copy = x => JSON.parse(JSON.stringify(x));

function browser({ storage = new Map(), hook, width = 400, height = 800, transform = identity(), denied = false, delayed } = {}) {
  const elements = new Map(), calls = [], draws = [], listeners = {}, button = {};
  let viewWidth = width, viewHeight = height;
  let state = { id: draftID, version: 1, transform, source_sha256: hash("photo"), frame_sha256: hash("frame") };
  let active, mainEnabled = false, closed = 0, controller, canvasResized;
  const ctx = { clearRect() {}, beginPath() {}, rect() {}, clip() {}, fillRect() {}, save() {}, restore() {}, translate() {},
    transform(...args) { draws.push({ kind: "matrix", args }); }, drawImage(image, ...args) { draws.push({ kind: image.kind, args }); } };
  function element(id) {
    return { id, hidden: true, disabled: false, textContent: "", listeners: {}, width: 400, height: 800,
      getContext: () => ctx, getBoundingClientRect: () => ({ width: viewWidth, height: viewHeight }), setAttribute() {},
      addEventListener(name, fn) { this.listeners[name] = fn; } };
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element(id)); return elements.get(id); };
  const mainButton = { setText(value) { button.text = value; }, enable() { mainEnabled = true; }, disable() { mainEnabled = false; }, show() {}, hide() {}, onClick(fn) { button.click = fn; } };
  const window = { devicePixelRatio: 2, Telegram: { WebApp: { MainButton: mainButton, BackButton: { show() {}, hide() {}, onClick() {} }, expand() {}, close() { closed++; } } },
    addEventListener(name, fn) { listeners[name] = fn; } };
  const context = { window, document: { getElementById: get, body: { classList: { add() { active = true; }, remove() { active = false; } } } },
    sessionStorage: { getItem: key => storage.get(key) || null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) },
    createImageBitmap: async (blob, options) => { assert.ok(blob === photo || blob === frame); if (blob === photo) assert.equal(options.imageOrientation, "from-image"); return { kind: blob === photo ? "photo" : "frame", width: blob === photo ? 1200 : 1024, height: blob === photo ? 800 : 1024, close() {} }; },
    ResizeObserver: class { constructor(callback) { canvasResized = callback; } observe() {} },
    crypto: webcrypto, Uint8Array, console };
  runInNewContext(source, context);
  const base = async (path, method = "GET", body) => {
    if (path.endsWith("/open")) return { draft_id: draftID, opened_at: new Date().toISOString() };
    if (path.includes("/operations/")) throw { status: 404 };
    if (method === "POST" && !path.endsWith("/finalize")) { state = { ...state, version: state.version + 1, transform: copy(body.transform) }; return copy(state); }
    if (method === "POST") return { artifact_id: randomUUID(), delivery_id: randomUUID(), draft_id: draftID, draft_version: state.version };
    if (path === "./avatars/drafts/" + draftID) return copy(state);
    if (path === "./avatars/finalized") return { avatars: [] };
    throw new Error("Unexpected request " + path);
  };
  controller = window.createZinusyaAvatarCrop({ draftID,
    setBusy(value) { controller.setBusy(value); }, denied() { controller.clear(); },
    async request(path, method = "GET", body) { calls.push({ path, method, body: body && copy(body) }); if (denied) throw { status: 403 }; if (delayed) await delayed; return hook ? hook(path, method, body, base) : base(path, method, body); },
    async readBlob(path) { calls.push({ path, method: "BLOB" }); if (path.endsWith("/original")) return photo; if (path.includes("/frame/")) return frame; if (path.includes("/preview/")) return new Blob(["preview"]); throw new Error(path); }
  });
  const event = (target, type, props = {}) => (target === "window" ? listeners : get(target).listeners)[type]({ preventDefault() {}, ...props });
  return { calls, draws, storage, get, controller, button, enabled: () => mainEnabled, active: () => active, closed: () => closed,
    start: () => controller.start(owner, "en"), event,
    resize(w, h) { viewWidth = w; viewHeight = h; canvasResized(); },
    finish: () => button.click(), posts: () => calls.filter(c => c.method === "POST" && !c.path.endsWith("/open")) };
}

test("crop opens exact authenticated source, records the actual open and preserves centered EXIF-oriented cover fit", async () => {
  const ui = browser(); await ui.start();
  assert.equal(ui.posts().length, 0); assert.equal(ui.enabled(), true);
  assert.equal(ui.calls[0].path, "./avatars/drafts/" + draftID);
  assert.equal(ui.calls.filter(c => c.path.endsWith("/open")).length, 1);
  assert.ok(ui.calls.findIndex(c => c.path.endsWith("/open")) > ui.calls.findIndex(c => c.path.includes("/frame/")));
  assert.ok(ui.draws.some(d => d.kind === "photo" && d.args[0] === -600 && d.args[1] === -400));
  const matrix = ui.draws.find(d => d.kind === "matrix").args;
  assert.equal(matrix[0], 1); assert.equal(matrix[3], 1); // 800 device pixels / 800 oriented photo height.
});

test("uncertain crop-open audit retries the same passive operation and never generates", async () => {
  const storage = new Map(); let original;
  const first = browser({ storage, hook(path, method, body, base) {
    if (path.endsWith("/open")) { original = body.operation_id; throw new TypeError("lost open receipt"); }
    return base(path, method, body);
  } });
  await first.start(); await first.finish(); assert.equal(first.enabled(), false); assert.equal(first.posts().length, 0);
  const resumed = browser({ storage }); await resumed.start();
  assert.equal(resumed.calls.find(c => c.path.endsWith("/open")).body.operation_id, original);
  assert.equal(resumed.posts().length, 0); assert.equal(resumed.enabled(), true); assert.equal(storage.size, 0);
});

test("one-finger drag maps screen pixels to frame-relative translation, then uses saved preview before finalization", async () => {
  const ui = browser(); await ui.start();
  ui.event("crop-canvas", "touchstart", { touches: [{ clientX: 100, clientY: 400 }] });
  ui.event("crop-canvas", "touchmove", { touches: [{ clientX: 140, clientY: 380 }] });
  await ui.finish();
  const save = ui.posts()[0]; assert.deepEqual(save.body.transform, { ...identity(), offset_x_ppm: 100000, offset_y_ppm: -50000 });
  assert.equal(save.body.expected_version, 1);
  assert.equal(ui.posts()[1].body.expected_version, 2);
  const preview = ui.calls.findIndex(c => c.path.endsWith("/preview/2"));
  const finalize = ui.calls.findIndex(c => c.path.endsWith("/finalize"));
  assert.ok(preview > 0 && preview < finalize);
  assert.equal(ui.storage.size, 0); assert.match(ui.get("crop-status").textContent, /queued/);
});

test("two-finger pinch and quarter-turn retain the gesture center and persist the corresponding crop", async () => {
  const ui = browser(); await ui.start();
  ui.event("crop-canvas", "touchstart", { touches: [{ clientX: 150, clientY: 400 }, { clientX: 250, clientY: 400 }] });
  ui.event("crop-canvas", "touchmove", { touches: [{ clientX: 200, clientY: 300 }, { clientX: 200, clientY: 500 }] });
  await ui.finish();
  assert.deepEqual(ui.posts()[0].body.transform, { ...identity(), zoom_ppm: 2000000, angle_millidegrees: 90000 });
});

test("wheel zoom pivots around cursor, with Shift providing fine zoom", async () => {
  for (const [fine, expectedZoom, expectedOffset] of [[false, 1100000, -25000], [true, 1005000, -1250]]) {
    const ui = browser(); await ui.start();
    ui.event("crop-canvas", "wheel", { clientX: 300, clientY: 400, deltaY: -1, shiftKey: fine });
    await ui.finish();
    assert.deepEqual(ui.posts()[0].body.transform, { ...identity(), zoom_ppm: expectedZoom, offset_x_ppm: expectedOffset });
  }
});

test("saved rotated crop survives reload and resize without changing model-space geometry", async () => {
  const transform = { ...identity(), offset_x_ppm: 125000, offset_y_ppm: -100000, zoom_ppm: 1500000, angle_millidegrees: -45000 };
  const ui = browser({ transform }); await ui.start(); ui.resize(390, 730); await ui.finish();
  assert.equal(ui.posts().length, 1); assert.ok(ui.posts()[0].path.endsWith("/finalize"));
});

test("portrait stable-viewport resize uses the measured canvas for subsequent drag geometry", async () => {
  const ui = browser(); await ui.start(); ui.resize(390, 730);
  ui.event("crop-canvas", "touchstart", { touches: [{ clientX: 100, clientY: 365 }] });
  ui.event("crop-canvas", "touchmove", { touches: [{ clientX: 139, clientY: 326 }] });
  await ui.finish();
  assert.deepEqual(ui.posts()[0].body.transform, { ...identity(), offset_x_ppm: 100000, offset_y_ppm: -100000 });
});

test("Shift mouse drag rotates around frame center without changing cover scale", async () => {
  const ui = browser(); await ui.start();
  ui.event("crop-canvas", "mousedown", { clientX: 300, clientY: 400 });
  ui.event("window", "mousemove", { clientX: 200, clientY: 500, shiftKey: true });
  await ui.finish();
  assert.deepEqual(ui.posts()[0].body.transform, { ...identity(), angle_millidegrees: 90000 });
});

test("automatic original document is not generated again until the crop changes", async () => {
  const ui = browser({ hook(path, method, body, base) {
    if (path === "./avatars/finalized") return { avatars: [{ avatar: { draft_id: draftID, draft_version: 1 } }] };
    return base(path, method, body);
  } });
  await ui.start(); await ui.finish(); assert.equal(ui.posts().length, 0); assert.equal(ui.enabled(), false);
  ui.event("crop-canvas", "mousedown", { clientX: 100, clientY: 400 });
  ui.event("window", "mousemove", { clientX: 140, clientY: 400 });
  assert.equal(ui.enabled(), true); await ui.finish(); assert.equal(ui.posts().length, 2);
});

test("a lost finalization response retains its original operation and recovers without another send", async () => {
  const storage = new Map(); let receipt, originalOperation;
  const first = browser({ storage, async hook(path, method, body, base) {
    if (path.endsWith("/finalize")) { originalOperation = body.operation_id; receipt = await base(path, method, body); throw new TypeError("lost response"); }
    return base(path, method, body);
  } });
  await first.start(); await first.finish();
  assert.equal(JSON.parse(storage.get("zinusya-avatar-pending-v1:" + owner)).operation_id, originalOperation);
  const reopened = browser({ storage, hook(path, method, body, base) { if (path.endsWith("/operations/" + originalOperation)) return receipt; return base(path, method, body); } });
  await reopened.start(); assert.equal(reopened.posts().length, 0); assert.equal(storage.size, 0); assert.equal(reopened.enabled(), false);
});

test("uncertain save is recovered on reload without automatically generating a document", async () => {
  const storage = new Map(), operation = randomUUID();
  const pending = { owner, kind: "save", operation_id: operation, draft_id: draftID, body: { operation_id: operation, expected_version: 1, transform: identity() } };
  storage.set("zinusya-avatar-pending-v1:" + owner, JSON.stringify(pending));
  const ui = browser({ storage, hook(path, method, body, base) { if (path.endsWith("/operations/" + operation)) return { id: draftID, version: 2, transform: identity(), source_sha256: hash("photo"), frame_sha256: hash("frame") }; return base(path, method, body); } });
  await ui.start(); assert.equal(ui.posts().length, 0); assert.equal(ui.storage.size, 0); assert.equal(ui.enabled(), true);
});

test("unknown earlier request retains its exact body and blocks a replacement", async () => {
  const storage = new Map(), operation = randomUUID();
  const pending = { owner, kind: "finalize", operation_id: operation, draft_id: draftID, body: { operation_id: operation, expected_version: 1 } };
  const raw = JSON.stringify(pending); storage.set("zinusya-avatar-pending-v1:" + owner, raw);
  const ui = browser({ storage }); await ui.start(); await ui.finish();
  assert.equal(ui.posts().length, 0); assert.equal(storage.get("zinusya-avatar-pending-v1:" + owner), raw); assert.equal(ui.enabled(), false);
});

test("authorization denial or stale owner leaves no visible crop or mutation", async () => {
  const denied = browser({ denied: true }); await denied.start(); assert.equal(denied.active(), false); assert.equal(denied.posts().length, 0);
  let release; const delayed = new Promise(resolve => { release = resolve; });
  const stale = browser({ delayed }); const start = stale.start(); stale.controller.clear(); release(); await start;
  assert.equal(stale.active(), false); assert.equal(stale.posts().length, 0);
});
