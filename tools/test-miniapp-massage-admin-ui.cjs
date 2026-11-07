"use strict";

const assert = require("node:assert/strict");
const { webcrypto, randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/massage-admin.js"), "utf8");
const appSource = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/app.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218", otherOwner = "510e58dd-e547-48d4-b9fa-571d24953219";
const eventID = "8cc5e2de-729c-4604-a85b-f97cdd184795", nextEvent = "8cc5e2de-729c-4604-a85b-f97cdd184796";
const targetID = "7b98c667-479d-41be-9eb0-2a9d2be6a3c0", foreignID = "7b98c667-479d-41be-9eb0-2a9d2be6a3c1";
const clone = value => JSON.parse(JSON.stringify(value));
const storageKey = id => "zinusya.massage-admin." + id;
const selection = (active = true, expected = 0) => ({ event_id: eventID, specialist_id: "alex", user_id: targetID, active, expected_version: expected, expected_event_version: 3, expected_schedule_version: 2 });
function backend() {
  const api = { active: true, admin: true, module: true, eventVersion: 3, scheduleVersion: 2, bindings: new Map(), proposals: new Map(), operations: new Map(), effects: 0, pageSize: 20, hook: undefined };
  api.events = [{ id: eventID, name: "Berlin <script>" }];
  api.specialists = [{ id: "alex", name: { en: "Alex <img>", ru: "Алекс <img>" } }, { id: "beth", name: { en: "Beth", ru: "Бет" } }];
  api.targets = new Map([[targetID, { user_id: targetID, telegram_user_id: 11002, display_name: "PRIVATE_TARGET <script>", linked: true, revoked: false }], [foreignID, { user_id: foreignID, telegram_user_id: 11003, display_name: "OTHER_TARGET", linked: true, revoked: false }]]);
  api.addProposal = (args = selection(), operation = randomUUID()) => {
    const value = { proposal_id: randomUUID(), operation_id: operation, arguments: clone(args), expires_at: new Date(Date.now() + 300000).toISOString(), state: "pending" };
    api.proposals.set(value.proposal_id, value); api.operations.set(operation, value); return value;
  };
  const currentStatus = value => { if (!value) throw { status: 404 }; if (value.state === "pending" && Date.parse(value.expires_at) <= (api.clockNow ?? Date.now())) value.state = "expired"; return value; };
  api.base = (path, method = "GET", body) => {
    if (!api.active) throw { status: 401 }; if (!api.admin) throw { status: 403 };
    const url = new URL(path, "https://fixture.invalid/app/"), parts = url.pathname.split("/"), last = parts.at(-1);
    if (path.startsWith("./massage-admin/events")) { const after = url.searchParams.get("after"); return { events: after ? api.events.slice(1) : api.events.slice(0, 1), ...(!after && api.events.length > 1 ? { next: api.events[0].id } : {}) }; }
    if (path.startsWith("./massage-admin/pending")) {
      const after = url.searchParams.get("after"), rows = [...api.proposals.values()].map(currentStatus).filter(p => p.state === "pending"), start = after ? rows.findIndex(p => p.proposal_id === after) + 1 : 0, page = rows.slice(start, start + api.pageSize);
      return { proposals: page, ...(rows.length > start + page.length ? { next_after: page.at(-1).proposal_id } : {}) };
    }
    if (path === "./massage-admin/target") { assert.equal(method, "POST"); assert.equal(Object.keys(body).length, 1); const found = body.user_id ? api.targets.get(body.user_id) : [...api.targets.values()].find(t => t.telegram_user_id === body.telegram_user_id); if (!found) throw { status: 404 }; return found; }
    if (path.includes("/status/operations/")) return currentStatus(api.operations.get(last));
    if (path.includes("/status/proposals/")) return currentStatus(api.proposals.get(last));
    const event = parts[3]; assert.ok([eventID, nextEvent].includes(event), "Unknown event " + path);
    if (last === "schedule") return { event_id: event, event_name: "Berlin <script>", timezone: "Europe/Berlin", event_version: api.eventVersion, version: api.scheduleVersion, document: { specialists: api.specialists } };
    if (last === "bindings" && method === "GET") {
      const after = url.searchParams.get("after"), rows = [...api.bindings.values()].sort((a, b) => a.specialist_id.localeCompare(b.specialist_id)).filter(b => !after || b.specialist_id > after), page = rows.slice(0, api.pageSize);
      return { event_id: event, event_version: api.eventVersion, schedule_version: api.scheduleVersion, module_enabled: api.module, bindings: page, ...(rows.length > page.length ? { next_after: page.at(-1).specialist_id } : {}) };
    }
    if (parts.at(-2) === "bindings" && method === "GET") { const b = api.bindings.get(last); if (!b) throw { status: 404 }; return b; }
    if (last === "prepare") {
      assert.equal(method, "POST"); assert.equal(body.confirmed, undefined);
      if (api.operations.has(body.operation_id)) return currentStatus(api.operations.get(body.operation_id));
      const p = body.proposal_id ? currentStatus(api.proposals.get(body.proposal_id)) : api.addProposal(body.arguments, body.operation_id);
      if (p.state !== "pending" || JSON.stringify(p.arguments) !== JSON.stringify(body.arguments)) throw { status: 409 };
      api.operations.set(body.operation_id, p); return p;
    }
    if (last === "bindings" && method === "POST") {
      assert.equal(body.confirmed, true); const p = currentStatus(api.operations.get(body.operation_id));
      assert.equal(body.proposal_id, p.proposal_id); assert.deepEqual(body.arguments, p.arguments);
      if (p.state === "completed") return p.result; if (p.state !== "pending") throw { status: 409 };
      const a = p.arguments, old = api.bindings.get(a.specialist_id);
      if ((old?.version || 0) !== a.expected_version || a.expected_event_version !== api.eventVersion || (a.active && a.expected_schedule_version !== api.scheduleVersion) || (old && old.user_id !== a.user_id)) throw { status: 409 };
      const result = { event_id: event, specialist_id: a.specialist_id, user_id: a.user_id, active: a.active, version: (old?.version || 0) + 1, updated_by: owner, updated_at: new Date().toISOString() };
      api.bindings.set(a.specialist_id, result); api.effects++; p.state = "completed"; p.result = result; return result;
    }
    throw new Error("Unexpected request " + path);
  };
  api.request = async (path, method, body) => clone(await (api.hook ? api.hook(path, method, body, api.base) : api.base(path, method, body)));
  api.bound = (active = true, alias = "alex", user = targetID) => { const b = { event_id: eventID, specialist_id: alias, user_id: user, active, version: 1, updated_by: owner, updated_at: new Date().toISOString() }; api.bindings.set(alias, b); return b; };
  return api;
}
function browser(api, { storage = new Map(), app = false } = {}) {
  const elements = new Map(), calls = [], writes = [];
  const b = { calls, writes, storage, busy: [], denied: 0, failRead: false, failWrite: false };
  function element(tag = "div") {
    let value = "", text = "";
    const e = { tag, children: [], hidden: false, disabled: false, listeners: {}, dataset: {}, append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { text = ""; this.children = nodes; if (tag === "select" && !nodes.some(n => n.value === value)) value = nodes[0]?.value || ""; }, addEventListener(name, fn) { this.listeners[name] = fn; }, querySelectorAll(selector) { const walk = n => [...(n.tag === selector ? [n] : []), ...n.children.flatMap(walk)]; return this.children.flatMap(walk); }, scrollIntoView() {} };
    Object.defineProperties(e, { value: { get: () => value, set: input => { value = tag === "select" && !e.children.some(n => n.value === String(input)) ? "" : String(input); } }, options: { get: () => e.children.length ? e.children : [{ textContent: "" }] }, textContent: { get: () => text, set: input => { text = String(input); e.children = []; } }, innerHTML: { set() { throw new Error("Unsafe HTML"); } } }); return e;
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element(["ma-event", "ma-alias", "ma-action", "profile-language"].includes(id) ? "select" : "div")); return elements.get(id); };
  const context = { window: { location: { hash: "#massage-admin" }, Telegram: { WebApp: { initData: "fixture-only", ready() {} } } }, navigator: { language: "en" }, document: { getElementById: get, createElement: element, documentElement: {} }, crypto: webcrypto, Intl, Date: class extends Date { static now() { return b.clockNow ?? Date.now(); } }, TextEncoder, Uint8Array, btoa, console,
    sessionStorage: { getItem: key => { if (b.failRead) throw new Error("Read denied"); return storage.get(key) ?? null; }, setItem: (key, value) => { if (b.failWrite) throw new Error("Write denied"); writes.push({ key, value }); storage.set(key, value); }, removeItem: key => { if (b.failWrite) throw new Error("Write denied"); storage.delete(key); } }
  };
  runInNewContext(source, context);
  let controller = context.window.createZinusyaMassageAdmin({ request: async (path, method, body) => { calls.push({ path, method, body: body && clone(body) }); return api.request(path, method, body && clone(body)); }, setBusy: value => { b.busy.push(value); controller.setBusy(value); }, denied: () => { b.denied++; controller.clear(); } });
  if (app) {
    const create = context.window.createZinusyaMassageAdmin;
    context.window.createZinusyaMassageAdmin = env => { controller = create(env); return controller; };
    context.fetch = async (path, options) => {
    if (path === "./profile" && api.sessionExpired) return { ok: false, status: 401 };
      try {
        let reply;
        if (path.startsWith("./massage-admin/")) { const body = options.body && JSON.parse(options.body); calls.push({ path, method: options.method, body, authorization: options.headers.Authorization }); reply = await api.request(path, options.method, body); }
        else if (path === "./session/bootstrap" || path === "./me") reply = { user_id: owner, session_token: "a".repeat(43), full_name: "Owner", bot_language: "en" };
        else if (path === "./profile") reply = { version: 0, full_name: "Owner", bot_language: "en" };
        else if (path === "./account") reply = { events: [], has_more: false, observed_at: "2026-10-18T00:00:00Z" };
        else if (path === "./registration-details") return { ok: false, status: 404 };
        else throw new Error("Unexpected app request " + path);
        return { ok: true, status: 200, json: async () => reply };
      } catch (error) { return { ok: false, status: error.status || 503 }; }
    }; runInNewContext(appSource, context);
  }
  b.get = get; b.controller = controller; b.start = (who = owner, locale = "en") => controller.start(who, locale);
  b.click = async (id, event = "click") => { const e = typeof id === "string" ? get(id) : id; assert.equal(e.disabled, false, "Disabled action " + id); await e.listeners[event]({ preventDefault() {} }); };
  b.text = id => { const walk = e => [e.textContent, ...e.children.map(walk)].join("\n"); return walk(get(id)); };
  b.chooseEvent = async () => { get("ma-event").value = eventID; await b.click("ma-event", "change"); };
  b.lookup = async (value = "11002") => { get("ma-lookup").value = value; await b.click("ma-lookup", "input"); await b.click("ma-lookup-button"); };
  b.prepare = async () => { await b.chooseEvent(); await b.lookup(); await b.click("ma-prepare"); };
  b.settle = async () => { for (let i = 0; i < 30; i++) await new Promise(resolve => setImmediate(resolve)); };
  return b;
}

test("native administrator grants only after readable exact confirmation and stores identifiers only", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.prepare();
  assert.equal(api.effects, 0); assert.match(b.text("ma-preview"), /Berlin <script>/); assert.match(b.text("ma-preview"), /PRIVATE_TARGET <script>/); assert.match(b.text("ma-preview"), /Telegram 11002/); assert.match(b.text("ma-preview"), /alex/);
  const saved = JSON.parse(b.storage.get(storageKey(owner))); assert.equal(saved.phase, "prepared");
  for (const write of b.writes) { assert.doesNotMatch(write.value, /PRIVATE_TARGET|11002|arguments|display_name/); assert.ok(!write.value.includes(targetID)); }
  await b.click("ma-confirm"); assert.equal(api.effects, 1); assert.equal(api.bindings.get("alex").active, true); assert.ok(!b.storage.has(storageKey(owner))); assert.match(b.text("ma-receipt"), /Earlier operation result/); assert.match(b.text("ma-current"), /granted/);
  const apply = b.calls.find(c => c.path.endsWith("/bindings") && c.method === "POST"); assert.deepEqual(apply.body.arguments, selection()); assert.equal(apply.body.confirmed, true);
});

test("existing alias owner cannot be replaced; revoked or unlinked target can still lose access after module/alias removal", async () => {
  const api = backend(); api.bound(); api.module = false; api.specialists = [{ id: "beth", name: { en: "Beth" } }]; Object.assign(api.targets.get(targetID), { revoked: true, linked: false });
  const b = browser(api); await b.start(); await b.chooseEvent(); assert.equal(b.get("ma-lookup").disabled, true); assert.equal(b.get("ma-action").value, "revoke");
  await b.click("ma-prepare"); assert.match(b.text("ma-preview"), /Revoke access/); await b.click("ma-confirm");
  assert.equal(api.effects, 1); assert.equal(api.bindings.get("alex").active, false); assert.equal(api.bindings.get("alex").user_id, targetID);
  const a = b.calls.find(c => c.path.endsWith("/prepare")).body.arguments; assert.equal(a.active, false); assert.equal(a.expected_version, 1);
});

test("unsafe numeric Telegram IDs never round or reach target lookup; UUID lookup is canonicalized", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.chooseEvent(); await b.lookup("9007199254740993"); assert.match(b.text("ma-status"), /without rounding/); assert.equal(b.calls.filter(c => c.path.endsWith("/target")).length, 0);
  await b.lookup(targetID.toUpperCase()); assert.match(b.text("ma-target"), /PRIVATE_TARGET/); assert.deepEqual(b.calls.filter(c => c.path.endsWith("/target")).at(-1).body, { user_id: targetID });
});

test("changing a typed lookup clears the resolved account and disables preparation", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.chooseEvent(); await b.lookup(); assert.equal(b.get("ma-prepare").disabled, false);
  b.get("ma-lookup").value = "11003"; await b.click("ma-lookup", "input"); assert.equal(b.text("ma-target"), ""); assert.equal(b.get("ma-prepare").disabled, true);
});

test("unlinked and revoked accounts and disabled modules cannot receive a grant", async () => {
  for (const mode of ["unlinked", "revoked", "module"]) { const api = backend(); if (mode === "unlinked") api.targets.get(targetID).linked = false; if (mode === "revoked") api.targets.get(targetID).revoked = true; if (mode === "module") api.module = false; const b = browser(api); await b.start(); await b.chooseEvent(); await b.lookup(); assert.equal(b.get("ma-prepare").disabled, true); assert.equal(api.effects, 0); }
});

test("adopting an agent proposal retains local operation ID and canonical proposal/expiry", async () => {
  const api = backend(), original = api.addProposal(), b = browser(api); await b.start(); await b.click(b.get("ma-choices").querySelectorAll("button")[0]);
  const saved = JSON.parse(b.storage.get(storageKey(owner))); assert.notEqual(saved.operation_id, original.operation_id); assert.equal(saved.proposal_id, original.proposal_id);
  assert.equal(api.operations.get(saved.operation_id), original); assert.equal(api.effects, 0);
  const reload = browser(api, {storage:b.storage}); await reload.start();
  assert.equal(JSON.parse(reload.storage.get(storageKey(owner))).operation_id,saved.operation_id);
  await reload.click("ma-confirm"); assert.equal(api.effects, 1);
  assert.equal(reload.calls.find(c => c.path.endsWith("/bindings") && c.method === "POST").body.operation_id, saved.operation_id);
});

test("prepare response loss recovers original proposal after reload without replaying or granting automatically", async () => {
  const api = backend(); let lost = false; api.hook = (path, method, body, base) => { const result = base(path, method, body); if (path.endsWith("/prepare") && !lost) { lost = true; throw { status: 503 }; } return result; };
  const b = browser(api); await b.start(); await b.prepare(); const saved = b.storage.get(storageKey(owner)); assert.equal(JSON.parse(saved).phase, "prepare"); assert.equal(api.effects, 0);
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.match(reload.text("ma-preview"), /PRIVATE_TARGET/); assert.equal(reload.get("ma-confirm").disabled, false); assert.equal(reload.calls.filter(c => c.path.endsWith("/prepare")).length, 0); assert.equal(api.effects, 0);
  await reload.click("ma-confirm"); assert.equal(api.effects, 1);
});

test("lost apply response recovers the original receipt after reload and does not grant twice", async () => {
  const api = backend(); let lost = false; api.hook = (path, method, body, base) => { const result = base(path, method, body); if (method === "POST" && path.endsWith("/bindings") && !lost) { lost = true; throw { status: 503 }; } return result; };
  const b = browser(api); await b.start(); await b.prepare(); await b.click("ma-confirm"); assert.equal(api.effects, 1); const op = JSON.parse(b.storage.get(storageKey(owner))).operation_id;
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.match(reload.text("ma-receipt"), /Earlier operation result/); assert.equal(api.effects, 1); assert.ok(!reload.storage.has(storageKey(owner))); assert.ok(reload.calls.some(c => c.path.endsWith("/operations/" + op))); assert.equal(reload.calls.filter(c => c.method === "POST" && c.path.endsWith("/bindings")).length, 0);
});

test("uncertain apply retries only its original operation after status reconciliation", async () => {
  const api = backend(); let dropped = false; api.hook = (path, method, body, base) => { if (path.endsWith("/bindings") && method === "POST" && !dropped) { dropped = true; throw { status: 503 }; } return base(path, method, body); };
  const b = browser(api); await b.start(); await b.prepare(); await b.click("ma-confirm"); assert.equal(api.effects, 0); const saved = JSON.parse(b.storage.get(storageKey(owner)));
  assert.equal(b.get("ma-close").hidden, true); await b.click("ma-retry"); assert.equal(api.effects, 1);
  const attempts = b.calls.filter(c => c.path.endsWith("/bindings") && c.method === "POST"); assert.equal(attempts.length, 2); assert.ok(attempts.every(c => c.body.operation_id === saved.operation_id)); assert.deepEqual(attempts[0].body, attempts[1].body);
});

test("expired original proposal cannot be renewed or confirmed", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.prepare(); const p = [...api.proposals.values()][0];
  b.clockNow = api.clockNow = Date.parse(p.expires_at) + 1;
  await b.click("ma-confirm"); assert.match(b.text("ma-status"), /expired/); assert.equal(api.effects, 0);
  assert.equal(b.calls.filter(c => c.method === "POST" && c.path.endsWith("/bindings")).length, 0);
  await b.click("ma-recover"); assert.equal(b.get("ma-confirm").disabled, true);
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.match(reload.text("ma-status"), /expired/); assert.equal(api.effects, 0);
});

test("unknown prepare after reload keeps its key, blocks new changes and never invents arguments", async () => {
  const api = backend(); api.hook = (path, method, body, base) => { if (path.endsWith("/prepare")) throw { status: 503 }; return base(path, method, body); };
  const b = browser(api); await b.start(); await b.prepare(); const saved = b.storage.get(storageKey(owner)); const reload = browser(api, { storage: b.storage }); await reload.start();
  assert.equal(reload.storage.get(storageKey(owner)), saved); assert.equal(reload.get("ma-prepare").disabled, true); assert.equal(reload.get("ma-retry").disabled, true); assert.equal(reload.calls.filter(c => c.path.endsWith("/prepare")).length, 0); assert.equal(api.effects, 0);
  await reload.click("ma-close"); assert.ok(!reload.storage.has(storageKey(owner))); assert.equal(api.effects, 0);
});

test("a completed receipt remains historical when current binding was subsequently revoked", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.prepare(); const p = [...api.proposals.values()][0];
  api.base("./massage-admin/" + eventID + "/bindings", "POST", { operation_id: p.operation_id, proposal_id: p.proposal_id, confirmed: true, arguments: p.arguments }); api.bindings.set("alex", { ...p.result, active: false, version: 2 });
  await b.click("ma-recover"); assert.match(b.text("ma-receipt"), /Grant access/); assert.match(b.text("ma-current"), /revoked/); assert.equal(api.effects, 1);
});

test("current administrator loss hides private data and preserves only recovery identifiers", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.prepare(); api.admin = false; await b.click("ma-confirm");
  assert.equal(api.effects, 0); assert.equal(b.get("massage-admin").hidden, true); for (const id of ["ma-target", "ma-preview", "ma-current", "ma-receipt"]) assert.equal(b.text(id), ""); assert.ok(b.storage.has(storageKey(owner))); assert.equal(b.busy.at(-1), false);
});

test("nonadministrators never see this workspace; expired session signs out", async () => {
  const api = backend(); api.admin = false; const b = browser(api); await b.start(); assert.equal(b.get("massage-admin").hidden, true); assert.equal(b.denied, 0);
  const expired = backend(); expired.active = false; const c = browser(expired); await c.start(); assert.equal(c.denied, 1); assert.equal(c.get("massage-admin").hidden, true);
});

test("storage failures block before sending; unreadable or foreign-owner recovery cannot authorize a change", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.chooseEvent(); await b.lookup(); b.failWrite = true; await b.click("ma-prepare"); assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 0); assert.equal(api.effects, 0);
  for (const value of ["{", JSON.stringify({ owner: otherOwner, event_id: eventID, operation_id: randomUUID(), phase: "prepare" })]) { const c = browser(api, { storage: new Map([[storageKey(owner), value]]) }); await c.start(); assert.equal(c.get("ma-prepare").disabled, true); assert.equal(c.get("massage-admin").hidden,false); assert.match(c.text("ma-status"),/saved recovery identifiers/); assert.equal(api.effects, 0); }
  const c = browser(api); c.failRead = true; await c.start(); assert.equal(c.get("ma-prepare").disabled, true);
});

test("late response cannot restore target data after logout or switching account", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.chooseEvent(); let finish;
  api.hook = (path, method, body, base) => path.endsWith("/target") ? new Promise(resolve => { finish = () => resolve(base(path, method, body)); }) : base(path, method, body);
  const pending = b.lookup(); await b.settle(); b.controller.clear(); await b.start(otherOwner); finish(); await pending;
  assert.equal(b.text("ma-target"), ""); assert.equal(b.text("ma-preview"), ""); assert.equal(api.effects, 0);
});

test("binding and pending-choice pages are bounded explicit navigation, and removed aliases remain selectable", async () => {
  const api = backend(); api.pageSize = 1; api.bound(true, "aaa_removed"); api.bound(false, "zzz_removed"); api.addProposal(); api.addProposal({ ...selection(), specialist_id: "beth" });
  const b = browser(api); await b.start(); assert.equal(b.get("ma-choices").querySelectorAll("button").length, 1); await b.click("ma-choices-next"); assert.match(b.text("ma-choices"), /beth/); await b.click("ma-choices-first"); assert.match(b.text("ma-choices"), /alex/);
  await b.chooseEvent(); assert.equal(b.get("ma-alias").value, "aaa_removed"); await b.click("ma-bindings-next"); assert.equal(b.get("ma-alias").value, "zzz_removed"); await b.click("ma-bindings-first"); assert.equal(b.get("ma-alias").value, "aaa_removed");
});

test("Russian exact preview and app wiring use session authentication; session invalidation clears private data", async () => {
  const api = backend(), ru = browser(api); await ru.start(owner, "ru"); await ru.prepare(); assert.match(ru.text("ma-preview"), /Выдать доступ/); assert.match(ru.text("ma-title"), /Доступ специалистов/); ru.controller.setLanguage("en"); assert.match(ru.text("ma-preview"), /Grant access/);
  const appAPI = backend(), b = browser(appAPI, { app: true }); await b.settle(); assert.ok(b.calls.some(c => c.path === "./massage-admin/events" && c.authorization === "ZinusyaSession " + "a".repeat(43))); await b.prepare(); appAPI.sessionExpired = true; await b.click("profile-refresh"); assert.equal(b.get("massage-admin").hidden, true); assert.equal(b.text("ma-preview"), ""); assert.equal(b.text("ma-target"), "");
});


test("prepare dropped before saving is retried in memory with the same operation", async () => {
 const api = backend(); let dropped = false;
 api.hook = (path,method,body,base) => { if(path.endsWith("/prepare") && !dropped) {dropped=true;throw {status:503};} return base(path,method,body); };
 const b = browser(api); await b.start(); await b.prepare(); const saved = JSON.parse(b.storage.get(storageKey(owner)));
 await b.click("ma-retry"); const attempts=b.calls.filter(c=>c.path.endsWith("/prepare")); assert.equal(attempts.length,2);assert.ok(attempts.every(c=>c.body.operation_id===saved.operation_id)); assert.equal(api.effects,0);await b.click("ma-confirm");assert.equal(api.effects,1);
});

test("uncertain apply after reload recovers original arguments without automatic application", async () => {
 const api = backend(); let dropped=false;
 api.hook = (path,method,body,base) => { if(path.endsWith("/bindings") && method==="POST" && !dropped) {dropped=true;throw {status:503};} return base(path,method,body); };
 const b = browser(api);await b.start();await b.prepare();await b.click("ma-confirm");const original=JSON.parse(b.storage.get(storageKey(owner)));
 const reload=browser(api,{storage:b.storage});await reload.start();assert.equal(api.effects,0);assert.equal(reload.get("ma-confirm").hidden,true);assert.equal(reload.get("ma-close").hidden,true);
 await reload.click("ma-retry");assert.equal(api.effects,1);const request=reload.calls.find(c=>c.path.endsWith("/bindings") && c.method==="POST");assert.equal(request.body.operation_id,original.operation_id);assert.deepEqual(request.body.arguments,selection());
});

test("a mismatched prepared account cannot authorize confirmation", async () => {
 const api=backend();api.hook=(path,method,body,base)=> {const value=clone(base(path,method,body));if(path.endsWith("/prepare"))value.arguments.user_id=foreignID;return value;};
 const b=browser(api);await b.start();await b.prepare();assert.equal(b.get("ma-confirm").disabled,true);assert.equal(b.get("ma-confirm").hidden,true);assert.equal(api.effects,0);assert.ok(b.storage.has(storageKey(owner)));
});

test("receipt cleanup storage failure keeps recovery identifiers and blocks new operations", async () => {
 const api=backend(),b=browser(api);await b.start();await b.prepare();
 api.hook=(path,method,body,base)=>{const value=base(path,method,body);if(path.endsWith("/bindings") && method==="POST")b.failWrite=true;return value;};
 await b.click("ma-confirm");assert.equal(api.effects,1);assert.ok(b.storage.has(storageKey(owner)));assert.equal(b.get("ma-prepare").disabled,true);
 const reload=browser(api,{storage:b.storage});await reload.start();assert.equal(api.effects,1);assert.ok(!reload.storage.has(storageKey(owner)));assert.match(reload.text("ma-receipt"),/Earlier operation/);
});


test("a schedule-only edit preserves original revoke consent while a grant requires the original schedule version", async () => {
 for (const revoke of [true,false]) {
  const api=backend();api.bound(revoke);const b=browser(api);await b.start();await b.chooseEvent();await b.click("ma-prepare");
  api.scheduleVersion++;
  await b.click("ma-recover");
  assert.equal(b.get("ma-confirm").disabled,!revoke);
  if(revoke) {await b.click("ma-confirm");assert.equal(api.effects,1);assert.equal(api.bindings.get("alex").active,false);const apply=b.calls.find(c=>c.path.endsWith("/bindings") && c.method==="POST");assert.equal(apply.body.arguments.expected_schedule_version,2);}
  else {assert.match(b.text("ma-preview"),/cannot be confirmed/);assert.equal(api.effects,0);}
 }
});

test("initial service outage exposes safe recovery controls and preserves an earlier operation", async () => {
 for(const existing of [false,true]) {
  const api=backend();let failed=false;const proposal=existing?api.addProposal():undefined;
  const saved=proposal?JSON.stringify({owner,event_id:eventID,operation_id:proposal.operation_id,proposal_id:proposal.proposal_id,phase:"apply"}):undefined;
  const storage=new Map(saved?[[storageKey(owner),saved]]:[]);
  api.hook=(path,method,body,base)=>{if(path==="./massage-admin/events" && !failed){failed=true;throw {status:503};}return base(path,method,body);};
  const b=browser(api,{storage});await b.start();assert.equal(b.get("massage-admin").hidden,false);assert.equal(b.get("ma-event").disabled,true);assert.equal(b.get("ma-reload").disabled,false);assert.equal(api.effects,0);
  if(existing)assert.equal(b.storage.get(storageKey(owner)),saved);
  await b.click("ma-reload");assert.equal(api.effects,0);
  if(existing){assert.equal(b.get("ma-confirm").hidden,true);await b.click("ma-retry");assert.equal(api.effects,1);}
  else {await b.prepare();await b.click("ma-confirm");assert.equal(api.effects,1);}
 }
});
