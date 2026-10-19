"use strict";
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { createHash, randomUUID, webcrypto } = require("node:crypto");
const { runInNewContext } = require("node:vm");
const { test } = require("node:test");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/avatars.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218", other = "510e58dd-e547-48d4-b9fa-571d24953219";
const copy = value => JSON.parse(JSON.stringify(value));
const hash = value => createHash("sha256").update(value).digest("hex");
const originalBytes = "original", frameBytes = "frame";
const identity = () => ({ offset_x_ppm: 0, offset_y_ppm: 0, zoom_ppm: 1000000, angle_millidegrees: 0 });
const key = id => "zinusya-avatar-pending-v1:" + id;
function backend() {
  const api = { current: { id: randomUUID(), version: 1, source_sha256: hash(originalBytes), frame_sha256: hash(frameBytes), transform: identity() }, receipts: new Map(), finalizations: new Map(), documents: new Map(), finalizationWrites: [], writes: [], hook: undefined, reads: [] };
  api.request = async (path, method, body) => {
    if (api.hook) return api.hook(path, method, body, api.base);
    return api.base(path, method, body);
  };
  api.base = async (path, method, body) => {
    api.reads.push(path);
    if (path.startsWith("./avatars/finalizations/operations/")) { const r = api.finalizations.get(path.split("/").at(-1)); if (!r) throw { status: 404 }; return copy(r); }
    if (path === "./avatars/finalized") return { avatars: [...api.documents.values()].map(copy) };
    if (method === "POST" && path.endsWith("/finalize")) {
      api.finalizationWrites.push(copy(body));
      if (api.finalizations.has(body.operation_id)) return copy(api.finalizations.get(body.operation_id));
      if (body.expected_version !== api.current.version) throw { status: 409 };
      const receipt = { artifact_id: randomUUID(), delivery_id: randomUUID(), draft_id: api.current.id, draft_version: api.current.version, source_sha256: api.current.source_sha256, frame_sha256: api.current.frame_sha256, processor_version: "jpeg-v1", transform: copy(api.current.transform), sha256: hash("jpeg"), size_bytes: 4, width: 1024, height: 1024, created_at: "2026-10-19T12:00:00Z" };
      api.finalizations.set(body.operation_id, receipt); api.documents.set(receipt.artifact_id, { avatar: receipt, delivery_state: "pending", delivery_version: 1 }); return copy(receipt);
    }
    if (path.includes("/operations/")) { const r = api.receipts.get(path.split("/").at(-1)); if (!r) throw { status: 404 }; return copy(r); }
    if (method === "POST") {
      api.writes.push(copy(body));
      if (api.receipts.has(body.operation_id)) return copy(api.receipts.get(body.operation_id));
      if (body.expected_version !== api.current.version) throw { status: 409 };
      api.current = { ...api.current, version: api.current.version + 1, transform: copy(body.transform) };
      api.receipts.set(body.operation_id, copy(api.current)); return copy(api.current);
    }
    return path === "./avatars/drafts" ? { drafts: [copy(api.current)] } : copy(api.current);
  };
  api.uploads = [];
  api.upload = async (file, operationID) => {
    api.uploads.push(operationID);
    if (!api.receipts.has(operationID)) { api.current = { ...api.current, id: randomUUID(), version: 1, transform: identity() }; api.receipts.set(operationID, copy(api.current)); }
    return copy(api.receipts.get(operationID));
  };
  return api;
}
function browser(api, { storage = new Map(), failStorage = false, readBlob } = {}) {
  const elements = new Map(), revoked = [], images = [], downloads = []; let denied = false, controller;
  const context = { clearRect() {}, fillRect() {}, save() {}, translate() {}, rotate() {}, scale() {}, drawImage() {}, restore() {} };
  function get(id) {
    if (!elements.has(id)) elements.set(id, { value: "", textContent: "", files: [], hidden: false, disabled: false, listeners: {}, attrs: {}, children: [],
      setAttribute(k, v) { this.attrs[k] = v; }, removeAttribute(k) { delete this.attrs[k]; }, addEventListener(k, fn) { this.listeners[k] = fn; }, replaceChildren(...nodes) { this.children = nodes; },
      get options() { return this.children; }, getContext() { return context; }, getBoundingClientRect() { return { width: 512, height: 512 }; }, setPointerCapture() {} });
    return elements.get(id);
  }
  const env = { request: (...args) => api.request(...args), upload: (...args) => api.upload(...args), setBusy: v => controller.setBusy(v), denied() { denied = true; controller.clear(); },
    readBlob: readBlob || (async path => { api.reads.push(path); return new Blob([path.includes("/frame/") ? frameBytes : (path.includes("/preview/") || path.includes("/finalized/")) ? "jpeg" : originalBytes]); }) };
  const window = {};
  runInNewContext(source, { window, document: { getElementById: get, createElement: () => ({ value: "", textContent: "", click() { downloads.push({ href: this.href, download: this.download }); } }) }, crypto: { subtle: webcrypto.subtle, randomUUID },
    sessionStorage: { getItem: k => storage.get(k) || null, setItem(k, v) { if (failStorage) throw new Error("storage"); storage.set(k, v); }, removeItem(k) { storage.delete(k); } },
    URL: { createObjectURL: () => "blob:" + randomUUID(), revokeObjectURL: v => revoked.push(v) },
    async createImageBitmap() { const image = { width: 128, height: 128, closed: false, close() { this.closed = true; } }; images.push(image); return image; } });
  controller = window.createZinusyaAvatars(env);
  const fire = async (id, name = "click", args = {}) => { const e = get("avatars-" + id); assert.ok(e.listeners[name], id); await e.listeners[name]({ preventDefault() {}, ...args }); };
  return { controller, get: id => get(id === "avatars" ? id : "avatars-" + id), storage, revoked, images, downloads, get denied() { return denied; },
    start: (id = owner, locale = "en") => controller.start(id, locale), fire,
    async edit(id, value) { get("avatars-" + id).value = String(value); await fire(id, "input"); },
    async selectFile(bytes = originalBytes) { get("avatars-file").files = [new Blob([bytes], { type: "image/png" })]; await fire("file", "change"); },
    submit: () => fire("form", "submit") };
}

test("native crop saves exact units, previews saved state, and continues agent changes", async () => {
  const api = backend(), ui = browser(api); await ui.start();
  await ui.edit("zoom", 150); await ui.edit("angle", 30);
  await ui.fire("canvas", "pointerdown", { pointerId: 1, clientX: 0, clientY: 0 });
  await ui.fire("canvas", "pointermove", { pointerId: 1, clientX: 128, clientY: -64 });
  await ui.fire("canvas", "pointerup");
  assert.equal(ui.get("preview").disabled, true);
  api.hook = (path, method, body, base) => { if (method === "POST") assert.equal(JSON.parse(ui.storage.get(key(owner))).body.operation_id, body.operation_id); return base(path, method, body); };
  await ui.submit();
  assert.deepEqual(api.current.transform, { offset_x_ppm: 250000, offset_y_ppm: -125000, zoom_ppm: 1500000, angle_millidegrees: 30000 });
  assert.equal(api.current.version, 2); assert.equal(ui.storage.has(key(owner)), false);
  await ui.fire("preview"); assert.equal(ui.get("saved-preview").hidden, false);
  assert.ok(api.reads.some(path => path.endsWith("/preview/2")));
  api.current = { ...api.current, version: 3, transform: { ...identity(), zoom_ppm: 2000000 } };
  await ui.fire("reload"); assert.equal(ui.get("zoom").value, 200); assert.equal(ui.revoked.length, 1);
  ui.controller.setLanguage("ru"); assert.equal(ui.get("save").textContent, "Сохранить изменения"); assert.match(ui.get("draft").children[0].textContent, /Версия/);
  await ui.fire("reset"); await ui.submit(); assert.deepEqual(api.current.transform, identity());
});

test("agent edit rejects stale native save and keeps local values until explicit reload", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.edit("x", 42);
  api.current.version++; api.current.transform.zoom_ppm = 2500000;
  await ui.submit(); assert.match(ui.get("status").textContent, /draft changed/); assert.equal(ui.get("x").value, "42"); assert.equal(ui.get("save").disabled, true);
  assert.equal(api.current.transform.zoom_ppm, 2500000); await ui.fire("reload"); assert.equal(ui.get("zoom").value, 250);
});

test("lost save reply recovers original receipt then current agent state after reload", async () => {
  const api = backend(), storage = new Map(), ui = browser(api, { storage }); await ui.start(); await ui.edit("y", 12);
  api.hook = async (path, method, body, base) => { const result = await base(path, method, body); if (method === "POST") throw new TypeError("lost reply"); return result; };
  await ui.submit(); const pending = JSON.parse(storage.get(key(owner))); assert.equal(ui.get("x").disabled, true);
  assert.equal(pending.body.transform.offset_y_ppm, 120000); assert.equal(api.writes.length, 1);
  api.current.version++; api.current.transform.zoom_ppm = 3000000;
  const reopened = browser(api, { storage }); await reopened.start();
  assert.equal(storage.has(key(owner)), false); assert.equal(reopened.get("zoom").value, 300); assert.equal(api.writes.length, 1);
});

test("uncertain upload after reload requires exact original and retains operation ID", async () => {
  const api = backend(), storage = new Map(), ui = browser(api, { storage }); await ui.start(); await ui.selectFile();
  api.upload = async () => { throw new TypeError("no response"); }; await ui.fire("upload");
  const pending = JSON.parse(storage.get(key(owner))); assert.deepEqual(Object.keys(pending).sort(), ["kind", "operation_id", "owner", "sha256", "size"]);
  const reopened = browser(api, { storage }); await reopened.start(); assert.match(reopened.get("status").textContent, /outcome is unknown/);
  let retried; api.upload = async (file, id) => { retried = id; api.receipts.set(id, copy(api.current)); return copy(api.current); };
  await reopened.selectFile("different"); await reopened.fire("retry"); assert.equal(retried, undefined); assert.match(reopened.get("status").textContent, /exact original/);
  await reopened.selectFile(); await reopened.fire("retry"); assert.equal(retried, pending.operation_id); assert.equal(storage.has(key(owner)), false);
});

test("committed upload with lost reply is recovered without uploading twice", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.selectFile();
  const upload = api.upload; api.upload = async (...args) => { await upload(...args); throw new TypeError("lost reply"); };
  await ui.fire("upload"); const reopened = browser(api, { storage: ui.storage }); await reopened.start();
  assert.equal(api.uploads.length, 1); assert.equal(reopened.get("recover").hidden, true);
});

test("storage failure and corrupt pending data stop new writes", async () => {
  const api = backend(), ui = browser(api, { failStorage: true }); await ui.start(); await ui.edit("x", 1); await ui.submit(); assert.equal(api.writes.length, 0); assert.match(ui.get("status").textContent, /storage/);
  const storage = new Map([[key(owner), '{"kind":"save"}']]), corrupt = browser(api, { storage }); await corrupt.start(); assert.equal(corrupt.get("upload").disabled, true); assert.match(corrupt.get("status").textContent, /invalid/);
  await corrupt.start(other); assert.equal(corrupt.get("x").disabled, false); assert.equal(storage.has(key(owner)), true);
});

test("session expiry clears image resources; stale reads cannot restore a signed-out editor", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.fire("preview");
  api.hook = () => { throw { status: 401 }; }; await ui.fire("reload");
  assert.equal(ui.denied, true); assert.equal(ui.get("avatars").hidden, true); assert.equal(ui.revoked.length, 1); assert.ok(ui.images.every(i => i.closed));
  let release; const held = new Promise(resolve => { release = resolve; });
  const delayed = browser(backend(), { readBlob: async () => held }); const opening = delayed.start();
  await new Promise(resolve => setImmediate(resolve)); delayed.controller.clear(); release(new Blob([originalBytes])); await opening;
  assert.equal(delayed.get("avatars").hidden, true); assert.equal(delayed.get("editor").hidden, true);
});

test("invalid transforms never create an operation or write", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.edit("zoom", 0); await ui.submit();
  assert.equal(api.writes.length, 0); assert.equal(ui.storage.size, 0); assert.match(ui.get("status").textContent, /Check/);
});

test("failed switch restores selector and preserves the actual active draft", async () => {
  const api = backend(), ui = browser(api); await ui.start(); const loaded = api.current.id, second = randomUUID();
  api.hook = (path, method, body, base) => { if (path.endsWith(second)) throw { status: 503 }; return base(path, method, body); };
  ui.get("draft").value = second; await ui.fire("draft", "change"); assert.equal(ui.get("draft").value, loaded);
  await ui.edit("x", 10); await ui.submit(); assert.equal(api.current.id, loaded); assert.equal(api.current.transform.offset_x_ppm, 100000);
});

test("late terminal rejection cannot erase another owner's pending request", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.edit("x", 1);
  let reject;
  api.hook = (path, method, body, base) => method === "POST" ? new Promise((resolve, fail) => { reject = fail; }) : base(path, method, body);
  const oldSave = ui.submit(); await new Promise(resolve => setImmediate(resolve)); assert.ok(reject);
  const replacement = { ...JSON.parse(ui.storage.get(key(owner))), owner: other, operation_id: randomUUID() }; replacement.body.operation_id = replacement.operation_id;
  ui.storage.set(key(other), JSON.stringify(replacement)); await ui.start(other);
  const status = ui.get("status").textContent; reject({ status: 409 }); await oldSave;
  assert.deepEqual(JSON.parse(ui.storage.get(key(other))), replacement); assert.equal(ui.get("status").textContent, status);
});


test("explicit generation requires saved preview, persists intent and exposes delivery/download separately", async () => {
  const api = backend(), ui = browser(api); await ui.start();
  assert.equal(ui.get("generate").disabled, true); await ui.fire("generate"); assert.equal(api.finalizationWrites.length, 0);
  await ui.fire("preview"); assert.equal(ui.get("generate").disabled, false);
  api.hook = (path, method, body, base) => { if (path.endsWith("/finalize")) { const p = JSON.parse(ui.storage.get(key(owner))); assert.equal(p.kind, "finalize"); assert.equal(p.body.operation_id, body.operation_id); assert.deepEqual(p.body, copy(body)); } return base(path, method, body); };
  await ui.fire("generate"); assert.equal(api.finalizationWrites.length, 1); assert.equal(ui.storage.has(key(owner)), false);
  assert.equal(ui.get("generate").disabled, true); assert.match(ui.get("document-status").textContent, /waiting/); assert.equal(ui.get("download").disabled, false);
  await ui.fire("download"); assert.equal(ui.downloads.length, 1); assert.equal(ui.downloads[0].download, "zinusya-avatar.jpg");
  const document = [...api.documents.values()][0]; document.delivery_state = "delivered"; document.delivery_version++;
  await ui.fire("refresh-documents"); assert.match(ui.get("document-status").textContent, /^Sent/); assert.equal(api.finalizationWrites.length, 1);
  ui.controller.setLanguage("ru"); assert.equal(ui.get("generate").textContent, "Сформировать и отправить"); assert.match(ui.get("document-status").textContent, /Отправлено/);
  ui.controller.clear(); assert.equal(ui.get("download").disabled, true); assert.ok(ui.revoked.length >= 2);
});

test("lost finalized reply recovers same receipt after reload and later agent edit", async () => {
  const api = backend(), storage = new Map(), ui = browser(api, { storage }); await ui.start(); await ui.fire("preview");
  api.hook = async (path, method, body, base) => { const result = await base(path, method, body); if (path.endsWith("/finalize")) throw new TypeError("lost response"); return result; };
  await ui.fire("generate"); const pending = JSON.parse(storage.get(key(owner))); assert.equal(pending.kind, "finalize");
  assert.deepEqual(Object.keys(pending.body).sort(), ["expected_version", "operation_id"]);
  api.current = { ...api.current, version: 2, transform: { ...identity(), zoom_ppm: 2000000 } };
  const reopened = browser(api, { storage }); await reopened.start();
  assert.equal(storage.has(key(owner)), false); assert.equal(api.finalizationWrites.length, 1); assert.equal(reopened.get("zoom").value, 200);
  assert.equal(reopened.get("documents").children.length, 1); assert.match(reopened.get("documents").children[0].textContent, /Version 1/);
  assert.equal(reopened.get("generate").disabled, true); await reopened.fire("preview"); assert.equal(reopened.get("generate").disabled, false);
});

test("uncommitted lost request retries the exact finalization after reload without new preview or ID", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.fire("preview");
  api.hook = (path, method, body, base) => { if (path.endsWith("/finalize")) throw new TypeError("request interrupted"); return base(path, method, body); };
  await ui.fire("generate"); const pending = JSON.parse(ui.storage.get(key(owner)));
  const reopened = browser(api, { storage: ui.storage }); await reopened.start(); assert.match(reopened.get("status").textContent, /outcome is unknown/);
  api.hook = undefined; await reopened.fire("retry"); assert.equal(api.finalizationWrites.length, 1); assert.equal(api.finalizationWrites[0].operation_id, pending.operation_id); assert.equal(ui.storage.has(key(owner)), false);
});

test("server document history recovers downloads without browser state and never retries unknown delivery", async () => {
  const api = backend(), first = browser(api); await first.start(); await first.fire("preview"); await first.fire("generate");
  [...api.documents.values()][0].delivery_state = "delivery_unknown";
  const reopened = browser(api); await reopened.start(); assert.match(reopened.get("document-status").textContent, /uncertain/);
  await reopened.fire("preview"); assert.equal(reopened.get("generate").disabled, true); await reopened.fire("generate"); assert.equal(api.finalizationWrites.length, 1);
  await reopened.fire("download"); assert.equal(reopened.downloads.length, 1); assert.match(reopened.get("document-status").textContent, /uncertain/);
});

test("late finalization rejection cannot clear a new owner's recovery operation", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.fire("preview"); let reject;
  api.hook = (path, method, body, base) => path.endsWith("/finalize") ? new Promise((resolve, fail) => { reject = fail; }) : base(path, method, body);
  const old = ui.fire("generate"); while (!reject) await new Promise(resolve => setImmediate(resolve));
  const replacement = { ...JSON.parse(ui.storage.get(key(owner))), owner: other, operation_id: randomUUID() }; replacement.body.operation_id = replacement.operation_id;
  ui.storage.set(key(other), JSON.stringify(replacement)); await ui.start(other); const status = ui.get("status").textContent;
  reject({ status: 409 }); await old; assert.deepEqual(JSON.parse(ui.storage.get(key(other))), replacement); assert.equal(ui.get("status").textContent, status);
});

test("terminal generation rejection and recovery-storage failure do not send another request", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.fire("preview");
  api.hook = (path, method, body, base) => { if (path.endsWith("/finalize")) throw { status: 409 }; return base(path, method, body); };
  await ui.fire("generate"); assert.equal(ui.storage.has(key(owner)), false); assert.equal(ui.get("generate").disabled, true); assert.match(ui.get("status").textContent, /twenty-document limit/);
  const denied = browser(backend(), { failStorage: true }); await denied.start(); await denied.fire("preview"); await denied.fire("generate"); assert.match(denied.get("status").textContent, /storage/); assert.equal(denied.get("generate").disabled, false);
});

test("corrupt document download is refused and late document list cannot restore a signed-out owner", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.fire("preview"); await ui.fire("generate");
  [...api.documents.values()][0].avatar.sha256 = hash("different"); await ui.fire("refresh-documents"); await ui.fire("download"); assert.equal(ui.downloads.length, 0); assert.match(ui.get("status").textContent, /Check/);
  let release; api.hook = (path, method, body, base) => path === "./avatars/finalized" ? new Promise(resolve => { release = resolve; }) : base(path, method, body);
  const held = ui.fire("refresh-documents"); await new Promise(resolve => setImmediate(resolve)); ui.controller.clear(); release({ avatars: [...api.documents.values()].map(copy) }); await held;
  assert.equal(ui.get("avatars").hidden, true); assert.equal(ui.get("documents").children.length, 0); assert.equal(ui.get("download").disabled, true);
});
