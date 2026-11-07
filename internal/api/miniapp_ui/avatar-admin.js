"use strict";

window.createZinusyaAvatarAdmin = env => {
  const ids = ["title", "note", "status", "delivery-label", "delivery", "inspect", "current", "action-label", "action", "reason-label", "reason", "prepare", "preview", "confirm", "close", "recover", "receipt", "pending-title", "pending", "reload", "first", "next"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("aa-" + id)]));
  const section = document.getElementById("avatar-admin");
  const words = {
    en: { title: "Avatar delivery repair", note: "Administrator tools. Check the recipient's chat before retrying an uncertain delivery. A retry can send a second copy. Dismiss closes recovery; it does not prove delivery.", "delivery-label": "Delivery UUID", inspect: "Read current delivery", "action-label": "Action", retry: "Retry delivery", dismiss: "Dismiss recovery", "reason-label": "Reason", prepare: "Review exact change", confirm: "Confirm this exact change", retryConfirm: "Retry this exact confirmation", close: "Close local preview", recover: "Check earlier request", "pending-title": "Unfinished proposals", reload: "Reload unfinished proposals", first: "First page", next: "Next page", choose: "Review proposal", current: "Current delivery", snapshot: "Delivery at preparation", historical: "Earlier operation result", version: "Version", recipient: "Recipient", expiry: "Confirm before (UTC)", pending: "Pending confirmation", expired: "The original proposal expired. It cannot authorize another change.", completed: "Operation completed. Current delivery is shown separately.", prepared: "Check the recipient, action, reason and version before confirming.", closeNote: "Closing removes only this browser preview. The server proposal remains until expiry.", unavailable: "Could not read data. Try reading again.", unknown: "The result is unknown. Check the same operation before taking another action.", storage: "Recovery identifiers could not be saved. Further changes are blocked.", blocked: "Saved recovery identifiers cannot be read. Further changes are blocked.", conflict: "The delivery or proposal changed. Check the original operation and current delivery.", empty: "No unfinished proposals on this page.", mismatch: "Current delivery differs from this proposal. Confirmation is unavailable." },
    ru: { title: "Восстановление доставки аватарки", note: "Инструменты администратора. Перед повтором неопределённой доставки проверь чат получателя. Повтор может отправить вторую копию. Закрытие восстановления не доказывает доставку.", "delivery-label": "UUID доставки", inspect: "Прочитать текущую доставку", "action-label": "Действие", retry: "Повторить доставку", dismiss: "Закрыть восстановление", "reason-label": "Причина", prepare: "Проверить точное изменение", confirm: "Подтвердить именно это изменение", retryConfirm: "Повторить именно это подтверждение", close: "Закрыть локальный предпросмотр", recover: "Проверить прежний запрос", "pending-title": "Незавершённые предложения", reload: "Обновить незавершённые предложения", first: "Первая страница", next: "Следующая страница", choose: "Проверить предложение", current: "Текущая доставка", snapshot: "Доставка при подготовке", historical: "Результат прежней операции", version: "Версия", recipient: "Получатель", expiry: "Подтвердить до (UTC)", pending: "Ожидает подтверждения", expired: "Исходное предложение истекло и не разрешает новое изменение.", completed: "Операция завершена. Текущая доставка показана отдельно.", prepared: "Проверь получателя, действие, причину и версию перед подтверждением.", closeNote: "Закрытие убирает только предпросмотр в этом браузере. Предложение на сервере остаётся до истечения срока.", unavailable: "Не удалось прочитать данные. Попробуй прочитать ещё раз.", unknown: "Результат неизвестен. Проверь ту же операцию перед следующим действием.", storage: "Не удалось сохранить идентификаторы восстановления. Новые изменения заблокированы.", blocked: "Не удалось прочитать сохранённые идентификаторы восстановления. Новые изменения заблокированы.", conflict: "Доставка или предложение изменились. Проверь исходную операцию и текущую доставку.", empty: "На этой странице нет незавершённых предложений.", mismatch: "Текущая доставка отличается от предложения. Подтверждение недоступно." }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const version = v => Number.isSafeInteger(v) && v > 0;
  const validDelivery = d => d && uuid(d.id) && uuid(d.user_id) && Number.isSafeInteger(d.telegram_user_id) && d.telegram_user_id > 0 && version(d.version) && typeof d.state === "string";
  const validArgs = a => a && uuid(a.delivery_id) && version(a.expected_version) && ["retry", "dismiss"].includes(a.action) && typeof a.reason === "string" && a.reason.trim() !== "" && [...a.reason].length <= 512;
  const validProposal = p => p && uuid(p.proposal_id) && validArgs(p.arguments) && validDelivery(p.delivery) && p.delivery.id === p.arguments.delivery_id && p.delivery.version === p.arguments.expected_version && typeof p.expires_at === "string" && /Z$/.test(p.expires_at) && Number.isFinite(Date.parse(p.expires_at));
  const validView = p => validProposal(p) && uuid(p.operation_id) && ["pending", "expired", "completed"].includes(p.state) && (p.state !== "completed" || validResult(p.result, p.arguments));
  const validResult = (r, a) => r && r.delivery_id === a.delivery_id && uuid(r.recipient_id) && r.previous_version === a.expected_version && version(r.version) && typeof r.state === "string";
  let owner, locale = "en", epoch = 0, locked = false, working = false, blocked = false, ready = false;
  let current, record, proposal, receipt, items = [], after, next;
  const w = () => words[locale];
  const key = () => "zinusya.avatar-admin." + owner;
  const node = (tag, text) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; };
  const validRecord = r => r && r.owner === owner && uuid(r.operation_id) && ["prepare", "prepared", "confirm"].includes(r.phase) && (r.proposal_id === undefined || uuid(r.proposal_id)) && (r.phase === "prepare" || uuid(r.proposal_id)) && Object.keys(r).every(k => ["owner", "operation_id", "proposal_id", "phase"].includes(k));
  // Store identifiers before a mutation. Arguments and receipts are recovered
  // through the authorized API, never from browser storage.
  function persist(r) {
    if (!validRecord(r)) throw new Error("Invalid recovery identifiers");
    try { sessionStorage.setItem(key(), JSON.stringify(r)); } catch { blocked = true; throw { local: "storage" }; }
    record = r;
  }
  function clear() {
    epoch++; owner = current = record = proposal = receipt = after = next = undefined;
    working = blocked = ready = false; items = [];
    for (const name of ["current", "preview", "receipt", "pending"]) el[name].replaceChildren();
    el.delivery.value = el.reason.value = el.status.textContent = ""; section.hidden = true; controls();
  }
  const matches = () => proposal && current && current.id === proposal.arguments.delivery_id && current.version === proposal.arguments.expected_version && ["delivery_unknown", "delivery_failed", "delivery_blocked"].includes(current.state);
  const canConfirm = () => ready && ["prepared", "confirm"].includes(record?.phase) && proposal?.state === "pending" && Date.parse(proposal.expires_at) > Date.now() && matches();
  function controls() {
    const busy = locked || working || !owner;
    for (const name of ["delivery", "inspect", "action", "reason"]) el[name].disabled = busy || blocked || !!record || !ready;
    el.prepare.disabled = busy || blocked || !!record || !ready || !current || !["delivery_unknown", "delivery_failed", "delivery_blocked"].includes(current.state) || !el.reason.value.trim() || [...el.reason.value.trim()].length > 512;
    el.confirm.textContent = w()[record?.phase === "confirm" ? "retryConfirm" : "confirm"];
    el.confirm.hidden = !proposal || proposal.state !== "pending"; el.confirm.disabled = busy || blocked || !canConfirm();
    el.recover.hidden = !record; el.recover.disabled = busy || blocked;
    el.close.hidden = !record || (record.phase === "confirm" && !["completed", "expired"].includes(proposal?.state)); el.close.disabled = busy || blocked;
    el.reload.disabled = busy || blocked;
    for (const button of el.pending.querySelectorAll("button")) button.disabled = busy || blocked || !!record;
    el.first.hidden = !after; el.next.hidden = !next;
    el.first.disabled = el.next.disabled = busy || blocked || !!record;
  }
  function deliveryText(d) { return d.id + " · " + d.state + " · " + w().version + " " + d.version + " · " + w().recipient + " " + d.telegram_user_id + " · " + d.user_id; }
  function render() {
    el.current.replaceChildren(); el.preview.replaceChildren(); el.receipt.replaceChildren(); el.pending.replaceChildren();
    if (current) el.current.append(node("h2", w().current), node("p", deliveryText(current)));
    if (proposal) {
      const a = proposal.arguments;
      el.preview.append(node("h2", w()[a.action]), node("p", w().snapshot + ": " + deliveryText(proposal.delivery)), node("p", a.reason), node("p", w().expiry + ": " + new Date(proposal.expires_at).toISOString()), node("p", w().closeNote));
      if (proposal.state === "expired" || (proposal.state === "pending" && Date.parse(proposal.expires_at) <= Date.now())) el.preview.append(node("p", w().expired));
      else if (proposal.state === "pending" && !matches()) el.preview.append(node("p", w().mismatch));
    }
    if (receipt) el.receipt.append(node("h2", w().historical), node("p", receipt.delivery_id + " · " + receipt.state + " · " + w().version + " " + receipt.version));
    for (const p of items) {
      const b = node("button", w().choose + ": " + w()[p.arguments.action] + " · " + p.delivery.telegram_user_id + " · " + p.arguments.delivery_id); b.type = "button";
      b.addEventListener("click", () => run(async e => { persist({ owner, operation_id: p.operation_id, proposal_id: p.proposal_id, phase: "prepared" }); await recover(e); })); el.pending.append(b);
    }
    if (!items.length) el.pending.append(node("p", w().empty));
    controls();
  }
  function labels() {
    for (const name of ["title", "note", "delivery-label", "inspect", "action-label", "reason-label", "prepare", "confirm", "close", "recover", "pending-title", "reload", "first", "next"]) el[name].textContent = w()[name];
    const chosen = el.action.value || "retry";
    el.action.replaceChildren(...["retry", "dismiss"].map(value => { const n = node("option", w()[value]); n.value = value; return n; })); el.action.value = chosen;
    render();
  }
  async function call(e, path, method = "GET", body) {
    try { const data = await env.request("./avatar-admin/" + path, method, body); if (e !== epoch) throw { stale: true }; return data; }
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
  async function readDelivery(e, id) {
    const d = await call(e, "deliveries/" + id); if (!validDelivery(d) || d.id !== id) throw new Error("Invalid delivery");
    current = d; el.delivery.value = id; render();
  }
  async function loadPending(e, cursor) {
    const p = await call(e, "pending" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(p.items) || p.items.length > 20 || p.items.some(v => !validView(v) || v.state !== "pending") || (p.next_cursor != null && !uuid(p.next_cursor))) throw new Error("Invalid pending proposals");
    items = p.items; after = cursor; next = p.next_cursor; ready = true; section.hidden = false; render();
  }
  async function recover(e) {
    const p = await call(e, "operations/" + record.operation_id);
    if (!validView(p) || (record.proposal_id && record.proposal_id !== p.proposal_id)) throw new Error("Invalid proposal");
    proposal = p; receipt = p.result; el.action.value = p.arguments.action; el.reason.value = p.arguments.reason;
    persist({ ...record, proposal_id: p.proposal_id, phase: p.state === "pending" && record.phase !== "confirm" ? "prepared" : record.phase });
    current = undefined; render(); await readDelivery(e, p.arguments.delivery_id);
    el.status.textContent = w()[p.state === "pending" ? (record.phase === "confirm" ? "unknown" : "prepared") : p.state];
  }
  el.inspect.addEventListener("click", () => run(async e => { const id = el.delivery.value.trim().toLowerCase(); if (!uuid(id)) return; current = undefined; render(); await readDelivery(e, id); el.status.textContent = ""; }));
  el.delivery.addEventListener("input", () => { current = undefined; render(); });
  el.reason.addEventListener("input", controls);
  el.prepare.addEventListener("click", () => run(async e => {
    if (!current || record || !ready || !["delivery_unknown", "delivery_failed", "delivery_blocked"].includes(current.state)) return;
    const arguments_ = { delivery_id: current.id, expected_version: current.version, action: el.action.value, reason: el.reason.value.trim() };
    if (!validArgs(arguments_)) return;
    persist({ owner, operation_id: crypto.randomUUID(), phase: "prepare" });
    const p = await call(e, "prepare", "POST", { operation_id: record.operation_id, arguments: arguments_ });
    if (!validProposal(p)) throw new Error("Invalid preparation");
    await recover(e);
  }));
  el.confirm.addEventListener("click", () => run(async e => {
    if (!canConfirm()) {
      if (proposal?.state === "pending" && Date.parse(proposal.expires_at) <= Date.now()) { el.status.textContent = w().expired; render(); }
      return;
    }
    persist({ ...record, phase: "confirm" });
    const r = await call(e, "confirm", "POST", { operation_id: record.operation_id, proposal_id: proposal.proposal_id, arguments: proposal.arguments, confirmed: true });
    if (!validResult(r, proposal.arguments)) throw new Error("Invalid result");
    await recover(e);
  }));
  el.recover.addEventListener("click", () => run(recover));
  el.close.addEventListener("click", () => run(async e => {
    if (!record || (record.phase === "confirm" && !["completed", "expired"].includes(proposal?.state))) return;
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
    record = proposal = receipt = current = undefined; el.reason.value = el.delivery.value = ""; el.status.textContent = w().closeNote; render(); await loadPending(e);
  }));
  el.reload.addEventListener("click", () => run(async e => { await loadPending(e, after); if (record) await recover(e); }));
  el.first.addEventListener("click", () => run(e => loadPending(e)));
  el.next.addEventListener("click", () => run(e => loadPending(e, next)));
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(value) { locale = value === "ru" ? "ru" : "en"; labels(); },
    async start(value, language) {
      clear(); if (!uuid(value)) return; owner = value; locale = language === "ru" ? "ru" : "en"; labels(); const e = epoch;
      try {
        let raw; try { raw = sessionStorage.getItem(key()); } catch { throw { local: "blocked" }; }
        if (raw) { if (raw.length > 1024) throw { local: "blocked" }; try { record = JSON.parse(raw); } catch { throw { local: "blocked" }; } if (!validRecord(record)) throw { local: "blocked" }; }
        await loadPending(e); if (record) await recover(e);
      } catch (error) { if (e === epoch) { if (error.local === "blocked") blocked = true; errorStatus(error); } }
      finally { if (e === epoch) controls(); }
    }
  };
};
