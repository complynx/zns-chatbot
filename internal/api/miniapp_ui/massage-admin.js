"use strict";

window.createZinusyaMassageAdmin = env => {
  const ids = ["title", "note", "status", "event-label", "event", "events-first", "events-next", "alias-label", "alias", "bindings-first", "bindings-next", "current", "lookup-label", "lookup", "lookup-button", "target", "action-label", "action", "prepare", "preview", "confirm", "close", "recover", "retry", "receipt", "choices", "choices-first", "choices-next", "reload"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("ma-" + id)]));
  const section = document.getElementById("massage-admin");
  const words = {
    en: { title: "Massage specialist access", note: "Administrator tools. Access permits reading the event timetable and the specialist's own client contacts. An existing alias keeps its original account.", "event-label": "Event", "alias-label": "Specialist", "lookup-label": "Known account UUID or Telegram user ID", "lookup-button": "Find this account", "action-label": "Action", grant: "Grant access", revoke: "Revoke access", prepare: "Review access change", confirm: "Confirm this exact change", close: "Close local preview", recover: "Check earlier request", retry: "Retry the same request", reload: "Reload current state", "events-first": "First events", "events-next": "Next events", "bindings-first": "First bindings", "bindings-next": "Next bindings", "choices-first": "First unfinished choices", "choices-next": "Next unfinished choices", choose: "Choose an event", enabled: "Current access: granted", disabled: "Current access: revoked", absent: "No account is bound to this alias.", version: "Version", unavailable: "Could not load data. Reload to retry.", conflict: "The request no longer matches current data. Check its original status; close an unapplied preview before choosing again.", unknown: "The earlier result is unknown. Check it or retry the same request. A missing status is not proof of failure.", storage: "Could not save recovery identifiers. Further changes are blocked. Reload to check the earlier request.", blocked: "The saved recovery identifiers cannot be read. Further changes are blocked.", invalid: "Enter one valid account UUID or a Telegram ID without rounding.", prepared: "Review this exact account, action and event before confirming.", expired: "The original preview expired. It cannot authorize a new change.", expiry: "Confirm before", account: "Account", alias: "Specialist ID", stale: "Current versions or eligibility changed. This preview cannot be confirmed.", historical: "Earlier operation result; current access is shown separately below.", nochoices: "No unfinished choices on this page.", continue: "Review saved access change", none: "No configured massage events.", noRetry: "The original parameters are not available yet. Check again. You can close a preview that was never submitted for application.", closeNote: "Closing removes this browser preview only. A saved server proposal remains discoverable until expiry.", revoked: "Account is revoked", unlinked: "Account has no linked identity" },
    ru: { title: "Доступ специалистов массажа", note: "Инструменты администратора. Доступ позволяет читать расписание эвента и контакты собственных клиентов. Существующий идентификатор специалиста сохраняет исходный аккаунт.", "event-label": "Эвент", "alias-label": "Специалист", "lookup-label": "UUID известного аккаунта или Telegram ID", "lookup-button": "Найти этот аккаунт", "action-label": "Действие", grant: "Выдать доступ", revoke: "Отозвать доступ", prepare: "Проверить изменение доступа", confirm: "Подтвердить именно это изменение", close: "Закрыть локальный предпросмотр", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", reload: "Обновить текущее состояние", "events-first": "Первые эвенты", "events-next": "Следующие эвенты", "bindings-first": "Первые привязки", "bindings-next": "Следующие привязки", "choices-first": "Первые незавершённые действия", "choices-next": "Следующие незавершённые действия", choose: "Выбери эвент", enabled: "Текущий доступ: выдан", disabled: "Текущий доступ: отозван", absent: "К этому специалисту пока не привязан аккаунт.", version: "Версия", unavailable: "Не удалось прочитать данные. Обнови и попробуй снова.", conflict: "Запрос больше не соответствует текущим данным. Проверь его исходный статус; закрой неприменённый предпросмотр перед новым выбором.", unknown: "Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос. Отсутствие статуса не доказывает ошибку.", storage: "Не удалось сохранить идентификаторы восстановления. Новые изменения заблокированы. Перезагрузи форму для проверки прежнего запроса.", blocked: "Не удалось прочитать сохранённые идентификаторы восстановления. Новые изменения заблокированы.", invalid: "Введи один корректный UUID аккаунта или Telegram ID без округления.", prepared: "Проверь именно этот аккаунт, действие и эвент перед подтверждением.", expired: "Исходный предпросмотр истёк и не разрешает новое изменение.", expiry: "Подтвердить до", account: "Аккаунт", alias: "ID специалиста", stale: "Текущие версии или доступность изменились. Этот предпросмотр нельзя подтвердить.", historical: "Результат прежней операции; текущий доступ показан отдельно ниже.", nochoices: "На этой странице нет незавершённых действий.", continue: "Проверить сохранённое изменение доступа", none: "Нет эвентов с настройками массажа.", noRetry: "Исходные параметры пока недоступны. Проверь ещё раз. Предпросмотр, не отправленный на применение, можно закрыть.", closeNote: "Закрытие убирает только предпросмотр в этом браузере. Сохранённое предложение на сервере остаётся доступно до истечения срока.", revoked: "Аккаунт отозван", unlinked: "У аккаунта нет связанной identity" }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const id = v => typeof v === "string" && /^[a-z][a-z0-9_-]{0,31}$/.test(v);
  const version = v => Number.isSafeInteger(v) && v > 0;
  const instant = v => typeof v === "string" && /(?:Z|[+-]\d{2}:\d{2})$/.test(v) && Number.isFinite(Date.parse(v));
  const argumentKeys = ["event_id", "specialist_id", "user_id", "active", "expected_version", "expected_event_version", "expected_schedule_version"];
  const validArgs = a => a && uuid(a.event_id) && id(a.specialist_id) && uuid(a.user_id) && typeof a.active === "boolean" && Number.isSafeInteger(a.expected_version) && a.expected_version >= 0 && version(a.expected_event_version) && version(a.expected_schedule_version);
  const sameArgs = (a, b) => validArgs(a) && validArgs(b) && argumentKeys.every(k => a[k] === b[k]);
  const validBinding = b => b && uuid(b.event_id) && id(b.specialist_id) && uuid(b.user_id) && typeof b.active === "boolean" && version(b.version);
  const validTarget = t => t && uuid(t.user_id) && Number.isSafeInteger(t.telegram_user_id) && t.telegram_user_id > 0 && typeof t.display_name === "string" && t.display_name.length <= 1024 && typeof t.linked === "boolean" && typeof t.revoked === "boolean";
  let owner, language = "en", epoch = 0, working = false, locked = false, blocked = false, ready = false;
  let events = [], eventID, eventAfter, eventNext, selectedAlias, context, bindingsAfter, bindingsNext, binding, bindingKnown = false, target;
  let choices = [], choicesAfter, choicesNext, record, args, proposal, receipt;
  const w = () => words[language];
  const key = () => "zinusya.massage-admin." + owner;
  const path = () => "./massage-admin/" + eventID;
  function node(tag, text) { const value = document.createElement(tag); if (text !== undefined) value.textContent = text; return value; }
  function options(select, values, selected) { select.replaceChildren(...values.map(v => { const item = node("option", v.name); item.value = v.id; return item; })); select.value = selected || ""; }
  function accountText(t) { return t.display_name + " · Telegram " + t.telegram_user_id + " · " + t.user_id + (t.revoked ? " · " + w().revoked : !t.linked ? " · " + w().unlinked : ""); }
  function validRecord(r) { return r && r.owner === owner && uuid(r.operation_id) && uuid(r.event_id) && ["prepare", "prepared", "apply"].includes(r.phase) && (r.proposal_id === undefined || uuid(r.proposal_id)) && (r.phase === "prepare" || uuid(r.proposal_id)) && Object.keys(r).every(k => ["owner", "operation_id", "event_id", "phase", "proposal_id"].includes(k)); }
  // Recovery storage contains identifiers only. Original arguments and identities
  // are read again through the authorized server after a page reload.
  function persist(r) {
    if (!validRecord(r)) throw new Error("Invalid recovery identifiers");
    try { sessionStorage.setItem(key(), JSON.stringify(r)); } catch { blocked = true; throw { local: "storage" }; }
    record = r;
  }
  function removeRecord() {
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
    record = args = proposal = undefined; el.preview.replaceChildren();
  }
  function clearFields() {
    context = binding = target = undefined; bindingKnown = false; selectedAlias = undefined;
    bindingsAfter = bindingsNext = undefined; el.alias.replaceChildren(); el.current.replaceChildren(); el.target.replaceChildren(); el.lookup.value = "";
  }
  function clear() {
    epoch++; owner = eventID = eventAfter = eventNext = choicesAfter = choicesNext = record = args = proposal = receipt = undefined;
    working = blocked = ready = false; events = []; choices = []; clearFields();
    for (const name of ["event", "choices", "preview", "receipt"]) el[name].replaceChildren();
    el.status.textContent = ""; section.hidden = true; controls();
  }
  function eligible(a) {
    if (!context || !bindingKnown || !target || target.user_id !== a.user_id) return false;
    if (a.expected_event_version !== context.page.event_version || a.expected_version !== (binding?.version || 0)) return false;
    if (binding && binding.user_id !== a.user_id) return false;
    return a.active ? a.expected_schedule_version === context.page.schedule_version && context.page.module_enabled && target.linked && !target.revoked && context.schedule.document.specialists.some(s => s.id === a.specialist_id) : !!binding;
  }
  function canConfirm() { return record?.phase === "prepared" && proposal?.state === "pending" && Date.parse(proposal.expires_at) > Date.now() && eligible(args); }
  function controls() {
    const busy = locked || working || !owner, editing = busy || blocked || !!record || !ready;
    for (const name of ["event", "events-first", "events-next", "alias", "bindings-first", "bindings-next", "action"]) el[name].disabled = editing;
    el.lookup.disabled = editing || !bindingKnown || !!binding;
    el["lookup-button"].disabled = el.lookup.disabled;
    el.prepare.disabled = editing || !candidate();
    el.confirm.hidden = record?.phase !== "prepared"; el.confirm.disabled = busy || blocked || !ready || !canConfirm();
    el.close.hidden = !record || (record.phase === "apply" && proposal?.state !== "expired"); el.close.disabled = busy || blocked;
    el.recover.hidden = !record; el.recover.disabled = busy || blocked;
    el.retry.hidden = !record || record.phase === "prepared" || proposal?.state === "expired"; el.retry.disabled = busy || blocked || !args;
    el.reload.disabled = busy || blocked;
    for (const button of el.choices.querySelectorAll("button")) button.disabled = editing;
    el["choices-first"].disabled = el["choices-next"].disabled = editing;
    for (const [first, next, after, cursor] of [["events-first", "events-next", eventAfter, eventNext], ["bindings-first", "bindings-next", bindingsAfter, bindingsNext], ["choices-first", "choices-next", choicesAfter, choicesNext]]) { el[first].hidden = !after; el[next].hidden = !cursor; }
  }
  function candidate() {
    if (!eventID || !selectedAlias || !context || !bindingKnown || !target) return undefined;
    const active = el.action.value === "grant";
    const a = { event_id: eventID, specialist_id: selectedAlias, user_id: target.user_id, active, expected_version: binding?.version || 0, expected_event_version: context.page.event_version, expected_schedule_version: context.page.schedule_version };
    return validArgs(a) && eligible(a) && (!binding ? active : binding.active !== active) ? a : undefined;
  }
  function renderSelectors() {
    const selectedEvent = context && !events.some(e => e.id === eventID) ? [{ id: eventID, name: context.schedule.event_name }] : [];
    options(el.event, [{ id: "", name: w().choose }, ...events, ...selectedEvent], eventID);
    const aliases = new Map();
    for (const s of context?.schedule.document.specialists || []) aliases.set(s.id, s.name?.[language] || s.name?.en || s.id);
    for (const b of context?.page.bindings || []) if (!aliases.has(b.specialist_id)) aliases.set(b.specialist_id, b.specialist_id);
    if (selectedAlias && !aliases.has(selectedAlias)) aliases.set(selectedAlias, selectedAlias);
    options(el.alias, [...aliases].map(([id, name]) => ({ id, name: name + " · " + id })), selectedAlias);
    options(el.action, [{ id: "grant", name: w().grant }, { id: "revoke", name: w().revoke }], el.action.value || "grant");
    el.current.textContent = bindingKnown ? binding ? w()[binding.active ? "enabled" : "disabled"] + " · " + w().version + " " + binding.version : w().absent : "";
    el.target.textContent = target ? accountText(target) : "";
  }
  function renderPreview() {
    el.preview.replaceChildren();
    if (!proposal || !context || !target || !args) return;
    const expiry = new Intl.DateTimeFormat(language, { timeZone: context.schedule.timezone, dateStyle: "medium", timeStyle: "long" }).format(new Date(proposal.expires_at));
    el.preview.append(node("h2", w()[args.active ? "grant" : "revoke"]), node("p", context.schedule.event_name), node("p", w().alias + ": " + args.specialist_id), node("p", w().account + ": " + accountText(target)), node("p", w().expiry + ": " + expiry + " · " + context.schedule.timezone), node("p", w().closeNote));
    if (!eligible(args)) el.preview.append(node("p", w().stale));
    controls();
  }
  function renderReceipt() {
    el.receipt.replaceChildren(); if (!receipt) return;
    el.receipt.append(node("h2", w().historical), node("p", w()[receipt.active ? "grant" : "revoke"] + " · " + receipt.specialist_id + " · " + receipt.user_id + " · " + w().version + " " + receipt.version));
  }
  function renderChoices() {
    el.choices.replaceChildren();
    for (const choice of choices) {
      const button = node("button", w().continue + ": " + choice.arguments.specialist_id + " · " + w()[choice.arguments.active ? "grant" : "revoke"]); button.type = "button";
      button.addEventListener("click", () => run(e => begin(e, choice.arguments, choice)));
      el.choices.append(button);
    }
    if (!choices.length) el.choices.append(node("p", w().nochoices)); controls();
  }
  function labels() {
    for (const name of ["title", "note", "event-label", "alias-label", "lookup-label", "lookup-button", "action-label", "prepare", "confirm", "close", "recover", "retry", "reload", "events-first", "events-next", "bindings-first", "bindings-next", "choices-first", "choices-next"]) el[name].textContent = w()[name];
    renderSelectors(); renderChoices(); renderPreview(); renderReceipt(); controls();
  }
  async function call(e, url, method = "GET", body) {
    try { const result = await env.request(url, method, body); if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.stale) return;
    if ([401, 403].includes(error.status)) { env.setBusy(false); clear(); if (error.status === 401) env.denied(); return; }
    section.hidden = false;
    el.status.textContent = w()[error.local || (error.status === 409 ? "conflict" : record ? "unknown" : "unavailable")]; controls();
  }
  async function run(action) {
    if (working || locked || !owner || blocked) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  async function loadEvents(e, after) {
    const page = await call(e, "./massage-admin/events" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.events) || page.events.length > 20 || page.events.some(v => !uuid(v.id) || typeof v.name !== "string") || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid administrative events");
    ready = true; events = page.events; eventAfter = after; eventNext = page.next; section.hidden = false; renderSelectors(); el.status.textContent = events.length ? "" : w().none;
  }
  async function lookup(e, query) {
    target = undefined; el.target.replaceChildren();
    const result = await call(e, "./massage-admin/target", "POST", query);
    if (!validTarget(result) || (query.user_id && result.user_id !== query.user_id) || (query.telegram_user_id && result.telegram_user_id !== query.telegram_user_id)) throw new Error("Invalid target lookup");
    target = result; renderSelectors(); controls();
  }
  async function loadAlias(e) {
    binding = target = undefined; bindingKnown = false; el.lookup.value = ""; renderSelectors();
    if (!selectedAlias) return;
    try {
      binding = await call(e, path() + "/bindings/" + selectedAlias);
      if (!validBinding(binding) || binding.event_id !== eventID || binding.specialist_id !== selectedAlias) throw new Error("Invalid binding");
    } catch (error) { if (error.status !== 404) throw error; }
    bindingKnown = true;
    if (binding) { el.action.value = binding.active ? "revoke" : "grant"; await lookup(e, { user_id: binding.user_id }); }
    else { el.action.value = "grant"; renderSelectors(); }
    controls();
  }
  async function loadContext(e, after) {
    clearFields(); if (!eventID) { renderSelectors(); return; }
    const schedule = await call(e, path() + "/schedule");
    const page = await call(e, path() + "/bindings" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (schedule.event_id !== eventID || page.event_id !== eventID || !version(schedule.version) || !version(schedule.event_version) || page.schedule_version !== schedule.version || page.event_version !== schedule.event_version || typeof schedule.event_name !== "string" || !schedule.event_name || schedule.event_name.length > 2048 || typeof schedule.timezone !== "string" || !Array.isArray(schedule.document?.specialists) || schedule.document.specialists.length > 32 || schedule.document.specialists.some(s => !id(s.id)) || typeof page.module_enabled !== "boolean" || !Array.isArray(page.bindings) || page.bindings.length > 20 || page.bindings.some(b => !validBinding(b) || b.event_id !== eventID) || (page.next_after !== undefined && !id(page.next_after))) throw new Error("Invalid administrative configuration");
    new Intl.DateTimeFormat(language, { timeZone: schedule.timezone });
    ready = true; context = { schedule, page }; bindingsAfter = after; bindingsNext = page.next_after;
    selectedAlias = args?.specialist_id || page.bindings[0]?.specialist_id || schedule.document.specialists[0]?.id;
    renderSelectors(); await loadAlias(e);
  }
  function validateStatus(value) {
    if (!value || !uuid(value.proposal_id) || !uuid(value.operation_id) || !validArgs(value.arguments) || value.arguments.event_id !== record.event_id || !instant(value.expires_at) || !["pending", "expired", "completed"].includes(value.state) || (args && !sameArgs(args, value.arguments)) || (record.proposal_id && record.proposal_id !== value.proposal_id) || (proposal && Date.parse(proposal.expires_at) !== Date.parse(value.expires_at))) throw new Error("Invalid original proposal status");
    return value;
  }
  async function completed(e, value) {
    if (!validBinding(value) || !args || value.event_id !== args.event_id || value.specialist_id !== args.specialist_id || value.user_id !== args.user_id || value.active !== args.active) throw new Error("Invalid original operation result");
    const result = value, alias = args.specialist_id;
    removeRecord(); receipt = result; renderReceipt(); await loadContext(e, bindingsAfter);
    selectedAlias = alias; renderSelectors(); await loadAlias(e);
    await loadChoices(e); el.status.textContent = w().historical;
  }
  async function present(e, value, knownOperation) {
    const status = validateStatus(value); args = status.arguments; proposal = status; eventID = args.event_id;
    if (status.state === "completed") return completed(e, status.result);
    persist({ ...record, proposal_id: status.proposal_id, ...(knownOperation && record.phase !== "apply" ? { phase: "prepared" } : {}) });
    await loadContext(e); if (!target || target.user_id !== args.user_id) await lookup(e, { user_id: args.user_id });
    renderPreview(); el.status.textContent = w()[status.state === "expired" ? "expired" : record.phase === "apply" || !knownOperation ? "unknown" : "prepared"];
  }
  async function recover(e) {
    if (!record) return;
    let value, knownOperation = true;
    try { value = await call(e, "./massage-admin/status/operations/" + record.operation_id); }
    catch (error) {
      if (error.status !== 404 || !record.proposal_id) throw error;
      knownOperation = false; value = await call(e, "./massage-admin/status/proposals/" + record.proposal_id);
    }
    await present(e, value, knownOperation);
  }
  async function dispatch(e) {
    if (!record || !args) { el.status.textContent = w().noRetry; return; }
    const applying = record.phase === "apply";
    if (applying && (proposal?.state !== "pending" || Date.parse(proposal.expires_at) <= Date.now())) { el.status.textContent = w().expired; return; }
    const body = { operation_id: record.operation_id, arguments: args, ...(record.proposal_id ? { proposal_id: record.proposal_id } : {}), ...(applying ? { confirmed: true } : {}) };
    const value = await call(e, path() + "/bindings" + (applying ? "" : "/prepare"), "POST", body);
    if (applying) return completed(e, value);
    await present(e, value, true);
  }
  async function begin(e, selection, saved) {
    if (record || blocked || !validArgs(selection)) return;
    args = { ...selection }; proposal = saved; receipt = undefined; renderReceipt(); eventID = args.event_id;
    persist({ owner, event_id: eventID, operation_id: crypto.randomUUID(), phase: "prepare", ...(saved ? { proposal_id: saved.proposal_id } : {}) });
    await dispatch(e);
  }
  async function loadChoices(e, after) {
    const page = await call(e, "./massage-admin/pending" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.proposals) || page.proposals.length > 20 || page.proposals.some(p => !uuid(p.proposal_id) || !uuid(p.operation_id) || !validArgs(p.arguments) || p.state !== "pending" || !instant(p.expires_at)) || (page.next_after !== undefined && !uuid(page.next_after))) throw new Error("Invalid pending proposals");
    choices = page.proposals; choicesAfter = after; choicesNext = page.next_after; renderChoices();
  }
  el.event.addEventListener("change", () => run(async e => { if (record) return; eventID = el.event.value || undefined; receipt = undefined; renderReceipt(); await loadContext(e); }));
  el.alias.addEventListener("change", () => run(async e => { if (record) return; selectedAlias = el.alias.value || undefined; await loadAlias(e); }));
  el.action.addEventListener("change", controls);
  el.lookup.addEventListener("input", () => { if (!record && !binding) { target = undefined; el.target.replaceChildren(); controls(); } });
  el["lookup-button"].addEventListener("click", () => run(async e => {
    if (record || binding) return; const value = el.lookup.value.trim();
    const query = uuid(value) ? { user_id: value.toLowerCase() } : /^[1-9][0-9]*$/.test(value) && Number.isSafeInteger(Number(value)) ? { telegram_user_id: Number(value) } : undefined;
    if (!query) throw { local: "invalid" }; await lookup(e, query);
  }));
  el.prepare.addEventListener("click", () => run(e => { const a = candidate(); if (a) return begin(e, a); }));
  el.confirm.addEventListener("click", () => run(async e => { if (proposal && Date.parse(proposal.expires_at) <= Date.now()) { el.status.textContent = w().expired; return; } if (!canConfirm()) return; persist({ ...record, phase: "apply" }); await dispatch(e); }));
  el.close.addEventListener("click", () => run(async e => { if (!record || (record.phase === "apply" && proposal?.state !== "expired")) return; removeRecord(); await loadContext(e, bindingsAfter); await loadChoices(e); el.status.textContent = w().closeNote; }));
  el.recover.addEventListener("click", () => run(recover));
  el.retry.addEventListener("click", () => run(async e => { if (record?.phase === "apply") { await recover(e); if (record?.phase === "apply") await dispatch(e); } else if (args && record?.phase === "prepare") await dispatch(e); else await recover(e); }));
  el.reload.addEventListener("click", () => run(async e => { if (record) return recover(e); await loadEvents(e, eventAfter); await loadContext(e, bindingsAfter); await loadChoices(e, choicesAfter); }));
  for (const [first, next, cursor, load] of [["events-first", "events-next", () => eventNext, loadEvents], ["bindings-first", "bindings-next", () => bindingsNext, loadContext], ["choices-first", "choices-next", () => choicesNext, loadChoices]]) {
    el[first].addEventListener("click", () => run(e => load(e))); el[next].addEventListener("click", () => run(e => load(e, cursor())));
  }
  clear();
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(locale) { language = locale === "ru" ? "ru" : "en"; if (owner) labels(); }, async start(value, locale) {
    clear(); if (!uuid(value)) return; owner = value; language = locale === "ru" ? "ru" : "en"; labels(); working = true; controls();
    const e = epoch;
    try {
      try {
        const raw = sessionStorage.getItem(key());
        if (raw) { if (raw.length > 2048) throw new Error("Oversized recovery identifiers"); const saved = JSON.parse(raw); if (!validRecord(saved)) throw new Error("Invalid recovery identifiers"); record = saved; eventID = record.event_id; }
      } catch { throw { local: "blocked" }; }
      await loadEvents(e); await loadChoices(e); if (record) await recover(e);
      if (/^#massage-admin(?:[?&]|$)/.test(window.location?.hash || "")) section.scrollIntoView?.({ block: "start" });
    } catch (error) { if (e === epoch) { if (error.local === "blocked") blocked = true; errorStatus(error); } }
    finally { if (e === epoch) { working = false; controls(); } }
  } };
};
