"use strict";
const assert = require("node:assert/strict");
const { webcrypto, randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/orders.js"), "utf8");
const proofSource = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/order-proof.js"), "utf8");
const appSource = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/app.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218";
const other = "510e58dd-e547-48d4-b9fa-571d24953219";
const eventID = "8cc5e2de-729c-4604-a85b-f97cdd184795";
const clone = value => JSON.parse(JSON.stringify(value));
const storageKey = user => "zinusya-orders-pending-v1:" + user;
const catalog = () => ({ event_id: eventID, event_version: 1, version: 1, event_name: "Fixture event", timezone: "Europe/Berlin", currency: "BYN", currency_scale: 2,
  complete_pricing: { version: 1, supplements: [] }, items: [{ id: "soup", name: { en: "Soup <img>", ru: "Суп <img>" }, base_price_minor: 1234 }],
  offers: [{ id: "lunch", name: { en: "Lunch", ru: "Обед" }, item_ids: ["soup"], sales_open: true,
    starts_at: "2026-10-25T00:30:00Z", ends_at: "2026-10-25T01:30:00Z", sales_opens_at: "2026-10-01T00:00:00Z", sales_closes_at: "2026-10-24T00:00:00Z" }] });
function backend() {
  const api = { catalog: catalog(), current: { event_id: eventID, version: 0, selections: [] }, receipts: new Map(), hook: undefined };
  api.quote = rows => ({ event_id: eventID, event_version: api.catalog.event_version, version: api.catalog.version,
    timezone: api.catalog.timezone, observed_at: "2026-10-16T14:00:00Z", currency: "BYN", currency_scale: 2,
    total_minor: rows.reduce((sum, r) => sum + r.quantity * 1234, 0),
    items: rows.map(r => ({ offer_id: r.offer_id, id: r.item_id, quantity: r.quantity, name: { en: "Soup <img>", ru: "Суп <img>" }, unit_price_minor: 1234, amount_minor: r.quantity * 1234 })), supplements: [] });
  api.base = (path, method, body) => {
    if (path.startsWith("./orders/events")) return { events: [{ id: eventID, name: "Event <script>", timezone: "Europe/Berlin" }] };
    if (path.endsWith("/catalog")) return api.catalog;
    if (path.endsWith("/submitted")) return { orders: [] };
    if (path.includes("/operations/")) {
      const receipt = api.receipts.get(path.split("/").at(-1)); if (!receipt) throw { status: 404 }; return receipt;
    }
    if (path.endsWith("/quote")) return api.quote(body.selections);
    if (path.endsWith("/selection") && method === "POST") {
      if (api.receipts.has(body.operation_id)) return api.receipts.get(body.operation_id);
      if (body.expected_draft_version !== api.current.version || body.expected_catalog_version !== api.catalog.version) throw { status: 409 };
      api.current = { event_id: eventID, version: api.current.version + 1, event_version: api.catalog.event_version,
        catalog_version: api.catalog.version, selections: clone(body.selections) };
      if (body.selections.length) api.current.quote = api.quote(body.selections);
      api.receipts.set(body.operation_id, clone(api.current)); return api.current;
    }
    if (path.endsWith("/selection")) return api.current;
    throw new Error("Unexpected request " + path);
  };
  api.request = async (path, method = "GET", body) => {
    const result = api.hook ? await api.hook(path, method, body, api.base) : api.base(path, method, body);
    return clone(result);
  };
  return api;
}
function browser(api, { storage = new Map(), app = false, proof = false, failSet = false, continuation, hash = "" } = {}) {
  const elements = new Map(), calls = [], writes = [];
  function element(tag = "div") {
    const e = { tag, children: [], hidden: false, disabled: false, value: "", listeners: {},
      append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { this.children = nodes; },
      addEventListener(name, fn) { this.listeners[name] = fn; }, removeAttribute(name) { delete this[name]; } };
    let text = "", id;
    Object.defineProperties(e, {
      textContent: { get: () => text, set: value => { text = String(value); e.children = []; } },
      id: { get: () => id, set: value => { id = value; elements.set(value, e); } },
      options: { get: () => e.children.length ? e.children : [{ textContent: "" }] },
      innerHTML: { set() { throw new Error("Unsafe HTML rendering"); } }
    });
    return e;
  }
  const get = id => { if (!elements.has(id)) elements.set(id, element()); return elements.get(id); };
  const context = { document: { getElementById: get, createElement: element, documentElement: {} },
    window: { location: { hash }, Telegram: { WebApp: { initData: "non-authorizing-fixture", ready() {} } } }, navigator: { language: "en" },
    crypto: webcrypto, TextEncoder, Uint8Array, btoa, Intl, console, URL: { createObjectURL: () => "blob:proof", revokeObjectURL() {} },
    sessionStorage: { getItem: key => storage.get(key) ?? null,
      setItem: (key, value) => { if (failSet) throw new Error("Storage unavailable"); storage.set(key, value); writes.push({ key, value }); },
      removeItem: key => storage.delete(key) } };
  let controller;
  const request = async (path, method, body) => {
    calls.push({ path, method, body: body === undefined ? undefined : clone(body), storedBefore: storage.get(storageKey(owner)) });
    return api.request(path, method, body);
  };
  const env = { continuation, request, setBusy: value => controller?.setBusy(value), denied() { controller.clear(); } };
  context.fetch = async (path, options) => {
    const body = options.body ? JSON.parse(options.body) : undefined;
    let result;
    try {
      if (proof && path.endsWith("/content")) { calls.push({ path, authorization: options.headers.Authorization }); return { ok: true, blob: async () => ({}) }; }
      if (path.startsWith("./orders/")) {
        calls.push({ path, method: options.method, body, authorization: options.headers.Authorization });
        result = await api.request(path, options.method, body);
      } else if (path === "./session/bootstrap" || path === "./me") result = { user_id: owner, session_token: "a".repeat(43), full_name: "Owner", bot_language: "en" };
      else if (path === "./profile") result = { version: 0, full_name: "Owner", bot_language: "en" };
      else if (path === "./account") result = { events: [], has_more: false, observed_at: "2026-10-16T14:00:00Z" };
      else if (path === "./registration-details") return { ok: false, status: 404 };
      else if (path === "./session/logout") return { ok: true, status: 204 };
      else throw new Error("Unexpected app request " + path);
      return { ok: true, status: 200, json: async () => result };
    } catch (error) { return { ok: false, status: error.status || 503 }; }
  };
  if (proof) runInNewContext(proofSource, context);
  runInNewContext(source, context);
  if (app) runInNewContext(appSource, context); else controller = context.window.createZinusyaOrders(env);
  return { calls, writes, storage, get, controller,
    async start(id = owner, language = "en") { await controller.start(id, language); },
    async click(id, event = "click") { const listener = get(id).listeners[event]; assert.ok(listener, id + " listener missing"); await listener({ preventDefault() {} }); },
    async select() { get("orders-event").value = eventID; await this.click("orders-event", "change"); },
    async quantity(value, offer = "lunch", item = "soup") { const input = get("order-qty-" + offer + ":" + item); input.value = String(value); await this.click("order-qty-" + offer + ":" + item, "input"); },
    async submit() { await this.click("orders-form", "submit"); },
    text(id) { const walk = e => [e.textContent, ...e.children.map(walk)].join("\n"); return walk(get(id)); },
    async settled() { for (let i = 0; i < 100; i++) { if (!get("retry").disabled && !get("orders").hidden) return; await new Promise(r => setTimeout(r, 5)); } assert.fail("App did not connect orders UI"); }
  };
}
const posts = b => b.calls.filter(c => c.method === "POST" && c.path.endsWith("/selection"));

test("explicit event, DST-aware quote, save and clear use server versions and durable operation", async () => {
  const api = backend(), b = browser(api); await b.start();
  assert.equal(b.get("orders-save").disabled, true); await b.select(); await b.quantity(2);
  await b.click("orders-calculate"); assert.match(b.text("orders-quote"), /BYN 24.68/);
  assert.match(b.text("orders-items"), /GMT\+2/); assert.match(b.text("orders-items"), /GMT\+1/);
  assert.match(b.text("orders-items"), /Soup <img>/);
  await b.submit(); assert.equal(posts(b).length, 1);
  const sent = posts(b)[0], saved = JSON.parse(sent.storedBefore);
  assert.deepEqual(saved.body, sent.body); assert.equal(saved.owner, owner); assert.equal(saved.event_id, eventID);
  assert.deepEqual(Object.keys(saved).sort(), ["body", "event_id", "owner"]); assert.equal(saved.body.expected_draft_version, 0);
  assert.equal(b.storage.has(storageKey(owner)), false); assert.match(b.text("orders-saved"), /version 1/);
  await b.click("orders-clear"); assert.equal(api.current.version, 2); assert.deepEqual(api.current.selections, []); assert.equal(api.current.quote, undefined);
});

test("lost committed reply reloads original receipt separately from newer agent edit", async () => {
  const api = backend(), storage = new Map(), b = browser(api, { storage }); await b.start(); await b.select(); await b.quantity(2);
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (method === "POST") throw { status: 503 }; return result; };
  await b.submit(); assert.equal(b.get("orders-save").disabled, true); const original = JSON.parse(storage.get(storageKey(owner)));
  api.hook = undefined; api.current = { ...api.current, version: 3, selections: [{ offer_id: "lunch", item_id: "soup", quantity: 7 }] };
  const reload = browser(api, { storage }); await reload.start();
  assert.equal(posts(reload).length, 0); assert.match(reload.text("orders-receipt"), /version 1/); assert.match(reload.text("orders-saved"), /version 3/);
  assert.equal(reload.get("order-qty-lunch:soup").value, "7"); assert.equal(storage.has(storageKey(owner)), false);
  assert.ok(api.receipts.has(original.body.operation_id));
});

test("missing receipt keeps operation through reload and explicit retry sends identical body", async () => {
  const api = backend(), storage = new Map(), b = browser(api, { storage }); await b.start(); await b.select(); await b.quantity(4);
  api.hook = (path, method, body, normal) => { if (method === "POST") throw { status: 503 }; return normal(path, method, body); };
  await b.submit(); const original = posts(b)[0].body; api.hook = undefined;
  const reload = browser(api, { storage }); await reload.start(); assert.equal(posts(reload).length, 0); assert.equal(reload.get("orders-save").disabled, true);
  assert.match(reload.text("orders-status"), /no confirmed result/); await reload.click("orders-retry");
  assert.deepEqual(posts(reload)[0].body, original); assert.equal(api.current.version, 1); assert.equal(api.receipts.size, 1);
});

test("401 after commit preserves owner-bound operation and another account cannot recover it", async () => {
  const api = backend(), storage = new Map(), b = browser(api, { storage }); await b.start(); await b.select(); await b.quantity(1);
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (method === "POST") throw { status: 401 }; return result; };
  await b.submit(); assert.equal(b.get("orders").hidden, true); assert.equal(b.text("orders-items"), ""); assert.ok(storage.has(storageKey(owner)));
  api.hook = undefined; const second = browser(api, { storage }); await second.start(other);
  assert.equal(second.calls.some(c => c.path.includes("/operations/")), false); assert.ok(storage.has(storageKey(owner)));
  const returned = browser(api, { storage }); await returned.start(owner); assert.match(returned.text("orders-receipt"), /version 1/); assert.equal(posts(returned).length, 0);
});

test("terminal CAS rejection blocks a new edit until explicit reload, without silent reprice", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.quantity(5);
  api.current = { ...api.current, version: 2, selections: [{ offer_id: "lunch", item_id: "soup", quantity: 3 }] };
  await b.submit(); assert.equal(posts(b).length, 1); assert.equal(b.get("orders-save").disabled, true); assert.equal(b.get("order-qty-lunch:soup").value, "5");
  assert.equal(b.storage.has(storageKey(owner)), false); await b.click("orders-reload");
  assert.equal(b.get("order-qty-lunch:soup").value, "3"); assert.equal(b.get("orders-save").disabled, false); assert.equal(posts(b).length, 1);
});

test("storage failure prevents dispatch and malformed recovery record prevents replacement", async () => {
  const api = backend(), b = browser(api, { failSet: true }); await b.start(); await b.select(); await b.quantity(1); await b.submit();
  assert.equal(posts(b).length, 0); assert.match(b.text("orders-status"), /storage is unavailable/);
  const storage = new Map([[storageKey(owner), "{broken"]]), reload = browser(api, { storage }); await reload.start();
  assert.equal(reload.calls.length, 0); assert.equal(reload.get("orders-save").disabled, true); assert.match(reload.text("orders-status"), /invalid/);
});

test("closed and removed quantities stay visible until explicitly removed", async () => {
  const api = backend(); api.catalog.offers[0].sales_open = false;
  api.current = { event_id: eventID, version: 2, selections: [{ offer_id: "lunch", item_id: "soup", quantity: 2 }, { offer_id: "old", item_id: "dish", quantity: 3 }] };
  const b = browser(api); await b.start(); await b.select();
  assert.equal(b.get("order-qty-lunch:soup").value, "2"); assert.equal(b.get("order-qty-old:dish").value, "3");
  await b.submit(); assert.equal(posts(b).length, 0); assert.match(b.text("orders-status"), /must be set to zero/);
  await b.quantity(0); await b.quantity(0, "old", "dish"); await b.submit(); assert.deepEqual(posts(b)[0].body.selections, []); assert.equal(api.current.version, 3);
});

test("locale switch preserves unsent quantities and navigation requires an explicit discard", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.quantity(6);
  b.controller.setLanguage("ru"); assert.equal(b.get("order-qty-lunch:soup").value, "6"); assert.match(b.text("orders-title"), /Сохранённый/);
  b.get("orders-event").value = "510e58dd-e547-48d4-b9fa-571d24953220"; await b.click("orders-event", "change"); assert.equal(b.get("orders-event").value, eventID);
  assert.equal(posts(b).length, 0); await b.click("orders-reload"); assert.equal(b.get("order-qty-lunch:soup").value, "0");
});

test("verified receipt remains confirmed when loading the current catalog fails", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select(); await b.quantity(1);
  api.hook = (path, method, body, normal) => { if (method === "GET" && path.endsWith("/catalog")) throw { status: 404 }; return normal(path, method, body); };
  await b.submit(); assert.match(b.text("orders-receipt"), /version 1/); assert.equal(b.storage.has(storageKey(owner)), false);
  assert.doesNotMatch(b.text("orders-status"), /no confirmed result/); assert.equal(b.get("orders-save").disabled, true);
});

test("delayed previous-owner read cannot repaint cleared UI", async () => {
  const api = backend(), b = browser(api); await b.start(); let finish;
  api.hook = (path, method, body, normal) => path.endsWith("/selection") ? new Promise(r => { finish = () => r(normal(path, method, body)); }) : normal(path, method, body);
  const reading = b.select(); await Promise.resolve(); b.controller.clear(); finish(); await reading;
  assert.equal(b.get("orders").hidden, true); assert.equal(b.text("orders-saved"), ""); assert.equal(b.text("orders-items"), "");
});

test("real app hook gives the module its verified owner/session and logout clears rendered values", async () => {
  const api = backend(), b = browser(api, { app: true }); await b.settled(); await b.select(); await b.quantity(2); await b.submit();
  assert.equal(posts(b).length, 1); assert.equal(posts(b)[0].authorization, "ZinusyaSession " + "a".repeat(43));
  await b.click("logout"); assert.equal(b.get("orders").hidden, true); assert.equal(b.text("orders-saved"), ""); assert.equal(b.text("orders-items"), "");
  for (const write of b.writes.filter(w => w.key.startsWith("zinusya-orders-pending-v1:"))) assert.doesNotMatch(write.value, /session_token|Authorization|aaaaaaaa/);
});

test("receipt recovery off the first page keeps explicit event identity, and switching clears its confirmation", async () => {
  const api = backend(), storage = new Map(), first = browser(api, { storage }); await first.start(); await first.select(); await first.quantity(1);
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (method === "POST") throw { status: 503 }; return result; };
  await first.submit();
  api.hook = (path, method, body, normal) => path.startsWith("./orders/events") ? { events: [] } : normal(path, method, body);
  const reload = browser(api, { storage }); await reload.start();
  assert.ok(Array.from(reload.get("orders-event").options).some(option => option.value === eventID && option.textContent.includes("Fixture event")));
  assert.equal(reload.get("orders-event").value, eventID); assert.match(reload.text("orders-receipt"), /version 1/);
  api.hook = (path, method, body, normal) => path.includes(other)
    ? path.endsWith("/catalog") ? { ...api.catalog, event_id: other, event_name: "Second event" } : { event_id: other, version: 0, selections: [] }
    : normal(path, method, body);
  reload.get("orders-event").value = other; await reload.click("orders-event", "change");
  assert.equal(reload.text("orders-receipt"), ""); assert.match(reload.text("orders-saved"), /version 0/);
});

test("choosing the placeholder cannot leave an editable event without its visible identity", async () => {
  const api = backend(), b = browser(api); await b.start(); await b.select();
  b.get("orders-event").value = ""; await b.click("orders-event", "change");
  assert.equal(b.get("orders-event").value, eventID); assert.equal(posts(b).length, 0);
});

const orderStorageKey = user => "zinusya-orders-operation-v1:" + user;
function submissionBackend() {
  const api = backend(), selectionBase = api.base;
  api.preparations = new Map(); api.orderReceipts = new Map(); api.orders = [];
  api.base = (path, method, body) => {
    if (path.includes("/operation-receipts/")) {
      const parts = path.split("/"), kind = parts.at(-2), id = parts.at(-1), result = api.orderReceipts.get(kind + ":" + id);
      if (!result) throw { status: 404 };
      return { operation_id: id, event_id: eventID, kind, result: clone(result) };
    }
    const kind = path.endsWith("/submit/prepare") ? "prepare" : path.endsWith("/submit") ? "submit" : path.endsWith("/cancel") ? "cancel" : undefined;
    if (method === "POST" && kind) {
      const key = kind + ":" + body.operation_id;
      if (api.orderReceipts.has(key)) return clone(api.orderReceipts.get(key));
      let result;
      if (kind === "cancel") {
        const order = api.orders.find(o => o.id === body.order_id);
        if (!order || order.version !== body.expected_version || order.state !== "submitted") throw { status: 409 };
        order.state = "cancelled"; order.version++; result = clone(order);
      } else {
        if (body.expected_draft_version !== api.current.version || body.expected_catalog_version !== api.catalog.version || body.expected_event_version !== api.catalog.event_version) throw { status: 409 };
        if (kind === "prepare") {
          result = { proposal_id: randomUUID(), arguments: { event_id: eventID, expected_event_version: body.expected_event_version, expected_catalog_version: body.expected_catalog_version, expected_draft_version: body.expected_draft_version }, expires_at: "2099-10-24T00:00:00Z",
            snapshot: { event_name: "Fixture event <script>", selections: clone(api.current.selections), quote: api.quote(api.current.selections), sales_closes_at: "2099-10-24T00:00:00Z" } };
          result.snapshot.quote.supplements = [{ offer_id: "lunch", id: "service", name: { en: "Service", ru: "Сервис" }, quantity: 1, unit_price_minor: 100, amount_minor: 100 }];
          result.snapshot.quote.total_minor += 100;
          api.preparations.set(result.proposal_id, clone(result));
        } else {
          const prepared = api.preparations.get(body.proposal_id); assert.ok(prepared); assert.equal(body.confirmed, true);
          const order = { id: randomUUID(), event_id: eventID, source_draft_version: api.current.version, version: 1, state: "submitted", snapshot: clone(prepared.snapshot), submitted_at: "2026-10-17T00:00:00Z" };
          api.orders.push(order); api.current = { event_id: eventID, version: api.current.version + 1, selections: [] };
          result = { order: clone(order), selection: clone(api.current) };
        }
      }
      api.orderReceipts.set(key, clone(result)); return result;
    }
    if (path.includes("/submitted")) {
      const cursor = new URL(path, "https://fixture.invalid").searchParams.get("after"), start = cursor ? api.orders.findIndex(o => o.id === cursor) + 1 : 0;
      const rows = api.orders.slice(start, start + 1), next = start + rows.length < api.orders.length ? rows.at(-1)?.id : undefined;
      return { orders: clone(rows), ...(next ? { next } : {}) };
    }
    return selectionBase(path, method, body);
  };
  return api;
}
const orderPosts = (b, kind) => b.calls.filter(c => c.method === "POST" && c.path.endsWith(kind === "prepare" ? "/submit/prepare" : "/" + kind));
async function savedOrderBrowser(api = submissionBackend(), options) {
  const b = browser(api, options); await b.start(); await b.select(); await b.quantity(2); await b.submit(); return { api, b };
}

test("review shows exact lines and charges; only explicit confirmation submits; cancellation targets that order", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare");
  assert.equal(orderPosts(b, "submit").length, 0); assert.match(b.text("orders-preview"), /Soup <img>/); assert.match(b.text("orders-preview"), /BYN 12.34 each = BYN 24.68/);
  assert.match(b.text("orders-preview"), /Required charge: Service/); assert.match(b.text("orders-preview"), /Total: BYN 25.68/); assert.match(b.text("orders-preview"), /Europe\/Berlin/);
  assert.equal(b.get("orders-confirm").disabled, false); await b.click("orders-confirm"); assert.equal(api.orders.length, 1);
  assert.equal(orderPosts(b, "submit")[0].body.operation_id, orderPosts(b, "prepare")[0].body.operation_id);
  assert.equal(b.storage.has(orderStorageKey(owner)), false); assert.match(b.text("orders-order-status"), /unpaid|payment is not confirmed/);
  assert.match(b.text("orders-submitted"), /Submitted; check payment proofs/); assert.match(b.text("orders-submitted"), /BYN 25.68/);
  const article = b.get("orders-submitted").children[0], cancel = article.children.find(e => e.tag === "button"); assert.ok(cancel); await cancel.listeners.click();
  assert.equal(api.orders[0].state, "cancelled"); assert.equal(api.orders[0].version, 2); assert.equal(orderPosts(b, "cancel")[0].body.order_id, api.orders[0].id);
  assert.match(b.text("orders-submitted"), /Order cancelled/);
});

test("lost submit reply recovers original receipt without clearing a newer agent draft or resubmitting", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare");
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (method === "POST" && path.endsWith("/submit")) throw { status: 503 }; return result; };
  await b.click("orders-confirm"); const record = JSON.parse(b.storage.get(orderStorageKey(owner))); assert.equal(record.kind, "submit"); assert.deepEqual(record.body, orderPosts(b, "submit")[0].body);
  assert.equal(b.get("orders-save").disabled, true); api.hook = undefined;
  api.current = { event_id: eventID, version: 7, selections: [{ offer_id: "lunch", item_id: "soup", quantity: 9 }] };
  const reload = browser(api, { storage: b.storage }); await reload.start();
  assert.equal(orderPosts(reload, "submit").length, 0); assert.equal(api.orders.length, 1); assert.match(reload.text("orders-order-status"), /Earlier request completed/);
  assert.equal(reload.get("order-qty-lunch:soup").value, "9"); assert.equal(reload.storage.has(orderStorageKey(owner)), false);
});

test("unknown submit phase stays pending across reload; only explicit retry sends the identical body", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare");
  api.hook = (path, method, body, normal) => { if (method === "POST" && path.endsWith("/submit")) throw { status: 503 }; return normal(path, method, body); };
  await b.click("orders-confirm"); const original = orderPosts(b, "submit")[0].body; api.hook = undefined;
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.equal(api.orders.length, 0);
  assert.match(reload.text("orders-order-status"), /no confirmed result/); assert.equal(reload.get("orders-event").disabled, true);
  await reload.click("orders-order-retry"); assert.deepEqual(orderPosts(reload, "submit")[0].body, original); assert.equal(api.orders.length, 1);
});

test("lost preparation recovers review after page restart and never automatically confirms", async () => {
  const { api, b } = await savedOrderBrowser();
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (path.endsWith("/submit/prepare")) throw { status: 503 }; return result; };
  await b.click("orders-prepare"); assert.equal(api.preparations.size, 1); api.hook = undefined;
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.match(reload.text("orders-preview"), /Total: BYN 25.68/);
  assert.equal(reload.get("orders-confirm").disabled, false); assert.equal(orderPosts(reload, "submit").length, 0); assert.equal(api.orders.length, 0);
  await reload.click("orders-confirm"); assert.equal(api.orders.length, 1);
  assert.equal(orderPosts(reload, "submit")[0].body.operation_id, orderPosts(b, "prepare")[0].body.operation_id);
});

test("unsaved edits invalidate review; stale submission does not reprice or replace its confirmation", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare"); await b.quantity(3);
  assert.equal(b.get("orders-confirm").disabled, true); assert.equal(b.text("orders-preview").trim(), "");
  await b.click("orders-confirm"); assert.equal(orderPosts(b, "submit").length, 0); await b.submit(); await b.click("orders-prepare");
  api.catalog.version++; await b.click("orders-confirm"); assert.equal(api.orders.length, 0); assert.match(b.text("orders-order-status"), /rejected/);
  assert.equal(b.get("orders-prepare").disabled, true); await b.click("orders-reload"); assert.equal(b.get("orders-prepare").disabled, false);
});

test("order pending storage and owner isolation prevent replacement or cross-account recovery", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare");
  api.hook = (path, method, body, normal) => { if (method === "POST" && path.endsWith("/submit")) throw { status: 401 }; return normal(path, method, body); };
  await b.click("orders-confirm"); assert.equal(b.get("orders").hidden, true); const raw = b.storage.get(orderStorageKey(owner)); assert.ok(raw);
  const otherBrowser = browser(api, { storage: b.storage }); await otherBrowser.start(other); assert.equal(otherBrowser.calls.some(c => c.path.includes("operation-receipts")), false);
  const broken = browser(api, { storage: new Map([[orderStorageKey(owner), "{}"]]) }); await broken.start(); assert.equal(broken.calls.length, 0); assert.equal(broken.get("orders-prepare").disabled, true);
  const noStorage = browser(api, { failSet: true }); await noStorage.start(); await noStorage.select(); await noStorage.click("orders-prepare"); assert.equal(orderPosts(noStorage, "prepare").length, 0);
});

test("submitted pages retain full snapshots and RU labels switch without issuing another write", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare"); await b.click("orders-confirm");
  await b.quantity(1); await b.submit(); await b.click("orders-prepare"); await b.click("orders-confirm"); assert.equal(api.orders.length, 2);
  const first = b.text("orders-submitted"); await b.click("orders-submitted-next"); const second = b.text("orders-submitted"); assert.notEqual(first, second);
  assert.match(second, /Soup <img>/); b.controller.setLanguage("ru"); assert.match(b.text("orders-submitted"), /Суп <img>/); assert.match(b.text("orders-submitted"), /Обязательная доплата: Сервис/);
  assert.equal(orderPosts(b, "submit").length, 2); await b.click("orders-submitted-first"); assert.match(b.text("orders-submitted"), new RegExp(api.orders[0].id));
});


test("failed event switch removes the previous event's orders and cancellation controls", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare"); await b.click("orders-confirm");
  assert.match(b.text("orders-submitted"), new RegExp(api.orders[0].id));
  const eventB = "8cc5e2de-729c-4604-a85b-f97cdd184796";
  api.hook = (path, method, body, normal) => { if (path.includes(eventB)) throw { status: 503 }; return normal(path, method, body); };
  b.get("orders-event").value = eventB; await b.click("orders-event", "change");
  assert.doesNotMatch(b.text("orders-submitted"), new RegExp(api.orders[0].id));
  assert.equal(b.get("orders-submitted").children.some(e => e.children.some(c => c.tag === "button")), false);
  assert.equal(b.text("orders-order-status"), ""); assert.equal(orderPosts(b, "cancel").length, 0);
});


test("lost cancellation recovers its receipt after reload and can continue editing the current selection", async () => {
  const { api, b } = await savedOrderBrowser(); await b.click("orders-prepare"); await b.click("orders-confirm");
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (method === "POST" && path.endsWith("/cancel")) throw { status: 503 }; return result; };
  const button = b.get("orders-submitted").children[0].children.find(e => e.tag === "button"); await button.listeners.click(); assert.equal(api.orders[0].state, "cancelled");
  const pending = JSON.parse(b.storage.get(orderStorageKey(owner))); assert.equal(pending.kind, "cancel"); api.hook = undefined;
  const reload = browser(api, { storage: b.storage }); await reload.start(); assert.match(reload.text("orders-submitted"), /Order cancelled/);
  assert.equal(orderPosts(reload, "cancel").length, 0); assert.equal(reload.get("orders-save").disabled, false);
  assert.equal(reload.get("orders-event").value, eventID); assert.equal(reload.storage.has(orderStorageKey(owner)), false);
});

test("expired preparation cannot submit and a late successful reply cannot repaint logged-out UI", async () => {
  const { api, b } = await savedOrderBrowser();
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); if (path.endsWith("/submit/prepare")) result.expires_at = "2000-01-01T00:00:00Z"; return result; };
  await b.click("orders-prepare"); assert.equal(b.get("orders-confirm").disabled, true); await b.click("orders-confirm"); assert.equal(api.orders.length, 0);
  api.hook = undefined; await b.click("orders-prepare"); let release;
  api.hook = (path, method, body, normal) => { const result = normal(path, method, body); return method === "POST" && path.endsWith("/submit") ? new Promise(resolve => { release = () => resolve(result); }) : result; };
  const submitting = b.click("orders-confirm"); assert.equal(typeof release, "function"); b.controller.clear(); release(); await submitting;
  assert.equal(b.get("orders").hidden, true); assert.equal(b.text("orders-submitted"), ""); assert.equal(b.text("orders-order-status"), ""); assert.ok(b.storage.get(orderStorageKey(owner)));
});


test("same-name same-price items remain distinguishable in exact order review", async () => {
  const api = submissionBackend(), item = api.catalog.items[0];
  api.catalog.items = [{ ...item, id: "item_a" }, { ...item, id: "item_b" }];
  api.catalog.offers[0].item_ids = ["item_a", "item_b"];
  const b = browser(api); await b.start(); await b.select();
  await b.quantity(2, "lunch", "item_a"); await b.quantity(2, "lunch", "item_b"); await b.submit();
  await b.click("orders-prepare"); assert.match(b.text("orders-preview"), /lunch\/item_a/); assert.match(b.text("orders-preview"), /lunch\/item_b/);
  assert.match(b.text("orders-preview"), /Total: BYN 50.36/); assert.equal(orderPosts(b, "submit").length, 0);
});


test("service names and DST times remain frozen in review and history across catalog and language changes", async () => {
  const { api, b } = await savedOrderBrowser();
  const normal = api.base;
  api.hook = (path, method, body) => {
    const result = normal(path, method, body);
    if (method === "POST" && path.endsWith("/submit/prepare")) {
      result.snapshot.services = [{ offer_id: "lunch", name: { en: "Sold lunch <img>", ru: "Проданный обед <img>" },
        starts_at: "2026-10-25T00:30:00Z", ends_at: "2026-10-25T01:30:00Z" }];
      api.preparations.set(result.proposal_id, clone(result));
    }
    return result;
  };
  await b.click("orders-prepare");
  const preview = b.text("orders-preview");
  assert.match(preview, /Sold lunch <img>/);
  assert.match(preview, /GMT\+2/); assert.match(preview, /GMT\+1/);
  await b.click("orders-confirm");
  api.catalog.offers[0].name = { en: "Changed catalog", ru: "Изменено" };
  api.catalog.offers[0].starts_at = "2099-01-01T00:00:00Z";
  await b.click("orders-submitted-reload");
  assert.match(b.text("orders-submitted"), /Sold lunch <img>/);
  assert.doesNotMatch(b.text("orders-submitted"), /Changed catalog|2099, 1/);
  b.controller.setLanguage("ru");
  assert.match(b.text("orders-submitted"), /Проданный обед <img>/);
  assert.match(b.text("orders-submitted"), /GMT\+2/); assert.match(b.text("orders-submitted"), /GMT\+1/);
  assert.equal(api.orders.length, 1);
});


test("resource requirements remain frozen across catalog edits and RU history rendering", async () => {
  const { api, b } = await savedOrderBrowser();
  const normal = api.base;
  api.hook = (path, method, body) => {
    const result = normal(path, method, body);
    if (method === "POST" && path.endsWith("/submit/prepare")) {
      result.snapshot.capacity = { version: 1, demands: [{ resource_id: "seat", name: { en: "Sold seat <img>", ru: "Проданное место <img>" }, units: 8 }] };
      api.preparations.set(result.proposal_id, clone(result));
    }
    return result;
  };
  await b.click("orders-prepare");
  assert.match(b.text("orders-preview"), /Sold seat <img> · seat × 8/);
  assert.doesNotMatch(b.text("orders-preview"), /not configured/);
  await b.click("orders-confirm");
  api.catalog.complete_capacity = { version: 1, resources: [], bindings: [] };
  await b.click("orders-submitted-reload");
  b.controller.setLanguage("ru");
  assert.match(b.text("orders-submitted"), /Проданное место <img> · seat × 8/);
  assert.equal(api.orders.length, 1);
});

test("unknown resources differ from explicit none and malformed requirements prevent consent", async () => {
  for (const [capacity, expected] of [[undefined, /not configured/], [{ version: 1, demands: [] }, /No limited resources required/]]) {
    const { api, b } = await savedOrderBrowser();
    const normal = api.base;
    api.hook = (path, method, body) => {
      const result = normal(path, method, body);
      if (method === "POST" && path.endsWith("/submit/prepare")) {
        result.snapshot.capacity = capacity;
        api.preparations.set(result.proposal_id, clone(result));
      }
      return result;
    };
    await b.click("orders-prepare"); assert.match(b.text("orders-preview"), expected);
    assert.equal(b.get("orders-confirm").disabled, false);
  }
  const { api, b } = await savedOrderBrowser(); const normal = api.base;
  api.hook = (path, method, body) => {
    const result = normal(path, method, body);
    if (method === "POST" && path.endsWith("/submit/prepare")) result.snapshot.capacity = { version: 1, demands: [{ resource_id: "seat", units: -1 }] };
    return result;
  };
  await b.click("orders-prepare"); assert.equal(b.get("orders-confirm").disabled, true);
  await b.click("orders-confirm"); assert.equal(orderPosts(b, "submit").length, 0);
});

test("real app connects order selection, proof preview and owner-bound submission to its session", async () => {
  const api=submissionBackend(), id=randomUUID(), file=randomUUID();
  const order={id,event_id:eventID,version:1,state:"submitted",snapshot:{event_name:"Synthetic proof order",sales_closes_at:"2099-10-24T00:00:00Z",quote:api.quote([{offer_id:"lunch",item_id:"soup",quantity:2}])},submitted_at:"2026-10-17T00:00:00Z"};
  api.orders=[order];let claim;
  api.hook=(path,method,body,base)=>{
    if(path.includes("/submitted/"))return order;
    if(path.includes("/proof-files"))return {files:[{id:file,filename:"proof.pdf",kind:"document",sent_at:"2026-10-17T00:00:00Z"}]};
    if(path.includes("/payment-evidence?"))return {evidence:claim?[claim]:[]};
    if(path.endsWith("/payment-evidence")&&method==="POST")return claim={id:randomUUID(),event_id:eventID,order_id:id,user_id:owner,file_id:body.file_id,amount_minor:body.amount_minor,quoted_amount_minor:2468,currency:"BYN",currency_scale:2,version:1,state:"pending",sent_at:"2026-10-17T00:00:00Z",due_at:order.snapshot.sales_closes_at};
    return base(path,method,body);
  };
  const b=browser(api,{app:true,proof:true});await b.settled();await b.select();
  const button=b.get("orders-submitted").children[0].children.find(n=>n.tag==="button"&&n.textContent==="Payment and proofs");assert.ok(button);await button.listeners.click();
  assert.equal(b.get("order-proof").hidden,false);b.get("op-file").value=file;await b.click("op-file","change");await b.click("op-preview");assert.equal(b.get("op-download").hidden,false);
  await b.click("op-form","submit");assert.equal(claim.order_id,id);assert.equal(claim.file_id,file);assert.equal(claim.amount_minor,2468);
  for(const call of b.calls.filter(c=>c.path.includes("/proof-files")||c.path.includes("/payment-evidence")))assert.equal(call.authorization,"ZinusyaSession "+"a".repeat(43));
  await b.click("logout");assert.equal(b.get("order-proof").hidden,true);assert.equal(b.text("op-target"),"");assert.equal(b.get("op-download").hidden,true);
});

function linkedPreparation(api) {
  api.current={event_id:eventID,version:1,selections:[{offer_id:"lunch",item_id:"soup",quantity:2}]};
  const operation_id=randomUUID();
  const body={operation_id,expected_event_version:1,expected_catalog_version:1,expected_draft_version:1};
  const prepared=api.base("./orders/"+eventID+"/submit/prepare","POST",body);
  return {continuation:{event_id:eventID,operation_id},prepared,body};
}
test("linked preparation opens read-only and explicit confirmation retains original operation",async()=>{
 const api=submissionBackend(), link=linkedPreparation(api), b=browser(api,link);
 await b.start();
 assert.match(b.text("orders-preview"),/BYN 25.68/);
 assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
 assert.equal(b.get("orders-confirm").disabled,false);
 await b.click("orders-confirm");
 assert.equal(orderPosts(b,"submit")[0].body.operation_id,link.body.operation_id);
 assert.equal(orderPosts(b,"prepare").length,0);
 assert.equal(api.orders.length,1);
});
test("linked completed operation wins and a newer draft is preserved",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api);
 api.base("./orders/"+eventID+"/submit","POST",{...link.body,proposal_id:link.prepared.proposal_id,confirmed:true});
 api.current={event_id:eventID,version:7,selections:[{offer_id:"lunch",item_id:"soup",quantity:9}]};
 const b=browser(api,link);await b.start();
 assert.match(b.text("orders-order-status"),/Earlier request completed/);
 assert.equal(b.get("order-qty-lunch:soup").value,"9");
 assert.equal(b.calls.some(c=>c.path.includes("/operation-receipts/prepare/")),false);
 assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
 assert.equal(api.orders.length,1);
});
test("linked old preparation shows frozen terms but cannot consume a newer draft",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api);
 api.current={event_id:eventID,version:2,selections:[{offer_id:"lunch",item_id:"soup",quantity:8}]};
 const b=browser(api,link);await b.start();
 assert.match(b.text("orders-preview"),/BYN 25.68/);
 assert.equal(b.get("order-qty-lunch:soup").value,"8");
 assert.equal(b.get("orders-confirm").disabled,true);
 await b.click("orders-confirm");assert.equal(orderPosts(b,"submit").length,0);
});
test("unknown local save takes priority over linked operation and keeps its original key",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api),operation_id=randomUUID();
 const pending={owner,event_id:eventID,body:{...link.body,operation_id,selections:[{offer_id:"lunch",item_id:"soup",quantity:4}]}};
 const storage=new Map([[storageKey(owner),JSON.stringify(pending)]]);
 const b=browser(api,{...link,storage});await b.start();
 assert.equal(b.calls.some(c=>c.path.includes("/operation-receipts/")),false);
 assert.equal(storage.get(storageKey(owner)),JSON.stringify(pending));
 assert.match(b.text("orders-order-status"),/earlier.*before|прежн/i);
});
test("unknown linked operation is read-only and can be rechecked without a replacement",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api);api.orderReceipts.clear();
 const b=browser(api,link);await b.start();
 assert.match(b.text("orders-order-status"),/no confirmed result/);
 assert.equal(b.get("orders-order-recover").hidden,false);
 assert.equal(b.get("orders-prepare").disabled,true);
 await b.click("orders-prepare");assert.equal(orderPosts(b,"prepare").length,0);
 api.orderReceipts.set("prepare:"+link.body.operation_id,clone(link.prepared));
 await b.click("orders-order-recover");assert.match(b.text("orders-preview"),/BYN 25.68/);
 assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
});
test("real app passes the authenticated order operation fragment to the form",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api);
 const b=browser(api,{app:true,hash:"#orders?event="+eventID+"&operation="+link.body.operation_id+"&tgWebAppVersion=9.0"});
 await b.settled();for(let n=0;n<100&&!b.text("orders-preview");n++)await new Promise(r=>setTimeout(r,5));
 assert.match(b.text("orders-preview"),/BYN 25.68/);
 assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
});

test("failed cross-event link recheck clears the old editor and cannot send its quantities",async()=>{
 const api=submissionBackend(),link=linkedPreparation(api),b=browser(api,link);await b.start();
 api.hook=(path,method,body,normal)=>{
  const value=normal(path,method,body);
  if(path.startsWith("./orders/"+other+"/selection"))return {...value,event_id:other};
  if(path.startsWith("./orders/"+other+"/catalog"))return {...value,event_id:other,event_name:"Other event"};
  return value;
 };
 b.get("orders-event").value=other;await b.click("orders-event","change");
 assert.equal(b.get("order-qty-lunch:soup").value,"2");
 api.hook=(path,method,body,normal)=>{if(path.includes("/operation-receipts/"))throw {status:503};return normal(path,method,body);};
 await b.click("orders-order-recover");
 assert.equal(b.get("orders-event").value,eventID);
 assert.equal(b.get("orders-items").children.length,0);
 assert.equal(b.get("orders-saved").children.length,0);
 assert.equal(b.get("orders-save").disabled,true);
 assert.equal(b.get("orders-prepare").disabled,true);
 await b.submit();await b.click("orders-prepare");
 assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
});
