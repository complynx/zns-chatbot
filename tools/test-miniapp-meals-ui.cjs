"use strict";
const assert = require("node:assert/strict");
const { randomUUID } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/meals.js"), "utf8");
const owner = "510e58dd-e547-48d4-b9fa-571d24953218", other = "510e58dd-e547-48d4-b9fa-571d24953219", eventID = "8cc5e2de-729c-4604-a85b-f97cdd184795";
const copy = x => JSON.parse(JSON.stringify(x));
const key = user => "zinusya-meals-pending-v1:" + user;
function backend() {
  const service = { starts_at: "2026-10-25T00:30:00Z", ends_at: "2026-10-25T01:30:00Z", sales_opens_at: "2026-10-01T00:00:00Z", sales_closes_at: "2026-10-24T00:00:00Z", items: ["soup", "main", "side", "salad"].map(id => ({ id, category: id, name: { en: id + " <img>", ru: "Блюдо " + id }, price_minor: 100 })) };
  const catalog = { event_id: eventID, event_version: 1, version: 1, event_name: "Event <script>", timezone: "Europe/Berlin", document: { currency: "BYN", currency_scale: 2, days: [{ id: "day", name: { en: "Day <script>", ru: "День" }, lunch: { service, combos: { with_soup_minor: 665, without_soup_minor: 555 } }, dinner: { ...service, items: [{ id: "tea", name: { en: "Tea <img>", ru: "Чай" }, price_minor: 50 }] } }] } };
  const api = { catalog, history: new Map([[1, copy(catalog)]]), current: { event_id: eventID, version: 0 }, receipts: new Map(), hook: undefined };
  api.base = (path, method, body) => {
    if (path.startsWith("./meals/events")) return { events: [{ id: eventID, name: "Event <script>", timezone: "Europe/Berlin" }] };
    if (path.includes("/catalog?version=")) return copy(api.history.get(Number(path.split("=").at(-1))));
    if (path.endsWith("/catalog")) return api.catalog;
    if (path.includes("/operations/")) { const r = api.receipts.get(path.split("/").at(-1)); if (!r) throw { status: 404 }; return r; }
    if (path.endsWith("/draft") && method === "POST") {
      if (api.receipts.has(body.operation_id)) return api.receipts.get(body.operation_id);
      if (body.expected_draft_version !== api.current.version || body.expected_catalog_version !== api.catalog.version) throw { status: 409 };
      const choices = body.choices.length ? copy(body.choices) : [{ day_id: "day", lunch: null, dinner_ids: [] }], l = choices[0].lunch;
      const complete = !api.catalog.document.days[0].lunch || l?.mode === "no-lunch" || l?.mode === "individual-items" || (l?.main_id && l?.side_id && l?.salad_id && (l.mode !== "combo-with-soup" || l.soup_id));
      const amount = (l?.mode === "combo-with-soup" && complete ? 665 : l?.mode === "combo-no-soup" && complete ? 555 : (l?.individual_ids || []).length * 100) + (choices[0].dinner_ids || []).length * 50;
      api.current = { event_id: eventID, event_version: 1, catalog_version: api.catalog.version, version: api.current.version + 1, timezone: "Europe/Berlin", saved_at: "2026-10-19T10:00:00Z", cleared: !body.choices.length, evaluation: { choices, complete: !!complete, missing: complete ? [] : [{ day_id: "day", field: "lunch" }], known_amount_minor: amount, ...(complete ? { quote: { total_minor: amount } } : {}) } };
      api.receipts.set(body.operation_id, copy(api.current)); return api.current;
    }
    if (path.endsWith("/draft")) return api.current;
    throw new Error("Unexpected request " + path);
  };
  return api;
}
function browser(api, { storage = new Map(), failSet = false } = {}) {
  const elements = new Map(), calls = [];
  let activeElement, busy = false;
  function element(tag = "div") {
    const e = { tag, children: [], focus() { activeElement = this; }, listeners: {}, hidden: false, disabled: false, value: "", append(...nodes) { this.children.push(...nodes); }, replaceChildren(...nodes) { this.children = nodes; }, addEventListener(name, fn) { this.listeners[name] = fn; } };
    let text = "", id;
    Object.defineProperties(e, { textContent: { get: () => text, set: value => { text = String(value); e.children = []; } }, id: { get: () => id, set: value => { id = value; elements.set(id, e); } }, options: { get: () => e.children }, innerHTML: { set() { throw new Error("Unsafe HTML"); } } });
    return e;
  }
  const get = id => { if (!elements.has(id)) { const e = element(); e.id = id; } return elements.get(id); };
  let controller;
  const env = { request: async (path, method = "GET", body) => { calls.push({ path, method, body: body && copy(body) }); const r = api.hook ? await api.hook(path, method, body, api.base) : api.base(path, method, body); return copy(r); }, setBusy: value => { busy = value; controller.setBusy(value); }, denied: () => controller.clear() };
  const context = { window: { location: { hash: "#meals" } }, document: { getElementById: get, createElement: element, createTextNode: text => ({ textContent: text, children: [] }) }, sessionStorage: { getItem: k => storage.get(k) || null, setItem(k, v) { if (failSet) throw new Error("storage full"); storage.set(k, v); }, removeItem: k => storage.delete(k) }, crypto: { randomUUID }, Intl, console };
  runInNewContext(source, context); controller = context.window.createZinusyaMeals(env);
  const click = async (id, event = "click") => { const e = get(id); assert.ok(e.listeners[event], id + " listener"); await e.listeners[event]({ preventDefault() {} }); };
  const text = e => e.textContent + e.children.map(text).join(" ");
  return { get, elements, calls, storage, controller, get activeElement() { return activeElement; }, get busy() { return busy; }, text: id => text(get(id)), click,
    start: (user = owner, locale = "en") => controller.start(user, locale),
    async select() { get("meals-event").value = eventID; await click("meals-event", "change"); },
    async choose(id, value) { get(id).value = value; await click(id, "change"); },
    async check(id, checked = true) { get(id).checked = checked; await click(id, "change"); },
    submit: () => click("meals-form", "submit")
  };
}
const posts = ui => ui.calls.filter(c => c.method === "POST");

test("incomplete combo continues to complete with category choices and exact server total", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.select();
  await ui.choose("meal-mode:day", "combo-with-soup"); await ui.choose("meal-main:day", "main"); await ui.submit();
  assert.match(ui.text("meals-saved"), /Known partial amount/); assert.equal(api.current.evaluation.complete, false);
  for (const category of ["soup", "side", "salad"]) await ui.choose("meal-" + category + ":day", category);
  await ui.check("meal-dinner:day:tea"); await ui.submit();
  assert.match(ui.text("meals-saved"), /Complete draft total: BYN 7.15/); assert.equal(api.current.version, 2);
  assert.match(ui.text("meals-items"), /Europe\/Berlin/); assert.match(ui.text("meals-saved"), /Tea <img>/);
  ui.controller.setLanguage("ru"); assert.equal(ui.get("meals-save").textContent, "Сохранить питание"); assert.match(ui.text("meals-saved"), /Чай/); assert.match(ui.get("meals-status").textContent, /Сохранение подтверждено/);
});
test("no lunch, empty individual selection and clear remain distinct explicit requests", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.select();
  await ui.choose("meal-mode:day", "no-lunch"); await ui.submit(); assert.equal(api.current.evaluation.complete, true);
  await ui.choose("meal-mode:day", "individual-items"); await ui.submit(); assert.equal(api.current.evaluation.choices[0].lunch.mode, "individual-items");
  assert.equal(api.current.evaluation.known_amount_minor, 0);
  await ui.click("meals-clear"); assert.equal(api.current.cleared, true); assert.deepEqual(posts(ui).at(-1).body.choices, []);
});
test("dinner-only catalog has no forced lunch choice", async () => {
  const api = backend(); delete api.catalog.document.days[0].lunch;
  const ui = browser(api); await ui.start(); await ui.select(); assert.equal(ui.elements.has("meal-mode:day"), false);
  await ui.check("meal-dinner:day:tea"); await ui.submit(); assert.match(ui.text("meals-saved"), /BYN 0.50/);
});
test("committed response loss recovers original operation after page recreation without another write", async () => {
  const api = backend(), storage = new Map(); let lost = false;
  api.hook = (path, method, body, base) => { const result = base(path, method, body); if (method === "POST" && !lost) { lost = true; throw { status: 503 }; } return result; };
  const first = browser(api, { storage }); await first.start(); await first.select(); await first.choose("meal-mode:day", "no-lunch"); await first.submit();
  const pending = JSON.parse(storage.get(key(owner))); assert.equal(first.get("meals-save").disabled, true);
  const second = browser(api, { storage }); await second.start(); assert.equal(posts(second).length, 0); assert.equal(api.current.version, 1);
  assert.equal(storage.has(key(owner)), false); assert.ok(second.calls.some(c => c.path.endsWith(pending.body.operation_id)));
  assert.match(second.get("meals-status").textContent, /Save confirmed/);
});
test("unknown outcome retries exactly the same operation and body", async () => {
  const api = backend(); let down = true; api.hook = (path, method, body, base) => { if (method === "POST" && down) throw { status: 503 }; return base(path, method, body); };
  const ui = browser(api); await ui.start(); await ui.select(); await ui.choose("meal-mode:day", "no-lunch"); await ui.submit();
  await ui.click("meals-recover"); assert.match(ui.get("meals-status").textContent, /unknown/); down = false; await ui.click("meals-retry");
  assert.deepEqual(posts(ui)[0].body, posts(ui)[1].body); assert.equal(api.receipts.size, 1);
});
test("version conflict preserves edits and requires explicit reload", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.select(); await ui.choose("meal-mode:day", "no-lunch"); api.current.version = 1;
  await ui.submit(); assert.equal(ui.get("meal-mode:day").value, "no-lunch"); assert.equal(ui.get("meals-save").disabled, true);
  assert.equal(ui.storage.size, 0); assert.match(ui.get("meals-status").textContent, /rejected/);
});
test("old draft uses retained names and amount until explicit review of a new menu", async () => {
  const api = backend(), first = browser(api); await first.start(); await first.select(); await first.choose("meal-mode:day", "no-lunch"); await first.check("meal-dinner:day:tea"); await first.submit();
  api.catalog.version = 2; api.catalog.document.days[0].dinner.items[0] = { id: "coffee", name: { en: "Coffee", ru: "Кофе" }, price_minor: 99 };
  const ui = browser(api); await ui.start(); await ui.select(); assert.match(ui.text("meals-saved"), /Tea <img>/); assert.match(ui.text("meals-saved"), /BYN 0.50/);
  assert.equal(ui.get("meals-save").disabled, true); assert.equal(ui.get("meals-latest").hidden, false);
  await ui.click("meals-latest"); assert.equal(posts(ui).length, 0); assert.equal(api.current.catalog_version, 1);
  assert.match(ui.text("meals-items"), /Coffee/); assert.match(ui.text("meals-items"), /Menu version 2/);
  assert.equal(ui.get("meal-dinner:day:coffee").checked, false); assert.equal(ui.get("meals-save").disabled, false);
});
test("storage failure or corrupt recovery state prevents a new write", async () => {
  const api = backend(), bad = browser(api, { storage: new Map([[key(owner), "broken"]]) }); await bad.start(); assert.equal(bad.get("meals-save").disabled, true); assert.equal(bad.calls.length, 0);
  const ui = browser(api, { failSet: true }); await ui.start(); await ui.select(); await ui.submit(); assert.equal(posts(ui).length, 0); assert.equal(api.current.version, 0);
});
test("an old owner's delayed save cannot overwrite a new account view or its recovery key", async () => {
  const api = backend(); let resolveSave; api.hook = (path, method, body, base) => method === "POST" ? new Promise(resolve => { resolveSave = () => resolve(base(path, method, body)); }) : base(path, method, body);
  const ui = browser(api); await ui.start(); await ui.select(); await ui.choose("meal-mode:day", "no-lunch"); const waiting = ui.submit();
  await ui.start(other); resolveSave(); await waiting;
  assert.equal(ui.storage.has(key(owner)), true); assert.equal(ui.storage.has(key(other)), false); assert.equal(ui.text("meals-saved"), ""); assert.equal(ui.get("meals-save").disabled, true);
});

test("authentication denial releases the parent busy state and retains unknown operation", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.select();
  api.hook = (path, method, body, base) => { if (method === "POST") throw { status: 401 }; return base(path, method, body); };
  await ui.submit(); assert.equal(ui.busy, false); assert.equal(ui.get("meals").hidden, true); assert.equal(ui.storage.has(key(owner)), true);
});
test("selecting the event placeholder clears the old writable event", async () => {
  const api = backend(), ui = browser(api); await ui.start(); await ui.select();
  ui.get("meals-event").value = ""; await ui.click("meals-event", "change");
  assert.equal(ui.get("meals-save").disabled, true); assert.equal(ui.text("meals-items"), ""); await ui.submit(); assert.equal(posts(ui).length, 0);
});
test("lunch mode changes restore keyboard focus to the replacement select", async () => {
  const ui = browser(backend()); await ui.start(); await ui.select(); await ui.choose("meal-mode:day", "combo-with-soup");
  assert.equal(ui.activeElement, ui.get("meal-mode:day")); assert.equal(ui.activeElement.value, "combo-with-soup");
});
