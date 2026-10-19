"use strict";

// Auth stays in app.js. This controller receives only its request capability and
// verified owner ID; pending ordinary quantities are stored under that owner.
window.createZinusyaOrders = function (env) {
  const el = Object.fromEntries(["orders", "title", "note", "status", "event-label", "event", "first", "next",
    "receipt", "saved", "form", "items", "quote", "calculate", "save", "clear", "reload", "recover", "retry", "prepare", "confirm", "preview", "submitted", "submitted-title", "submitted-note", "submitted-reload", "submitted-first", "submitted-next", "order-status", "order-recover", "order-retry"]
    .map(id => [id, document.getElementById(id === "orders" ? id : "orders-" + id)]));
  const words = {
    en: { title: "Saved selection", note: "This is a draft. Saving does not place an order or reserve items.", event: "Event",
      service: "Service time", sales: "Sales window", choose: "Choose an event", empty: "No events available for orders.", loading: "Loading…", ready: "Edit quantities, then calculate or save.",
      save: "Save selection", clear: "Clear saved selection", reload: "Reload saved selection", calculate: "Calculate current price",
      recover: "Check earlier save", retry: "Retry same save", first: "First page", next: "Next page", quantity: "Quantity",
      unknown: "The earlier save has no confirmed result. Check it or retry the same save; other edits are blocked.",
      unavailable: "Could not load this selection. Reload to try again.", storage: "Browser storage is unavailable. Nothing new was sent; any earlier save remains tracked.",
      blocked: "The earlier save record is invalid. Nothing new will be sent from this page.",
      conflict: "The save was rejected. Reload the current selection before editing again.",
      dirty: "Save your edits or reload the saved selection before changing event or page.",
      invalid: "Check quantities. Closed or removed items must be set to zero before saving.",
      closed: "Sales closed; use zero to remove", removed: "No longer offered; use zero to remove",
      noPricing: "Complete pricing is unavailable. You can still read or clear a saved selection.",
      saved: "Saved selection version", receipt: "Request saved at version", historical: "Historical quote, not a current price guarantee",
      current: "Current calculation; saving will check versions again", none: "No saved quantities.", recovered: "The earlier request was saved. Current saved state is shown below." },
    ru: { title: "Сохранённый выбор", note: "Это черновик. Сохранение не оформляет заказ и не резервирует товары.", event: "Эвент",
      service: "Время услуги", sales: "Период продаж", choose: "Выбери эвент", empty: "Нет доступных эвентов для заказов.", loading: "Загружаю…", ready: "Укажи количество, затем рассчитай цену или сохрани выбор.",
      save: "Сохранить выбор", clear: "Очистить сохранённый выбор", reload: "Загрузить сохранённый выбор", calculate: "Рассчитать текущую цену",
      recover: "Проверить прежнее сохранение", retry: "Повторить то же сохранение", first: "Первая страница", next: "Следующая страница", quantity: "Количество",
      unknown: "Результат прежнего сохранения не подтверждён. Проверь его или повтори тот же запрос; другие изменения пока заблокированы.",
      unavailable: "Не удалось загрузить выбор. Повтори загрузку.", storage: "Хранилище браузера недоступно. Новый запрос не отправлен; прежнее сохранение не сброшено.",
      blocked: "Запись прежнего запроса повреждена. Новый запрос с этой страницы не будет отправлен.",
      conflict: "Сохранение отклонено. Загрузи текущий выбор перед новым изменением.",
      dirty: "Сохрани изменения или загрузи сохранённый выбор перед сменой эвента или страницы.",
      invalid: "Проверь количество. Для закрытых или удалённых позиций укажи ноль перед сохранением.",
      closed: "Продажи закрыты; ноль удалит позицию", removed: "Позиция больше не предлагается; ноль удалит её",
      noPricing: "Полная цена недоступна. Сохранённый выбор можно просмотреть или очистить.",
      saved: "Версия сохранённого выбора", receipt: "Запрос сохранён с версией", historical: "Исторический расчёт, не гарантия текущей цены",
      current: "Текущий расчёт; сохранение ещё раз проверит версии", none: "Нет сохранённых позиций.", recovered: "Прежний запрос сохранён. Ниже показано текущее состояние." }
  };
  Object.assign(words.en, { prepare: "Review order", confirm: "Confirm this order", "submitted-title": "My orders", "submitted-note": "Order submission alone does not confirm payment or reserve items. Check payment proofs for the current status.", "submitted-reload": "Reload orders", "submitted-first": "First orders", "submitted-next": "Next orders", "order-recover": "Check earlier order request", "order-retry": "Retry same order request", prepared: "Check the exact items and total before confirming.", orderUnknown: "The earlier order request has no confirmed result. Check it or retry the same request before making changes.", orderConflict: "The request was rejected. Reload saved selection and orders before trying again.", submitted: "Order submitted; payment is not confirmed and items are not reserved.", cancelled: "Order cancelled", cancelOrder: "Cancel this order", emptyOrders: "No submitted orders.", total: "Total", cutoff: "Cancellation deadline", expires: "Confirmation expires", unit: "each", charge: "Required charge", orderID: "Order", submittedAt: "Submitted", state: "State", unpaid: "Submitted; check payment proofs", receiptOrder: "Earlier request completed", saveFirst: "Save your edits before reviewing the order." });
  Object.assign(words.ru, { prepare: "Проверить заказ", confirm: "Подтвердить этот заказ", "submitted-title": "Мои заказы", "submitted-note": "Оформление заказа само по себе не подтверждает оплату и не резервирует товары. Текущий статус смотри в подтверждениях оплаты.", "submitted-reload": "Загрузить заказы", "submitted-first": "Первые заказы", "submitted-next": "Следующие заказы", "order-recover": "Проверить прежний запрос заказа", "order-retry": "Повторить тот же запрос заказа", prepared: "Проверь точный состав и итоговую цену перед подтверждением.", orderUnknown: "Результат прежнего запроса заказа неизвестен. Проверь его или повтори тот же запрос перед изменениями.", orderConflict: "Запрос отклонён. Загрузи сохранённый выбор и заказы перед новой попыткой.", submitted: "Заказ оформлен; оплата не подтверждена, товары не зарезервированы.", cancelled: "Заказ отменён", cancelOrder: "Отменить этот заказ", emptyOrders: "Нет оформленных заказов.", total: "Итого", cutoff: "Срок отмены", expires: "Подтвердить до", unit: "за единицу", charge: "Обязательная доплата", orderID: "Заказ", submittedAt: "Оформлен", state: "Статус", unpaid: "Оформлен; проверь подтверждения оплаты", receiptOrder: "Прежний запрос выполнен", saveFirst: "Сохрани изменения перед проверкой заказа." });
  const uuid = value => typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(value);
  const version = value => Number.isSafeInteger(value) && value >= 0;
  const itemID = value => typeof value === "string" && /^[a-z][a-z0-9_-]{0,31}$/.test(value);
  let owner, language = "en", epoch = 0, locked = false, working = false;
  let eventID, after, next, catalog, draft, pending, blocked = false, dirty = false;
  let inputs = [], cancelButtons = [], proofButtons = [];
  let pendingOrder, preparation, submittedOrders = [], orderAfter, orderNext;
  const orderKey = () => "zinusya-orders-operation-v1:" + owner;
  const w = () => words[language];
  const key = () => "zinusya-orders-pending-v1:" + owner;
  const path = () => "./orders/" + eventID;
  function node(tag, text) { const n = document.createElement(tag); n.textContent = text; return n; }
  function date(value, zone) {
    return new Intl.DateTimeFormat(language, { timeZone: zone, year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", timeZoneName: "shortOffset" }).format(new Date(value));
  }
  function money(q) {
    if (!Number.isSafeInteger(q.total_minor) || q.total_minor < 0 || !Number.isInteger(q.currency_scale) || q.currency_scale < 0 || q.currency_scale > 6 || !/^[A-Z]{3}$/.test(q.currency)) throw new Error("Invalid quote");
    const digits = BigInt(q.total_minor).toString().padStart(q.currency_scale + 1, "0");
    return q.currency + " " + (q.currency_scale ? digits.slice(0, -q.currency_scale) + "." + digits.slice(-q.currency_scale) : digits);
  }
  function validSelections(rows) {
    return Array.isArray(rows) && rows.length <= 100 && rows.every(r => itemID(r.offer_id) && itemID(r.item_id) && Number.isSafeInteger(r.quantity) && r.quantity > 0 && r.quantity <= 1000);
  }
  function validPending(p) {
    if (!p || p.owner !== owner || !uuid(p.event_id) || !p.body || !uuid(p.body.operation_id)) return false;
    const b = p.body;
    return Object.keys(p).sort().join() === "body,event_id,owner" && Object.keys(b).sort().join() === "expected_catalog_version,expected_draft_version,expected_event_version,operation_id,selections" &&
      version(b.expected_draft_version) && version(b.expected_event_version) && b.expected_event_version > 0 &&
      version(b.expected_catalog_version) && b.expected_catalog_version > 0 && validSelections(b.selections);
  }
  function checkDraft(value, id) {
    if (!value || value.event_id !== id || !version(value.version) || !validSelections(value.selections)) throw new Error("Invalid saved selection");
    return value;
  }
  function controls() {
    const disabled = locked || working || !owner;
    const editing = disabled || !!pending || !!pendingOrder || blocked || !catalog || !draft;
    el.event.disabled = el.first.disabled = el.next.disabled = disabled || !!pending || !!pendingOrder || blocked;
    el.save.disabled = editing;
    el.clear.disabled = editing || draft.version === 0 || draft.selections.length === 0;
    el.calculate.disabled = editing || !catalog.complete_pricing;
    el.reload.disabled = disabled;
    el.prepare.disabled = editing || dirty || !draft?.selections.length || !catalog?.complete_pricing;
    el.confirm.disabled = editing || dirty || !preparation || Date.parse(preparation.expires_at) <= Date.now();
    el["submitted-reload"].disabled = disabled || !eventID;
    el["submitted-first"].disabled = el["submitted-next"].disabled = disabled || !!pendingOrder || !!pending || blocked || !eventID;
    el["order-recover"].hidden = el["order-retry"].hidden = !pendingOrder;
    el["order-recover"].disabled = el["order-retry"].disabled = disabled || blocked;
    for (const button of [...cancelButtons, ...proofButtons]) button.disabled = disabled || !!pending || !!pendingOrder || blocked;
    el.recover.hidden = el.retry.hidden = !pending || blocked;
    el.recover.disabled = el.retry.disabled = disabled;
    for (const entry of inputs) entry.input.disabled = editing;
  }
  function labels() {
    for (const id of ["title", "note", "save", "clear", "reload", "calculate", "recover", "retry", "first", "next", "prepare", "confirm", "submitted-title", "submitted-note", "submitted-reload", "submitted-first", "submitted-next", "order-recover", "order-retry"]) el[id].textContent = w()[id];
    el["event-label"].textContent = w().event;
  }
  function clear() {
    epoch++; owner = undefined; working = false; pending = undefined; blocked = false;
    pendingOrder = preparation = orderAfter = orderNext = undefined; submittedOrders = []; cancelButtons = []; proofButtons = [];
    el.preview.replaceChildren(); el.submitted.replaceChildren(); el["order-status"].textContent = "";
    eventID = after = next = catalog = draft = undefined; dirty = false; inputs = [];
    el.orders.hidden = true;
    for (const id of ["event", "saved", "items"]) el[id].replaceChildren();
    el.quote.textContent = el.receipt.textContent = el.status.textContent = "";
    controls();
  }
  async function call(e, url, method = "GET", body) {
    try {
      const result = await env.request(url, method, body);
      if (e !== epoch) throw { stale: true };
      return result;
    } catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.stale) return;
    if (error.status === 401) { env.denied(); return; }
    if (pendingOrder) { el["order-status"].textContent = w()[error.local || "orderUnknown"]; return; }
    el.status.textContent = w()[error.local || (pending ? "unknown" : "unavailable")];
  }
  async function run(action) {
    if (working || locked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) working = false; env.setBusy(false); controls(); }
  }
  async function loadPage(e, cursor) {
    const result = await call(e, "./orders/events" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!result || !Array.isArray(result.events) || result.events.length > 20 || (result.next !== undefined && !uuid(result.next))) throw new Error("Invalid event page");
    const options = [node("option", w().choose)]; options[0].value = "";
    for (const event of result.events) {
      if (!uuid(event.id) || typeof event.name !== "string" || typeof event.timezone !== "string") throw new Error("Invalid event");
      const option = node("option", event.name + " · " + event.timezone); option.value = event.id; options.push(option);
    }
    after = cursor; next = result.next; el.event.replaceChildren(...options); el.event.value = eventID || "";
    el.first.hidden = !after; el.next.hidden = !next;
    if (!eventID) el.status.textContent = result.events.length ? w().choose : w().empty;
  }
  function renderSaved() {
    el.saved.replaceChildren(node("h2", w().saved + " " + draft.version));
    if (!draft.selections.length) el.saved.append(node("p", w().none));
    for (const row of draft.selections) {
      const line = draft.quote?.items?.find(i => i.offer_id === row.offer_id && i.id === row.item_id);
      el.saved.append(node("p", (line?.name?.[language] || line?.name?.en || row.item_id) + " · " + row.offer_id + " × " + row.quantity));
    }
    if (draft.quote) el.saved.append(node("p", w().historical + ": " + money(draft.quote) + " · " + date(draft.quote.observed_at, draft.quote.timezone) + " · " + draft.quote.timezone));
  }
  function renderEditor() {
    el.items.replaceChildren(); inputs = [];
    const selected = new Map(draft.selections.map(s => [s.offer_id + ":" + s.item_id, s.quantity]));
    function add(container, offer, item, available, label) {
      const pair = offer + ":" + item, qty = selected.get(pair) || 0; selected.delete(pair);
      const input = document.createElement("input"); input.type = "number"; input.min = "0"; input.max = available ? "1000" : "0"; input.step = "1"; input.value = String(qty);
      const id = "order-qty-" + offer + ":" + item; input.id = id;
      const caption = node("label", label + " · " + w().quantity); caption.htmlFor = id;
      container.append(caption, input);
      if (!available) container.append(node("p", w().closed));
      inputs.push({ offer_id: offer, item_id: item, input, available });
      input.addEventListener("input", () => { dirty = true; preparation = undefined; el.preview.replaceChildren(); el.quote.textContent = ""; controls(); });
    }
    for (const offer of catalog.offers) {
      const article = document.createElement("article");
      article.append(node("h2", offer.name[language] || offer.name.en));
      article.append(node("p", w().service + ": " + date(offer.starts_at, catalog.timezone) + " – " + date(offer.ends_at, catalog.timezone) + " · " + catalog.timezone));
      article.append(node("p", w().sales + ": " + date(offer.sales_opens_at, catalog.timezone) + " – " + date(offer.sales_closes_at, catalog.timezone) + " · " + catalog.timezone));
      for (const itemID of offer.item_ids) {
        const item = catalog.items.find(i => i.id === itemID); if (!item) throw new Error("Invalid catalog item");
        add(article, offer.id, item.id, offer.sales_open, item.name[language] || item.name.en);
      }
      el.items.append(article);
    }
    for (const [pair] of selected) {
      const [offer, item] = pair.split(":"); const article = document.createElement("article"); article.append(node("p", w().removed));
      // Keep the old quantity until the user explicitly removes this unavailable row.
      add(article, offer, item, false, item + " · " + offer); el.items.append(article);
    }
  }
  async function loadEvent(e, id) {
    if (!uuid(id)) throw new Error("Invalid event ID");
    if (id !== eventID) {
      el.receipt.textContent = el["order-status"].textContent = "";
      submittedOrders = []; orderAfter = orderNext = undefined; renderSubmitted();
    }
    eventID = id; catalog = draft = preparation = undefined; el.preview.replaceChildren(); inputs = []; dirty = false;
    el.items.replaceChildren(); el.saved.replaceChildren(); el.quote.textContent = ""; el.status.textContent = w().loading; controls();
    draft = checkDraft(await call(e, path() + "/selection"), id); renderSaved();
    const value = await call(e, path() + "/catalog");
    if (!value || value.event_id !== id || !version(value.event_version) || !version(value.version) || !Array.isArray(value.offers) || value.offers.length > 20 || !Array.isArray(value.items) || value.items.length > 100) throw new Error("Invalid catalog");
    catalog = value; renderEditor();
    let option = Array.from(el.event.options).find(row => row.value === id);
    if (!option) { option = node("option", ""); option.value = id; el.event.append(option); }
    option.textContent = (catalog.event_name || id) + " · " + catalog.timezone;
    el.event.value = id;
    el.status.textContent = catalog.complete_pricing ? w().ready : w().noPricing;
    await loadSubmitted(e);
  }
  function selections() {
    const rows = []; let total = 0;
    for (const entry of inputs) {
      const quantity = entry.input.value === "" ? NaN : Number(entry.input.value);
      if (!Number.isSafeInteger(quantity) || quantity < 0 || quantity > 1000 || (!entry.available && quantity !== 0)) throw { local: "invalid" };
      if (quantity) { total += quantity; rows.push({ offer_id: entry.offer_id, item_id: entry.item_id, quantity }); }
    }
    if (rows.length > 100 || total > 10000) throw { local: "invalid" };
    return rows;
  }
  function forget() {
    try { sessionStorage.removeItem(key()); } catch { throw { local: "storage" }; }
    pending = undefined;
  }
  async function accepted(e, result) {
    checkDraft(result, pending.event_id);
    if (result.version !== pending.body.expected_draft_version + 1) throw new Error("Invalid receipt revision");
    const id = pending.event_id; forget(); el.receipt.textContent = w().receipt + " " + result.version;
    try { await loadEvent(e, id); el.status.textContent = w().recovered; }
    catch (error) { if (error.stale) throw error; errorStatus(error); }
  }
  async function recover(e) {
    if (!pending || blocked) return;
    eventID = pending.event_id; el.event.value = eventID;
    try { await accepted(e, await call(e, path() + "/selection/operations/" + pending.body.operation_id)); }
    catch (error) { if (error.status === 404) el.status.textContent = w().unknown; else throw error; }
  }
  async function dispatch(e) {
    if (!pending || blocked) return;
    eventID = pending.event_id; el.event.value = eventID;
    try { await accepted(e, await call(e, path() + "/selection", "POST", pending.body)); }
    catch (error) {
      if (error.status === 400 || error.status === 409) { forget(); catalog = draft = undefined; el.status.textContent = w().conflict; }
      else throw error;
    }
  }
  async function saveSelection(e, empty) {
    if (pending || pendingOrder || blocked || !catalog || !draft) return;
    const rows = empty ? [] : selections();
    if (rows.length && !catalog.complete_pricing) throw { local: "noPricing" };
    const body = { operation_id: crypto.randomUUID(), expected_event_version: catalog.event_version,
      expected_catalog_version: catalog.version, expected_draft_version: draft.version, selections: rows };
    const record = { owner, event_id: eventID, body };
    try { sessionStorage.setItem(key(), JSON.stringify(record)); } catch { throw { local: "storage" }; }
    pending = record; controls(); await dispatch(e);
  }
  el.form.addEventListener("submit", event => { event.preventDefault(); return run(e => saveSelection(e, false)); });
  el.clear.addEventListener("click", () => run(e => saveSelection(e, true)));
  el.calculate.addEventListener("click", () => run(async e => {
    if (!catalog || !draft || pending || pendingOrder || blocked) return;
    const quote = await call(e, path() + "/quote", "POST", { expected_event_version: catalog.event_version, expected_catalog_version: catalog.version, selections: selections() });
    el.quote.textContent = w().current + ": " + money(quote) + " · " + date(quote.observed_at, quote.timezone) + " · " + quote.timezone;
  }));
  el.reload.addEventListener("click", () => run(e => pending ? recover(e) : eventID ? loadEvent(e, eventID) : loadPage(e, after)));
  el.recover.addEventListener("click", () => run(recover));
  el.retry.addEventListener("click", () => run(dispatch));
  el.event.addEventListener("change", () => {
    const id = el.event.value;
    if (dirty || pending || pendingOrder || blocked) { el.event.value = eventID || ""; el.status.textContent = w().dirty; return; }
    if (!id) { el.event.value = eventID || ""; return; }
    return run(e => loadEvent(e, id));
  });
  for (const [button, cursor] of [[el.first, () => undefined], [el.next, () => next]]) button.addEventListener("click", () => {
    if (dirty) { el.status.textContent = w().dirty; return; }
    return run(async e => { if (pending || pendingOrder || blocked) return; preparation = undefined; el.preview.replaceChildren(); submittedOrders = []; renderSubmitted(); eventID = catalog = draft = undefined; inputs = []; el.saved.replaceChildren(); el.items.replaceChildren(); el.quote.textContent = el.receipt.textContent = ""; await loadPage(e, cursor()); });
  });
  function validOrderPending(p) {
    if (!p || Object.keys(p).sort().join() !== "body,event_id,kind,owner" || p.owner !== owner || !uuid(p.event_id) || !uuid(p.body?.operation_id)) return false;
    const b = p.body, keys = Object.keys(b).sort().join();
    if (p.kind === "cancel") return keys === "expected_version,operation_id,order_id" && uuid(b.order_id) && version(b.expected_version) && b.expected_version > 0;
    const base = "expected_catalog_version,expected_draft_version,expected_event_version,operation_id";
    if (![b.expected_event_version, b.expected_catalog_version, b.expected_draft_version].every(v => version(v) && v > 0)) return false;
    return p.kind === "prepare" ? keys === base : p.kind === "submit" && keys === "confirmed," + base + ",proposal_id" && uuid(b.proposal_id) && b.confirmed === true;
  }
  function renderSnapshot(container, snapshot) {
    const q = snapshot.quote;
    container.append(node("h2", snapshot.event_name));
    if (snapshot.services !== undefined) {
      if (!Array.isArray(snapshot.services) || snapshot.services.length > 20) throw new Error("Invalid order services");
      for (const service of snapshot.services) {
        if (!service || typeof service.offer_id !== "string" || !Number.isFinite(Date.parse(service.starts_at)) ||
          !Number.isFinite(Date.parse(service.ends_at)) || Date.parse(service.ends_at) <= Date.parse(service.starts_at)) throw new Error("Invalid order service time");
        const label = service.name?.[language] || service.name?.en || service.offer_id;
        container.append(node("p", label + " · " + service.offer_id + " · " + date(service.starts_at, q.timezone) + " — " + date(service.ends_at, q.timezone) + " · " + q.timezone));
      }
    }
    renderCapacity(container, snapshot.capacity);
    for (const [rows, charge] of [[q.items, false], [q.supplements, true]]) {
      if (!Array.isArray(rows) || rows.length > 400) throw new Error("Invalid quote lines");
      for (const line of rows) {
        if (!Number.isSafeInteger(line.quantity) || line.quantity < 1) throw new Error("Invalid quote quantity");
        const label = line.name?.[language] || line.name?.en || line.id;
        container.append(node("p", (charge ? w().charge + ": " : "") + label + " · " + line.offer_id + "/" + line.id + " × " + line.quantity + " · " +
          money({ ...q, total_minor: line.unit_price_minor }) + " " + w().unit + " = " + money({ ...q, total_minor: line.amount_minor })));
      }
    }
    container.append(node("p", w().total + ": " + money(q)));
    container.append(node("p", w().cutoff + ": " + date(snapshot.sales_closes_at, q.timezone) + " · " + q.timezone));
  }
  function renderCapacity(container, capacity) {
    const ru = language === "ru";
    if (capacity === undefined) {
      container.append(node("p", ru ? "Требования к ограниченным ресурсам не настроены." : "Limited resource requirements are not configured."));
      return;
    }
    if (!capacity || capacity.version !== 1 || !Array.isArray(capacity.demands) || capacity.demands.length > 64) throw new Error("Invalid order resource requirements");
    container.append(node("p", ru ? "Требования к ограниченным ресурсам:" : "Limited resource requirements:"));
    if (capacity.demands.length === 0) container.append(node("p", ru ? "Ограниченные ресурсы не требуются." : "No limited resources required."));
    for (const demand of capacity.demands) {
      if (!demand || typeof demand.resource_id !== "string" || !Number.isSafeInteger(demand.units) || demand.units < 1 || demand.units > 1000000000) throw new Error("Invalid order resource demand");
      container.append(node("p", (demand.name?.[language] || demand.name?.en || demand.resource_id) + " · " + demand.resource_id + " × " + demand.units));
    }
  }
  function renderPreparation() {
    el.preview.replaceChildren();
    if (!preparation) return;
    renderSnapshot(el.preview, preparation.snapshot);
    el.preview.append(node("p", w().expires + ": " + date(preparation.expires_at, preparation.snapshot.quote.timezone) + " · " + preparation.snapshot.quote.timezone));
  }
  function checkOrder(value) {
    if (!value || !uuid(value.id) || value.event_id !== eventID || !version(value.version) || value.version < 1 || !["submitted", "cancelled"].includes(value.state) || !value.snapshot?.quote) throw new Error("Invalid order");
    return value;
  }
  function renderSubmitted() {
    el.submitted.replaceChildren(); cancelButtons = []; proofButtons = [];
    if (!submittedOrders.length) el.submitted.append(node("p", w().emptyOrders));
    for (const order of submittedOrders) {
      const article = document.createElement("article");
      article.append(node("p", w().orderID + ": " + order.id));
      article.append(node("p", w().state + ": " + (order.state === "cancelled" ? w().cancelled : w().unpaid)));
      renderSnapshot(article, order.snapshot);
      article.append(node("p", w().submittedAt + ": " + date(order.submitted_at, order.snapshot.quote.timezone) + " · " + order.snapshot.quote.timezone));
      if (order.state === "submitted" && Date.parse(order.snapshot.sales_closes_at) > Date.now()) {
        const button = node("button", w().cancelOrder); button.type = "button";
        button.addEventListener("click", () => run(e => beginOrder(e, "cancel", { operation_id: crypto.randomUUID(), order_id: order.id, expected_version: order.version })));
        cancelButtons.push(button); article.append(button);
      }
      if (env.openProof) {
        const button = node("button", language === "ru" ? "Оплата и подтверждения" : "Payment and proofs"); button.type = "button";
        button.addEventListener("click", () => env.openProof(order, language)); proofButtons.push(button); article.append(button);
      }
      el.submitted.append(article);
    }
    el["submitted-first"].hidden = !orderAfter; el["submitted-next"].hidden = !orderNext;
  }
  async function loadSubmitted(e, cursor) {
    const page = await call(e, path() + "/submitted" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!page || !Array.isArray(page.orders) || page.orders.length > 20 || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid orders page");
    const rows = page.orders.map(checkOrder);
    orderAfter = cursor; orderNext = page.next; submittedOrders = rows; renderSubmitted(); controls();
  }
  function forgetOrder() {
    try { sessionStorage.removeItem(orderKey()); } catch { throw { local: "storage" }; }
    pendingOrder = undefined;
  }
  async function acceptOrder(e, result) {
    const p = pendingOrder;
    if (p.kind === "prepare") {
      if (!uuid(result?.proposal_id) || !result.snapshot?.quote || !Number.isFinite(Date.parse(result.expires_at)) || result.arguments?.event_id !== eventID ||
        ["expected_event_version", "expected_catalog_version", "expected_draft_version"].some(k => result.arguments[k] !== p.body[k])) throw new Error("Invalid preparation");
      // A recovered preparation is displayed for review; it never authorizes submission.
      renderSnapshot(document.createElement("div"), result.snapshot);
      if (!catalog || !draft) await loadEvent(e, eventID);
      forgetOrder(); preparation = result; renderPreparation(); el["order-status"].textContent = w().prepared;
      return;
    }
    const order = checkOrder(p.kind === "submit" ? result?.order : result);
    if (p.kind === "cancel" && (order.id !== p.body.order_id || order.state !== "cancelled" || order.version !== p.body.expected_version + 1)) throw new Error("Invalid cancellation receipt");
    forgetOrder(); preparation = undefined; el.preview.replaceChildren();
    el["order-status"].textContent = w().receiptOrder + ": " + order.id + " · " + (p.kind === "submit" ? w().submitted : w().cancelled);
    // The receipt can be historical. Read current draft/orders separately.
    if (p.kind === "submit" || !catalog || !draft) await loadEvent(e, eventID); else await loadSubmitted(e);
  }
  async function recoverOrder(e) {
    if (!pendingOrder || blocked) return;
    eventID = pendingOrder.event_id; el.event.value = eventID;
    try {
      const receipt = await call(e, path() + "/operation-receipts/" + pendingOrder.kind + "/" + pendingOrder.body.operation_id);
      if (receipt.operation_id !== pendingOrder.body.operation_id || receipt.event_id !== eventID || receipt.kind !== pendingOrder.kind) throw new Error("Invalid operation receipt");
      await acceptOrder(e, receipt.result);
    } catch (error) { if (error.status === 404) el["order-status"].textContent = w().orderUnknown; else throw error; }
  }
  async function dispatchOrder(e) {
    if (!pendingOrder || blocked) return;
    eventID = pendingOrder.event_id; el.event.value = eventID;
    try { await acceptOrder(e, await call(e, path() + (pendingOrder.kind === "prepare" ? "/submit/prepare" : "/" + pendingOrder.kind), "POST", pendingOrder.body)); }
    catch (error) {
      if (error.status === 400 || error.status === 409) { forgetOrder(); preparation = undefined; el.preview.replaceChildren(); catalog = draft = undefined; el["order-status"].textContent = w().orderConflict; }
      else throw error;
    }
  }
  async function beginOrder(e, kind, body) {
    if (pending || pendingOrder || blocked || !eventID) return;
    const record = { owner, event_id: eventID, kind, body };
    try { sessionStorage.setItem(orderKey(), JSON.stringify(record)); } catch { throw { local: "storage" }; }
    pendingOrder = record; controls(); await dispatchOrder(e);
  }
  el.prepare.addEventListener("click", () => run(e => {
    if (dirty) throw { local: "saveFirst" };
    if (!catalog || !draft?.selections.length) return;
    preparation = undefined; el.preview.replaceChildren();
    return beginOrder(e, "prepare", { operation_id: crypto.randomUUID(), expected_event_version: catalog.event_version, expected_catalog_version: catalog.version, expected_draft_version: draft.version });
  }));
  el.confirm.addEventListener("click", () => run(e => {
    if (dirty || !preparation || Date.parse(preparation.expires_at) <= Date.now()) return;
    const a = preparation.arguments;
    return beginOrder(e, "submit", { operation_id: crypto.randomUUID(), expected_event_version: a.expected_event_version, expected_catalog_version: a.expected_catalog_version, expected_draft_version: a.expected_draft_version, proposal_id: preparation.proposal_id, confirmed: true });
  }));
  el["order-recover"].addEventListener("click", () => run(recoverOrder));
  el["order-retry"].addEventListener("click", () => run(dispatchOrder));
  el["submitted-reload"].addEventListener("click", () => run(e => eventID && loadSubmitted(e)));
  for (const [button, cursor] of [[el["submitted-first"], () => undefined], [el["submitted-next"], () => orderNext]]) button.addEventListener("click", () => run(e => !pendingOrder && !pending && !blocked && eventID && loadSubmitted(e, cursor())));
  clear();
  return {
    clear,
    setLanguage(locale) {
      const changed = (locale === "ru" ? "ru" : "en") !== language;
      language = locale === "ru" ? "ru" : "en";
      if (!owner || !changed) return;
      labels();
      if (el.event.options[0]) el.event.options[0].textContent = w().choose;
      const values = new Map(inputs.map(entry => [entry.offer_id + ":" + entry.item_id, entry.input.value]));
      if (draft) renderSaved();
      if (catalog && draft) {
        renderEditor();
        for (const entry of inputs) if (values.has(entry.offer_id + ":" + entry.item_id)) entry.input.value = values.get(entry.offer_id + ":" + entry.item_id);
      }
      el.quote.textContent = ""; renderPreparation(); renderSubmitted(); controls();
    },
    setBusy(value) { locked = value; controls(); },
    async start(id, locale) {
      clear(); if (!uuid(id)) return;
      owner = id; language = locale === "ru" ? "ru" : "en"; labels(); el.orders.hidden = false;
      const e = epoch;
      try {
        const raw = sessionStorage.getItem(key());
        if (raw) { if (raw.length > 65536) throw new Error("Invalid recovery record"); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error("Invalid recovery record"); }
        const orderRaw = sessionStorage.getItem(orderKey());
        if (orderRaw) { if (orderRaw.length > 2048) throw new Error("Invalid order recovery record"); pendingOrder = JSON.parse(orderRaw); if (!validOrderPending(pendingOrder) || pending) throw new Error("Invalid order recovery record"); }
      } catch { blocked = true; pending = undefined; el.status.textContent = w().blocked; controls(); return; }
      try { await loadPage(e); if (pending) await recover(e); else if (pendingOrder) await recoverOrder(e); }
      catch (error) { if (e === epoch) errorStatus(error); }
      controls();
    }
  };
};
