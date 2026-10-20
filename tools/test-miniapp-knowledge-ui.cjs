"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(__dirname + "/../internal/api/miniapp_ui/knowledge.js", "utf8");
class Element {
  constructor(tag = "div") { this.tagName = tag; this.children = []; this.listeners = {}; this.textContent = ""; this.value = ""; this.hidden = false; this.disabled = false; this.checked = false; }
  append(...items) { this.children.push(...items); for (const item of items) item.parent = this; }
  replaceChildren(...items) { this.children = []; this.textContent = ""; this.append(...items); }
  addEventListener(name, action) { this.listeners[name] = action; }
  remove() { this.parent.children = this.parent.children.filter(v => v !== this); }
  querySelectorAll(selector) { return this.children.flatMap(n => [...(selector.split(/,\s*/).includes(n.tagName) ? [n] : []), ...n.querySelectorAll(selector)]); }
}
function backend() {
  const b = { owner: randomUUID(), articleID: randomUUID(), rows: [], caps: {}, calls: [], receipts: new Map(), effects: 0, moderators: [], preps: new Map() };
  b.article = { id: b.articleID, revision: 2, current_revision: 2, slug: "test", title: "Zouk <img>", body: "# Text\n<script>alert(1)</script>", language: "en", topics: ["dance"], sources: [{ label: "Source", url: "https://example.org/" }], historical: false, event_name: "Event" };
  b.rows = [b.article];
  b.request = async (path, method, args) => {
    b.calls.push(structuredClone({ path, method, args }));
    if (b.hook) { const result = await b.hook(path, args); if (result !== undefined) return result; }
    const route = path.replace("./knowledge/", "");
    if (route === "overview") return { items: [], truncated: false };
    if (route === "recent_actions") return { items: b.recent || [] };
    if (route === "capabilities") return structuredClone(b.caps);
    if (route === "moderators") return { items: structuredClone(b.moderators) };
    if (route === "list" || route === "search") return { items: b.rows, next: b.next };
    if (route === "read") { if (args.article_id !== b.articleID) throw { status: 404 }; return { article: { ...b.article, revision: args.revision || b.article.revision, older_revision: !!args.revision && args.revision !== b.article.revision }, body: b.article.body }; }
    if (route === "revisions") return { items: [{ revision: 1, change_kind: "edit", reason: "Earlier" }] };
    if (route.endsWith("suggestions")) return { items: b.suggestions || [] };
    if (route === "suggestion") return { suggestion: b.suggestion, body: "New proposed text" };
    if (route.endsWith("/status")) {
      const base = { operation_id: args.operation_id, observed_at: new Date().toISOString() };
      if (b.receipts.has(args.operation_id)) return { ...base, status: "completed", result: b.receipts.get(args.operation_id) };
      const prep = b.preps.get(args.operation_id);
      if (!prep || b.statusUnknown) return { ...base, status: "unknown" };
      return { ...base, status: b.serverExpired ? "expired" : "prepared", preparation: structuredClone(prep) };
    }
    if (route.endsWith("/prepare")) {
      let prep = b.preps.get(args.operation_id);
      if (!prep) {
        const normalized = { ...args.arguments };
        if (route === "set_moderator/prepare") {
          const current = b.moderators.find(m => m.user_id === normalized.moderator_user_id);
          if ((current?.version || 0) !== normalized.expected_moderator_version) throw { status: 409 };
        } else normalized.article_id ||= randomUUID();
        prep = { arguments: normalized, preconditions: { proposal_id: randomUUID() }, description: "Review exact content <img>", expires_at: new Date(Date.now() + 300000).toISOString() }; b.preps.set(args.operation_id, prep); }
      if (b.losePrepare) { b.losePrepare = false; throw Error("Lost preparation response"); }
      return structuredClone(prep);
    }
    if (route.endsWith("/confirm")) {
      const prep = b.preps.get(args.operation_id); assert.equal(args.proposal_id, prep.preconditions.proposal_id); assert.equal(JSON.stringify(args.arguments), JSON.stringify(prep.arguments)); assert.equal(args.confirmed, true);
      if (!b.receipts.has(args.operation_id)) {
        const result = { kind: route.split("/")[0], article: { id: b.articleID, revision: 3 } };
        if (route === "set_moderator/confirm") {
          const previous = b.moderators.find(m => m.user_id === args.arguments.moderator_user_id);
          if ((previous?.version || 0) !== args.arguments.expected_moderator_version) throw { status: 409 };
          const moderator = { user_id: args.arguments.moderator_user_id, enabled: args.arguments.moderator_enabled, version: (previous?.version || 0) + 1 };
          b.moderators = [...b.moderators.filter(m => m.user_id !== moderator.user_id), moderator];
          if (moderator.user_id === b.owner) b.caps = { ...b.caps, moderator: moderator.enabled };
          delete result.article; result.moderator = moderator;
        }
        b.effects++; b.receipts.set(args.operation_id, result);
      }
      if (b.loseConfirm) { b.loseConfirm = false; throw Error("Lost confirmation response"); }
      return b.receipts.get(args.operation_id);
    }
    throw Error("Unexpected route " + route);
  }; return b;
}
function harness(b = backend(), store = new Map()) {
  const map = new Map(), section = new Element("section");
  const get = id => { if (id === "knowledge") return section; if (!map.has(id)) { const e = new Element(id === "kb-consent" ? "input" : id.includes("label") ? "label" : "button"); map.set(id, e); section.append(e); } return map.get(id); };
  const storage = { getItem: key => store.get(key) || null, setItem: (key, value) => { if (h.failStorage) throw Error("Unavailable"); store.set(key, value); }, removeItem: key => store.delete(key) };
  const window = {}; let ui;
  runInNewContext(source, { window, document: { getElementById: get, createElement: tag => new Element(tag) }, sessionStorage: storage, crypto: { randomUUID }, TextEncoder, Date, console });
  const h = { b, store, get: id => get(id.startsWith("kb-") || id === "knowledge" ? id : "kb-" + id), ui: undefined, failStorage: false, denied: 0 };
  ui = window.createZinusyaKnowledge({ request: b.request, setBusy: value => ui.setBusy(value), denied: () => h.denied++ }); h.ui = ui;
  h.start = (id = b.owner, locale = "en") => ui.start(id, locale);
  h.click = async id => { const e = h.get(id); assert.equal(e.disabled, false, id + " disabled"); assert.equal(e.hidden, false, id + " hidden"); await e.listeners.click(); };
  h.text = id => { const flatten = e => e.textContent + e.children.map(flatten).join(" "); return flatten(h.get(id)); };
  h.choose = async (id, index = 0) => { const button = h.get(id).querySelectorAll("button")[index]; assert.ok(button); assert.equal(button.disabled, false); await button.listeners.click(); };
  h.consent = () => { h.get("consent").checked = true; h.get("consent").listeners.change(); };
  h.fill = () => { for (const [key, value] of Object.entries({ slug: "zouk", name: "Zouk", text: "Dance knowledge", reason: "Useful community reference" })) h.get(key).value = value; };
  return h;
}
test("read exact text and old revision safely without executing Markdown HTML", async () => {
  const h = harness(); await h.start(); await h.choose("results"); assert.match(h.text("body"), /<script>/); assert.equal(h.get("body").querySelectorAll("script").length, 0); assert.match(h.text("provenance"), /Revision: 2/);
  await h.click("history"); await h.choose("revisions"); assert.match(h.text("provenance"), /Revision: 1/); assert.equal(h.get("correct").hidden, true); await h.click("current"); assert.equal(h.get("correct").hidden, false);
});
test("ordinary suggestion only commits after exact preparation and fresh explicit consent", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); await h.click("prepare"); assert.equal(h.b.effects, 0); assert.equal(h.get("confirm").disabled, true); assert.match(h.text("preview"), /Dance knowledge/); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.store.size, 0); assert.match(h.text("receipt"), /historical/);
});
test("lost prepare response reload reads original preparation without automatic consent or another prepare", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); h.b.losePrepare = true; await h.click("prepare"); const op = JSON.parse([...h.store.values()][0]).operation_id;
  const reloaded = harness(h.b, h.store); await reloaded.start(); assert.equal(h.b.effects, 0); assert.equal(reloaded.get("confirm").hidden, false); assert.equal(reloaded.get("confirm").disabled, true); reloaded.consent(); await reloaded.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.b.preps.size, 1); assert.equal(h.b.calls.at(-1).args.operation_id, op);
});
test("lost confirm response reload reconciles saved receipt without a second confirmation", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); await h.click("prepare"); h.b.loseConfirm = true; h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1); assert.equal(h.store.size, 1);
  const reloaded = harness(h.b, h.store); await reloaded.start(); assert.equal(reloaded.get("confirm").hidden, true); assert.equal(h.store.size, 0); assert.equal(h.b.calls.filter(c => c.path.endsWith("/confirm")).length, 1);
});
test("body size uses UTF-8 bytes and failed durable storage blocks mutation", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); h.get("text").value = "я".repeat(50000); await h.click("prepare"); assert.equal(h.b.preps.size, 0); assert.match(h.text("status"), /96 KiB/);
  h.fill(); h.failStorage = true; await h.click("prepare"); assert.equal(h.b.preps.size, 0); assert.equal(h.get("retry").disabled, true); assert.equal(h.get("prepare").disabled, true);
});
test("review cannot approve own suggestion and another reviewer uses current revision", async () => {
  const b = backend(); b.caps = { can_edit: true, moderator: true }; b.suggestion = { id: randomUUID(), article_id: b.articleID, base_revision: 1, proposer_id: b.owner, state: "pending", content: { title: "Suggested", language: "en" } }; b.suggestions = [b.suggestion];
  const h = harness(b); await h.start(); await h.click("pending"); await h.choose("suggestions"); assert.equal(h.get("review").hidden, true);
  b.suggestion.proposer_id = randomUUID(); await h.choose("suggestions"); assert.equal(h.get("review").hidden, false); h.get("review-reason").value = "Verified sources"; await h.click("approve"); assert.equal(b.calls.find(c => c.path === "./knowledge/review/prepare").args.arguments.expected_revision, 2); assert.equal(b.effects, 0);
});
test("pagination retains original query and late old-owner reads do not leak", async () => {
  const h = harness(); h.b.next = "cursor"; await h.start(); h.get("query").value = "dance"; await h.click("search"); h.get("query").value = "changed"; await h.click("next"); assert.deepEqual(h.b.calls.at(-1).args.queries, ["dance"]); assert.equal(h.b.calls.at(-1).args.cursor, "cursor");
  let release; const wait = new Promise(r => { release = r; }); let first = true;
  h.b.hook = async path => { if (first && path.endsWith("overview")) { first = false; await wait; return { items: [] }; } };
  const old = h.start(); await h.start(randomUUID()); release(); await old; assert.equal(h.denied, 0); assert.equal(h.get("knowledge").hidden, false);
});
test("RU, Belarusian and Ukrainian language fallback and no implicit new-article publication", async () => {
  const h = harness(); await h.start(h.b.owner, "uk-UA"); assert.equal(h.get("confirm").textContent, "Подтвердить изменение"); assert.equal(h.get("new-edit").hidden, true); assert.equal(h.b.effects, 0);
});

test("only server-confirmed expiry enables explicit close; local expiry and unknown preserve the original", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); await h.click("prepare");
  const record = JSON.parse([...h.store.values()][0]); record.expires_at = new Date(Date.now() - 60000).toISOString(); h.store.set("zinusya.knowledge." + h.b.owner, JSON.stringify(record)); h.b.statusUnknown = true;
  const reload = harness(h.b, h.store); await reload.start(); assert.equal(reload.get("close-expired").hidden, true); assert.equal(reload.get("confirm").disabled, true); assert.equal(reload.get("new").disabled, true); assert.equal(h.store.size, 1);
  h.b.statusUnknown = false; h.b.serverExpired = true; await reload.click("recover"); assert.equal(reload.get("close-expired").hidden, false); assert.equal(reload.get("confirm").disabled, true); assert.equal(h.store.size, 1); assert.equal(h.b.effects, 0);
  await reload.click("close-expired"); assert.equal(h.store.size, 0); assert.equal(reload.get("new").disabled, false); assert.equal(h.b.preps.size, 1); assert.equal(h.b.effects, 0);
});
test("completed receipt wins even after proposal expiry and never offers close-as-expired", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); await h.click("prepare"); h.b.loseConfirm = true; h.consent(); await h.click("confirm"); h.b.serverExpired = true;
  const reload = harness(h.b, h.store); await reload.start(); assert.equal(h.store.size, 0); assert.equal(reload.get("close-expired").hidden, true); assert.match(reload.text("receipt"), /historical/); assert.equal(h.b.effects, 1); assert.equal(h.b.calls.filter(c => c.path.endsWith("/confirm")).length, 1);
});
test("unknown after preparation disables a stale saved preview; prepared status requires fresh consent", async () => {
  const h = harness(); await h.start(); await h.click("new"); h.fill(); await h.click("prepare"); h.consent(); h.b.statusUnknown = true; await h.click("recover"); assert.equal(h.get("confirm").disabled, true); assert.equal(h.get("close-expired").hidden, true); h.consent(); assert.equal(h.get("confirm").disabled, true);
  h.b.statusUnknown = false; await h.click("recover"); assert.equal(h.get("confirm").disabled, true); h.consent(); await h.click("confirm"); assert.equal(h.b.effects, 1);
});

test("event pages include empty events and preserve the selected collection", async () => {
 const b = backend(), first = randomUUID(), second = randomUUID();
 b.hook = async (path, args) => path.endsWith("/overview") ? { items: [{ collection: "event", event_id: args.after ? second : first, event_name: args.after ? "Second empty event" : "First empty event", count: 0 }], ...(args.after ? {} : { next_event: first }), truncated: !args.after } : undefined;
 const h = harness(b); await h.start(); h.get("collection").value = "event:" + first;
 await h.click("events-next"); assert.equal(h.get("collection").value, "event:" + first); assert.equal(h.get("events-next").hidden, true);
 assert.equal(h.get("collection").children.at(-1).value, "event:" + second);
 await h.click("new"); h.fill(); await h.click("prepare"); assert.equal(b.calls.at(-1).args.arguments.event_id, first);
});
test("another channel's request is recovered with original terms and fresh consent", async () => {
 const b = backend(), op = randomUUID(), proposal = randomUUID(), targetArticle = randomUUID(), targetEvent = randomUUID();
 b.recent = [{ kind: "suggest", operation_id: op, state: "prepared", created_at: new Date().toISOString() }];
 b.preps.set(op, { arguments: { article_id: targetArticle, event_id: targetEvent, expected_revision: 0, content: { ...b.article, slug: "agent-slug", body: "Original agent proposal" }, reason: "Agent initiated" }, preconditions: { proposal_id: proposal }, expires_at: new Date(Date.now()+300000).toISOString() });
 const h = harness(b); await h.start(); await h.click("recent"); await h.choose("recent-list");
 assert.match(h.text("preview"), /Original agent proposal/); assert.ok(h.text("preview").includes(targetArticle)); assert.ok(h.text("preview").includes(targetEvent)); assert.match(h.text("preview"), /agent-slug/); assert.match(h.text("preview"), /Based on revision: 0/); assert.equal(b.effects, 0); assert.equal(h.get("confirm").disabled, true);
 h.consent(); await h.click("confirm"); assert.equal(b.effects, 1); assert.equal(b.calls.at(-1).args.operation_id, op); assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 0);
});

test("unknown cross-channel requests survive reload without preparing guessed arguments", async () => {
 const b = backend(), op = randomUUID(); b.recent = [{ kind: "suggest", operation_id: op, state: "unknown", created_at: new Date().toISOString() }];
 const h = harness(b); await h.start(); await h.click("recent"); await h.choose("recent-list");
 assert.equal(h.get("retry").hidden, true); assert.equal(h.get("confirm").hidden, true);
 const restored = harness(b, h.store); await restored.start(); assert.equal(restored.get("retry").hidden, true); assert.equal(restored.get("new").disabled, true);
 assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 0);
});

test("definitive initial validation rejection keeps the form editable but uncertain attempts retain recovery", async () => {
 const b = backend(); let attempts = 0;
 b.hook = async path => { if (path.endsWith("/prepare") && attempts++ === 0) throw { status: 400 }; };
 const h = harness(b); await h.start(); await h.click("new"); h.fill(); await h.click("prepare");
 assert.equal(h.store.size, 0); assert.equal(h.get("prepare").disabled, false); assert.equal(h.get("name").value, "Zouk");
 await h.click("prepare"); assert.equal(b.preps.size, 1);
 const uncertain = harness(); await uncertain.start(); await uncertain.click("new"); uncertain.fill(); uncertain.b.losePrepare = true; await uncertain.click("prepare");
 const saved = JSON.parse([...uncertain.store.values()][0]); assert.equal(saved.preparation_attempted, true); const original = saved.operation_id;
 uncertain.b.hook = async path => { if (path.endsWith("/prepare")) throw { status: 400 }; };
 await uncertain.click("retry"); assert.equal(JSON.parse([...uncertain.store.values()][0]).operation_id, original); assert.equal(uncertain.get("new").disabled, true);
 await uncertain.click("recover"); assert.equal(uncertain.get("confirm").disabled, true);
});
test("changing event clears previous suggestion results and cursor", async () => {
 const b = backend(); b.hook = async path => path.endsWith("/my_suggestions") ? { items: [], next: randomUUID() } : undefined;
 const h = harness(b); await h.start(); await h.click("mine"); assert.equal(h.get("suggestions-next").hidden, false);
 h.get("collection").value = "event:" + randomUUID(); await h.get("collection").listeners.change();
 assert.equal(h.get("suggestions-next").hidden, true); assert.equal(h.get("suggestions").children.length, 0);
});
test("prepared approval keeps its original comparison visible until reconciliation", async () => {
 const b = backend(); b.caps = { moderator: true, can_edit: true }; b.article.slug = "current-slug"; const id = randomUUID(), event = randomUUID();
 b.suggestion = { id, article_id: b.articleID, base_revision: 2, event_id: event, content: { ...b.article, slug: "proposed-slug", title: "Original suggestion" }, state: "pending", proposer_id: randomUUID(), reason: "Improve reference" };
 b.suggestions = [b.suggestion]; const h = harness(b); await h.start(); await h.click("pending"); await h.choose("suggestions");
 assert.match(h.text("suggestion"), /current-slug/); assert.match(h.text("suggestion"), /proposed-slug/); assert.ok(h.text("suggestion").includes(event));
 h.get("review-reason").value = "Verified"; await h.click("approve");
 assert.match(h.text("suggestion"), /Original suggestion/); assert.equal(h.get("collection").disabled, true); assert.equal(h.get("suggestions").querySelectorAll("button")[0].disabled, true);
 assert.equal(h.get("consent").disabled, false); h.consent(); await h.click("confirm"); assert.equal(b.effects, 1);
});

test("first preparation conflict or access rejection preserves an editable draft", async () => {
 for (const status of [403, 404, 409]) {
  const b = backend(); b.hook = async path => { if (path.endsWith("/prepare")) throw { status }; };
  const h = harness(b); await h.start(); await h.click("new"); h.fill(); await h.click("prepare");
  assert.equal(h.store.size, 0); assert.equal(h.get("prepare").disabled, false); assert.equal(h.get("name").value, "Zouk"); assert.match(h.text("status"), /draft remains editable/);
 }
});


test("moderator management is unavailable to ordinary users without fetching the directory", async () => {
 const h = harness(); await h.start(); assert.equal(h.get("moderators").hidden, true); assert.equal(h.get("moderator-prepare").disabled, true);
 assert.equal(h.b.calls.filter(c => c.path.endsWith("/moderators")).length, 0);
});

test("new moderator assignment previews scope and only changes access after exact consent", async () => {
 const b = backend(); b.caps = { global_administrator: true }; const target = randomUUID();
 const h = harness(b); await h.start(); assert.equal(h.get("moderators").hidden, false);
 h.get("moderator-user").value = target; h.get("moderator-reason").value = "Maintain general references";
 await h.click("moderator-prepare"); assert.equal(b.effects, 0); assert.equal(h.get("confirm").disabled, true);
 assert.ok(h.text("preview").includes(target)); assert.match(h.text("preview"), /Enabled/); assert.match(h.text("preview"), /Assignment version: 0/); assert.match(h.text("preview"), /general and historical/); assert.doesNotMatch(h.text("preview"), /undefined/);
 assert.equal(h.get("moderator-user").disabled, true); h.consent(); await h.click("confirm");
 assert.equal(b.effects, 1); assert.deepEqual(b.moderators, [{ user_id: target, enabled: true, version: 1 }]); assert.match(h.text("receipt"), /Assignment version: 1/);
 assert.equal(h.store.size, 0); assert.equal(h.get("moderator-user").value, ""); assert.ok(h.text("moderators-list").includes(target));
});

test("revoke uses the selected current assignment version and refreshes self permissions", async () => {
 const b = backend(); b.caps = { global_administrator: true, moderator: true }; b.moderators = [{ user_id: b.owner, enabled: true, version: 4 }];
 const h = harness(b); await h.start(); await h.click("moderators-reload"); await h.choose("moderators-list");
 assert.equal(h.get("moderator-user").readOnly, true); assert.match(h.text("moderator-version"), /4/);
 h.get("moderator-enabled").checked = false; h.get("moderator-reason").value = "End moderation duty";
 await h.click("moderator-prepare"); assert.match(h.text("preview"), /Disabled/); h.consent(); await h.click("confirm");
 assert.equal(b.moderators[0].enabled, false); assert.equal(b.moderators[0].version, 5); assert.equal(h.get("new-edit").hidden, true); assert.match(h.text("moderators-list"), /Disabled/);
 assert.equal(b.calls.find(c => c.path.endsWith("set_moderator/confirm")).args.arguments.expected_moderator_version, 4);
});

test("moderator preparation loss reloads the same operation and still needs fresh consent", async () => {
 const b = backend(); b.caps = { global_administrator: true }; const target = randomUUID(); const h = harness(b); await h.start();
 h.get("moderator-user").value = target; h.get("moderator-reason").value = "Knowledge maintenance"; b.losePrepare = true; await h.click("moderator-prepare");
 const original = JSON.parse([...h.store.values()][0]).operation_id; const restored = harness(b, h.store); await restored.start();
 assert.ok(restored.text("preview").includes(target)); assert.equal(restored.get("confirm").disabled, true); assert.equal(b.effects, 0);
 restored.consent(); await restored.click("confirm"); assert.equal(b.effects, 1); assert.equal(b.calls.find(c => c.path.endsWith("set_moderator/confirm")).args.operation_id, original);
 assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 1);
});

test("lost moderator confirmation reconciles its receipt without a second change", async () => {
 const b = backend(); b.caps = { global_administrator: true }; const h = harness(b); await h.start();
 h.get("moderator-user").value = randomUUID(); h.get("moderator-reason").value = "Review references"; await h.click("moderator-prepare"); h.consent(); b.loseConfirm = true; await h.click("confirm");
 const restored = harness(b, h.store); await restored.start(); assert.equal(b.effects, 1); assert.equal(b.calls.filter(c => c.path.endsWith("/confirm")).length, 1);
 assert.match(restored.text("receipt"), /Enabled/); assert.equal(restored.get("confirm").hidden, true); assert.equal(restored.store.size, 0);
});

test("agent moderator preparation can be continued natively with current global rights", async () => {
 const b = backend(), op = randomUUID(), target = randomUUID(); b.caps = { global_administrator: true };
 b.recent = [{ kind: "set_moderator", operation_id: op, state: "prepared", created_at: new Date().toISOString() }];
 b.preps.set(op, { arguments: { moderator_user_id: target, moderator_enabled: true, expected_moderator_version: 0, reason: "Agent prepared terms" }, preconditions: { proposal_id: randomUUID() }, description: "Exact named moderator access", expires_at: new Date(Date.now() + 300000).toISOString() });
 const h = harness(b); await h.start(); await h.click("recent"); await h.choose("recent-list"); assert.ok(h.text("preview").includes(target)); h.consent();
 b.caps = { global_administrator: false }; await h.click("recover"); assert.equal(h.get("moderators").hidden, true); h.consent(); assert.equal(h.get("confirm").disabled, true); assert.equal(b.effects, 0);
 b.caps = { global_administrator: true }; await h.click("recover"); assert.equal(h.get("confirm").disabled, true); h.consent(); await h.click("confirm");
 assert.equal(b.effects, 1); assert.equal(b.calls.filter(c => c.path.endsWith("/prepare")).length, 0);
 assert.equal(b.calls.find(c => c.path.endsWith("/confirm")).args.operation_id, op);
});

test("moderator pagination is explicit and a stale assignment stays editable after rejection", async () => {
 const b = backend(), target = randomUUID(), first = randomUUID(); b.caps = { global_administrator: true }; b.moderators = [{ user_id: target, enabled: false, version: 3 }];
 b.hook = async (path, args) => path.endsWith("/moderators") ? (args.after ? { items: b.moderators } : { items: [{ user_id: first, enabled: true, version: 1 }], next: first }) : undefined;
 const h = harness(b); await h.start(); await h.click("moderators-reload"); await h.click("moderators-next"); await h.choose("moderators-list");
 h.get("moderator-enabled").checked = true; h.get("moderator-reason").value = "Resume moderation"; b.moderators[0].version = 4;
 await h.click("moderator-prepare"); assert.equal(b.effects, 0); assert.equal(h.get("moderator-prepare").disabled, false); assert.equal(h.get("moderator-reason").value, "Resume moderation"); assert.match(h.text("status"), /current assignment/);
 await h.click("moderators-reload"); await h.click("moderators-next"); await h.choose("moderators-list"); assert.match(h.text("moderator-version"), /4/);
});

test("moderator fields and rows clear on signout and translated history has no internal action tags", async () => {
 const b = backend(); b.caps = { global_administrator: true }; b.moderators = [{ user_id: randomUUID(), enabled: true, version: 1 }]; const h = harness(b); await h.start(b.owner, "uk");
 assert.match(h.text("moderators-heading"), /Модераторы/); assert.match(h.text("moderator-version"), /Новое назначение/); await h.click("moderators-reload"); await h.choose("moderators-list");
 await h.choose("results"); await h.click("history"); assert.match(h.text("revisions"), /Редактирование/); assert.doesNotMatch(h.text("revisions"), / · edit · /);
 h.ui.clear(); assert.equal(h.get("moderators").hidden, true); assert.equal(h.get("moderator-user").value, ""); assert.equal(h.get("moderators-list").children.length, 0);
});
