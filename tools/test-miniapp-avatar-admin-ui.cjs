"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/avatar-admin.js"), "utf8");
class Element {
  constructor(tag = "div") { this.tagName = tag; this.children = []; this.listeners = {}; this.textContent = ""; this.value = ""; this.hidden = false; this.disabled = false; }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; this.textContent = ""; }
  addEventListener(event, action) { this.listeners[event] = action; }
  querySelectorAll(tag) { return this.children.flatMap(n => [ ...(n.tagName === tag ? [n] : []), ...n.querySelectorAll(tag) ]); }
}
function backend() {
  const b = { owner: randomUUID(), proposals: new Map(), deliveries: new Map(), writes: [], effects: 0, active: true, admin: true, lose: undefined, hook: undefined, pageSize: 20 };
  b.delivery = () => { const d = { id: randomUUID(), artifact_id: randomUUID(), user_id: randomUUID(), telegram_user_id: 123456, state: "delivery_unknown", version: 3, attempt_id: randomUUID(), receipt: {} }; b.deliveries.set(d.id, d); return d; };
  b.prepare = (d, action = "dismiss", reason = "Checked the chat") => { const p = { operation_id: randomUUID(), proposal_id: randomUUID(), arguments: { delivery_id: d.id, expected_version: d.version, action, reason }, delivery: structuredClone(d), expires_at: new Date(Date.now() + 300000).toISOString(), state: "pending" }; b.proposals.set(p.operation_id, p); return p; };
  b.request = async (path, method = "GET", body) => {
    if (b.hook) { const result = await b.hook(path, method, body); if (result !== undefined) return result; }
    if (!b.active) throw { status: 401 }; if (!b.admin) throw { status: 403 };
    const url = new URL(path, "https://fixture.invalid/app/"); const route = url.pathname.replace("/app/avatar-admin/", "");
    if (method === "GET") {
      if (route === "pending") { const all = [...b.proposals.values()].filter(p => p.state === "pending" && Date.parse(p.expires_at) > Date.now()).sort((a, c) => a.proposal_id.localeCompare(c.proposal_id)); const items = all.filter(p => !url.searchParams.get("after") || p.proposal_id > url.searchParams.get("after")); const page = items.slice(0, b.pageSize); return structuredClone({ items: page, next_cursor: items.length > page.length ? page.at(-1).proposal_id : undefined }); }
      if (route.startsWith("deliveries/")) { const d = b.deliveries.get(route.split("/")[1]); if (!d) throw { status: 404 }; return structuredClone(d); }
      if (route.startsWith("operations/")) { const p = b.proposals.get(route.split("/")[1]); if (!p) throw { status: 404 }; if (p.state === "pending" && Date.parse(p.expires_at) <= Date.now()) p.state = "expired"; return structuredClone(p); }
      throw Error("Unexpected GET " + route);
    }
    b.writes.push(structuredClone({ route, body }));
    if (route === "prepare") {
      const d = b.deliveries.get(body.arguments.delivery_id); if (!d || d.version !== body.arguments.expected_version) throw { status: 409 };
      let p = b.proposals.get(body.operation_id); if (!p) { p = b.prepare(d, body.arguments.action, body.arguments.reason.trim()); b.proposals.delete(p.operation_id); p.operation_id = body.operation_id; b.proposals.set(p.operation_id, p); }
      if (b.lose === "prepare") { b.lose = undefined; throw Error("Response lost after commit"); }
      const { operation_id, state, ...preparation } = p; return structuredClone(preparation);
    }
    if (route === "confirm") {
      const p = b.proposals.get(body.operation_id); assert.equal(body.proposal_id, p.proposal_id); assert.equal(body.confirmed, true); assert.deepEqual(body.arguments, p.arguments);
      if (!p.result) { const d = b.deliveries.get(p.arguments.delivery_id); if (Date.parse(p.expires_at) <= Date.now() || d.version !== p.arguments.expected_version) throw { status: 409 }; p.result = { delivery_id: d.id, recipient_id: d.user_id, previous_version: d.version, state: p.arguments.action === "retry" ? "pending" : "dismissed", version: ++d.version }; d.state = p.result.state; p.state = "completed"; b.effects++; }
      if (b.lose === "confirm") { b.lose = undefined; throw Error("Response lost after commit"); } return structuredClone(p.result);
    }
    throw Error("Unexpected mutation " + route);
  };
  return b;
}
function harness(b = backend(), store = new Map()) {
  const elements = new Map(); const get = id => { if (!elements.has(id)) elements.set(id, new Element()); return elements.get(id); };
  let ui; let denied = 0; let failStorage = false; let failRead = false;
  const storage = { getItem: key => { if (failRead) throw Error("Storage unavailable"); return store.get(key) || null; }, setItem: (key, value) => { if (failStorage) throw Error("Storage failed"); store.set(key, value); }, removeItem: key => { if (failStorage) throw Error("Storage failed"); store.delete(key); } };
  const sandbox = { window: {}, document: { getElementById: get, createElement: tag => new Element(tag) }, sessionStorage: storage, crypto: { randomUUID }, Date: class extends Date { static now() { return b.now ?? Date.now(); } }, Intl, console };
  runInNewContext(source, sandbox);
  ui = sandbox.window.createZinusyaAvatarAdmin({ request: b.request, setBusy: v => ui.setBusy(v), denied: () => { denied++; } });
  const h = { b, ui, store, get: id => get(id === "avatar-admin" ? id : "aa-" + id), text: id => { const flatten = n => n.textContent + n.children.map(flatten).join(" "); return flatten(h.get(id)); }, denied: () => denied,
    failStorage: () => { failStorage = true; }, failRead: () => { failRead = true; }, start: (owner = b.owner, locale = "en") => ui.start(owner, locale),
    input: (id, value) => { const n = h.get(id); n.value = value; n.listeners.input?.(); },
    click: async id => { const n = h.get(id); assert.equal(n.disabled, false, id + " disabled"); assert.equal(n.hidden, false, id + " hidden"); await n.listeners.click(); },
    choose: async (index = 0) => { const n = h.get("pending").querySelectorAll("button")[index]; assert.ok(n); assert.equal(n.disabled, false); await n.listeners.click(); },
    async prepare(action = "dismiss") { const d = b.delivery(); h.input("delivery", d.id); await h.click("inspect"); h.input("reason", "Checked chat <img src=x onerror=evil()> "); h.get("action").value = action; await h.click("prepare"); return d; }
  }; return h;
}
for (const action of ["retry", "dismiss"]) test("explicit " + action + " prepares exact arguments, confirms once and separates current state from receipt", async () => {
  const h = harness(); await h.start(); const d = await h.prepare(action); assert.equal(h.b.effects, 0); assert.match(h.text("preview"), /<img src=x/); assert.equal(h.get("preview").querySelectorAll("img").length, 0); assert.match(h.text("preview"), /UTC/);
  await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(d.version, 4); assert.match(h.text("receipt"), /Earlier operation/); d.state = "delivered"; d.version++; await h.click("recover"); assert.match(h.text("current"), /delivered.*5/); assert.match(h.text("receipt"), /4/); assert.equal(h.b.writes.length, 2);
});
test("lost prepare response recovers the same original proposal after reload, without another POST", async () => {
  const h = harness(); await h.start(); h.b.lose = "prepare"; await h.prepare(); const key = "zinusya.avatar-admin." + h.b.owner; const r = JSON.parse(h.store.get(key)); assert.deepEqual(Object.keys(r).sort(), ["operation_id", "owner", "phase"]); assert.equal(r.phase, "prepare");
  const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("confirm").disabled, false); assert.equal(h.b.writes.length, 1); await reload.click("confirm"); assert.equal(h.b.effects, 1);
});
test("lost confirmation response recovers immutable result after reload with no additional mutation", async () => {
  const h = harness(); await h.start(); await h.prepare(); h.b.lose = "confirm"; await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.get("close").hidden, true); assert.match(h.text("status"), /unknown/);
  const reload = harness(h.b, h.store); await reload.start(); assert.match(reload.text("receipt"), /Earlier operation/); assert.equal(reload.get("confirm").hidden, true); assert.equal(h.b.writes.length, 2); await reload.click("close"); assert.equal(h.store.size, 0);
});
test("agent-origin pending proposal is discovered and confirmed using its original operation and deadline", async () => {
  const h = harness(); const p = h.b.prepare(h.b.delivery()); await h.start(); await h.choose(); assert.match(h.text("preview"), new RegExp(p.expires_at.replaceAll(".", "\\."))); await h.click("confirm"); assert.equal(h.b.writes[0].body.operation_id, p.operation_id); assert.equal(h.b.writes.length, 1); assert.equal(h.b.effects, 1);
});
test("expired proposal and changed current delivery cannot be confirmed", async () => {
  const h = harness(); const d = h.b.delivery(), p = h.b.prepare(d); await h.start(); await h.choose(); p.expires_at = new Date(Date.now() - 1000).toISOString(); await h.click("recover"); assert.equal(h.get("confirm").hidden, true); assert.match(h.text("preview"), /expired/); assert.equal(h.b.effects, 0);
  await h.click("close"); const p2 = h.b.prepare(d); await h.click("reload"); d.version++; await h.choose(); assert.equal(h.get("confirm").disabled, true); assert.match(h.text("preview"), /differs/); assert.equal(h.b.effects, 0); assert.ok(p2);
});
test("storage failure prevents POST; corrupt identifiers block mutation", async () => {
  const h = harness(); await h.start(); h.failStorage(); await h.prepare(); assert.equal(h.b.writes.length, 0); assert.equal(h.get("prepare").disabled, true); assert.match(h.text("status"), /identifiers/);
  const b = backend(); const broken = harness(b, new Map([["zinusya.avatar-admin." + b.owner, '{"owner":"wrong"}']])); await broken.start(); assert.equal(broken.get("prepare").disabled, true); assert.match(broken.text("status"), /cannot be read/);
});
test("recovery storage has no reason, receipt or session credential", async () => {
  const h = harness(); await h.start(); await h.prepare(); const raw = [...h.store.values()][0]; assert.equal(Object.keys(JSON.parse(raw)).length, 4); assert.doesNotMatch(raw, /reason|receipt|token|Checked chat/);
});
test("owner change fences late fulfilled responses and late 401 without denying the new account", async () => {
  for (const reject of [false, true]) {
    const h = harness(); let release; const wait = new Promise(r => { release = r; }); let intercepted = false;
    h.b.hook = async path => { if (!intercepted && path.endsWith("pending")) { intercepted = true; await wait; if (reject) throw { status: 401 }; return { items: [h.b.prepare(h.b.delivery())] }; } };
    const old = h.start(); const second = randomUUID(); await h.start(second); release(); await old; assert.equal(h.denied(), 0); assert.equal(h.get("pending").querySelectorAll("button").length, 0); assert.equal(h.get("avatar-admin").hidden, false);
  }
});
test("current role loss hides details and retains owner-bound pending recovery", async () => {
  const h = harness(); await h.start(); await h.prepare(); h.b.admin = false; await h.click("recover"); assert.equal(h.get("avatar-admin").hidden, true); assert.equal(h.store.size, 1); assert.equal(h.denied(), 0); h.b.admin = true; await h.start(); assert.equal(h.get("confirm").disabled, false); assert.equal(h.b.effects, 0);
});
test("pending pagination and language changes only read; local close does not dismiss server proposal", async () => {
  const h = harness(); h.b.pageSize = 1; h.b.prepare(h.b.delivery()); h.b.prepare(h.b.delivery()); await h.start(); const first = h.text("pending"); await h.click("next"); assert.notEqual(h.text("pending"), first); await h.click("first"); assert.equal(h.text("pending"), first); h.ui.setLanguage("ru"); assert.equal(h.get("confirm").textContent, "Подтвердить именно это изменение"); await h.choose(); await h.click("close"); assert.equal(h.b.writes.length, 0); assert.equal(h.b.proposals.size, 2);
});
test("identifiers are durably saved before either POST leaves the browser", async () => {
  const h = harness(); await h.start(); h.b.hook = async (_path, method, body) => { if (method === "POST") { const record = JSON.parse([...h.store.values()][0]); assert.equal(record.operation_id, body.operation_id); assert.equal(record.phase, body.confirmed ? "confirm" : "prepare"); } }; await h.prepare(); await h.click("confirm"); assert.equal(h.b.effects, 1);
});

test("unreadable recovery storage cannot be bypassed by reloading the pending list", async () => {
  const h = harness(); h.failRead(); await h.start(); assert.equal(h.get("reload").disabled, true); assert.equal(h.get("prepare").disabled, true); assert.equal(h.b.writes.length, 0); assert.match(h.text("status"), /cannot be read/);
});

test("an interrupted confirmation can be explicitly repeated with identical identifiers and arguments", async () => {
  const h = harness(); await h.start(); await h.prepare(); let interrupt = true;
  h.b.hook = async (path, method) => { if (interrupt && method === "POST" && path.endsWith("confirm")) { interrupt = false; throw Error("Request never arrived"); } };
  await h.click("confirm"); assert.equal(h.b.effects, 0); assert.equal(h.get("close").hidden, true); assert.equal(h.get("confirm").textContent, "Retry this exact confirmation"); await h.click("recover"); await h.click("confirm"); assert.equal(h.b.effects, 1); const p = [...h.b.proposals.values()][0]; assert.equal(h.b.writes.at(-1).body.operation_id, p.operation_id); assert.equal(h.b.writes.at(-1).body.proposal_id, p.proposal_id);
});
test("recovered original action and reason fill the frozen fields", async () => {
  const h = harness(); const p = h.b.prepare(h.b.delivery(), "dismiss"); await h.start(); await h.choose(); assert.equal(h.get("action").value, p.arguments.action); assert.equal(h.get("reason").value, p.arguments.reason); assert.equal(h.get("reason").disabled, true);
});

test("clicking a form left open past expiry explains why confirmation is blocked", async () => {
  const h = harness(); await h.start(); await h.prepare(); const p = [...h.b.proposals.values()][0]; assert.equal(h.get("confirm").disabled, false); h.b.now = Date.parse(p.expires_at) + 1; await h.click("confirm"); assert.match(h.text("status"), /expired/); assert.match(h.text("preview"), /expired/); assert.equal(h.get("confirm").disabled, true); assert.equal(h.b.effects, 0); assert.equal(h.b.writes.length, 1);
});

for (const state of ["delivery_failed", "delivery_blocked"]) test("actual API state " + state + " allows explicit repair", async () => {
  const h = harness(); const d = h.b.delivery(); d.state = state; await h.start(); h.input("delivery", d.id); await h.click("inspect"); h.input("reason", "Checked delivery failure"); h.get("action").value = "dismiss"; await h.click("prepare"); assert.equal(h.get("confirm").disabled, false); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(d.state, "dismissed");
});
