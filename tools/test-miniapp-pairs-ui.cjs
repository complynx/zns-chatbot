"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/pairs.js"), "utf8");
const owner = randomUUID(), partner = randomUUID(), eventID = randomUUID(), invitationID = randomUUID();
const copy = v => JSON.parse(JSON.stringify(v));
const key = id => "zinusya-pair-pending-v1:" + id;
function backend() {
  const invitation = { id: invitationID, event_id: eventID, target_user_id: owner, state: "pending", version: 1, expires_at: "2026-10-25T01:30:00Z" };
  const api = { owned: [], receipts: new Map(), context: { event_id: eventID, event_name: "Event <script>", timezone: "Europe/Berlin", role: "leader", expected_version: 1, expires_at: invitation.expires_at }, rows: [{ invitation, event_name: "Event <script>", inviter_name: "Name <img>", timezone: "Europe/Berlin", required_role: "follower" }] };
  api.base = (path, method, body) => {
    if (path.startsWith("./pairs/events") && !path.endsWith("/context")) return { events: [{ id: eventID, name: "Event <script>" }] };
    if (path.endsWith("/context")) return api.context;
    if (path.startsWith("./pairs/my")) return { pairs: api.owned };
    if (path.startsWith("./pairs/invitations")) return { invitations: api.rows };
    if (path.includes("/operations/")) { const r = api.receipts.get(path.split("/").at(-1).split("?")[0]); if (!r) throw { status: 404 }; return r; }
    if (method === "POST") {
      if (api.receipts.has(body.operation_id)) return api.receipts.get(body.operation_id);
      const kind = path.split("/").at(-1);
      if (kind === "dissolve") {
        const row = api.owned.find(v => v.pair.id === body.arguments.pair_id);
        if (!row || row.pair.version !== body.arguments.expected_version) throw { status: 409 };
        const result = { ...row.pair, state: "dissolved", version: row.pair.version + 1 };
        api.owned = api.owned.filter(v => v !== row); api.receipts.set(body.operation_id, result); return result;
      }
      const result = { invitation: { ...invitation, target_user_id: kind === "invite" ? body.arguments.target_user_id : owner, state: { invite: "pending", accept: "accepted", decline: "declined", revoke: "revoked" }[kind], version: 2 } };
      api.receipts.set(body.operation_id, result); return result;
    }
    throw new Error("Unexpected request " + path);
  };
  return api;
}
function browser(api, { storage = new Map(), failSet = false, failRemove = false } = {}) {
  const elements = new Map(), calls = [];
  function element(tag = "div") {
    const e = { tag, children: [], value: "", checked: false, disabled: false, hidden: false, textContent: "", listeners: {},
      append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { this.children = nodes; this.textContent = ""; }, addEventListener(name, fn) { this.listeners[name] = fn; },
      querySelectorAll() { return this.children.flatMap(c => [c, ...c.querySelectorAll()]).filter(c => c.tag === "button" || c.tag === "input"); }
    };
    Object.defineProperty(e, "options", { get: () => e.children });
    Object.defineProperty(e, "innerHTML", { set() { throw new Error("Unsafe HTML"); } });
    return e;
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element()); return elements.get(id); };
  let controller;
  const context = { window: {}, document: { getElementById: get, createElement: element, createTextNode(text) { const n = element("text"); n.textContent = text; return n; } },
    crypto: { randomUUID }, Intl, sessionStorage: { getItem: k => storage.get(k) || null, setItem(k, v) { if (failSet) throw Error("full"); storage.set(k, v); }, removeItem(k) { if (failRemove) throw Error("blocked"); storage.delete(k); } } };
  runInNewContext(source, context);
  controller = context.window.createZinusyaPairs({ request: async (path, method, body) => { calls.push({ path, method, body: body && copy(body) }); return copy(await (api.hook ? api.hook(path, method, body, api.base) : api.base(path, method, body))); }, setBusy: b => controller.setBusy(b), denied: () => controller.clear() });
  const click = (id, event = "click") => get(id).listeners[event]();
  return { get, calls, storage, controller, click, start: (id = owner, language = "en") => controller.start(id, language),
    async invite() { get("pairs-event").value = eventID; await click("pairs-event", "change"); get("pairs-target").value = partner; await click("pairs-invite"); },
    buttons: () => get("pairs-list").querySelectorAll().filter(v => v.tag === "button"),
    checkbox: () => get("pairs-list").querySelectorAll().find(v => v.tag === "input") };
}
const writes = b => b.calls.filter(v => v.method === "POST");
test("native invitation loads fresh role, holds only after explicit send and keeps text inert", async () => {
  const api = backend(), b = browser(api); await b.start(); assert.equal(writes(b).length, 0);
  assert.equal(b.get("pairs-code").textContent, owner);
  assert.ok(b.get("pairs-list").children[0].children[0].textContent.includes("Name <img>"));
  await b.invite(); assert.equal(writes(b).length, 1); assert.equal(writes(b)[0].body.arguments.target_user_id, partner);
  assert.equal(b.storage.size, 0); assert.match(b.get("pairs-receipt").textContent, /Pending/);
  assert.match(b.get("pairs-note").textContent, /not proof/);
});
test("accept requires an explicit complementary-role checkbox; decline needs no acceptance", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.buttons()[0].listeners.click(); assert.equal(writes(b).length, 0);
  b.checkbox().checked = true; await b.buttons()[0].listeners.click(); assert.equal(writes(b)[0].body.arguments.role, "follower");
  assert.match(b.get("pairs-receipt").textContent, /Accepted/);
  const c = browser(backend()); await c.start(); await c.buttons()[1].listeners.click(); assert.equal(writes(c)[0].path, "./pairs/decline");
});
test("lost committed response recovers original receipt after reauthentication without a second write", async () => {
  const api = backend(), storage = new Map(), b = browser(api, { storage }); let lost = false;
  api.hook = (p, m, body, base) => { const result = base(p, m, body); if (m === "POST" && !lost) { lost = true; throw { status: 503 }; } return result; };
  await b.start(); await b.invite(); const original = writes(b)[0].body; assert.equal(storage.size, 1); assert.equal(b.get("pairs-invite").disabled, true);
  const recovered = browser(api, { storage }); await recovered.start(); assert.equal(writes(recovered).length, 0); assert.equal(storage.size, 0); assert.equal(api.receipts.size, 1);
  assert.ok(recovered.calls.some(v => v.path.includes(original.operation_id)));
});
test("unknown uncommitted response retries byte-equivalent original request, even after reload", async () => {
  const api = backend(), storage = new Map(), b = browser(api, { storage }); let fail = true;
  api.hook = (p, m, body, base) => { if (m === "POST" && fail) { fail = false; throw { status: 503 }; } return base(p, m, body); };
  await b.start(); await b.invite(); const original = writes(b)[0]; const c = browser(api, { storage }); await c.start();
  assert.equal(c.get("pairs-retry").hidden, false); await c.click("pairs-retry"); assert.deepEqual(writes(c)[0], original); assert.equal(storage.size, 0);
});
test("HTTP200 expired result is shown as expired and never as accepted", async () => {
  const api = backend(); api.hook = (p, m, body, base) => { const result = base(p, m, body); if (m === "POST") result.invitation.state = "expired"; return result; };
  const b = browser(api); await b.start(); b.checkbox().checked = true; await b.buttons()[0].listeners.click(); assert.match(b.get("pairs-receipt").textContent, /Expired/); assert.doesNotMatch(b.get("pairs-receipt").textContent, /Accepted/);
});
test("storage failure prevents writes; corrupt owner-bound recovery blocks new actions", async () => {
  const b = browser(backend(), { failSet: true }); await b.start(); await b.invite(); assert.equal(writes(b).length, 0); assert.match(b.get("pairs-status").textContent, /storage/);
  const c = browser(backend(), { storage: new Map([[key(owner), '{"owner":"foreign"}']]) }); await c.start(); assert.equal(c.get("pairs-invite").disabled, true); assert.equal(c.calls.length, 0);
});
test("fresh registration version change requires a second explicit send", async () => {
  const api = backend(), b = browser(api); await b.start(); b.get("pairs-event").value = eventID; await b.click("pairs-event", "change"); b.get("pairs-target").value = partner;
  api.context.expected_version++; await b.click("pairs-invite"); assert.equal(writes(b).length, 0); assert.match(b.get("pairs-status").textContent, /changed/);
  await b.click("pairs-invite"); assert.equal(writes(b)[0].body.arguments.expected_version, 2);
});
test("outgoing revoke and Russian event-zone display", async () => {
  const api = backend(), b = browser(api); await b.start(owner, "ru"); b.get("pairs-direction").value = "outgoing"; await b.click("pairs-direction", "change");
  assert.equal(b.buttons().length, 1); await b.buttons()[0].listeners.click(); assert.equal(writes(b)[0].path, "./pairs/revoke"); assert.match(b.get("pairs-receipt").textContent, /Отозвано/);
  assert.ok(b.get("pairs-list").children[0].children[0].textContent.includes("Europe/Berlin"));
});
test("logout during a request prevents a late response from revealing the previous owner", async () => {
  const api = backend(), b = browser(api); let release; api.hook = (p, m, body, base) => p.startsWith("./pairs/events") ? new Promise(r => { release = () => r(base(p, m, body)); }) : base(p, m, body);
  const start = b.start(); await Promise.resolve(); b.controller.clear(); release(); await start;
  assert.equal(b.get("pairs").hidden, true); assert.equal(b.get("pairs-code").textContent, ""); assert.equal(b.get("pairs-list").children.length, 0);
});

test("nonexistent partner POST404 can be checked and closed before correcting the code", async () => {
  const api = backend(); let reject = true;
  api.hook = (p, m, body, base) => { if (m === "POST" && reject) throw { status: 404 }; return base(p, m, body); };
  const b = browser(api); await b.start(); await b.invite(); const old = writes(b)[0].body.operation_id;
  assert.equal(b.get("pairs-discard").hidden, false); await b.click("pairs-discard");
  assert.equal(b.storage.size, 0); assert.equal(b.get("pairs-target").disabled, false);
  reject = false; await b.invite(); assert.equal(writes(b).length, 2); assert.notEqual(writes(b)[1].body.operation_id, old);
});
test("receipt GET404 alone never enables discarding an uncertain request", async () => {
  const api = backend(); api.hook = (p, m, body, base) => { if (m === "POST") throw { status: 503 }; return base(p, m, body); };
  const b = browser(api); await b.start(); await b.invite(); await b.click("pairs-recover");
  assert.equal(b.get("pairs-discard").hidden, true); assert.equal(b.storage.size, 1); assert.equal(b.get("pairs-invite").disabled, true);
});

test("an ambiguous retry revokes prior rejection certainty and cannot be discarded", async () => {
  const api = backend(); let attempt = 0;
  api.hook = (p, m, body, base) => { if (m === "POST") throw { status: ++attempt === 1 ? 404 : 503 }; return base(p, m, body); };
  const b = browser(api); await b.start(); await b.invite(); assert.equal(b.get("pairs-discard").hidden, false);
  await b.click("pairs-retry"); assert.equal(b.get("pairs-discard").hidden, true); await b.click("pairs-recover");
  assert.equal(b.get("pairs-discard").hidden, true); await b.click("pairs-discard"); assert.equal(b.storage.size, 1);
  assert.equal(b.get("pairs-invite").disabled, true); assert.deepEqual(writes(b)[1].body, writes(b)[0].body);
});
test("invitation and event pagination replace the displayed page and use their own cursor", async () => {
  const api = backend(), cursor = randomUUID(), calls = []; api.hook = (p, m, body, base) => {
    calls.push(p); const result = base(p, m, body);
    if (p.startsWith("./pairs/invitations")) return p.includes("after=") ? { invitations: [] } : { ...result, next: cursor };
    if (p === "./pairs/events") return { ...result, next: cursor };
    return result;
  };
  const b = browser(api); await b.start(); assert.equal(b.get("pairs-next").hidden, false); await b.click("pairs-next");
  assert.ok(calls.some(p => p.includes("direction=incoming&after=" + cursor))); assert.equal(b.get("pairs-list").children.length, 0);
  await b.click("pairs-events-next"); assert.ok(calls.includes("./pairs/events?after=" + cursor));
});

function currentPair(api) {
 const pair = { id: randomUUID(), event_id: eventID, version: 2, state: "assigned" };
 api.owned = [{ pair, event_name: "Event <script>", timezone: "Europe/Berlin", starts_at: "2026-10-24T20:00:00Z", ends_at: "2026-10-25T06:00:00Z" }];
 return pair;
}
const ownedButtons = b => b.get("pairs-owned-list").querySelectorAll().filter(v => v.tag === "button");
const ownedConsent = b => b.get("pairs-owned-list").querySelectorAll().find(v => v.tag === "input");
test("current pair is independent of accepted invitation history; leaving requires explicit consent", async () => {
 const api=backend(), pair=currentPair(api), b=browser(api);await b.start();
 assert.match(b.get("pairs-owned-list").children[0].children[0].textContent,/Passes assigned/);
 await ownedButtons(b)[0].listeners.click();assert.equal(writes(b).length,0);
 ownedConsent(b).checked=true;await ownedButtons(b)[0].listeners.click();
 assert.equal(writes(b)[0].path,"./pairs/dissolve");assert.equal(writes(b)[0].body.arguments.pair_id,pair.id);assert.equal(writes(b)[0].body.arguments.expected_version,2);
 assert.match(b.get("pairs-receipt").textContent,/Pair ended/);assert.match(b.get("pairs-owned-list").textContent,/No current pairs/);
});
test("committed lost dissolution response is recovered after reload without another write", async () => {
 const api=backend();currentPair(api);const storage=new Map(),b=browser(api,{storage});let lost=true;
 api.hook=(p,m,body,base)=>{const r=base(p,m,body);if(m==="POST"&&lost){lost=false;throw{status:503}}return r};
 await b.start();ownedConsent(b).checked=true;await ownedButtons(b)[0].listeners.click();assert.equal(storage.size,1);
 const again=browser(api,{storage});await again.start();assert.equal(writes(again).length,0);assert.equal(storage.size,0);assert.match(again.get("pairs-receipt").textContent,/Pair ended/);assert.equal(api.receipts.size,1);
});
test("changed pair requires renewed explicit review before a leave request", async () => {
 const api=backend();currentPair(api);const b=browser(api);await b.start();ownedConsent(b).checked=true;api.owned[0].pair.version++;
 await ownedButtons(b)[0].listeners.click();assert.equal(writes(b).length,0);assert.match(b.get("pairs-status").textContent,/pair changed/i);
 assert.equal(ownedConsent(b).checked,false);
});
test("agent dissolution disappears from current list on refresh without changing invitation receipt history", async () => {
 const api=backend();currentPair(api);const b=browser(api);await b.start();api.owned=[];await b.click("pairs-owned-first");
 assert.equal(ownedButtons(b).length,0);assert.match(b.get("pairs-owned-list").textContent,/No current pairs/);assert.ok(b.buttons().length>0);assert.equal(writes(b).length,0);
});
