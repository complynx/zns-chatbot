"use strict";
const assert = require("node:assert/strict");
const { randomUUID, webcrypto } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const appSource = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/app.js"), "utf8");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/massage.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218", other = "510e58dd-e547-48d4-b9fa-571d24953219", eventID = "8cc5e2de-729c-4604-a85b-f97cdd184795";
const clone = x => JSON.parse(JSON.stringify(x));
const key = "zinusya-massage-operation-v1:" + owner;
function backend() {
  const api = { bookings: [], receipts: new Map(), preparations: new Map(), hook: undefined, disabled: false };
  const name = { en: "Service <img>", ru: "Услуга <img>" };
  api.schedule = { event_id: eventID, event_version: 1, version: 1, timezone: "Europe/Berlin", document: { currency: "BYN", currency_scale: 2, services: [{ id: "short", name, blocks: 1, price_minor: 1234 }], windows: [{ id: "night", name, starts_at: "2026-10-25T00:00:00Z", ends_at: "2026-10-27T00:00:00Z" }], specialists: [{ id: "alex", name, service_ids: ["short"], work_spans: [{ window_id: "night" }] }] } };
  api.snapshot = args => ({ event_name: "Event <script>", terms: { ...args.selection, window_name: name, specialist_name: name, service_name: name, timezone: "Europe/Berlin", price_minor: 1234, currency: "BYN", currency_scale: 2,
    treatment_ends_at: new Date(Date.parse(args.selection.starts_at) + 1200000).toISOString(), occupancy_ends_at: new Date(Date.parse(args.selection.starts_at) + 1500000).toISOString() } });
  api.base = (path, method = "GET", body) => {
    if (path.startsWith("./massage/events")) return { events: [{ id: eventID, name: "Event <script>", timezone: "Europe/Berlin" }] };
    if (path.endsWith("/schedule")) { if (api.disabled) throw { status: 404 }; return api.schedule; }
    if (path.includes("/bookings")) return { bookings: api.bookings };
    if (path.includes("/pending")) {
      const all = Array.from(new Map(Array.from(api.preparations.values()).map(p => [p.proposal_id, p])).values())
        .filter(p => Date.parse(p.expires_at) > Date.now() && !Array.from(api.receipts.values()).some(r => r.proposal_id === p.proposal_id))
        .sort((a, b) => a.proposal_id.localeCompare(b.proposal_id));
      const after = new URL(path, "https://test.invalid").searchParams.get("after");
      const remaining = all.filter(p => !after || p.proposal_id > after), slice = remaining.slice(0, api.pageSize || 20);
      return { proposals: slice.map(p => ({ proposal_id: p.proposal_id, event_id: eventID, kind: p.booking ? "cancel" : "book", source: "agent", expires_at: p.expires_at, ...(p.booking ? { cancellation: p.arguments } : { booking: p.arguments }) })), ...(remaining.length > slice.length ? { next_after: slice.at(-1).proposal_id } : {}) };
    }
    if (path.endsWith("/availability")) return { ...api.schedule, window_id: "night", specialist_id: "alex", service_id: "short", price_minor: 1234, currency: "BYN", currency_scale: 2, candidates: ["2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z"].map(starts_at => ({ starts_at, treatment_ends_at: new Date(Date.parse(starts_at) + 1200000).toISOString(), available: true })) };
    if (path.includes("/operations/")) { const receipt = api.receipts.get(path.split("/").at(-1)); if (!receipt) throw { status: 404 }; return receipt; }
    const kind = path.includes("/cancel") ? "cancel" : "book";
    if (path.endsWith("/prepare")) {
      if (!api.preparations.has(body.operation_id)) {
        if (body.proposal_id) {
          const existing = Array.from(api.preparations.values()).find(p => p.proposal_id === body.proposal_id);
          if (!existing || Date.parse(existing.expires_at) <= Date.now() || Array.from(api.receipts.values()).some(r => r.proposal_id === body.proposal_id)) throw { status: 409 };
          assert.deepEqual(body.arguments, existing.arguments); api.preparations.set(body.operation_id, existing);
        } else api.preparations.set(body.operation_id, { proposal_id: randomUUID(), arguments: body.arguments, expires_at: new Date(Date.now() + 300000).toISOString(), ...(kind === "book" ? { snapshot: api.snapshot(body.arguments) } : { booking: api.bookings.find(b => b.id === body.arguments.booking_id) }) });
      }
      return api.preparations.get(body.operation_id);
    }
    if (method === "POST" && /\/(book|cancel)$/.test(path)) {
      if (api.receipts.has(body.operation_id)) return api.receipts.get(body.operation_id).result;
      const canonical = Array.from(api.receipts.values()).find(r => r.proposal_id === body.proposal_id);
      if (canonical) { api.receipts.set(body.operation_id, clone({ ...canonical, operation_id: body.operation_id })); return canonical.result; }
      const proposal = api.preparations.get(body.operation_id);
      assert.equal(body.proposal_id, proposal.proposal_id); assert.equal(body.confirmed, true); assert.deepEqual(body.arguments, proposal.arguments);
      let booking;
      if (kind === "book") { booking = { id: randomUUID(), version: 1, state: "booked", snapshot: proposal.snapshot }; api.bookings.push(booking); }
      else { booking = api.bookings.find(b => b.id === body.arguments.booking_id); booking.state = "cancelled"; booking.version++; }
      api.receipts.set(body.operation_id, clone({ operation_id: body.operation_id, proposal_id: proposal.proposal_id, event_id: eventID, kind, result: booking })); return booking;
    }
    throw new Error("Unexpected request " + path);
  };
  api.request = async (path, method, body) => { const wire = body && clone(body); return clone(await (api.hook ? api.hook(path, method, wire, api.base) : api.base(path, method, wire))); };
  return api;
}
function browser(api, storage = new Map(), { app = false, hash = "" } = {}) {
  const elements = new Map(), calls = [];
  function element(tag = "div") {
    let text = "", disabled = false, value = "";
    const e = { tag, children: [], hidden: false, disabled: false, value: "", listeners: {}, dataset: {}, append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { this.children = nodes; }, addEventListener(name, fn) { this.listeners[name] = fn; } };
    Object.defineProperties(e, { value: { get: () => value, set: v => { value = tag === "select" && !e.children.some(option => option.value === String(v)) ? "" : String(v); } }, options: { get: () => e.children.length ? e.children : [{ textContent: "" }] }, disabled: { get: () => disabled, set: value => { disabled = Boolean(value); } }, textContent: { get: () => text, set: value => { text = String(value); e.children = []; } }, innerHTML: { set() { throw new Error("Unsafe HTML"); } } });
    return e;
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element(["massage-event", "massage-window", "massage-specialist", "massage-service", "profile-language"].includes(id) ? "select" : "div")); return elements.get(id); };
  let controller;
  const b = { get, calls, storage, failSet: false, now: Date.now(), scrolled: 0 };
  get("massage").scrollIntoView = () => { assert.equal(get("massage").hidden, false); b.scrolled++; };
  class Clock extends Date { static now() { return b.now; } }
  const context = { window: { location: { hash }, Telegram: { WebApp: { initData: "non-authorizing-fixture", ready() {} } } }, navigator: { language: "en" }, document: { getElementById: get, createElement: element, documentElement: {} }, crypto: webcrypto, Intl, Date: Clock, console, TextEncoder, Uint8Array, btoa,
    sessionStorage: { getItem: k => storage.get(k) ?? null, setItem: (k, v) => { if (b.failSet) throw new Error("Storage unavailable"); storage.set(k, v); }, removeItem: k => storage.delete(k) } };
  runInNewContext(source, context);
  controller = context.window.createZinusyaMassage({ request: async (path, method, body) => { calls.push({ path, method, body: body && clone(body), storedBefore: storage.get(key) }); return api.request(path, method, body); }, setBusy: value => controller.setBusy(value), denied: () => controller.clear() });
  if (app) {
    const create = context.window.createZinusyaMassage;
    context.window.createZinusyaMassage = env => { controller = create(env); return controller; };
    context.fetch = async (path, options) => {
      try {
        let result;
        if (path.startsWith("./massage/")) {
          const body = options.body && JSON.parse(options.body);
          calls.push({ path, method: options.method, body, authorization: options.headers.Authorization, storedBefore: storage.get(key) });
          result = await api.request(path, options.method, body);
        } else if (path === "./session/bootstrap" || path === "./me") result = { user_id: owner, session_token: "a".repeat(43), full_name: "Owner", bot_language: "en" };
        else if (path === "./profile") result = { version: 0, full_name: "Owner", bot_language: "en" };
        else if (path === "./account") result = { events: [], has_more: false, observed_at: "2026-10-17T13:00:00Z" };
        else if (path === "./registration-details") return { ok: false, status: 404 };
        else if (path === "./session/logout") return { ok: true, status: 204 };
        else throw new Error("Unexpected app request " + path);
        return { ok: true, status: 200, json: async () => result };
      } catch (error) { return { ok: false, status: error.status || 503 }; }
    };
    runInNewContext(appSource, context);
  }
  b.controller = controller;
  b.start = (id = owner, language = "en") => controller.start(id, language);
  b.click = async (id, event = "click") => { const n = typeof id === "string" ? get(id) : id; assert.equal(n.disabled, false, "Action disabled"); await n.listeners[event]({ preventDefault() {} }); };
  b.select = async () => { get("massage-event").value = eventID; await b.click("massage-event", "change"); };
  b.buttons = id => { const walk = n => [ ...(n.tag === "button" ? [n] : []), ...n.children.flatMap(walk) ]; return walk(get(id)); };
  b.text = id => { const walk = n => [n.textContent, ...n.children.map(walk)].join("\n"); return walk(get(id)); };
  b.prepare = async () => b.click(b.buttons("massage-slots").find(n => !n.disabled));
  return b;
}
const applies = b => b.calls.filter(c => /\/(book|cancel)$/.test(c.path) && c.method === "POST");

test("native booking and cancellation show exact preview and require separate confirmations", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select();
  assert.match(b.text("massage-slots"), /GMT\+02:00/); assert.match(b.text("massage-slots"), /GMT\+01:00/);
  await b.prepare(); assert.equal(api.bookings.length, 0); assert.match(b.text("massage-preview"), /12.34 BYN/); assert.match(b.text("massage-preview"), /Event <script>/); assert.match(b.text("massage-preview"), /Service <img>/);
  assert.equal(b.get("massage-event").disabled, true); await b.click("massage-confirm"); assert.equal(api.bookings.length, 1); assert.equal(storageRecord(b), undefined);
  await b.click(b.buttons("massage-history")[0]); assert.equal(api.bookings[0].state, "booked"); assert.match(b.text("massage-preview"), /Cancel this booking/);
  await b.click("massage-confirm"); assert.equal(api.bookings[0].state, "cancelled"); assert.equal(applies(b).length, 2);
  for (const call of applies(b)) assert.equal(JSON.parse(call.storedBefore).phase, "apply");
});
function storageRecord(b) { const raw = b.storage.get(key); return raw && JSON.parse(raw); }

test("reload recovers the original preparation without automatic apply", async () => {
  const api = backend(), first = browser(api); await first.start(); await first.select(); await first.prepare();
  const record = storageRecord(first), second = browser(api, first.storage); await second.start();
  assert.equal(applies(second).length, 0); assert.equal(storageRecord(second).body.operation_id, record.body.operation_id); assert.match(second.text("massage-preview"), /12.34 BYN/);
  await second.click("massage-confirm"); assert.equal(api.bookings.length, 1);
});

test("lost accepted reply is recovered by the original operation without another apply", async () => {
  const api = backend(), first = browser(api); let lose = true;
  api.hook = (path, method, body, base) => { const result = base(path, method, body); if (lose && /\/book$/.test(path)) { lose = false; throw { status: 503 }; } return result; };
  await first.start(); await first.select(); await first.prepare(); await first.click("massage-confirm"); assert.equal(api.bookings.length, 1); assert.equal(storageRecord(first).phase, "apply");
  const second = browser(api, first.storage); await second.start(); assert.equal(applies(second).length, 0); assert.equal(storageRecord(second), undefined); assert.match(second.text("massage-history"), /Booked/);
});

test("unknown 404 preserves exact request and permits only its retry", async () => {
  const api = backend(), b = browser(api); let reject = true;
  api.hook = (path, method, body, base) => { if (reject && /\/book$/.test(path)) throw { status: 503 }; return base(path, method, body); };
  await b.start(); await b.select(); await b.prepare(); await b.click("massage-confirm"); const original = storageRecord(b);
  await b.click("massage-recover"); assert.deepEqual(storageRecord(b), original); assert.equal(b.get("massage-discard").hidden, true); assert.equal(b.get("massage-event").disabled, true);
  reject = false; await b.click("massage-retry"); assert.equal(api.bookings.length, 1); assert.deepEqual(applies(b)[0].body, applies(b)[1].body);
});

test("unwritable or invalid recovery storage blocks new writes", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); b.failSet = true; await b.prepare();
  assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 0); assert.match(b.text("massage-status"), /Nothing new was sent/);
  const malformed = browser(api, new Map([[key, "{bad"]])); await malformed.start(); assert.equal(malformed.calls.length, 0); assert.equal(malformed.get("massage-event").disabled, true);
});

test("pass/module loss preserves history and cancellation while schedule is denied", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.prepare(); await b.click("massage-confirm"); api.disabled = true;
  await b.click("massage-reload"); assert.match(b.text("massage-status"), /Time selection is unavailable/); assert.equal(b.buttons("massage-history").length, 1);
  await b.click(b.buttons("massage-history")[0]); await b.click("massage-confirm"); assert.equal(api.bookings[0].state, "cancelled");
});

test("logout ignores an outstanding preparation reply and another owner cannot restore it", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); let release;
  api.hook = async (path, method, body, base) => { if (path.endsWith("/prepare")) await new Promise(resolve => { release = resolve; }); return base(path, method, body); };
  const pending = b.prepare(); assert.ok(release); b.controller.clear(); release(); await pending;
  assert.equal(b.get("massage").hidden, true); assert.equal(b.text("massage-preview"), "");
  const second = browser(api, b.storage); await second.start(other); assert.equal(second.calls.some(c => c.path.endsWith("/prepare")), false); assert.equal(api.bookings.length, 0);
});

test("Russian labels and later absolute-day range use the same backend selection", async () => {
  const api = backend(), b = browser(api); await b.start(owner, "ru"); await b.select(); assert.equal(b.get("massage-title").textContent, "Массаж");
  await b.click("massage-range-next"); const query = b.calls.filter(c => c.path.endsWith("/availability")).at(-1).body.query;
  assert.equal(query.from, "2026-10-26T00:00:00.000Z"); assert.equal(query.until, "2026-10-27T00:00:00.000Z");
  assert.equal(Date.parse(query.until) - Date.parse(query.from), 86400000);
});


test("definitive 409 reconciles receipt and allows reselect after reload; 404 alone stays unknown", async () => {
  const api = backend(), first = browser(api); let reject = true;
  api.hook = (path, method, body, base) => { if (reject && /\/book$/.test(path)) throw { status: 409 }; return base(path, method, body); };
  await first.start(); await first.select(); await first.prepare(); await first.click("massage-confirm");
  const rejected = storageRecord(first); assert.equal(rejected.phase, "rejected"); assert.equal(api.bookings.length, 0);
  const second = browser(api, first.storage); await second.start(); assert.match(second.text("massage-status"), /request was rejected/); assert.equal(second.get("massage-discard").hidden, false);
  reject = false; await second.click("massage-discard"); await second.prepare(); await second.click("massage-confirm");
  assert.equal(api.bookings.length, 1); assert.notEqual(applies(second)[0].body.operation_id, rejected.body.operation_id);
});

test("conflict reconciliation keeps a found original receipt instead of making a new action", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { const result = base(path, method, body); if (/\/book$/.test(path)) throw { status: 409 }; return result; };
  await b.start(); await b.select(); await b.prepare(); await b.click("massage-confirm");
  assert.equal(api.bookings.length, 1); assert.equal(storageRecord(b), undefined); assert.equal(applies(b).length, 1); assert.match(b.text("massage-history"), /Booked/);
});


test("access loss after preview is reconciled and does not prevent cancelling another booking", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { if (api.disabled && /\/book$/.test(path)) throw { status: 404 }; return base(path, method, body); };
  await b.start(); await b.select(); await b.prepare(); await b.click("massage-confirm");
  await b.prepare(); api.disabled = true; await b.click("massage-confirm"); assert.equal(storageRecord(b).phase, "rejected");
  await b.click("massage-discard"); assert.equal(b.buttons("massage-history").length, 1);
  await b.click(b.buttons("massage-history")[0]); await b.click("massage-confirm"); assert.equal(api.bookings[0].state, "cancelled");
});

test("preview expiring while idle reports expiry without dispatch and still permits discard", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.prepare();
  b.now += 360000; await b.click("massage-confirm");
  assert.equal(applies(b).length, 0); assert.match(b.text("massage-status"), /preview has expired/); assert.equal(b.get("massage-confirm").disabled, true); assert.equal(b.get("massage-discard").disabled, false);
});

test("real app wiring supplies current bearer and clears native presentation on logout", async () => {
  const api = backend(), b = browser(api, new Map(), { app: true });
  for (let i = 0; i < 100 && (b.get("massage").hidden || b.get("logout").disabled); i++) await new Promise(resolve => setTimeout(resolve, 5));
  assert.equal(b.get("massage").hidden, false); await b.select(); await b.prepare(); await b.click("massage-confirm");
  assert.equal(applies(b)[0].authorization, "ZinusyaSession " + "a".repeat(43));
  await b.click("logout"); assert.equal(b.get("massage").hidden, true); assert.equal(b.text("massage-preview"), ""); assert.equal(b.text("massage-history"), "");
  for (const call of applies(b)) assert.doesNotMatch(call.storedBefore, /session_token|Authorization|aaaaaaaa/);
});


test("paging event choices retains the exact selected event beside its active slots", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => path.startsWith("./massage/events") ? path.includes("?after=") ? { events: [{ id: other, name: "Other event", timezone: "Europe/Minsk" }] } : { ...base(path, method, body), next: other } : base(path, method, body);
  await b.start(); await b.select(); await b.click("massage-events-next");
  assert.equal(b.get("massage-event").value, eventID); assert.match(b.text("massage-event"), /Event <script>/); assert.match(b.text("massage-slots"), /12.34 BYN/);
  await b.prepare(); assert.equal(storageRecord(b).body.arguments.event_id, eventID);
});

test("an off-page recovered preparation keeps its visible event identity", async () => {
  const api = backend(), first = browser(api); await first.start(); await first.select(); await first.prepare();
  api.hook = (path, method, body, base) => path.startsWith("./massage/events") ? { events: [] } : base(path, method, body);
  const second = browser(api, first.storage); await second.start();
  assert.equal(second.get("massage-event").value, eventID); assert.match(second.text("massage-event"), /Event <script>/); assert.equal(applies(second).length, 0);
});


test("off-page unknown apply retains visible event identity and the identical approved retry", async () => {
  const api = backend(), first = browser(api);
  api.hook = (path, method, body, base) => /\/book$/.test(path) ? (() => { throw { status: 503 }; })() : base(path, method, body);
  await first.start(); await first.select(); await first.prepare(); await first.click("massage-confirm"); const original = storageRecord(first);
  api.hook = (path, method, body, base) => path.startsWith("./massage/events") ? { events: [] } : base(path, method, body);
  const second = browser(api, first.storage); await second.start();
  assert.equal(storageRecord(second).phase, "apply"); assert.equal(second.get("massage-event").value, eventID); assert.match(second.text("massage-event"), /Event <script>/);
  assert.equal(applies(second).length, 0); await second.click("massage-retry"); assert.deepEqual(applies(second)[0].body, original.body); assert.equal(api.bookings.length, 1);
});


test("authenticated massage launch reaches the visible module without submitting a write", async () => {
  const api = backend(), b = browser(api, new Map(), { app: true, hash: "#massage?tgWebAppData=non-authorizing-fixture" });
  for (let i = 0; i < 100 && (b.get("massage").hidden || b.get("logout").disabled); i++) await new Promise(resolve => setTimeout(resolve, 5));
  assert.ok(b.scrolled > 0); assert.equal(b.get("massage").hidden, false); assert.equal(applies(b).length, 0); assert.equal(api.preparations.size, 0);
  const otherModule = browser(api, new Map(), { hash: "#orders" }); await otherModule.start(); assert.equal(otherModule.scrolled, 0);
});

test("massage launch also reveals a blocked saved-request explanation", async () => {
  const api = backend(), b = browser(api, new Map([[key, "{bad"]]), { hash: "#massage" }); await b.start();
  assert.equal(b.scrolled, 1); assert.match(b.text("massage-status"), /saved request cannot be read/); assert.equal(applies(b).length, 0);
});

function savedChoice(api, kind = "book") {
  const args = kind === "book" ? { event_id: eventID, expected_event_version: 1, expected_schedule_version: 1,
    selection: { window_id: "night", specialist_id: "alex", service_id: "short", starts_at: "2026-10-25T01:30:00Z" } }
    : { event_id: eventID, booking_id: api.bookings[0].id, expected_version: api.bookings[0].version };
  const operation = randomUUID(), proposal = api.base(`./massage/${eventID}/${kind}/prepare`, "POST", { operation_id: operation, arguments: args });
  return { operation, proposal };
}

test("agent saved booking is adopted under a new operation and requires a separate click", async () => {
  const api = backend(), saved = savedChoice(api), b = browser(api);
  await b.start(); await b.select();
  assert.match(b.text("massage-choices"), /GMT\+01:00/);
  await b.click(b.buttons("massage-choices")[0]);
  const request = b.calls.find(c => c.path.endsWith("/prepare"));
  assert.equal(request.body.proposal_id, saved.proposal.proposal_id);
  assert.notEqual(request.body.operation_id, saved.operation);
  assert.deepEqual(JSON.parse(request.storedBefore).body, request.body);
  assert.equal(applies(b).length, 0); assert.equal(api.bookings.length, 0);
  assert.match(b.text("massage-preview"), /12.34 BYN/);
  assert.equal(storageRecord(b).body.proposal_id, saved.proposal.proposal_id);
  const restored = browser(api, b.storage); await restored.start();
  assert.equal(restored.calls.find(c => c.path.endsWith("/prepare")).body.operation_id, request.body.operation_id);
  assert.equal(applies(restored).length, 0);
  await restored.click("massage-confirm"); assert.equal(api.bookings.length, 1);
  assert.equal(api.receipts.get(request.body.operation_id).proposal_id, saved.proposal.proposal_id);
  assert.equal(new Set(Array.from(api.preparations.values()).map(p => p.proposal_id)).size, 1);
});

test("saved cancellation remains available without new booking access and needs confirmation", async () => {
  const api = backend(), saved = savedChoice(api), b = browser(api);
  api.base(`./massage/${eventID}/book`, "POST", { operation_id: saved.operation, arguments: saved.proposal.arguments, proposal_id: saved.proposal.proposal_id, confirmed: true });
  const cancellation = savedChoice(api, "cancel"); api.disabled = true;
  await b.start(); await b.select();
  await b.click(b.buttons("massage-choices")[0]);
  assert.equal(api.bookings[0].state, "booked"); assert.match(b.text("massage-preview"), /Cancel this booking/);
  await b.click("massage-confirm"); assert.equal(api.bookings[0].state, "cancelled");
  assert.equal(applies(b)[0].body.proposal_id, cancellation.proposal.proposal_id);
});

test("expired adoption is rejected without replacement and retains the exact retry", async () => {
  const api = backend(), saved = savedChoice(api), b = browser(api);
  await b.start(); await b.select(); saved.proposal.expires_at = new Date(Date.now() - 1).toISOString();
  await b.click(b.buttons("massage-choices")[0]);
  assert.match(b.text("massage-status"), /could not be revalidated/);
  assert.equal(b.get("massage-confirm").hidden, true); assert.equal(applies(b).length, 0);
  const original = storageRecord(b); await b.click("massage-retry");
  assert.deepEqual(storageRecord(b), original); assert.equal(api.preparations.size, 1);
  await b.click("massage-discard"); assert.equal(b.buttons("massage-choices").length, 0);
});

test("a replacement proposal in the prepare response never enables confirmation", async () => {
  const api = backend(); savedChoice(api); const b = browser(api);
  api.hook = (path, method, body, base) => {
    const result = base(path, method, body);
    return path.endsWith("/prepare") && body.proposal_id ? { ...result, proposal_id: randomUUID() } : result;
  };
  await b.start(); await b.select(); await b.click(b.buttons("massage-choices")[0]);
  assert.equal(b.get("massage-confirm").hidden, true); assert.equal(applies(b).length, 0);
  assert.match(b.text("massage-status"), /could not be revalidated/);
});

test("unfinished choices paginate independently and follow the language and busy state", async () => {
  const api = backend(); for (let i = 0; i < 3; i++) savedChoice(api); api.pageSize = 1;
  const b = browser(api); await b.start(owner, "ru"); await b.select();
  assert.match(b.text("massage-choices"), /Незавершённые действия/);
  for (let i = 0; i < 3; i++) {
    const button = b.buttons("massage-choices")[0]; assert.ok(button);
    b.controller.setBusy(true); assert.equal(button.disabled, true); b.controller.setBusy(false);
    if (i < 2) await b.click("massage-choices-next");
  }
  assert.equal(b.get("massage-choices-next").hidden, true);
  assert.equal(new Set(b.calls.filter(c => c.path.includes("/pending")).map(c => c.path)).size, 3);
  assert.equal(applies(b).length, 0); assert.equal(b.calls.some(c => c.path.endsWith("/prepare")), false);
  await b.click("massage-choices-first"); assert.equal(b.buttons("massage-choices").length, 1);
  b.controller.clear(); assert.equal(b.text("massage-choices"), ""); assert.equal(b.get("massage-choices-first").hidden, true);
});

test("logout ignores an outstanding saved-choice read", async () => {
  const api = backend(); savedChoice(api); const b = browser(api); await b.start(); let release;
  api.hook = async (path, method, body, base) => { if (path.includes("/pending")) await new Promise(resolve => { release = resolve; }); return base(path, method, body); };
  const selected = b.select();
  while (!release) await new Promise(resolve => setImmediate(resolve));
  b.controller.clear(); release(); await selected;
  assert.equal(b.text("massage-choices"), ""); assert.equal(b.get("massage").hidden, true);
  assert.equal(applies(b).length, 0);
});

test("another interface consuming the preview is recovered as the same booking", async () => {
  const api = backend(), saved = savedChoice(api), b = browser(api);
  await b.start(); await b.select(); await b.click(b.buttons("massage-choices")[0]);
  const original = api.base(`./massage/${eventID}/book`, "POST", { operation_id: saved.operation, arguments: saved.proposal.arguments, proposal_id: saved.proposal.proposal_id, confirmed: true });
  await b.click("massage-confirm"); assert.equal(api.bookings.length, 1);
  assert.equal(api.receipts.get(applies(b)[0].body.operation_id).result.id, original.id);
  assert.equal(storageRecord(b), undefined); assert.equal(b.buttons("massage-choices").length, 0);
});
