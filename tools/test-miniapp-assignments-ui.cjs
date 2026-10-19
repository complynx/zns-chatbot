"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/assignments.js"), "utf8");
class Element {
  constructor(tag = "div") { this.tagName = tag; this.children = []; this.listeners = {}; this.textContent = ""; this.value = ""; this.hidden = false; this.disabled = false; this.checked = false; }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; this.textContent = ""; }
  addEventListener(event, action) { this.listeners[event] = action; }
  querySelectorAll(tag) { return this.children.flatMap(n => [...(n.tagName === tag ? [n] : []), ...n.querySelectorAll(tag)]); }
}
function backend() {
  const b = { owner: randomUUID(), events: [], targets: [], proposals: new Map(), native: new Set(), writes: [], effects: 0, active: true, admin: true, lose: undefined, hook: undefined, pageSize: 20 };
  b.event = () => { const e = { id: randomUUID(), version: 3, document: { name: "Event <img src=x>", timezone: "Europe/Minsk" } }; b.events.push(e); return e; };
  b.target = event => { const t = { registration: { id: randomUUID(), event_id: event.id, user_id: randomUUID(), role: "leader", state: "waitlisted", version: 2 }, telegram_user_id: 123456, display_name: "Participant <script>" }; b.targets.push(t); return t; };
  b.proposal = (event, target, op = "loom/envelope:" + randomUUID()) => {
    const q = { registration_id: target.registration.id, price_minor: 12345, currency: "BYN", currency_scale: 2, tier: { id: "early", number: 1 }, deposit: { required_minor: 3000, final_due_at: new Date(Date.now() + 86400000).toISOString() } };
    const p = { original_operation_id: op, preparation: { proposal_id: randomUUID(), arguments: { event_id: event.id, registration_id: target.registration.id, expected_event_version: event.version, expected_version: target.registration.version }, description: "Assign unpaid leader pass to Telegram user 123456 for original event. Price 123.45 BYN. Payment window: 300 seconds. <img src=x>", expires_at: new Date(Date.now() + 300000).toISOString(), timezone: event.document.timezone, quotes: [q] }, state: "pending", observed_at: new Date().toISOString() };
    b.proposals.set(op, p); return p;
  };
  const page = (items, cursor, key) => { const all = items.sort((a, c) => key(a).localeCompare(key(c))).filter(x => !cursor || key(x) > cursor), result = all.slice(0, b.pageSize); return [result, all.length > result.length ? key(result.at(-1)) : undefined]; };
  b.request = async (path, method = "GET", body) => {
    if (b.hook) { const result = await b.hook(path, method, body); if (result !== undefined) return result; }
    if (!b.active) throw { status: 401 }; if (!b.admin) throw { status: 403 };
    const url = new URL(path, "https://fixture.invalid/app/"), route = url.pathname.replace("/app/pass-assignments/", ""), after = url.searchParams.get("after");
    if (method === "GET") {
      if (route === "events") { const [events, next] = page([...b.events], after, e => e.id); return structuredClone({ events, next }); }
      if (route.startsWith("events/")) { const [registrations, next] = page(b.targets.filter(t => t.registration.event_id === route.split("/")[1]), after, t => t.registration.id); return structuredClone({ registrations, next }); }
      if (route === "recent") return structuredClone({ proposals: [...b.proposals.values()].filter(p => b.native.has(p.original_operation_id)).slice(-20).reverse() });
      if (route === "proposals") { const [proposals, next] = page([...b.proposals.values()].filter(p => p.state === "pending" && Date.parse(p.preparation.expires_at) > Date.now()), after, p => p.preparation.proposal_id); return structuredClone({ proposals, next }); }
      if (route.startsWith("operations/")) { const p = b.proposals.get(decodeURIComponent(route.slice("operations/".length))); if (!p) throw { status: 404 }; if (p.state === "pending" && Date.parse(p.preparation.expires_at) <= Date.now()) p.state = "expired"; return structuredClone(p); }
      throw Error("Unexpected GET " + route);
    }
    b.writes.push(structuredClone({ route, body }));
    if (route === "prepare") {
      b.native.add(body.operation_id);
      let p = b.proposals.get(body.operation_id);
      if (!p) { const e = b.events.find(e => e.id === body.arguments.event_id), t = b.targets.find(t => t.registration.id === body.arguments.registration_id); if (!e || !t || e.version !== body.arguments.expected_event_version || t.registration.version !== body.arguments.expected_version) throw { status: 409 }; p = b.proposal(e, t, body.operation_id); }
      if (b.lose === "prepare") { b.lose = undefined; throw Error("Response lost after commit"); } return structuredClone(p.preparation);
    }
    if (route === "confirm") {
      b.native.add(body.operation_id);
      const p = b.proposals.get(body.operation_id); assert.equal(body.proposal_id, p.preparation.proposal_id); assert.equal(body.confirmed, true); assert.deepEqual(body.arguments, p.preparation.arguments);
      if (!p.result) { const t = b.targets.find(t => t.registration.id === p.preparation.arguments.registration_id); if (!t || t.registration.version !== p.preparation.arguments.expected_version || Date.parse(p.preparation.expires_at) <= Date.now()) throw { status: 409 }; t.registration.state = "assigned"; t.registration.version++; t.registration.pass = { id: randomUUID(), registration_id: t.registration.id, ...p.preparation.quotes[0], payment_due_at: new Date(Date.now() + 86400000).toISOString() }; p.result = structuredClone(t.registration); p.state = "completed"; b.effects++; }
      if (b.lose === "confirm") { b.lose = undefined; throw Error("Response lost after commit"); } return structuredClone(p.result);
    }
    throw Error("Unexpected write " + route);
  };
  return b;
}
function harness(b = backend(), store = new Map()) {
  const elements = new Map(), get = id => { if (!elements.has(id)) elements.set(id, new Element()); return elements.get(id); };
  let ui, denied = 0, failWrite = false, failRead = false;
  const storage = { getItem: key => { if (failRead) throw Error("Storage unavailable"); return store.get(key) || null; }, setItem: (key, value) => { if (failWrite) throw Error("Storage unavailable"); store.set(key, value); }, removeItem: key => { if (failWrite) throw Error("Storage unavailable"); store.delete(key); } };
  const window = {};
  runInNewContext(source, { window, document: { getElementById: get, createElement: tag => new Element(tag) }, sessionStorage: storage, crypto: { randomUUID }, Date: class extends Date { static now() { return b.now ?? Date.now(); } }, Intl, console });
  ui = window.createZinusyaAssignments({ request: b.request, setBusy: v => ui.setBusy(v), denied: () => { denied++; } });
  const h = { b, ui, store, get: id => get(id === "pass-assignments" ? id : "pa-" + id), text: id => { const flatten = n => n.textContent + n.children.map(flatten).join(" "); return flatten(h.get(id)); }, denied: () => denied, failStorage: () => { failWrite = true; }, failRead: () => { failRead = true; }, start: (owner = b.owner, locale = "en") => ui.start(owner, locale),
    click: async id => { const n = h.get(id); assert.equal(n.disabled, false, id + " disabled"); assert.equal(n.hidden, false, id + " hidden"); await n.listeners.click(); },
    select: async (id, value) => { const n = h.get(id); assert.equal(n.disabled, false, id + " disabled"); n.value = value; await n.listeners.change(); },
    consent: () => { h.get("consent").checked = true; h.get("consent").listeners.change(); },
    choose: async (index = 0) => { const n = h.get("pending").querySelectorAll("button")[index]; assert.ok(n); assert.equal(n.disabled, false); await n.listeners.click(); },
    async prepare() { const e = b.events[0], t = b.targets.find(t => t.registration.event_id === e.id); await h.select("event", e.id); assert.equal(h.get("target").value, ""); await h.select("target", t.registration.id); await h.click("prepare"); return { e, t }; }
  }; return h;
}
function fixture() { const h = harness(); const event = h.b.event(); h.b.target(event); return h; }

test("native solo assignment needs explicit event, participant, exact terms and consent", async () => {
  const h = fixture(); await h.start(); assert.equal(h.get("event").value, ""); assert.equal(h.get("prepare").disabled, true); const { t } = await h.prepare(); assert.equal(h.b.effects, 0); assert.equal(h.get("confirm").disabled, true); assert.match(h.text("preview"), /123\.45 BYN/); assert.match(h.text("preview"), /30\.00 BYN/); assert.match(h.text("preview"), /Europe\/Minsk/); assert.match(h.text("preview"), /<img src=x>/); assert.equal(h.get("preview").querySelectorAll("img").length, 0); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(t.registration.state, "assigned"); assert.match(h.text("receipt"), /historical/); assert.equal(h.get("confirm").hidden, true);
});
test("lost preparation response is recovered after reload without another write", async () => {
  const h = fixture(); await h.start(); h.b.lose = "prepare"; await h.prepare(); assert.equal(h.get("close").hidden, false); const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("confirm").disabled, true); reload.consent(); await reload.click("confirm"); assert.equal(h.b.writes.length, 2); assert.equal(h.b.effects, 1);
});
test("preparation never reaching server can be retried after reload with same IDs and old versions", async () => {
  const h = fixture(); await h.start(); let stop = true; h.b.hook = async (path, method) => { if (stop && method === "POST" && path.endsWith("prepare")) { stop = false; throw Error("Did not arrive"); } }; await h.prepare(); const before = JSON.parse([...h.store.values()][0]); const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("retry").hidden, false); await reload.click("retry"); assert.equal(h.b.writes[0].body.operation_id, before.operation_id); assert.deepEqual(h.b.writes[0].body.arguments, before.arguments); assert.equal(h.b.effects, 0);
});
test("lost confirmation recovers one historical result without another confirmation", async () => {
  const h = fixture(); await h.start(); await h.prepare(); h.consent(); h.b.lose = "confirm"; await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.get("close").hidden, true); const reload = harness(h.b, h.store); await reload.start(); assert.match(reload.text("receipt"), /historical/); assert.equal(h.b.writes.length, 2); await reload.click("close"); assert.equal(h.store.size, 0);
});
test("agent proposal uses original opaque operation and immutable timezone despite current event changes", async () => {
  const h = fixture(), e = h.b.events[0], p = h.b.proposal(e, h.b.targets[0]); e.document.timezone = "Asia/Yekaterinburg"; await h.start(); await h.choose(); assert.match(h.text("preview"), /Europe\/Minsk/); assert.doesNotMatch(h.text("preview"), /Asia\/Yekaterinburg/); assert.equal(h.b.writes.length, 0); h.consent(); await h.click("confirm"); assert.equal(h.b.writes[0].body.operation_id, p.original_operation_id); assert.equal(h.b.effects, 1);
});
test("legacy proposal without stored timezone explicitly displays UTC", async () => {
  const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); delete p.preparation.timezone; await h.start(); await h.choose(); assert.match(h.text("preview"), /UTC/); assert.doesNotMatch(h.text("preview"), /Europe\/Minsk/);
});
test("expired or stale proposals cannot cause a new assignment", async () => {
  const h = fixture(); await h.start(); await h.prepare(); const p = [...h.b.proposals.values()][0]; h.b.now = Date.parse(p.preparation.expires_at) + 1; h.consent(); assert.equal(h.get("confirm").disabled, true); assert.equal(h.b.effects, 0);
  h.b.now = undefined; h.b.targets[0].registration.version++; h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 0); assert.match(h.text("status"), /changed/); assert.equal(h.get("close").hidden, true);
});
test("storage failures and corrupt identifiers block all new writes", async () => {
  const h = fixture(); await h.start(); h.failStorage(); await h.prepare(); assert.equal(h.b.writes.length, 0); assert.equal(h.get("prepare").disabled, true); assert.match(h.text("status"), /identifiers/);
  const b = backend(); b.event(); const broken = harness(b, new Map([["zinusya.pass-assignments." + b.owner, '{"owner":"wrong"}']])); await broken.start(); assert.equal(broken.get("reload").disabled, true); assert.equal(b.writes.length, 0);
});
test("journal contains only operation and concurrency identifiers, never names, terms, prices or credentials", async () => {
  const h = fixture(); await h.start(); await h.prepare(); const raw = [...h.store.values()][0]; assert.doesNotMatch(raw, /Participant|Price|description|token|12345|Europe|passport/); assert.deepEqual(Object.keys(JSON.parse(raw)).sort(), ["arguments", "operation_id", "owner", "phase", "proposal_id"]);
});
test("current permission loss hides details but preserves same-owner recovery", async () => {
  for (const status of [401, 403]) { const h = fixture(); await h.start(); await h.prepare(); if (status === 401) h.b.active = false; else h.b.admin = false; await h.click("recover"); assert.equal(h.get("pass-assignments").hidden, true); assert.equal(h.store.size, 1); assert.equal(h.denied(), status === 401 ? 1 : 0); h.b.active = h.b.admin = true; await h.start(); assert.equal(h.get("confirm").disabled, true); }
});
test("owner change fences late data and late unauthorized errors", async () => {
  for (const deny of [false, true]) { const h = fixture(); let release, intercepted = false; const wait = new Promise(r => { release = r; }); h.b.hook = async path => { if (!intercepted && path.endsWith("events")) { intercepted = true; await wait; if (deny) throw { status: 401 }; return { events: [] }; } }; const old = h.start(); await h.start(randomUUID()); release(); await old; assert.equal(h.denied(), 0); assert.equal(h.get("pass-assignments").hidden, false); assert.equal(h.b.effects, 0); }
});
test("paging is read-only and no event or recipient is selected automatically", async () => {
  const h = fixture(); const second = h.b.event(); h.b.target(h.b.events[0]); h.b.target(second); h.b.target(second); h.b.pageSize = 1; await h.start(); const first = h.get("event").children[1].value; await h.click("events-next"); assert.notEqual(h.get("event").children[1].value, first); assert.equal(h.get("event").value, ""); await h.click("events-first"); assert.equal(h.get("event").children[1].value, first); await h.select("event", h.get("event").children[1].value); assert.equal(h.get("target").value, ""); await h.click("targets-next"); assert.equal(h.get("target").value, ""); h.ui.setLanguage("be"); assert.equal(h.get("confirm").textContent, "Назначить этот пасс"); assert.equal(h.b.writes.length, 0);
});
test("each request is journaled before POST and closing a known preview is local only", async () => {
  const h = fixture(); await h.start(); h.b.hook = async (_path, method, body) => { if (method === "POST") { const r = JSON.parse([...h.store.values()][0]); assert.equal(r.operation_id, body.operation_id); assert.equal(r.phase, body.confirmed ? "confirm" : "prepare"); } }; await h.prepare(); const count = h.b.proposals.size; await h.click("close"); assert.equal(h.b.proposals.size, count); assert.equal(h.b.effects, 0); assert.equal(h.b.writes.length, 1);
});

test("confirmation not reaching server is repeated only after explicit consent with same operation", async () => {
  const h = fixture(); await h.start(); await h.prepare(); let stop = true; h.b.hook = async (path, method) => { if (stop && method === "POST" && path.endsWith("confirm")) { stop = false; throw Error("Did not arrive"); } }; h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 0); assert.equal(h.get("close").hidden, true); await h.click("recover"); assert.equal(h.get("confirm").disabled, true); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.b.writes[0].body.operation_id, h.b.writes[1].body.operation_id);
});
test("a pending proposal on another event page can be recovered without selecting a current event", async () => {
  const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); h.b.events = []; await h.start(); await h.choose(); assert.match(h.text("preview"), /Europe\/Minsk/); assert.equal(h.get("event").value, ""); assert.equal(h.get("confirm").disabled, true); h.consent(); await h.click("confirm"); assert.equal(h.b.writes[0].body.operation_id, p.original_operation_id); assert.equal(h.b.effects, 1);
});
test("large safe integer prices retain exact minor units", async () => {
  const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); p.preparation.quotes[0].price_minor = Number.MAX_SAFE_INTEGER; await h.start(); await h.choose(); assert.match(h.text("preview"), /90071992547409\.91 BYN/);
});

test("stale preparation can be closed before a fresh request even without a saved proposal", async () => {
  const h = fixture(); await h.start(); const event = h.b.events[0], target = h.b.targets[0]; await h.select("event", event.id); await h.select("target", target.registration.id); target.registration.version++; await h.click("prepare");
  const old = JSON.parse([...h.store.values()][0]); assert.equal(old.phase, "prepare"); assert.equal(h.get("close").hidden, false);
  const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("close").hidden, false); await reload.click("close"); assert.equal(h.store.size, 0); await reload.prepare(); assert.equal(h.b.writes.length, 2); assert.notEqual(h.b.writes[1].body.operation_id, old.operation_id); assert.equal(h.b.writes[1].body.arguments.expected_version, target.registration.version); assert.equal(h.b.effects, 0);
});
test("unknown preparation can be closed locally while a late saved proposal remains discoverable", async () => {
  const h = fixture(); await h.start(); let pendingArrival = true; h.b.hook = async (path, method) => { if (pendingArrival && method === "POST" && path.endsWith("prepare")) { pendingArrival = false; throw Error("Response lost while request still in flight"); } }; await h.prepare(); const old = JSON.parse([...h.store.values()][0]); assert.equal(h.get("close").hidden, false); await h.click("close"); assert.equal(h.store.size, 0); assert.equal(h.b.effects, 0); assert.equal(h.get("pending").querySelectorAll("button").length, 0);
  h.b.proposal(h.b.events[0], h.b.targets[0], old.operation_id); await h.click("reload"); assert.equal(h.get("pending").querySelectorAll("button").length, 1); await h.choose(); assert.equal(JSON.parse([...h.store.values()][0]).operation_id, old.operation_id); assert.equal(h.get("confirm").disabled, true); assert.equal(h.b.effects, 0);
});
test("failed local discard storage keeps preparation blocked and original journal intact", async () => {
  const h = fixture(); await h.start(); let stop = true; h.b.hook = async (path, method) => { if (stop && method === "POST" && path.endsWith("prepare")) { stop = false; throw Error("Unknown result"); } }; await h.prepare(); const old = [...h.store.values()][0]; h.failStorage(); await h.click("close"); assert.equal(h.get("close").disabled, true); assert.equal(h.get("prepare").disabled, true); assert.equal([...h.store.values()][0], old); const reload = harness(h.b, h.store); await reload.start(); await reload.click("close"); assert.equal(h.store.size, 0); assert.equal(h.b.effects, 0);
});
test("revoked recipient preparation 404 can be closed before selecting a different participant", async () => {
  const h = fixture(); await h.start(); let revoked = true; h.b.hook = async (path, method) => { if (revoked && method === "POST" && path.endsWith("prepare")) throw { status: 404 }; }; await h.prepare(); const old = JSON.parse([...h.store.values()][0]); assert.equal(h.get("close").hidden, false); await h.click("close"); revoked = false; h.b.targets = []; const replacement = h.b.target(h.b.events[0]); await h.click("events-first"); await h.prepare(); assert.equal(h.b.writes.length, 1); assert.equal(h.b.writes[0].body.arguments.registration_id, replacement.registration.id); assert.notEqual(h.b.writes[0].body.operation_id, old.operation_id); assert.equal(h.b.effects, 0);
});
test("restored administrator can close a preparation missing after earlier role rejection", async () => {
  const h = fixture(); await h.start(); let denied = true; h.b.hook = async (path, method) => { if (denied && method === "POST" && path.endsWith("prepare")) throw { status: 403 }; }; await h.prepare(); assert.equal(h.get("pass-assignments").hidden, true); assert.equal(h.store.size, 1); denied = false; await h.start(); assert.equal(h.get("close").hidden, false); await h.click("close"); assert.equal(h.store.size, 0); assert.equal(h.b.effects, 0);
});
test("initial events failure has a visible refresh path that restores normal preparation", async () => {
  const h = fixture(); let fail = true; h.b.hook = async path => { if (fail && path.endsWith("events")) { fail = false; throw Error("Events unavailable"); } }; await h.start(); assert.equal(h.get("events-first").hidden, false); assert.equal(h.get("prepare").disabled, true); await h.click("events-first"); await h.prepare(); assert.equal(h.b.writes.length, 1); assert.equal(h.get("confirm").disabled, true);
});
test("event version rejection can be closed and refreshed before a fresh preparation", async () => {
  const h = fixture(); await h.start(); const event = h.b.events[0]; await h.select("event", event.id); await h.select("target", h.b.targets[0].registration.id); event.version++; await h.click("prepare"); const old = JSON.parse([...h.store.values()][0]); assert.equal(old.phase, "prepare"); await h.click("close"); assert.equal(h.get("events-first").hidden, false); await h.click("events-first"); await h.prepare(); assert.equal(h.b.writes.length, 2); assert.equal(h.b.writes[1].body.arguments.expected_event_version, event.version); assert.notEqual(h.b.writes[1].body.operation_id, old.operation_id);
});
test("fresh browsing session discovers original completed native receipt after lost confirmation", async () => {
  const h = fixture(); await h.start(); await h.prepare(); const original = [...h.b.proposals.values()][0]; h.consent(); h.b.lose = "confirm"; await h.click("confirm"); assert.equal(h.b.effects, 1);
  const fresh = harness(h.b); await fresh.start(); assert.equal(fresh.get("pending").querySelectorAll("button").length, 0); await fresh.click("recent"); assert.equal(fresh.get("pending-title").textContent, "Recent form operations"); await fresh.choose(); assert.match(fresh.text("receipt"), /historical/); assert.equal(fresh.get("confirm").hidden, true); assert.equal(JSON.parse([...fresh.store.values()][0]).operation_id, original.original_operation_id); assert.equal(h.b.writes.length, 2); assert.equal(h.b.effects, 1);
});
test("recent pending operation needs fresh consent and unfinished reload restores list mode", async () => {
  const h = fixture(); await h.start(); await h.prepare(); const fresh = harness(h.b); await fresh.start(); await fresh.click("recent"); await fresh.choose(); assert.equal(fresh.get("confirm").disabled, true); assert.equal(h.b.effects, 0); await fresh.click("close"); await fresh.click("recent"); await fresh.click("reload"); assert.equal(fresh.get("pending-title").textContent, "Unfinished assignments"); assert.equal(h.b.writes.length, 1);
});
