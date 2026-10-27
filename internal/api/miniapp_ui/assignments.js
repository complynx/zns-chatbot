"use strict";

window.createZinusyaAssignments = env => {
  const names = ["title", "note", "status", "event-label", "event", "events-first", "events-next", "target-label", "target", "targets-first", "targets-next", "prepare", "preview", "consent", "consent-label", "confirm", "close", "recover", "retry", "receipt", "pending-title", "pending", "reload", "recent", "first", "next"];
  const el = Object.fromEntries(names.map(n => [n, document.getElementById("pa-" + n)]));
  const section = document.getElementById("pass-assignments");
  const words = {
    en: { title: "Assign a solo pass", note: "Administrator tools. Select a waiting-list registration, review the exact terms and confirm. No payment is recorded.", "event-label": "Event", "target-label": "Participant", chooseEvent: "Select an event", chooseTarget: "Select a participant", "events-first": "Refresh events", "events-next": "Next events", "targets-first": "Refresh participants", "targets-next": "Next participants", prepare: "Review pass assignment", "consent-label": "I approve this exact pass assignment and its terms", confirm: "Assign this pass", retryConfirm: "Retry this exact confirmation", close: "Close local preview", recover: "Check earlier request", retry: "Retry the same preparation", "pending-title": "Unfinished assignments", reload: "Reload unfinished assignments", recent: "Recent form operations", recentEmpty: "No recent form operations.", first: "First proposals", next: "Next proposals", choose: "Review proposal", empty: "No unfinished assignments on this page.", prepared: "Review the original terms before confirming.", completed: "This earlier operation completed. The receipt is historical.", expired: "This proposal expired. It cannot authorize another assignment.", historical: "Earlier assignment result", expiry: "Confirm before", closeNote: "Closing removes this local preview. It does not cancel the proposal on the server.", unavailable: "Could not read data. Retry the same read.", unknown: "Result unknown. Check the same operation before starting another assignment.", notArrived: "No saved proposal was found. You may retry the same preparation with its original identifiers and versions.", conflict: "The registration or proposal changed. Check the earlier operation and review its result.", storage: "Recovery identifiers could not be saved. New changes are blocked.", blocked: "Saved recovery identifiers cannot be read. New changes are blocked.", price: "Price", tier: "Tier", deposit: "Deposit terms", leader: "Leader", follower: "Follower", role: "Role", version: "Version", recipient: "Recipient", event: "Event", registration: "Registration", final: "Final payment deadline", due: "Evidence deadline", timezone: "Event timezone", noTarget: "No waiting-list registrations on this page." },
    ru: { title: "Назначить одиночный пасс", note: "Инструменты администратора. Выбери регистрацию из очереди, проверь точные условия и подтверди. Оплата не фиксируется.", "event-label": "Эвент", "target-label": "Участник", chooseEvent: "Выбери эвент", chooseTarget: "Выбери участника", "events-first": "Обновить эвенты", "events-next": "Следующие эвенты", "targets-first": "Обновить участников", "targets-next": "Следующие участники", prepare: "Проверить назначение пасса", "consent-label": "Подтверждаю именно это назначение пасса и его условия", confirm: "Назначить этот пасс", retryConfirm: "Повторить именно это подтверждение", close: "Закрыть локальный предпросмотр", recover: "Проверить прежний запрос", retry: "Повторить ту же подготовку", "pending-title": "Незавершённые назначения", reload: "Обновить незавершённые назначения", recent: "Недавние операции формы", recentEmpty: "Недавних операций формы нет.", first: "Первая страница предложений", next: "Следующие предложения", choose: "Проверить предложение", empty: "На этой странице нет незавершённых назначений.", prepared: "Проверь исходные условия перед подтверждением.", completed: "Прежняя операция завершена. Квитанция показывает исторический результат.", expired: "Предложение истекло и не разрешает новое назначение.", historical: "Результат прежнего назначения", expiry: "Подтвердить до", closeNote: "Закрытие убирает только локальный предпросмотр. Предложение на сервере не отменяется.", unavailable: "Не удалось прочитать данные. Повтори чтение.", unknown: "Результат неизвестен. Проверь ту же операцию перед новым назначением.", notArrived: "Сохранённое предложение не найдено. Можно повторить ту же подготовку с исходными идентификаторами и версиями.", conflict: "Регистрация или предложение изменились. Проверь прежнюю операцию и её результат.", storage: "Не удалось сохранить идентификаторы восстановления. Новые изменения заблокированы.", blocked: "Не удалось прочитать сохранённые идентификаторы восстановления. Новые изменения заблокированы.", price: "Цена", tier: "Тариф", deposit: "Условия предоплаты", leader: "Лидер", follower: "Фолловер", role: "Роль", version: "Версия", recipient: "Получатель", event: "Эвент", registration: "Регистрация", final: "Срок доплаты", due: "Срок отправки подтверждения", timezone: "Таймзона эвента", noTarget: "На этой странице нет регистраций в очереди." }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const version = v => Number.isSafeInteger(v) && v > 0;
  const operation = v => typeof v === "string" && [...v].length > 0 && [...v].length <= 128;
  const instant = v => typeof v === "string" && /(?:Z|[+-]\d\d:\d\d)$/.test(v) && Number.isFinite(Date.parse(v));
  const validArgs = a => a && uuid(a.event_id) && uuid(a.registration_id) && version(a.expected_event_version) && version(a.expected_version) && Object.keys(a).every(k => ["event_id", "registration_id", "expected_event_version", "expected_version"].includes(k));
  const sameArgs = (a, b) => validArgs(a) && validArgs(b) && ["event_id", "registration_id", "expected_event_version", "expected_version"].every(k => a[k] === b[k]);
  const validQuote = q => q && uuid(q.registration_id) && Number.isSafeInteger(q.price_minor) && q.price_minor >= 0 && /^[A-Z]{3}$/.test(q.currency) && Number.isInteger(q.currency_scale) && q.currency_scale >= 0 && q.currency_scale <= 6;
  const validZone = v => typeof v === "string" && (() => { try { new Intl.DateTimeFormat("en", { timeZone: v }); return true; } catch { return false; } })();
  const validPreparation = p => p && (p.timezone === undefined || validZone(p.timezone)) && uuid(p.proposal_id) && validArgs(p.arguments) && typeof p.description === "string" && p.description.trim() && instant(p.expires_at) && Array.isArray(p.quotes) && p.quotes.length === 1 && validQuote(p.quotes[0]) && p.quotes[0].registration_id === p.arguments.registration_id;
  const validRegistration = r => r && uuid(r.id) && uuid(r.event_id) && uuid(r.user_id) && version(r.version) && ["leader", "follower"].includes(r.role) && ["waitlisted", "assigned", "cancelled"].includes(r.state);
  const validView = v => v && operation(v.original_operation_id) && validPreparation(v.preparation) && ["pending", "expired", "completed"].includes(v.state) && instant(v.observed_at) && (v.state !== "completed" || (validRegistration(v.result) && v.result.id === v.preparation.arguments.registration_id && v.result.event_id === v.preparation.arguments.event_id && v.result.state === "assigned" && v.result.pass));
  const validEvent = e => e && uuid(e.id) && version(e.version) && e.document && typeof e.document.name === "string" && typeof e.document.timezone === "string" && (() => { try { new Intl.DateTimeFormat("en", { timeZone: e.document.timezone }); return true; } catch { return false; } })();
  const validTarget = t => t && validRegistration(t.registration) && Number.isSafeInteger(t.telegram_user_id) && t.telegram_user_id > 0 && typeof t.display_name === "string";
  let owner, locale = "en", epoch = 0, working = false, locked = false, blocked = false, ready = false;
  let record, view, missing = false, recentMode = false, events = [], targets = [], proposals = [], eventsAfter, eventsNext, targetsAfter, targetsNext, after, next;
  const eventCache = new Map();
  const w = () => words[locale];
  const key = () => "zinusya.pass-assignments." + owner;
  const node = (tag, text) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; };
  const validRecord = r => r && r.owner === owner && operation(r.operation_id) && validArgs(r.arguments) && ["prepare", "prepared", "confirm"].includes(r.phase) && (r.proposal_id === undefined || uuid(r.proposal_id)) && (r.phase === "prepare" || uuid(r.proposal_id)) && Object.keys(r).every(k => ["owner", "operation_id", "arguments", "proposal_id", "phase"].includes(k));
  function persist(r) {
    if (!validRecord(r)) throw new Error("Invalid recovery identifiers");
    try { sessionStorage.setItem(key(), JSON.stringify(r)); } catch { blocked = true; throw { local: "storage" }; }
    record = r;
  }
  function clear() {
    epoch++; owner = record = view = eventsAfter = eventsNext = targetsAfter = targetsNext = after = next = undefined;
    events = []; targets = []; proposals = []; eventCache.clear(); ready = working = blocked = missing = recentMode = false;
    for (const n of ["preview", "receipt", "pending", "event", "target"]) el[n].replaceChildren();
    el.event.value = el.target.value = el.status.textContent = ""; el.consent.checked = false; section.hidden = true; controls();
  }
  const selectedEvent = () => eventCache.get(el.event.value);
  const selectedTarget = () => targets.find(t => t.registration.id === el.target.value);
  const pending = () => view?.state === "pending" && Date.parse(view.preparation.expires_at) > Date.now();
  const canClose = () => record && (record.phase !== "confirm" || ["completed", "expired"].includes(view?.state));
  function controls() {
    const busy = locked || working || !owner;
    el.event.disabled = el.target.disabled = busy || blocked || !!record || !ready;
    el.prepare.disabled = busy || blocked || !!record || !ready || !selectedEvent() || !selectedTarget() || selectedTarget().registration.state !== "waitlisted";
    el.consent.hidden = el["consent-label"].hidden = !pending();
    el.consent.disabled = busy || blocked || !pending();
    el.confirm.hidden = !view || view.state !== "pending";
    el.confirm.disabled = busy || blocked || !pending() || !el.consent.checked;
    el.confirm.textContent = w()[record?.phase === "confirm" ? "retryConfirm" : "confirm"];
    el.recover.hidden = !record; el.recover.disabled = busy || blocked;
    el.retry.hidden = !record || record.phase !== "prepare" || !missing; el.retry.disabled = busy || blocked;
    // Preparation has no business effect and may be closed locally.
    // A confirmation with no observed result must keep its original operation.
    el.close.hidden = !canClose(); el.close.disabled = busy || blocked;
    el.reload.disabled = busy || blocked; el.recent.disabled = busy || blocked || !!record;
    for (const b of el.pending.querySelectorAll("button")) b.disabled = busy || blocked || !!record;
    for (const [first, more, previous, following] of [["events-first", "events-next", eventsAfter, eventsNext], ["targets-first", "targets-next", targetsAfter, targetsNext], ["first", "next", after, next]]) {
      el[first].hidden = !["events-first", "targets-first"].includes(first) && !previous; el[more].hidden = !following;
      el[first].disabled = el[more].disabled = busy || blocked || !!record || (first === "targets-first" && !selectedEvent());
    }
  }
  function money(q) {
    const digits = String(q.price_minor).padStart(q.currency_scale + 1, "0");
    return (q.currency_scale ? digits.slice(0, -q.currency_scale) + "." + digits.slice(-q.currency_scale) : digits) + " " + q.currency;
  }
  function localTime(value, timezone) {
    const zone = timezone || "UTC";
    return new Intl.DateTimeFormat(locale, { timeZone: zone, dateStyle: "medium", timeStyle: "long" }).format(new Date(value)) + " · " + zone;
  }
  function render() {
    el.preview.replaceChildren(); el.receipt.replaceChildren(); el.pending.replaceChildren();
    if (view) {
      const p = view.preparation, a = p.arguments;
      el.preview.append(node("p", p.description), node("p", w().event + ": " + a.event_id), node("p", w().registration + ": " + a.registration_id), node("p", w().version + ": " + a.expected_version + " · " + w().event + " " + a.expected_event_version));
      for (const q of p.quotes) {
        el.preview.append(node("p", w().price + ": " + money(q)));
        if (q.tier) el.preview.append(node("p", w().tier + ": " + q.tier.number + " · " + q.tier.id));
        // The canonical description also includes the exact payment/deposit terms.
        if (q.deposit) { el.preview.append(node("p", w().deposit + ": " + money({ ...q, price_minor: q.deposit.required_minor }))); if (instant(q.deposit.final_due_at)) el.preview.append(node("p", w().final + ": " + localTime(q.deposit.final_due_at, p.timezone))); }
      }
      el.preview.append(node("p", w().expiry + ": " + localTime(p.expires_at, p.timezone)), node("p", w().closeNote));
      if (view.state === "expired" || (view.state === "pending" && !pending())) el.preview.append(node("p", w().expired));
      if (view.result) {
        const r = view.result;
        el.receipt.append(node("h2", w().historical), node("p", w().completed), node("p", r.id + " · " + w()[r.role] + " · " + w().version + " " + r.version));
        if (r.pass) { el.receipt.append(node("p", w().price + ": " + money(r.pass))); if (instant(r.pass.payment_due_at)) el.receipt.append(node("p", w().due + ": " + localTime(r.pass.payment_due_at, p.timezone))); }
      }
    }
    for (const p of proposals) {
      const b = node("button", w().choose + ": " + p.preparation.description); b.type = "button";
      b.addEventListener("click", () => run(async e => {
        if (record) return;
        persist({ owner, operation_id: p.original_operation_id, proposal_id: p.preparation.proposal_id, arguments: p.preparation.arguments, phase: "prepared" });
        await recover(e);
      })); el.pending.append(b);
    }
    el["pending-title"].textContent = w()[recentMode ? "recent" : "pending-title"];
    if (!proposals.length) el.pending.append(node("p", w()[recentMode ? "recentEmpty" : "empty"])); controls();
  }
  function options() {
    const ev = el.event.value, target = el.target.value;
    const placeholder = (label, value = "") => { const n = node("option", label); n.value = value; return n; };
    el.event.replaceChildren(placeholder(w().chooseEvent), ...events.map(e => placeholder(e.document.name + " · " + e.document.timezone, e.id)));
    el.event.value = events.some(e => e.id === ev) ? ev : "";
    el.target.replaceChildren(placeholder(w().chooseTarget), ...targets.filter(t => t.registration.state === "waitlisted").map(t => placeholder((t.display_name || String(t.telegram_user_id)) + " · Telegram " + t.telegram_user_id + " · " + w()[t.registration.role], t.registration.id)));
    el.target.value = targets.some(t => t.registration.id === target && t.registration.state === "waitlisted") ? target : "";
  }
  function labels() {
    for (const n of ["title", "note", "event-label", "target-label", "events-first", "events-next", "targets-first", "targets-next", "prepare", "consent-label", "confirm", "close", "recover", "retry", "pending-title", "reload", "recent", "first", "next"]) el[n].textContent = w()[n];
    options(); render();
  }
  async function call(e, path, method = "GET", body) {
    try { const data = await env.request("./pass-assignments/" + path, method, body); if (e !== epoch) throw { stale: true }; return data; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.stale) return;
    if ([401, 403].includes(error.status)) { env.setBusy(false); clear(); if (error.status === 401) env.denied(); return; }
    section.hidden = false; el.status.textContent = w()[error.local || (error.status === 409 ? "conflict" : record ? "unknown" : "unavailable")]; controls();
  }
  async function run(action) {
    if (working || locked || blocked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  const cursorValid = p => p.next == null || uuid(p.next);
  async function loadEvents(e, cursor) {
    const p = await call(e, "events" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(p.events) || p.events.length > 20 || p.events.some(v => !validEvent(v)) || !cursorValid(p)) throw new Error("Invalid events");
    events = p.events; for (const event of events) eventCache.set(event.id, event);
    eventsAfter = cursor; eventsNext = p.next; targets = []; targetsAfter = targetsNext = undefined; el.event.value = el.target.value = ""; options(); controls();
  }
  async function loadTargets(e, cursor) {
    const event = selectedEvent(); if (!event) return;
    const p = await call(e, "events/" + event.id + "/registrations" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(p.registrations) || p.registrations.length > 20 || p.registrations.some(v => !validTarget(v) || v.registration.event_id !== event.id) || !cursorValid(p)) throw new Error("Invalid participants");
    targets = p.registrations; targetsAfter = cursor; targetsNext = p.next; el.target.value = ""; options();
    el.status.textContent = targets.some(t => t.registration.state === "waitlisted") ? "" : w().noTarget; controls();
  }
  async function loadPending(e, cursor) {
    const p = await call(e, "proposals" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(p.proposals) || p.proposals.length > 20 || p.proposals.some(v => !validView(v) || v.state !== "pending") || !cursorValid(p)) throw new Error("Invalid proposals");
    recentMode = false; proposals = p.proposals; after = cursor; next = p.next; ready = true; section.hidden = false; render();
  }
  async function loadRecent(e) {
    const p = await call(e, "recent");
    if (!Array.isArray(p.proposals) || p.proposals.length > 20 || p.proposals.some(v => !validView(v)) || p.next != null) throw new Error("Invalid recent operations");
    recentMode = true; proposals = p.proposals; after = next = undefined; ready = true; section.hidden = false; render();
  }
  async function recover(e) {
    let p;
    try { p = await call(e, "operations/" + encodeURIComponent(record.operation_id)); }
    catch (error) { if (error.status !== 404 || record.phase !== "prepare") throw error; missing = true; view = undefined; el.consent.checked = false; el.status.textContent = w().notArrived; render(); return; }
    if (!validView(p) || !sameArgs(p.preparation.arguments, record.arguments) || (record.proposal_id && p.preparation.proposal_id !== record.proposal_id)) throw new Error("Invalid operation");
    persist({ ...record, proposal_id: p.preparation.proposal_id, phase: record.phase === "confirm" ? "confirm" : "prepared" });
    view = p; missing = false; el.consent.checked = false; render();
    el.status.textContent = w()[p.state === "pending" ? "prepared" : p.state];
  }
  async function prepare(e) {
    missing = false;
    const p = await call(e, "prepare", "POST", { operation_id: record.operation_id, arguments: record.arguments });
    if (!validPreparation(p) || !sameArgs(p.arguments, record.arguments)) throw new Error("Invalid preparation");
    persist({ ...record, proposal_id: p.proposal_id, phase: "prepared" });
    await recover(e);
  }
  el.event.addEventListener("change", () => run(async e => { targets = []; el.target.value = ""; options(); await loadTargets(e); }));
  el.target.addEventListener("change", controls); el.consent.addEventListener("change", controls);
  el.prepare.addEventListener("click", () => run(async e => {
    const event = selectedEvent(), target = selectedTarget();
    if (record || !ready || !event || !target || target.registration.state !== "waitlisted") return;
    persist({ owner, operation_id: crypto.randomUUID(), phase: "prepare", arguments: { event_id: event.id, registration_id: target.registration.id, expected_event_version: event.version, expected_version: target.registration.version } });
    await prepare(e);
  }));
  el.confirm.addEventListener("click", () => run(async e => {
    if (!pending() || !el.consent.checked) { el.status.textContent = w().expired; render(); return; }
    persist({ ...record, phase: "confirm" });
    const r = await call(e, "confirm", "POST", { operation_id: record.operation_id, proposal_id: view.preparation.proposal_id, arguments: view.preparation.arguments, confirmed: true });
    if (!validRegistration(r) || r.id !== record.arguments.registration_id || r.event_id !== record.arguments.event_id || r.state !== "assigned" || !r.pass) throw new Error("Invalid receipt");
    await recover(e);
  }));
  el.recover.addEventListener("click", () => run(recover));
  el.retry.addEventListener("click", () => run(async e => { if (!record || record.phase !== "prepare" || !missing) return; await prepare(e); }));
  el.close.addEventListener("click", () => run(async e => {
    if (!canClose()) return;
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
    record = view = undefined; missing = false; el.consent.checked = false; el.status.textContent = w().closeNote; render(); await loadPending(e);
  }));
  el.reload.addEventListener("click", () => run(async e => { await loadPending(e, after); if (record) await recover(e); }));
  el.recent.addEventListener("click", () => run(loadRecent));
  el["events-first"].addEventListener("click", () => run(async e => { await loadEvents(e); if (!ready) await loadPending(e); }));
  el["events-next"].addEventListener("click", () => run(e => loadEvents(e, eventsNext)));
  el["targets-first"].addEventListener("click", () => run(e => loadTargets(e)));
  el["targets-next"].addEventListener("click", () => run(e => loadTargets(e, targetsNext)));
  el.first.addEventListener("click", () => run(e => loadPending(e)));
  el.next.addEventListener("click", () => run(e => loadPending(e, next)));
  return { clear, setBusy(v) { locked = v; controls(); }, setLanguage(value) { locale = /^(ru|be|uk)(-|$)/i.test(value) ? "ru" : "en"; labels(); },
    async start(value, language) {
      clear(); if (!uuid(value)) return; owner = value; locale = /^(ru|be|uk)(-|$)/i.test(language) ? "ru" : "en"; labels(); const e = epoch;
      try {
        let raw; try { raw = sessionStorage.getItem(key()); } catch { throw { local: "blocked" }; }
        if (raw) { if (raw.length > 2048) throw { local: "blocked" }; try { record = JSON.parse(raw); } catch { throw { local: "blocked" }; } if (!validRecord(record)) throw { local: "blocked" }; }
        await loadEvents(e); await loadPending(e); if (record) await recover(e);
      } catch (error) { if (e === epoch) { if (error.local === "blocked") blocked = true; errorStatus(error); } }
      finally { if (e === epoch) controls(); }
    }
  };
};
