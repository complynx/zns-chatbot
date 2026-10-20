"use strict";

window.createZinusyaPairAssignments = env => {
  const names = ["title", "note", "status", "event-label", "event", "events-first", "events-next", "target-label", "target", "targets-first", "targets-next", "prepare", "preview", "consent", "consent-label", "confirm", "close", "recover", "retry", "receipt", "pending-title", "pending", "reload", "recent", "first", "next"];
  const el = Object.fromEntries(names.map(n => [n, document.getElementById("ppa-" + n)]));
  const section = document.getElementById("pair-assignments");
  const words = {
    en: { title: "Assign passes to a pair", note: "Administrator tools. Select a waiting-list pair, review the exact terms and confirm. No payment is recorded.", "event-label": "Event", "target-label": "Pair", chooseEvent: "Select an event", chooseTarget: "Select a pair", "events-first": "Refresh events", "events-next": "Next events", "targets-first": "Refresh pairs", "targets-next": "Next pairs", prepare: "Review pair assignment", "consent-label": "I approve this exact pair assignment and its terms", confirm: "Assign both passes", retryConfirm: "Retry this exact confirmation", close: "Close local preview", recover: "Check earlier request", retry: "Retry the same preparation", "pending-title": "Unfinished assignments", reload: "Reload unfinished assignments", recent: "Recent form operations", recentEmpty: "No recent form operations.", first: "First proposals", next: "Next proposals", choose: "Review proposal", empty: "No unfinished assignments on this page.", prepared: "Review the original terms before confirming.", completed: "This earlier operation completed. The receipt is historical.", expired: "This proposal expired. It cannot authorize another assignment.", historical: "Earlier assignment result", expiry: "Confirm before", closeNote: "Closing removes this local preview. It does not cancel the proposal on the server.", unavailable: "Could not read data. Retry the same read.", unknown: "Result unknown. Check the same operation before starting another assignment.", notArrived: "No saved proposal was found. You may retry the same preparation with its original identifiers and versions.", conflict: "The registration or proposal changed. Check the earlier operation and review its result.", storage: "Recovery identifiers could not be saved. You can inspect server proposals and receipts; new changes are blocked.", blocked: "Saved recovery identifiers cannot be read. You can inspect server proposals and receipts; new changes are blocked.", price: "Price", tier: "Tier", deposit: "Required deposit", minimum: "Minimum amount", share: "Minimum share", leader: "Leader", follower: "Follower", role: "Role", version: "Version", recipient: "Recipient", event: "Event", window: "Payment evidence window after assignment", cap: "No later than", registration: "Registration", final: "Final payment deadline", due: "Evidence deadline", timezone: "Event timezone", noTarget: "No waiting-list pairs on this page." },
    ru: { title: "Назначить пассы паре", note: "Инструменты администратора. Выбери пару из очереди, проверь точные условия и подтверди. Оплата не фиксируется.", "event-label": "Эвент", "target-label": "Пара", chooseEvent: "Выбери эвент", chooseTarget: "Выбери пару", "events-first": "Обновить эвенты", "events-next": "Следующие эвенты", "targets-first": "Обновить пары", "targets-next": "Следующие пары", prepare: "Проверить назначение пассов паре", "consent-label": "Подтверждаю назначение обоих пассов этой паре на указанных условиях", confirm: "Назначить оба пасса", retryConfirm: "Повторить именно это подтверждение", close: "Закрыть локальный предпросмотр", recover: "Проверить прежний запрос", retry: "Повторить ту же подготовку", "pending-title": "Незавершённые назначения", reload: "Обновить незавершённые назначения", recent: "Недавние операции формы", recentEmpty: "Недавних операций формы нет.", first: "Первая страница предложений", next: "Следующие предложения", choose: "Проверить предложение", empty: "На этой странице нет незавершённых назначений.", prepared: "Проверь исходные условия перед подтверждением.", completed: "Прежняя операция завершена. Квитанция показывает исторический результат.", expired: "Предложение истекло и не разрешает новое назначение.", historical: "Результат прежнего назначения", expiry: "Подтвердить до", closeNote: "Закрытие убирает только локальный предпросмотр. Предложение на сервере не отменяется.", unavailable: "Не удалось прочитать данные. Повтори чтение.", unknown: "Результат неизвестен. Проверь ту же операцию перед новым назначением.", notArrived: "Сохранённое предложение не найдено. Можно повторить ту же подготовку с исходными идентификаторами и версиями.", conflict: "Регистрация или предложение изменились. Проверь прежнюю операцию и её результат.", storage: "Не удалось сохранить идентификаторы восстановления. Предложения и квитанции сервера доступны для просмотра. Новые изменения заблокированы.", blocked: "Не удалось прочитать сохранённые идентификаторы восстановления. Предложения и квитанции сервера доступны для просмотра. Новые изменения заблокированы.", price: "Цена", tier: "Тариф", deposit: "Требуемая предоплата", minimum: "Минимальная сумма", share: "Минимальная доля", leader: "Лидер", follower: "Фолловер", role: "Роль", version: "Версия", recipient: "Получатель", event: "Эвент", window: "Срок отправки подтверждения после назначения", cap: "Не позднее", registration: "Регистрация", final: "Срок доплаты", due: "Срок отправки подтверждения", timezone: "Таймзона эвента", noTarget: "На этой странице нет пар в очереди." }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const version = v => Number.isSafeInteger(v) && v > 0;
  const operation = v => typeof v === "string" && [...v].length > 0 && [...v].length <= 128;
  const instant = v => typeof v === "string" && /(?:Z|[+-]\d\d:\d\d)$/.test(v) && Number.isFinite(Date.parse(v));
  const validArgs = a => a && uuid(a.event_id) && uuid(a.pair_id) && version(a.expected_event_version) && version(a.expected_pair_version) && version(a.expected_leader_version) && version(a.expected_follower_version) && Object.keys(a).every(k => ["event_id", "pair_id", "expected_event_version", "expected_pair_version", "expected_leader_version", "expected_follower_version"].includes(k));
  const sameArgs = (a, b) => validArgs(a) && validArgs(b) && ["event_id", "pair_id", "expected_event_version", "expected_pair_version", "expected_leader_version", "expected_follower_version"].every(k => a[k] === b[k]);
  const validDeposit = d => !d || (Number.isSafeInteger(d.required_minor) && d.required_minor >= 0 && instant(d.final_due_at) && (d.minimum_minor === undefined || (Number.isSafeInteger(d.minimum_minor) && d.minimum_minor >= 0)) && (d.minimum_bps === undefined || (Number.isInteger(d.minimum_bps) && d.minimum_bps >= 0 && d.minimum_bps <= 10000)));
  const validQuote = q => q && uuid(q.registration_id) && Number.isSafeInteger(q.price_minor) && q.price_minor >= 0 && /^[A-Z]{3}$/.test(q.currency) && Number.isInteger(q.currency_scale) && q.currency_scale >= 0 && q.currency_scale <= 6 && (!q.tier || (Number.isSafeInteger(q.tier.number) && q.tier.number > 0)) && validDeposit(q.deposit);
  const validZone = v => typeof v === "string" && (() => { try { new Intl.DateTimeFormat("en", { timeZone: v }); return true; } catch { return false; } })();
  const validDisplayTerms = (d, zone) => d === undefined || (d && typeof d.event_name === "string" && d.event_name.trim() && [...d.event_name].length <= 256 && Number.isInteger(d.payment_window_seconds) && d.payment_window_seconds > 0 && d.payment_window_seconds <= 2147483647 && instant(d.payment_closes_at) && validZone(zone));
  const validPreparation = p => p && validDisplayTerms(p.display_terms, p.timezone) && (p.timezone === undefined || validZone(p.timezone)) && uuid(p.proposal_id) && validArgs(p.arguments) && typeof p.description === "string" && p.description.trim() && instant(p.expires_at) && Array.isArray(p.quotes) && p.quotes.length === 2 && p.quotes.every(validQuote) && p.quotes[0].registration_id !== p.quotes[1].registration_id;
  const validRegistration = r => r && uuid(r.id) && uuid(r.event_id) && uuid(r.user_id) && version(r.version) && ["leader", "follower"].includes(r.role) && ["waitlisted", "assigned", "cancelled"].includes(r.state);
  const validPair = p => p && uuid(p.id) && uuid(p.event_id) && uuid(p.leader_registration_id) && uuid(p.follower_registration_id) && p.leader_registration_id !== p.follower_registration_id && version(p.version) && ["waitlisted", "assigned", "dissolved"].includes(p.state);
  const validResult = (r, a, quotes) => r && validPair(r.pair) && r.pair.id === a.pair_id && r.pair.event_id === a.event_id && r.pair.state === "assigned" && ["leader", "follower"].every(role => validRegistration(r[role]) && r[role].id === r.pair[role + "_registration_id"] && r[role].event_id === a.event_id && r[role].role === role && r[role].state === "assigned" && r[role].pass && validDeposit(r[role].pass.deposit) && quotes.some(q => q.registration_id === r[role].id));
  const validView = v => v && validRecipient(v.leader) && validRecipient(v.follower) && v.leader.user_id !== v.follower.user_id && v.leader.telegram_user_id !== v.follower.telegram_user_id && operation(v.original_operation_id) && validPreparation(v.preparation) && ["pending", "expired", "completed"].includes(v.state) && instant(v.observed_at) && (v.state !== "completed" || (validResult(v.result, v.preparation.arguments, v.preparation.quotes) && ["leader", "follower"].every(role => v.result[role].user_id === v[role].user_id)));
  const validEvent = e => e && uuid(e.id) && version(e.version) && e.document && typeof e.document.name === "string" && typeof e.document.timezone === "string" && (() => { try { new Intl.DateTimeFormat("en", { timeZone: e.document.timezone }); return true; } catch { return false; } })();
  const validRecipient = r => r && uuid(r.user_id) && Number.isSafeInteger(r.telegram_user_id) && r.telegram_user_id > 0 && typeof r.display_name === "string";
  const validTarget = t => t && validArgs(t.arguments) && validPair(t.pair) && t.pair.id === t.arguments.pair_id && t.pair.event_id === t.arguments.event_id && t.pair.version === t.arguments.expected_pair_version && t.pair.state === "waitlisted" && validRecipient(t.leader) && validRecipient(t.follower) && t.leader.user_id !== t.follower.user_id && t.leader.telegram_user_id !== t.follower.telegram_user_id;
  let storageIssue, owner, locale = "en", epoch = 0, working = false, locked = false, blocked = false, ready = false;
  let record, view, missing = false, recentMode = false, events = [], targets = [], proposals = [], eventsAfter, eventsNext, targetsAfter, targetsNext, after, next;
  const eventCache = new Map();
  const w = () => words[locale];
  const key = () => "zinusya.pair-assignments." + owner;
  const node = (tag, text) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; };
  const validRecord = r => r && r.owner === owner && operation(r.operation_id) && validArgs(r.arguments) && ["prepare", "prepared", "confirm"].includes(r.phase) && (r.proposal_id === undefined || uuid(r.proposal_id)) && (r.phase === "prepare" || uuid(r.proposal_id)) && Object.keys(r).every(k => ["owner", "operation_id", "arguments", "proposal_id", "phase"].includes(k));
  function persist(r) {
    if (!validRecord(r)) throw new Error("Invalid recovery identifiers");
    try { sessionStorage.setItem(key(), JSON.stringify(r)); } catch { blocked = true; storageIssue = "storage"; throw { local: "storage" }; }
    record = r;
  }
  function clear() {
    epoch++; owner = record = view = eventsAfter = eventsNext = targetsAfter = targetsNext = after = next = undefined;
    storageIssue = undefined; events = []; targets = []; proposals = []; eventCache.clear(); ready = working = blocked = missing = recentMode = false;
    for (const n of ["preview", "receipt", "pending", "event", "target"]) el[n].replaceChildren();
    el.event.value = el.target.value = el.status.textContent = ""; el.consent.checked = false; section.hidden = true; controls();
  }
  const selectedEvent = () => eventCache.get(el.event.value);
  const selectedTarget = () => targets.find(t => t.pair.id === el.target.value);
  const pending = () => view?.state === "pending" && Date.parse(view.preparation.expires_at) > Date.now();
  const canClose = () => record && (record.phase !== "confirm" || ["completed", "expired"].includes(view?.state));
  function controls() {
    const busy = locked || working || !owner;
    el.event.disabled = el.target.disabled = busy || blocked || !!record || !ready;
    el.prepare.disabled = busy || blocked || !!record || !ready || !selectedEvent() || !selectedTarget() || selectedTarget().pair.state !== "waitlisted";
    el.consent.hidden = el["consent-label"].hidden = blocked || !pending();
    el.consent.disabled = busy || blocked || !pending();
    el.confirm.hidden = !view || view.state !== "pending";
    el.confirm.disabled = busy || blocked || !pending() || !el.consent.checked;
    el.confirm.textContent = w()[record?.phase === "confirm" ? "retryConfirm" : "confirm"];
    el.recover.hidden = !record; el.recover.disabled = busy;
    el.retry.hidden = !record || record.phase !== "prepare" || !missing; el.retry.disabled = busy || blocked;
    // Preparation has no business effect and may be closed locally.
    // A confirmation with no observed result must keep its original operation.
    el.close.hidden = !canClose(); el.close.disabled = busy || blocked;
    el.reload.disabled = busy; el.recent.disabled = busy || (!!record && !blocked);
    for (const b of el.pending.querySelectorAll("button")) b.disabled = busy || (!!record && !blocked);
    for (const [first, more, previous, following] of [["events-first", "events-next", eventsAfter, eventsNext], ["targets-first", "targets-next", targetsAfter, targetsNext], ["first", "next", after, next]]) {
      el[first].hidden = !["events-first", "targets-first"].includes(first) && !previous; el[more].hidden = !following;
      el[first].disabled = el[more].disabled = busy || (!!record && !blocked) || (first === "targets-first" && !selectedEvent());
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
  function depositTerms(target, quote, timezone) {
    const d = quote.deposit;
    if (!d) return;
    target.append(node("p", w().deposit + ": " + money({ ...quote, price_minor: d.required_minor })));
    // Older receipts can lack policy fields. Do not invent their original values.
    if (d.minimum_minor !== undefined) target.append(node("p", w().minimum + ": " + money({ ...quote, price_minor: d.minimum_minor })));
    if (d.minimum_bps !== undefined) target.append(node("p", w().share + ": " + Math.floor(d.minimum_bps / 100) + "." + String(d.minimum_bps % 100).padStart(2, "0") + "%"));
    target.append(node("p", w().final + ": " + localTime(d.final_due_at, timezone)));
  }
  function paymentWindow(seconds) {
    const parts = [];
    for (const [size, unit] of [[86400, "day"], [3600, "hour"], [60, "minute"], [1, "second"]]) {
      const count = Math.floor(seconds / size); seconds %= size;
      if (count) parts.push(new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "long" }).format(count));
    }
    return parts.join(", ");
  }
  const recipientLabel = (v, role) => w()[role] + ": " + (v[role].display_name || String(v[role].telegram_user_id)) + " · Telegram " + v[role].telegram_user_id;
  function render() {
    el.preview.replaceChildren(); el.receipt.replaceChildren(); el.pending.replaceChildren();
    if (view) {
      const p = view.preparation;
      if (p.display_terms) {
        const d = p.display_terms;
        el.preview.append(node("p", w().event + ": " + d.event_name));
        el.preview.append(node("p", w().window + ": " + paymentWindow(d.payment_window_seconds)));
        el.preview.append(node("p", w().cap + ": " + localTime(d.payment_closes_at, p.timezone)));
      } else {
        // Legacy timing exists only in this immutable description, not current event settings.
        el.preview.append(node("p", p.description));
      }
      for (const [index, q] of p.quotes.entries()) {
        el.preview.append(node("h2", recipientLabel(view, index === 0 ? "leader" : "follower")));
        el.preview.append(node("p", w().price + ": " + money(q)));
        if (q.tier) el.preview.append(node("p", w().tier + ": " + q.tier.number));
        depositTerms(el.preview, q, p.timezone);
      }
      el.preview.append(node("p", w().expiry + ": " + localTime(p.expires_at, p.timezone)), node("p", w().closeNote));
      if (view.state === "expired" || (view.state === "pending" && !pending())) el.preview.append(node("p", w().expired));
      if (view.result) {
        el.receipt.append(node("h2", w().historical), node("p", w().completed));
        for (const role of ["leader", "follower"]) {
          const r = view.result[role];
          el.receipt.append(node("h3", recipientLabel(view, role)), node("p", w().price + ": " + money(r.pass)));
          depositTerms(el.receipt, r.pass, p.timezone);
          if (instant(r.pass.payment_due_at)) el.receipt.append(node("p", w().due + ": " + localTime(r.pass.payment_due_at, p.timezone)));
        }
      }
    }
    for (const p of proposals) {
      const b = node("button", w().choose + ": " + recipientLabel(p, "leader") + "; " + recipientLabel(p, "follower")); b.type = "button";
      b.addEventListener("click", () => run(async e => {
        if (blocked) { await inspectProposal(e, p); return; }
        if (record) return;
        try { persist({ owner, operation_id: p.original_operation_id, proposal_id: p.preparation.proposal_id, arguments: p.preparation.arguments, phase: "prepared" }); }
        catch (error) { if (!blocked) throw error; await inspectProposal(e, p); return; }
        await recover(e);
      })); el.pending.append(b);
    }
    el["pending-title"].textContent = w()[recentMode ? "recent" : "pending-title"];
    if (!proposals.length) el.pending.append(node("p", w()[recentMode ? "recentEmpty" : "empty"])); if (blocked) el.status.textContent = w()[storageIssue || "blocked"]; controls();
  }
  function options() {
    const ev = el.event.value, target = el.target.value;
    const placeholder = (label, value = "") => { const n = node("option", label); n.value = value; return n; };
    el.event.replaceChildren(placeholder(w().chooseEvent), ...events.map(e => placeholder(e.document.name + " · " + e.document.timezone, e.id)));
    el.event.value = events.some(e => e.id === ev) ? ev : "";
    el.target.replaceChildren(placeholder(w().chooseTarget), ...targets.map(t => placeholder(w().leader + ": " + (t.leader.display_name || String(t.leader.telegram_user_id)) + " · Telegram " + t.leader.telegram_user_id + "; " + w().follower + ": " + (t.follower.display_name || String(t.follower.telegram_user_id)) + " · Telegram " + t.follower.telegram_user_id, t.pair.id)));
    el.target.value = targets.some(t => t.pair.id === target) ? target : "";
  }
  function labels() {
    for (const n of ["title", "note", "event-label", "target-label", "events-first", "events-next", "targets-first", "targets-next", "prepare", "consent-label", "confirm", "close", "recover", "retry", "pending-title", "reload", "recent", "first", "next"]) el[n].textContent = w()[n];
    options(); render();
  }
  async function call(e, path, method = "GET", body) {
    try { const data = await env.request("./pair-assignments/" + path, method, body); if (e !== epoch) throw { stale: true }; return data; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.stale) return;
    if ([401, 403].includes(error.status)) { env.setBusy(false); clear(); if (error.status === 401) env.denied(); return; }
    section.hidden = false; el.status.textContent = w()[error.local || (error.status === 409 ? "conflict" : record ? "unknown" : "unavailable")]; controls();
  }
  async function run(action) {
    if (working || locked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); if (blocked) el.status.textContent = w()[storageIssue || "blocked"]; controls(); } }
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
    const p = await call(e, "events/" + event.id + "/candidates" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(p.candidates) || p.candidates.length > 20 || p.candidates.some(v => !validTarget(v) || v.pair.event_id !== event.id) || !cursorValid(p)) throw new Error("Invalid pairs");
    targets = p.candidates; targetsAfter = cursor; targetsNext = p.next; el.target.value = ""; options();
    el.status.textContent = targets.length > 0 ? "" : w().noTarget; controls();
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
  async function inspectProposal(e, listed) {
    const p = await call(e, "proposals/" + listed.preparation.proposal_id);
    if (!validView(p) || p.original_operation_id !== listed.original_operation_id || p.preparation.proposal_id !== listed.preparation.proposal_id || !sameArgs(p.preparation.arguments, listed.preparation.arguments)) throw new Error("Invalid proposal");
    // Read-only inspection must not replace an unreadable or uncertain local operation.
    view = p; el.consent.checked = false; render();
  }
  async function recover(e) {
    let p;
    try { p = await call(e, "operations/" + encodeURIComponent(record.operation_id)); }
    catch (error) { if (error.status !== 404 || record.phase !== "prepare") throw error; missing = true; view = undefined; el.consent.checked = false; el.status.textContent = w().notArrived; render(); return; }
    if (!validView(p) || p.original_operation_id !== record.operation_id || !sameArgs(p.preparation.arguments, record.arguments) || (record.proposal_id && p.preparation.proposal_id !== record.proposal_id)) throw new Error("Invalid operation");
    if (!blocked) {
      try { persist({ ...record, proposal_id: p.preparation.proposal_id, phase: record.phase === "confirm" ? "confirm" : "prepared" }); }
      catch (error) { if (!blocked) throw error; }
    }
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
    if (blocked || record || !ready || !event || !target || target.pair.state !== "waitlisted") return;
    persist({ owner, operation_id: crypto.randomUUID(), phase: "prepare", arguments: { ...target.arguments } });
    await prepare(e);
  }));
  el.confirm.addEventListener("click", () => run(async e => {
    if (blocked || !record || !pending() || !el.consent.checked) { el.status.textContent = w().expired; render(); return; }
    persist({ ...record, phase: "confirm" });
    el.consent.checked = false;
    const r = await call(e, "confirm", "POST", { operation_id: record.operation_id, proposal_id: view.preparation.proposal_id, arguments: view.preparation.arguments, confirmed: true });
    if (!validResult(r, record.arguments, view.preparation.quotes)) throw new Error("Invalid receipt");
    await recover(e);
  }));
  el.recover.addEventListener("click", () => run(recover));
  el.retry.addEventListener("click", () => run(async e => { if (blocked || !record || record.phase !== "prepare" || !missing) return; await prepare(e); }));
  el.close.addEventListener("click", () => run(async e => {
    if (blocked || !canClose()) return;
    try { sessionStorage.removeItem(key()); } catch { blocked = true; storageIssue = "storage"; throw { local: "storage" }; }
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
      clear(); if (!uuid(value)) return; owner = value; section.hidden = false; working = true; env.setBusy(true); locale = /^(ru|be|uk)(-|$)/i.test(language) ? "ru" : "en"; labels(); const e = epoch;
      try {
        try {
          const raw = sessionStorage.getItem(key());
          if (raw) {
            if (raw.length > 2048) throw new Error("Oversized recovery identifiers");
            const saved = JSON.parse(raw);
            if (!validRecord(saved)) throw new Error("Invalid recovery identifiers");
            record = saved;
          }
        } catch { blocked = true; storageIssue = "blocked"; }
        await loadEvents(e); await loadPending(e); if (record) await recover(e);
      } catch (error) { if (e === epoch) { if (error.local === "blocked") blocked = true; errorStatus(error); } }
      finally { if (e === epoch) { working = false; env.setBusy(false); if (blocked) el.status.textContent = w()[storageIssue || "blocked"]; controls(); } }
    }
  };
};
