"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/pair-assignments.js"), "utf8");
class Element {
  constructor(tag = "div") { this.tagName = tag; this.children = []; this.listeners = {}; this.textContent = ""; this.value = ""; this.hidden = false; this.disabled = false; this.checked = false; }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; this.textContent = ""; }
  addEventListener(event, action) { this.listeners[event] = action; }
  querySelectorAll(tag) { return this.children.flatMap(n => [...(n.tagName === tag ? [n] : []), ...n.querySelectorAll(tag)]); }
}
function backend() {
  const b = { owner: randomUUID(), events: [], targets: [], proposals: new Map(), native: new Set(), writes: [], effects: 0, active: true, admin: true, pageSize: 20 };
  b.event = () => { const e = { id: randomUUID(), version: 3, document: { name: "Dance event <img>", timezone: "Europe/Minsk" } }; b.events.push(e); return e; };
  b.target = e => {
    const pair = { id: randomUUID(), event_id: e.id, leader_registration_id: randomUUID(), follower_registration_id: randomUUID(), state: "waitlisted", version: 2 };
    const t = { pair, arguments: { event_id: e.id, pair_id: pair.id, expected_event_version: e.version, expected_pair_version: pair.version, expected_leader_version: 4, expected_follower_version: 5 }, leader: { user_id: randomUUID(), telegram_user_id: 123456, display_name: "Alex <script>" }, follower: { user_id: randomUUID(), telegram_user_id: 654321, display_name: "Sam" } }; b.targets.push(t); return t;
  };
  b.proposal = (e, t, op = "loom/envelope:pair/" + randomUUID()) => {
    const quotes = ["leader", "follower"].map((role, i) => ({ registration_id: t.pair[role + "_registration_id"], price_minor: 12345 + 10000 * i, currency: "BYN", currency_scale: 2, tier: { id: "early", number: 1 }, deposit: { required_minor: 3000 + 1000 * i, final_due_at: new Date(Date.now() + 86400000 * (i + 1)).toISOString() } }));
    const p = { original_operation_id: op, leader: structuredClone(t.leader), follower: structuredClone(t.follower), preparation: { proposal_id: randomUUID(), arguments: structuredClone(t.arguments), description: "Assign pair " + t.pair.id + " for event " + e.id + "; leader registration " + t.pair.leader_registration_id + "; follower registration " + t.pair.follower_registration_id + ". Original immutable terms <img src=x>", expires_at: new Date(Date.now() + 300000).toISOString(), timezone: e.document.timezone, quotes }, state: "pending", observed_at: new Date().toISOString() }; b.proposals.set(op, p); return p;
  };
  const page = (items, cursor, key) => { const all = items.sort((a, c) => key(a).localeCompare(key(c))).filter(x => !cursor || key(x) > cursor), result = all.slice(0, b.pageSize); return [result, all.length > result.length ? key(result.at(-1)) : undefined]; };
  b.request = async (path, method = "GET", body) => {
    if (b.hook) { const r = await b.hook(path, method, body); if (r !== undefined) return r; }
    if (!b.active) throw { status: 401 }; if (!b.admin) throw { status: 403 };
    const url = new URL(path, "https://fixture.invalid/app/"), route = url.pathname.replace("/app/pair-assignments/", ""), after = url.searchParams.get("after");
    if (method === "GET") {
      if (route === "events") { const [events, next] = page([...b.events], after, e => e.id); return structuredClone({ events, next }); }
      if (route.startsWith("events/")) { const [candidates, next] = page(b.targets.filter(t => t.pair.event_id === route.split("/")[1]), after, t => t.pair.id); return structuredClone({ candidates, next }); }
      if (route === "proposals") { const [proposals, next] = page([...b.proposals.values()].filter(p => p.state === "pending"), after, p => p.preparation.proposal_id); return structuredClone({ proposals, next }); }
      if (route.startsWith("proposals/")) { const p = [...b.proposals.values()].find(p => p.preparation.proposal_id === route.slice(10)); if (!p) throw { status: 404 }; return structuredClone(p); }
      if (route === "recent") return structuredClone({ proposals: [...b.proposals.values()].filter(p => b.native.has(p.original_operation_id)).slice(-20).reverse() });
      if (route.startsWith("operations/")) { const p = b.proposals.get(decodeURIComponent(route.slice(11))); if (!p) throw { status: 404 }; if (p.state === "pending" && Date.parse(p.preparation.expires_at) <= Date.now()) p.state = "expired"; return structuredClone(p); }
      throw Error("Unexpected GET " + route);
    }
    b.writes.push(structuredClone({ route, body })); b.native.add(body.operation_id);
    if (route === "prepare") {
      let p = b.proposals.get(body.operation_id);
      if (!p) { const e = b.events.find(e => e.id === body.arguments.event_id), t = b.targets.find(t => t.pair.id === body.arguments.pair_id); if (!e || !t || e.version !== body.arguments.expected_event_version || JSON.stringify(t.arguments) !== JSON.stringify(body.arguments)) throw { status: 409 }; p = b.proposal(e, t, body.operation_id); }
      if (b.lose === "prepare") { b.lose = undefined; throw Error("Lost response after commit"); } return structuredClone(p.preparation);
    }
    if (route === "confirm") {
      const p = b.proposals.get(body.operation_id); assert.ok(p); assert.equal(body.proposal_id, p.preparation.proposal_id); assert.equal(body.confirmed, true); assert.deepEqual(body.arguments, p.preparation.arguments);
      if (!p.result) {
        const t = b.targets.find(t => t.pair.id === p.preparation.arguments.pair_id); if (!t || t.pair.version !== body.arguments.expected_pair_version || Date.parse(p.preparation.expires_at) <= Date.now()) throw { status: 409 };
        const pair = { ...t.pair, state: "assigned", version: t.pair.version + 1 };
        p.result = { pair };
        for (const [i, role] of ["leader", "follower"].entries()) p.result[role] = { id: pair[role + "_registration_id"], user_id: t[role].user_id, event_id: pair.event_id, state: "assigned", role, version: body.arguments["expected_" + role + "_version"] + 1, pass: { ...p.preparation.quotes[i], payment_due_at: new Date(Date.now() + 3600000 * (i + 1)).toISOString() } };
        p.state = "completed"; b.effects++;
      }
      if (b.lose === "confirm") { b.lose = undefined; throw Error("Lost response after commit"); } return structuredClone(p.result);
    }
    throw Error("Unexpected POST " + route);
  }; return b;
}
function harness(b = backend(), store = new Map()) {
  const elements = new Map(), get = id => { if (!elements.has(id)) elements.set(id, new Element()); return elements.get(id); };
  let ui, denied = 0, failWrite = false, failRead = false;
  const storage = { getItem: key => { if (failRead) throw Error("Storage unavailable"); return store.get(key) || null; }, setItem: (key, value) => { if (failWrite) throw Error("Storage unavailable"); store.set(key, value); }, removeItem: key => { if (failWrite) throw Error("Storage unavailable"); store.delete(key); } };
  const window = {};
  runInNewContext(source, { window, document: { getElementById: get, createElement: tag => new Element(tag) }, sessionStorage: storage, crypto: { randomUUID }, Date: class extends Date { static now() { return b.now ?? Date.now(); } }, Intl, console });
  ui = window.createZinusyaPairAssignments({ request: b.request, setBusy: v => ui.setBusy(v), denied: () => { denied++; } });
  const h = { b, ui, store, get: id => get(id === "pair-assignments" ? id : "ppa-" + id), text: id => { const flatten = n => n.textContent + n.children.map(flatten).join(" "); return flatten(h.get(id)); }, denied: () => denied, failStorage: () => { failWrite = true; }, failRead: () => { failRead = true; }, start: (owner = b.owner, locale = "en") => ui.start(owner, locale),
    click: async id => { const n = h.get(id); assert.equal(n.disabled, false, id + " disabled"); assert.equal(n.hidden, false, id + " hidden"); await n.listeners.click(); },
    select: async (id, value) => { const n = h.get(id); assert.equal(n.disabled, false, id + " disabled"); n.value = value; await n.listeners.change(); },
    consent: () => { h.get("consent").checked = true; h.get("consent").listeners.change(); },
    choose: async (index = 0) => { const n = h.get("pending").querySelectorAll("button")[index]; assert.ok(n); assert.equal(n.disabled, false); await n.listeners.click(); },
    async prepare() { const e = b.events[0], t = b.targets.find(t => t.pair.event_id === e.id); await h.select("event", e.id); assert.equal(h.get("target").value, ""); await h.select("target", t.pair.id); await h.click("prepare"); return { e, t }; }
  }; return h;
}
function fixture() { const h = harness(); const event = h.b.event(); h.b.target(event); return h; }

test("explicit named pair selection and fresh consent assign both distinct quoted passes", async () => {
  const h = fixture(); await h.start(); assert.equal(h.get("event").value, ""); assert.equal(h.get("prepare").disabled, true);
  const { t } = await h.prepare(); assert.match(h.text("target"), /Alex <script>.*123456.*Sam.*654321/); assert.equal(h.b.effects, 0); assert.equal(h.get("confirm").disabled, true);
  for (const amount of ["123.45 BYN", "223.45 BYN", "30.00 BYN", "40.00 BYN"]) assert.ok(h.text("preview").includes(amount));
  assert.match(h.text("preview"), /Europe\/Minsk/); assert.match(h.text("preview"), /Original immutable terms <img src=x>/); assert.match(h.text("preview"), /Leader: Alex <script> · Telegram 123456/); assert.match(h.text("preview"), /Follower: Sam · Telegram 654321/); assert.equal(h.get("preview").querySelectorAll("img").length, 0);
  h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.get("confirm").hidden, true); assert.match(h.text("receipt"), /historical/);
  for (const amount of ["123.45 BYN", "223.45 BYN", "30.00 BYN", "40.00 BYN"]) assert.ok(h.text("receipt").includes(amount));
  assert.equal(h.b.writes[0].body.arguments.pair_id, t.pair.id);
});
test("lost prepare reply restarts with original operation and no duplicate preparation", async () => {
  const h = fixture(); await h.start(); h.b.lose = "prepare"; await h.prepare(); const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("confirm").disabled, true); reload.consent(); await reload.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.b.writes.length, 2);
});
test("lost confirm reply cannot be discarded and fresh browser finds historical receipt", async () => {
  const h = fixture(); await h.start(); await h.prepare(); h.consent(); h.b.lose = "confirm"; await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.get("close").hidden, true); assert.equal(h.get("consent").checked, false);
  const fresh = harness(h.b); await fresh.start(); await fresh.click("recent"); await fresh.choose(); assert.equal(fresh.get("confirm").hidden, true); assert.match(fresh.text("receipt"), /historical/); assert.match(fresh.text("receipt"), /Leader: Alex <script> · Telegram 123456/); assert.match(fresh.text("receipt"), /Follower: Sam · Telegram 654321/); assert.equal(h.b.writes.length, 2);
});
test("confirmation never reaching server requires recovery and new consent for same operation", async () => {
  const h = fixture(); await h.start(); await h.prepare(); let fail = true; h.b.hook = async (path, method) => { if (fail && method === "POST" && path.endsWith("confirm")) { fail = false; throw Error("Did not arrive"); } }; h.consent(); await h.click("confirm"); assert.equal(h.get("close").hidden, true); assert.equal(h.get("confirm").disabled, true); await h.click("recover"); assert.equal(h.get("confirm").disabled, true); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.b.writes[0].body.operation_id, h.b.writes[1].body.operation_id);
});
test("missing prepare retries original IDs and preparation-only preview closes locally", async () => {
  const h = fixture(); await h.start(); let fail = true; h.b.hook = async (path, method) => { if (fail && method === "POST" && path.endsWith("prepare")) { fail = false; throw Error("Did not arrive"); } }; await h.prepare(); const saved = JSON.parse([...h.store.values()][0]); const reload = harness(h.b, h.store); await reload.start(); await reload.click("retry"); assert.equal(h.b.writes[0].body.operation_id, saved.operation_id); assert.deepEqual(h.b.writes[0].body.arguments, saved.arguments); await reload.click("close"); assert.equal(h.b.proposals.size, 1); assert.equal(h.b.effects, 0); assert.equal(h.store.size, 0);
});
test("agent proposal retains opaque original operation, terms and timezone with no current events", async () => {
  const h = fixture(), e = h.b.events[0], p = h.b.proposal(e, h.b.targets[0]); e.document.timezone = "Asia/Yekaterinburg"; h.b.events = []; await h.start(); assert.match(h.text("pending"), /Alex <script>.*123456.*Sam.*654321/); await h.choose(); assert.equal(h.get("target").value, ""); assert.match(h.text("preview"), /Leader: Alex <script> · Telegram 123456/); assert.match(h.text("preview"), /Follower: Sam · Telegram 654321/); assert.ok(h.text("preview").includes(p.preparation.description)); assert.equal(h.b.writes.length, 0); assert.match(h.text("preview"), /Europe\/Minsk/); assert.doesNotMatch(h.text("preview"), /Asia\/Yekaterinburg/); h.consent(); await h.click("confirm"); assert.equal(h.b.writes[0].body.operation_id, p.original_operation_id); assert.equal(h.b.effects, 1);
});
test("legacy preparation explicitly uses UTC and expired preview never enables confirmation", async () => {
  const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); delete p.preparation.timezone; await h.start(); await h.choose(); assert.match(h.text("preview"), /UTC/); h.b.now = Date.parse(p.preparation.expires_at) + 1; h.consent(); assert.equal(h.get("confirm").disabled, true); assert.equal(h.b.effects, 0);
});
test("storage failure blocks writes and journal contains identifiers only", async () => {
  const h = fixture(); await h.start(); h.failStorage(); await h.prepare(); assert.equal(h.b.writes.length, 0); assert.equal(h.get("prepare").disabled, true);
  const good = fixture(); await good.start(); await good.prepare(); const raw = [...good.store.values()][0]; assert.doesNotMatch(raw, /Alex|Sam|price_minor|description|timezone|confirmed|passport|token/); assert.deepEqual(Object.keys(JSON.parse(raw)).sort(), ["arguments", "operation_id", "owner", "phase", "proposal_id"]);
  const broken = harness(good.b, new Map([["zinusya.pair-assignments." + good.b.owner, "bad json"]])); await broken.start(); assert.equal(broken.get("prepare").disabled, true); assert.equal(broken.get("reload").disabled, false);
});
test("initial events failure keeps visible first-page refresh; empty administrator list is valid", async () => {
  const h = fixture(); let fail = true; h.b.hook = async path => { if (fail && path.endsWith("events")) { fail = false; throw Error("Unavailable"); } }; await h.start(); assert.equal(h.get("pair-assignments").hidden, false); assert.equal(h.get("events-first").hidden, false); await h.click("events-first"); await h.prepare(); assert.equal(h.b.writes.length, 1);
  const empty = harness(); await empty.start(); assert.equal(empty.get("pair-assignments").hidden, false); assert.equal(empty.get("prepare").disabled, true);
});
test("paging and language changes never select a pair or grant consent", async () => {
  const h = fixture(); h.b.target(h.b.events[0]); const other = h.b.event(); h.b.target(other); h.b.target(other); h.b.pageSize = 1; await h.start(); await h.click("events-next"); assert.equal(h.get("event").value, ""); await h.click("events-first"); await h.select("event", h.get("event").children[1].value); await h.click("targets-next"); assert.equal(h.get("target").value, ""); for (const locale of ["be", "uk-UA", "ru"]) { h.ui.setLanguage(locale); assert.equal(h.get("confirm").textContent, "Назначить оба пасса"); } assert.equal(h.get("consent").checked, false); assert.equal(h.b.writes.length, 0);
});
test("authorization loss hides workspace; late old-owner reads cannot change new owner", async () => {
  for (const status of [401, 403]) { const h = fixture(); h.b.hook = async () => { throw { status }; }; await h.start(); assert.equal(h.get("pair-assignments").hidden, true); assert.equal(h.denied(), status === 401 ? 1 : 0); }
  for (const denied of [false, true]) { const h = fixture(); let release, first = true; const wait = new Promise(r => { release = r; }); h.b.hook = async path => { if (first && path.endsWith("events")) { first = false; await wait; if (denied) throw { status: 401 }; return { events: [] }; } }; const old = h.start(); await h.start(randomUUID()); release(); await old; assert.equal(h.denied(), 0); assert.equal(h.get("pair-assignments").hidden, false); }
});
test("POST sees persisted original operation and exact versions before network side effects", async () => {
  const h = fixture(); await h.start(); h.b.hook = async (_path, method, body) => { if (method === "POST") { const r = JSON.parse([...h.store.values()][0]); assert.equal(r.operation_id, body.operation_id); assert.deepEqual(r.arguments, structuredClone(body.arguments)); assert.equal(r.phase, body.confirmed ? "confirm" : "prepare"); assert.equal(r.confirmed, undefined); } }; await h.prepare(); assert.equal(h.b.writes.length, 1, "preparation reached backend after journal validation"); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1);
});

test("recovery rejects incomplete or mismatched role identities without exposing confirmation", async () => {
  for (const corrupt of [p => { delete p.follower; }, p => { p.follower.user_id = p.leader.user_id; }, p => { p.follower.telegram_user_id = p.leader.telegram_user_id; }]) {
    const h = fixture(); await h.start(); await h.prepare(); const p = [...h.b.proposals.values()][0]; corrupt(p); const fresh = harness(h.b, h.store); await fresh.start(); assert.equal(fresh.get("confirm").hidden, true); assert.equal(h.b.effects, 0); assert.equal(h.b.writes.length, 1);
  }
  const h = fixture(); await h.start(); await h.prepare(); h.consent(); await h.click("confirm"); const p = [...h.b.proposals.values()][0]; p.leader.user_id = randomUUID(); const fresh = harness(h.b, h.store); await fresh.start(); assert.equal(fresh.text("receipt"), ""); assert.equal(fresh.get("confirm").hidden, true); assert.equal(h.b.effects, 1);
});

test("corrupt or unreadable journal preserves bytes and permits authorized receipt inspection only", async () => {
  const original = fixture(); await original.start(); await original.prepare(); original.consent(); await original.click("confirm"); const b = original.b; b.writes = [];
  for (const mode of ["corrupt", "oversized", "unreadable"]) {
    const key = "zinusya.pair-assignments." + b.owner, raw = mode === "oversized" ? "x".repeat(2049) : "unreadable original identifiers";
    const store = new Map([[key, raw]]), h = harness(b, store); if (mode === "unreadable") h.failRead(); await h.start();
    assert.equal(h.get("prepare").disabled, true); assert.equal(h.get("recent").disabled, false); await h.click("recent"); await h.choose();
    assert.match(h.text("receipt"), /historical/); assert.match(h.text("receipt"), /Alex <script>.*123456/); assert.equal(h.get("confirm").disabled, true); assert.equal(h.get("consent").hidden, true); assert.match(h.text("status"), /identifiers/); assert.equal(store.get(key), raw); assert.equal(b.writes.length, 0);
  }
});
test("unwritable journal permits pending proposal inspection without adoption or confirmation", async () => {
  const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); await h.start(); h.failStorage(); await h.choose();
  assert.match(h.text("preview"), /Alex <script>.*123456/); assert.equal(h.get("confirm").disabled, true); assert.equal(h.get("consent").hidden, true); assert.equal(h.store.size, 0); await h.click("reload"); await h.choose(); assert.equal(h.b.writes.length, 0); assert.equal(h.b.proposals.get(p.original_operation_id).state, "pending");
});
test("read-only inspection never replaces unknown confirmation journal", async () => {
  const h = fixture(); await h.start(); await h.prepare(); h.consent(); h.b.lose = "confirm"; await h.click("confirm"); const before = [...h.store.values()][0];
  const pending = h.b.proposal(h.b.events[0], h.b.targets[0]); const read = harness(h.b, h.store); read.failStorage(); await read.start(); assert.match(read.text("receipt"), /historical/); await read.click("reload"); await read.choose(); assert.match(read.text("preview"), /Alex <script>/); assert.equal(read.get("confirm").disabled, true); assert.equal(read.get("close").disabled, true); assert.equal([...h.store.values()][0], before); assert.equal(JSON.parse(before).phase, "confirm"); assert.equal(pending.state, "pending"); assert.equal(h.b.writes.length, 2); assert.equal(h.b.effects, 1);
});

for (const [locale, minimum, share] of [["en", "Minimum amount", "Minimum share"], ["ru", "Минимальная сумма", "Минимальная доля"]]) {
  test(`original deposit minimum and fractional share remain visible before and after consent (${locale})`, async () => {
    const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]);
    for (const q of p.preparation.quotes) Object.assign(q.deposit, { minimum_minor: 2150, minimum_bps: 2550 });
    const original = JSON.stringify(p.preparation);
    await h.start(h.b.owner, locale); await h.choose();
    assert.ok(h.text("preview").includes(minimum + ": 21.50 BYN"));
    assert.ok(h.text("preview").includes(share + ": 25.50%"));
    assert.equal(h.get("confirm").disabled, true); assert.equal(h.b.effects, 0);
    h.consent(); await h.click("confirm");
    assert.ok(h.text("receipt").includes(minimum + ": 21.50 BYN"));
    assert.ok(h.text("receipt").includes(share + ": 25.50%"));
    assert.equal(JSON.stringify(p.preparation), original);
    const fresh = harness(h.b); await fresh.start(h.b.owner, locale); await fresh.click("recent"); await fresh.choose();
    assert.ok(fresh.text("receipt").includes(share + ": 25.50%"));
    assert.equal(h.b.effects, 1); assert.equal(h.b.writes.length, 1);
  });
}

test("invalid deposit policy values cannot authorize a recovered proposal", async () => {
  for (const d of [{ minimum_minor: -1 }, { minimum_bps: 10001 }, { minimum_bps: "25" }]) {
    const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]);
    await h.start(); Object.assign(p.preparation.quotes[0].deposit, d); await h.choose();
    assert.equal(h.get("confirm").hidden, true); assert.equal(h.b.effects, 0); assert.equal(h.b.writes.length, 0);
  }
});

for (const [locale, window, cap] of [["en", "Payment evidence window after assignment", "No later than"], ["ru", "Срок отправки подтверждения после назначения", "Не позднее"]]) {
  test(`frozen structured terms replace technical description and survive current event changes (${locale})`, async () => {
    const h = fixture(), e = h.b.events[0], p = h.b.proposal(e, h.b.targets[0]);
    p.preparation.display_terms = { event_name: "Original dance <script>", payment_window_seconds: 90061, payment_closes_at: new Date(Date.now() + 86400000).toISOString() };
    const original = JSON.stringify(p.preparation); e.document.name = "Changed dance"; e.document.timezone = "Asia/Yekaterinburg";
    await h.start(h.b.owner, locale); await h.choose();
    const text = h.text("preview");
    assert.ok(text.includes("Original dance <script>")); assert.ok(text.includes(window)); assert.ok(text.includes(cap));
    assert.match(text, /Europe\/Minsk/); assert.doesNotMatch(text, /Changed dance|Asia\/Yekaterinburg|Original immutable terms/);
    assert.ok(!text.includes(p.preparation.arguments.pair_id));
    assert.match(text, locale === "en" ? /1 day.*1 hour.*1 minute.*1 second/ : /1 день.*1 час.*1 минута.*1 секунда/);
    assert.equal(h.get("preview").querySelectorAll("script").length, 0); assert.equal(h.get("confirm").disabled, true);
    h.consent(); await h.click("confirm");
    assert.equal(JSON.stringify(p.preparation), original); assert.equal(h.b.effects, 1);
    const fresh = harness(h.b); await fresh.start(h.b.owner, locale); await fresh.click("recent"); await fresh.choose();
    assert.ok(fresh.text("preview").includes("Original dance <script>")); assert.equal(h.b.writes.length, 1);
  });
}

test("incomplete structured timing is rejected rather than replaced with current event settings", async () => {
  for (const corrupt of [d => { delete d.payment_closes_at; }, d => { d.payment_window_seconds = 0; }, d => { d.event_name = ""; }]) {
    const h = fixture(), p = h.b.proposal(h.b.events[0], h.b.targets[0]); await h.start();
    p.preparation.display_terms = { event_name: "Frozen dance", payment_window_seconds: 3600, payment_closes_at: new Date(Date.now()+86400000).toISOString() };
    corrupt(p.preparation.display_terms); await h.choose();
    assert.equal(h.get("confirm").hidden, true); assert.equal(h.b.effects, 0);
  }
});
