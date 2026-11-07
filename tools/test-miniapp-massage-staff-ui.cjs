"use strict";

const assert = require("node:assert/strict");
const { webcrypto } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/massage-staff.js"), "utf8");
const appSource = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/app.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218", other = "510e58dd-e547-48d4-b9fa-571d24953219";
const eventID = "8cc5e2de-729c-4604-a85b-f97cdd184795", nextEvent = "8cc5e2de-729c-4604-a85b-f97cdd184796";
const ownID = "7b98c667-479d-41be-9eb0-2a9d2be6a3c0", foreignID = "7b98c667-479d-41be-9eb0-2a9d2be6a3c1";
const clone = value => JSON.parse(JSON.stringify(value));
function backend() {
  const api = { hook: undefined, requests: [], pageSize: 20, events: [{ id: eventID, name: "Event <script>" }] };
  api.preference = { user_id: owner, notify_bookings: true, notify_next: true, version: 0 }; api.preferenceReceipts = new Map();
  const name = { en: "Alex <img>", ru: "Алекс <img>" };
  api.schedule = {
    event_id: eventID, event_name: api.events[0].name, event_version: 1, version: 1,
    observed_at: "2026-10-24T12:00:00Z", timezone: "Europe/Berlin", own_specialist_ids: ["alex"],
    document: {
      windows: [{ id: "night", name, starts_at: "2026-10-25T00:00:00Z", ends_at: "2026-10-27T00:00:00Z" }],
      specialists: [{ id: "alex", name, service_ids: ["short"], work_spans: [{ window_id: "night", starts_at: "2026-10-25T00:00:00Z", ends_at: "2026-10-27T00:00:00Z" }] }],
      services: [{ id: "short", name: { en: "Massage", ru: "Массаж" }, price_minor: 9999 }]
    }
  };
  const terms = { window_id: "night", specialist_id: "alex", service_id: "short", specialist_name: name, service_name: { en: "Massage", ru: "Массаж" }, timezone: "UTC", starts_at: "2026-10-25T00:30:00Z", treatment_ends_at: "2026-10-25T00:50:00Z", occupancy_ends_at: "2026-10-25T00:55:00Z", price_minor: 1234, currency: "BYN", currency_scale: 2 };
  api.rows = [{ id: ownID, version: 1, client_name: "PRIVATE_CLIENT <script>", contact: "tg://user?id=11002", terms }, { id: foreignID, version: 1, client_name: "OTHER_CLIENT <img>", terms: { ...terms, specialist_id: "beth", starts_at: "2026-10-25T01:30:00Z", treatment_ends_at: "2026-10-25T01:50:00Z", occupancy_ends_at: "2026-10-25T01:55:00Z" } }];
  api.base = (path, method, body) => {
    if (path.startsWith("./massage-staff/events")) { const after = new URL(path, "https://fixture.invalid").searchParams.get("after"); return { events: after ? api.events.slice(1) : api.events.slice(0, 1), ...(api.events.length > 1 && !after ? { next: api.events[0].id } : {}) }; }
    if (path.endsWith("/preferences")) {
      if (method === "POST") {
        const old = api.preferenceReceipts.get(body.operation_id);
        if (old) { assert.deepEqual(old.arguments, body.arguments); return old.result; }
        if (body.arguments.expected_version !== api.preference.version) throw { status: 409 };
        const patch = Object.fromEntries(["notify_bookings", "notify_next"].filter(key => key in body.arguments).map(key => [key, body.arguments[key]]));
        if (Object.keys(patch).some(key => patch[key] !== api.preference[key])) api.preference = { ...api.preference, ...patch, version: api.preference.version + 1 };
        api.preferenceReceipts.set(body.operation_id, { arguments: clone(body.arguments), result: clone(api.preference) });
      }
      return api.preference;
    }
    if (path.includes("/preference-operations/")) { const receipt = api.preferenceReceipts.get(path.split("/").at(-1)); if (!receipt) throw { status: 404 }; return receipt.result; }
    if (path.endsWith("/schedule")) return api.schedule;
    if (path.endsWith("/windows")) { assert.equal(method,"POST"); return { event_id: eventID, timezone: api.schedule.timezone, windows: api.windows || api.schedule.document.windows }; }
    if (path.endsWith("/timetable")) {
      assert.equal(method, "POST"); assert.equal(body.limit, 20);
      const from = Date.parse(body.from), until = Date.parse(body.until);
      const compare = (left, right) => Date.parse(left.terms.starts_at) - Date.parse(right.terms.starts_at) || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0);
      let rows = api.rows.filter(row => Date.parse(row.terms.starts_at) < until && Date.parse(row.terms.occupancy_ends_at) > from && (!body.window_id || row.terms.window_id === body.window_id) && (!body.own_only || api.schedule.own_specialist_ids.includes(row.terms.specialist_id))).sort(compare);
      if (body.after) rows = rows.filter(row => compare(row, { id: body.after.id, terms: { starts_at: body.after.starts_at } }) > 0);
      const page = rows.slice(0, api.pageSize), last = page.at(-1);
      return { event_id: eventID, timezone: api.schedule.timezone, bookings: page, ...(rows.length > page.length ? { next: { event_id: eventID, window_id: body.window_id, from: body.from, until: body.until, own_only: body.own_only, id: last.id, starts_at: last.terms.starts_at } } : {}) };
    }
    if (path.includes("/clients/")) { const row = api.rows.find(row => row.id === path.split("/").at(-1)); if (!row?.contact) throw { status: 404 }; return row; }
    throw new Error("Unexpected specialist request " + path);
  };
  api.request = async (path, method, body) => clone(await (api.hook ? api.hook(path, method, body, api.base) : api.base(path, method, body)));
  return api;
}
function browser(api, { app = false, hash = "", storage = new Map() } = {}) {
  const elements = new Map(), calls = [], writes = [];
  const b = { calls, storage, writes, busy: [], denied: 0, scrolled: 0 };
  function element(tag = "div") {
    let value = "", text = "";
    const e = { tag, children: [], hidden: false, disabled: false, listeners: {}, dataset: {},
      append(...nodes) { this.children.push(...nodes); },
      replaceChildren(...nodes) { this.children = nodes; if (tag === "select" && !nodes.some(node => node.value === value)) value = nodes[0]?.value || ""; },
      addEventListener(name, fn) { this.listeners[name] = fn; },
      querySelectorAll(selector) { const walk = node => [...(node.tag === selector ? [node] : []), ...node.children.flatMap(walk)]; return this.children.flatMap(walk); },
      scrollIntoView() { b.scrolled++; }
    };
    Object.defineProperties(e, { value: { get: () => value, set: input => { value = tag === "select" && !e.children.some(option => option.value === String(input)) ? "" : String(input); } }, options: { get: () => e.children.length ? e.children : [{ textContent: "" }] }, textContent: { get: () => text, set: input => { text = String(input); e.children = []; } }, innerHTML: { set() { throw new Error("Unsafe HTML"); } } });
    return e;
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element(["ms-event", "ms-window", "ms-filter", "profile-language"].includes(id) ? "select" : "div")); return elements.get(id); };
  const context = { window: { location: { hash }, Telegram: { WebApp: { initData: "non-authorizing-fixture", ready() {} } } }, navigator: { language: "en" }, document: { getElementById: get, createElement: element, documentElement: {} }, crypto: webcrypto, Intl, Date, TextEncoder, Uint8Array, btoa, console,
    sessionStorage: { getItem: key => storage.get(key) ?? null, setItem: (key, value) => { if (b.storageFail) throw new Error("Storage unavailable"); writes.push({ key, value }); storage.set(key, value); }, removeItem: key => { if (b.storageFail) throw new Error("Storage unavailable"); storage.delete(key); } }
  };
  runInNewContext(source, context);
  let controller = context.window.createZinusyaMassageStaff({ request: async (path, method, body) => { calls.push({ path, method, body: body && clone(body) }); return api.request(path, method, body && clone(body)); }, setBusy: value => { b.busy.push(value); controller.setBusy(value); }, denied: () => { b.denied++; controller.clear(); } });
  if (app) {
    const create = context.window.createZinusyaMassageStaff;
    let started;
    b.started = new Promise(resolve => { started = resolve; });
    context.window.createZinusyaMassageStaff = env => {
      controller = create(env);
      const start = controller.start.bind(controller);
      controller.start = (...args) => { const result = start(...args); started(result); return result; };
      return controller;
    };
    context.fetch = async (path, options) => {
    if (path === "./profile" && api.sessionExpired) return { ok: false, status: 401 };
      try {
        let reply;
        if (path.startsWith("./massage-staff/")) { const body = options.body && JSON.parse(options.body); calls.push({ path, method: options.method, body, authorization: options.headers.Authorization }); reply = await api.request(path, options.method, body); }
        else if (path === "./session/bootstrap" || path === "./me") reply = { user_id: owner, session_token: "a".repeat(43), full_name: "Owner", bot_language: "en" };
        else if (path === "./profile") reply = { version: 0, full_name: "Owner", bot_language: "en" };
        else if (path === "./account") reply = { events: [], has_more: false, observed_at: "2026-10-18T00:00:00Z" };
        else if (path === "./registration-details") return { ok: false, status: 404 };
        else throw new Error("Unexpected app request " + path);
        return { ok: true, status: 200, json: async () => reply };
      } catch (error) { return { ok: false, status: error.status || 503 }; }
    };
    runInNewContext(appSource, context);
  }
  b.get = get; b.controller = controller;
  b.start = (id = owner, locale = "en") => controller.start(id, locale);
  b.click = async (id, event = "click") => { const el = typeof id === "string" ? get(id) : id; assert.equal(el.disabled, false, "Disabled action " + id); await el.listeners[event]({ preventDefault() {} }); };
  b.select = async () => { get("ms-event").value = eventID; await b.click("ms-event", "change"); };
  b.text = id => { const walk = node => [node.textContent, ...node.children.map(walk)].join("\n"); return walk(get(id)); };
  b.buttons = id => get(id).querySelectorAll("button"); b.links = id => get(id).querySelectorAll("a");
  b.settle = async () => { for (let i = 0; i < 30; i++) await new Promise(resolve => setImmediate(resolve)); };
  return b;
}

test("specialist without a pass reads all bookings and only own contact/detail, with event DST offsets and frozen price", async () => {
  const api = backend(), b = browser(api);
  await b.start(); await b.select();
  assert.equal(b.get("ms-instant-specialist").disabled, false);
  assert.equal(b.get("ms-instant-specialist").value, "alex");
  assert.equal(b.get("ms-instant-service").value, "short");
  assert.match(b.text("ms-rows"), /PRIVATE_CLIENT <script>/); assert.match(b.text("ms-rows"), /OTHER_CLIENT <img>/);
  assert.match(b.text("ms-rows"), /12\.34 BYN/); assert.doesNotMatch(b.text("ms-rows"), /99\.99/);
  assert.match(b.text("ms-rows"), /GMT\+02:00/); assert.match(b.text("ms-rows"), /GMT\+01:00/);
  assert.match(b.text("ms-range"), /Europe\/Berlin/);
  assert.equal(b.buttons("ms-rows").length, 1); assert.equal(b.links("ms-rows").length, 0);
  await b.click(b.buttons("ms-rows")[0]); assert.equal(b.links("ms-detail")[0].href, "tg://user?id=11002");
  assert.match(b.text("ms-detail"), /Reserved until/); assert.equal(b.writes.length, 0);
  assert.ok(b.calls.every(call => call.path.startsWith("./massage-staff/")));
});

test("my-clients filter resets pagination and never requests foreign client detail", async () => {
  const api = backend(); api.pageSize = 1; const b = browser(api);
  await b.start(); await b.select(); assert.equal(b.get("ms-rows-next").hidden, false);
  b.get("ms-filter").value = "own"; await b.click("ms-filter", "change");
  assert.doesNotMatch(b.text("ms-rows"), /OTHER_CLIENT/); assert.equal(b.get("ms-rows-next").hidden, true);
  const query = b.calls.filter(call => call.path.endsWith("/timetable")).at(-1).body;
  assert.equal(query.own_only, true); assert.equal(query.after, undefined);
  await b.click(b.buttons("ms-rows")[0]); assert.ok(b.calls.at(-1).path.endsWith("/clients/" + ownID));
});

test("booking cursor is passed intact and range navigation has no time gaps across DST", async () => {
  const api = backend(); api.pageSize = 1; const b = browser(api);
  await b.start(); await b.select(); const first = b.calls.findLast(call => call.path.endsWith("/timetable")).body;
  await b.click("ms-rows-next"); const next = b.calls.at(-1).body;
  assert.equal(next.after.id, ownID); assert.equal(next.after.own_only, false); assert.equal(next.from, first.from); assert.equal(next.until, first.until);
  assert.match(b.text("ms-rows"), /OTHER_CLIENT/); assert.equal(b.buttons("ms-rows").length, 0);
  await b.click("ms-range-next"); const later = b.calls.at(-1).body;
  assert.equal(later.from, first.until); assert.equal(later.after, undefined); assert.equal(Date.parse(later.until) - Date.parse(later.from), 86400000);
  await b.click("ms-range-prev"); assert.equal(b.calls.at(-1).body.from, first.from);
});

test("timetable includes overlapping reservations and paginates equal start times without loss", async () => {
  const api = backend(); api.pageSize = 1;
  const base = clone(api.rows[0]);
  const crossing = { ...clone(base), id: "00000000-0000-4000-8000-000000000001", client_name: "CROSSING",
    terms: { ...base.terms, starts_at: "2026-10-24T23:50:00Z", treatment_ends_at: "2026-10-25T00:05:00Z", occupancy_ends_at: "2026-10-25T00:10:00Z" } };
  const tied = { ...clone(base), id: "ffffffff-ffff-4fff-8fff-ffffffffffff", client_name: "SAME_START" };
  api.rows = [tied, base, crossing];
  const b = browser(api); await b.start(); await b.select();
  assert.match(b.text("ms-rows"), /CROSSING/);
  await b.click("ms-rows-next"); assert.match(b.text("ms-rows"), /PRIVATE_CLIENT/);
  await b.click("ms-rows-next"); assert.match(b.text("ms-rows"), /SAME_START/);
  assert.equal(b.get("ms-rows-next").hidden, true);
});

test("event pagination clears prior client data and preserves the event cursor", async () => {
  const api = backend(); api.events.push({ id: nextEvent, name: "Another event" }); const b = browser(api);
  await b.start(); await b.select(); await b.click(b.buttons("ms-rows")[0]); await b.click("ms-events-next");
  assert.equal(b.text("ms-rows"), ""); assert.equal(b.text("ms-detail"), ""); assert.ok(b.calls.at(-1).path.includes("after=" + eventID));
  assert.equal(b.get("ms-events-first").hidden, false); assert.equal(b.get("ms-event").value, "");
  await b.click("ms-events-first"); assert.equal(b.calls.at(-1).path, "./massage-staff/events");
});

test("Russian labels and decimal price use the chosen interface language", async () => {
  const b = browser(backend(), { hash: "#massage-staff" }); await b.start(owner, "ru"); await b.select();
  assert.equal(b.get("ms-title").textContent, "Расписание специалиста"); assert.match(b.text("ms-rows"), /12,34 BYN/); assert.equal(b.scrolled, 1);
  b.controller.setLanguage("en"); assert.equal(b.get("ms-title").textContent, "Specialist timetable"); assert.match(b.text("ms-rows"), /12\.34 BYN/);
});

test("revoked access clears clients and contacts before offering reload", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.click(b.buttons("ms-rows")[0]);
  api.hook = (path, method, body, base) => { if (path.endsWith("/timetable")) throw { status: 404 }; return base(path, method, body); };
  b.get("ms-filter").value = "own"; await b.click("ms-filter", "change");
  assert.equal(b.text("ms-rows"), ""); assert.equal(b.text("ms-detail"), ""); assert.match(b.text("ms-status"), /access has changed/); assert.equal(b.get("ms-window").disabled, true);
  api.events = []; await b.click("ms-reload"); assert.match(b.text("ms-status"), /No specialist events/);
});

test("expired session clears the workspace and releases the global busy state", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select();
  api.hook = () => { throw { status: 401 }; }; await b.click("ms-reload");
  assert.equal(b.denied, 1); assert.equal(b.get("massage-staff").hidden, true); assert.equal(b.text("ms-rows"), ""); assert.equal(b.busy.at(-1), false);
});

test("late timetable response cannot restore private data after logout or another account", async () => {
  const api = backend(), b = browser(api); await b.start(); let release;
  api.hook = async (path, method, body, base) => { const reply = base(path, method, body); if (path.endsWith("/timetable")) await new Promise(resolve => { release = resolve; }); return reply; };
  const selected = b.select(); while (!release) await new Promise(resolve => setImmediate(resolve));
  b.controller.clear(); api.events = []; await b.start(other); release(); await selected;
  assert.equal(b.text("ms-rows"), ""); assert.equal(b.text("ms-detail"), ""); assert.match(b.text("ms-status"), /No specialist events/); assert.equal(b.writes.length, 0);
});

test("unsafe contact schemes are never clickable and foreign-filter cursors are rejected", async () => {
  const api = backend(); api.rows[0].contact = "javascript:alert(1)"; const b = browser(api);
  await b.start(); await b.select(); assert.equal(b.buttons("ms-rows").length, 0); assert.equal(b.links("ms-detail").length, 0);
  api.pageSize = 1; api.hook = (path, method, body, base) => { const reply = base(path, method, body); if (reply.next?.own_only !== undefined) reply.next.own_only = true; return reply; };
  b.get("ms-filter").value = "all"; await b.click("ms-filter", "change");
  assert.equal(b.text("ms-rows"), ""); assert.match(b.text("ms-status"), /Could not load/); assert.equal(b.get("ms-rows-next").hidden, true);
});

test("connection failure can be reloaded without any agent or business mutation", async () => {
  const api = backend(); api.hook = () => { throw { status: 503 }; }; const b = browser(api);
  await b.start(); assert.match(b.text("ms-status"), /Could not load/); api.hook = undefined; await b.click("ms-reload"); await b.select();
  assert.match(b.text("ms-rows"), /PRIVATE_CLIENT/); assert.ok(b.calls.filter(call => call.method === "POST").every(call => (call.path.endsWith("/timetable") || call.path.endsWith("/windows"))));
});

test("Mini App wiring uses the authenticated session, then session invalidation clears all specialist data", { timeout: 5000 }, async () => {
  const api = backend(), b = browser(api, { app: true }); await b.started; await b.settle();
  assert.equal(b.get("massage-staff").hidden, false); await b.select(); await b.click(b.buttons("ms-rows")[0]);
  assert.ok(b.calls.every(call => call.authorization === "ZinusyaSession " + "a".repeat(43)));
  api.sessionExpired = true; await b.click("profile-refresh"); assert.equal(b.get("massage-staff").hidden, true); assert.equal(b.text("ms-rows"), ""); assert.equal(b.text("ms-detail"), "");
  assert.ok([...b.storage.values()].every(value => !value.includes("PRIVATE_CLIENT") && !value.includes("tg://user")));
});


test("retained past bookings remain visible independently of participant module availability", async () => {
  const api = backend(); api.schedule.module_enabled = false;
  api.schedule.document.windows[0].starts_at = "2025-10-26T00:00:00Z";
  api.schedule.document.windows[0].ends_at = "2025-10-28T00:00:00Z";
  for (const row of api.rows) for (const field of ["starts_at", "treatment_ends_at", "occupancy_ends_at"]) row.terms[field] = row.terms[field].replace("2026-10-25", "2025-10-26");
  const b = browser(api); await b.start(); await b.select();
  assert.match(b.text("ms-rows"), /PRIVATE_CLIENT/); assert.match(b.text("ms-rows"), /OTHER_CLIENT/);
});

test("missing event timezone never falls back silently to the browser timezone", async () => {
  const api = backend(); api.hook = (path, method, body, base) => { const reply = base(path, method, body); if (path.endsWith("/timetable")) delete reply.timezone; return reply; };
  const b = browser(api); await b.start(); await b.select();
  assert.equal(b.text("ms-rows"), ""); assert.match(b.text("ms-status"), /Could not load/);
});


test("retained windows stay navigable after the editable party is removed or shortened", async () => {
 const api = backend();
 const old = clone(api.schedule.document.windows[0]); old.ends_at = "2026-10-26T00:00:00Z";
 api.windows = [old, { ...old, starts_at:"2026-10-25T01:00:00Z" }];
 api.schedule.document.windows = [{...old,id:"replacement"}];
 const b = browser(api); await b.start(); await b.select();
 assert.match(b.text("ms-rows"),/PRIVATE_CLIENT/);
 b.get("ms-window").value = "1"; await b.click("ms-window","change");
 assert.doesNotMatch(b.text("ms-rows"),/PRIVATE_CLIENT/); assert.match(b.text("ms-rows"),/OTHER_CLIENT/);
 assert.equal(b.calls.filter(x=>x.path.endsWith("/schedule")).length,1);
});

test("party pagination follows the complete window version cursor and clears client data", async () => {
 const api = backend(); const window = api.schedule.document.windows[0];
 const next = {event_id:eventID,window_id:window.id,starts_at:window.starts_at,ends_at:window.ends_at};
 api.hook = (path,method,body,base) => path.endsWith("/windows") ? {event_id:eventID,timezone:"Europe/Berlin",windows:body.after?[]:[window],...(!body.after?{next}:{})} : base(path,method,body);
 const b = browser(api); await b.start(); await b.select(); assert.match(b.text("ms-rows"),/PRIVATE_CLIENT/);
 await b.click("ms-windows-next"); assert.equal(b.text("ms-rows"),"");assert.equal(b.text("ms-detail"),"");
 assert.deepEqual(b.calls.filter(x=>x.path.endsWith("/windows")).at(-1).body.after,next);
 await b.click("ms-windows-first");assert.match(b.text("ms-rows"),/PRIVATE_CLIENT/);
});


test("specialist changes global notification preference without agent and sees current value after historical receipt", async () => {
  const api = backend(), b = browser(api);
  await b.start(); await b.select(); await b.settle();
  assert.equal(b.get("ms-notify").checked, true);
  assert.match(b.text("ms-preference-note"), /ALL your specialist events/);
  b.get("ms-notify").checked = false; await b.click("ms-notify", "change");
  await b.click("ms-preference-save"); await b.settle();
  assert.equal(api.preference.notify_bookings, false); assert.equal(api.preference.version, 1);
  assert.equal(b.storage.size, 0); assert.equal(api.preferenceReceipts.size, 1);
  assert.equal(b.get("ms-notify").checked, false);
  const writes = b.calls.filter(r => r.method === "POST" && r.path.endsWith("/preferences"));
  assert.equal(writes.length, 1); assert.equal(writes[0].body.arguments.expected_version, 0);
  assert.equal(JSON.parse(b.writes[0].value).operation_id, writes[0].body.operation_id);
  b.controller.setLanguage("ru"); assert.match(b.text("ms-preference-note"), /ВСЕХ/);
});

test("lost preference response survives reload and original receipt does not overwrite later agent change", async () => {
  const api = backend(), b = browser(api);
  let lost = true;
  api.hook = (path, method, body, base) => { const result = base(path, method, body); if (lost && method === "POST" && path.endsWith("/preferences")) { lost = false; throw { status: 503 }; } return result; };
  await b.start(); await b.select(); await b.settle();
  b.get("ms-notify").checked = false; await b.click("ms-notify", "change"); await b.click("ms-preference-save"); await b.settle();
  assert.equal(api.preference.notify_bookings, false); assert.equal(b.storage.size, 1);
  assert.equal(b.get("ms-preference-save").disabled, true);
  api.preference = { user_id: owner, notify_bookings: true, notify_next: true, version: 2 };
  const next = browser(api, { storage: b.storage }); await next.start(); await next.select(); await next.settle();
  assert.equal(next.get("ms-preference-save").disabled, true);
  await next.click("ms-preference-recover"); await next.settle();
  assert.equal(next.storage.size, 0); assert.equal(next.get("ms-notify").checked, true); assert.equal(api.preference.version, 2);
  assert.equal(next.calls.some(r => r.method === "POST" && r.path.endsWith("/preferences")), false);
});

test("unsubmitted preference request retries exact operation after reload, not a replacement", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { if (method === "POST" && path.endsWith("/preferences")) throw { status: 503 }; return base(path, method, body); };
  await b.start(); await b.select(); await b.settle(); b.get("ms-notify").checked = false;
  await b.click("ms-notify", "change"); await b.click("ms-preference-save"); await b.settle();
  const original = clone(b.calls.find(r => r.method === "POST" && r.path.endsWith("/preferences")).body);
  api.hook = undefined;
  const next = browser(api, { storage: b.storage }); await next.start();
  await next.click("ms-preference-recover"); await next.settle();
  assert.equal(next.storage.size, 1); assert.match(next.text("ms-preference-status"), /unresolved/);
  await next.click("ms-preference-retry"); await next.settle();
  assert.deepEqual(next.calls.find(r => r.method === "POST" && r.path.endsWith("/preferences")).body, original);
  assert.equal(api.preferenceReceipts.size, 1); assert.equal(api.preference.notify_bookings, false); assert.equal(next.storage.size, 0);
  assert.match(next.text("ms-preference-status"), /confirmed/);
});

test("preference failure does not clear timetable and inaccessible operation remains unresolved", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { if (path.endsWith("/preferences")) throw { status: 404 }; return base(path, method, body); };
  await b.start(); await b.select(); await b.settle();
  assert.match(b.text("ms-rows"), /PRIVATE_CLIENT/); assert.equal(b.get("ms-preference-save").disabled, true);
  assert.match(b.text("ms-preference-status"), /unavailable/);
  const key = "zinusya.massage-staff-preference." + owner;
  const record = { owner, operation_id: ownID, arguments: { event_id: eventID, expected_version: 0, notify_bookings: false } };
  b.storage.set(key, JSON.stringify(record));
  const next = browser(api, { storage: b.storage }); await next.start(); await next.click("ms-preference-recover"); await next.settle();
  assert.equal(next.storage.get(key), JSON.stringify(record)); assert.match(next.text("ms-preference-status"), /unresolved/);
});

test("storage failure blocks preference POST; malformed recovery and another account cannot adopt it", async () => {
  const api = backend(), b = browser(api);
  await b.start(); await b.select(); await b.settle(); b.storageFail = true; b.get("ms-notify").checked = false;
  await b.click("ms-notify", "change"); await b.click("ms-preference-save"); await b.settle();
  assert.equal(b.calls.some(r => r.method === "POST" && r.path.endsWith("/preferences")), false);
  assert.equal(b.get("ms-preference-save").disabled, true); assert.match(b.text("ms-preference-status"), /blocked/);
  b.storage.set("zinusya.massage-staff-preference." + owner, "broken json");
  const next = browser(api, { storage: b.storage }); await next.start(); await next.select(); await next.settle();
  assert.equal(next.get("ms-preference-save").disabled, true);
  await next.start(other); assert.equal(next.get("ms-preference-recover").hidden, true);
  assert.equal(next.calls.some(r => r.path.includes("/preference-operations/")), false);
});

test("concurrent agent preference change causes CAS rejection and requires current reload", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.settle();
  api.preference = { user_id: owner, notify_bookings: false, notify_next: true, version: 1 };
  b.get("ms-notify").checked = false; await b.click("ms-notify", "change"); await b.click("ms-preference-save"); await b.settle();
  assert.equal(b.storage.size, 0); assert.equal(b.get("ms-preference-save").disabled, true); assert.match(b.text("ms-preference-status"), /changed before/);
  await b.click("ms-preference-reload"); await b.settle(); assert.equal(b.get("ms-notify").checked, false);
  assert.equal(api.preferenceReceipts.size, 0);
});


test("late save result cannot restore preference or status after account change", async () => {
  const api = backend(), b = browser(api); let release;
  api.hook = async (path, method, body, base) => { const result = base(path, method, body); if (method === "POST" && path.endsWith("/preferences")) await new Promise(resolve => { release = resolve; }); return result; };
  await b.start(); await b.select(); await b.settle(); b.get("ms-notify").checked = false; await b.click("ms-notify", "change");
  const saving = b.click("ms-preference-save"); await b.settle(); assert.equal(typeof release, "function");
  await b.start(other); release(); await saving; await b.settle();
  assert.equal(b.text("ms-preference-status"), ""); assert.equal(b.get("ms-notify").checked, false); assert.equal(b.get("ms-preference-recover").hidden, true);
  assert.equal(b.storage.has("zinusya.massage-staff-preference." + owner), true);
});

test("receipt cleanup storage failure retains recoverable request and cannot repeat with a new key", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { const result = base(path, method, body); if (method === "POST" && path.endsWith("/preferences")) b.storageFail = true; return result; };
  await b.start(); await b.select(); await b.settle(); b.get("ms-notify").checked = false; await b.click("ms-notify", "change");
  await b.click("ms-preference-save"); await b.settle();
  assert.equal(api.preferenceReceipts.size, 1); assert.equal(b.storage.size, 1); assert.equal(b.get("ms-preference-save").disabled, true);
  assert.match(b.text("ms-preference-status"), /blocked/);
  api.hook = undefined;
  const next = browser(api, { storage: b.storage }); await next.start(); await next.click("ms-preference-recover"); await next.settle();
  assert.equal(next.storage.size, 0); assert.equal(api.preferenceReceipts.size, 1);
});


test("retry conflict with storage cleanup failure preserves timetable and reports blocked recovery", async () => {
  const api = backend(), b = browser(api);
  api.hook = (path, method, body, base) => { if (method === "POST" && path.endsWith("/preferences")) throw { status: 503 }; return base(path, method, body); };
  await b.start(); await b.select(); await b.settle(); b.get("ms-notify").checked = false;
  await b.click("ms-notify", "change"); await b.click("ms-preference-save"); await b.settle();
  api.hook = undefined; api.preference = { user_id: owner, notify_bookings: false, notify_next: true, version: 1 }; b.storageFail = true;
  await b.click("ms-preference-retry"); await b.settle();
  assert.match(b.text("ms-rows"), /PRIVATE_CLIENT/); assert.match(b.text("ms-preference-status"), /blocked/);
  assert.equal(b.storage.size, 1); assert.equal(b.get("ms-preference-save").disabled, true);
});


test("upcoming-session checkbox saves independently and both settings follow current agent state", async () => {
  const api = backend(), b = browser(api);
  await b.start(); await b.select(); await b.settle();
  assert.equal(b.get("ms-notify-next").checked, true);
  b.get("ms-notify-next").checked = false; await b.click("ms-notify-next", "change");
  await b.click("ms-preference-save"); await b.settle();
  assert.equal(api.preference.notify_next, false); assert.equal(api.preference.notify_bookings, true);
  assert.equal(b.get("ms-preference-save").disabled, true);
  api.preference = { ...api.preference, notify_bookings: false, notify_next: true, version: 2 };
  await b.click("ms-preference-reload"); await b.settle();
  assert.equal(b.get("ms-notify").checked, false); assert.equal(b.get("ms-notify-next").checked, true);
  b.controller.setLanguage("ru"); assert.match(b.text("ms-notify-next-label"), /предстоящих/);
  assert.ok(b.calls.every(call => call.path.startsWith("./massage-staff/")));
});

test("legacy unresolved booking-only save retries unchanged and preserves current upcoming preference", async () => {
  const api = backend(), storage = new Map(); api.preference.notify_next = false;
  const record = { owner, operation_id: ownID, arguments: { event_id: eventID, expected_version: 0, notify_bookings: false } };
  storage.set("zinusya.massage-staff-preference." + owner, JSON.stringify(record));
  const b = browser(api, { storage }); await b.start(); await b.select(); await b.settle();
  assert.equal(b.get("ms-notify-next").disabled, true);
  await b.click("ms-preference-retry"); await b.settle();
  const sent = b.calls.find(call => call.method === "POST" && call.path.endsWith("/preferences")).body;
  assert.deepEqual(sent, { operation_id: record.operation_id, arguments: record.arguments });
  assert.equal(api.preference.notify_next, false); assert.equal(api.preference.notify_bookings, false);
  assert.equal(storage.size, 0); assert.equal(b.get("ms-notify-next").checked, false);
});
