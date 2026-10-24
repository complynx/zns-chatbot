"use strict";

window.createZinusyaPairs = function (env) {
  const ids = ["pairs", "title", "note", "status", "code-label", "code", "event-label", "event", "events-next", "events-first", "context", "target-label", "target", "invite", "direction", "incoming", "outgoing", "refresh", "next", "first", "list", "recover", "retry", "discard", "receipt", "owned-title", "owned-note", "owned-list", "owned-first", "owned-next"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById(id === "pairs" ? id : "pairs-" + id)]));
  const words = {
    en: { "owned-title": "Current pairs", "owned-note": "Leaving a pair does not cancel passes or change payments.", "owned-first": "Refresh current pairs", "owned-next": "Next pairs", ownedEmpty: "No current pairs on this page.", waitlisted: "Waiting for assignment", assigned: "Passes assigned", dissolved: "Pair ended", dissolve: "Leave this pair", dissolveConsent: "I want to leave this pair; passes and payments stay unchanged.", pairChanged: "The pair changed. Review current pairs before trying again.", title: "Pair invitations", note: "Inviting holds your place in the singles queue until the invitation expires or is closed. The other person must explicitly accept. Accepted invitations are history, not proof of a currently active pair or assigned passes.", "code-label": "My partner code (share with your partner)", "event-label": "Event", "target-label": "Partner code", invite: "Send invitation", incoming: "Received", outgoing: "Sent", refresh: "Refresh invitations", next: "Next invitations", first: "First invitations", "events-next": "More events", "events-first": "First events", recover: "Check earlier request", retry: "Retry the same request", discard: "Close rejected request", choose: "Choose an event", ready: "Current invitations loaded", empty: "No invitations on this page.", unavailable: "Could not finish. Refresh or check the earlier request.", invalid: "Check the event, partner code and selected role.", storage: "Recovery storage is unavailable. No new request was sent.", blocked: "Recovery data is invalid. New actions are blocked on this page.", unknown: "The earlier request outcome is unknown. Check it or retry the same request before starting another.", rejected: "The earlier request was rejected. Check its receipt or close it before starting another.", receipt: "Historical request result: ", pending: "Pending", accepted: "Accepted", declined: "Declined", revoked: "Revoked", expired: "Expired", accept: "Accept invitation", decline: "Decline invitation", revoke: "Revoke invitation", consent: "I agree to join as ", leader: "Leader", follower: "Follower", until: "Invitation deadline: ", role: "Your role: ", context: "You need a current waiting-list registration. Check /register or your registration details if this event is unavailable.", from: "From: ", to: "To partner code: ", select: "Select", changed: "Your registration changed. Review the newly loaded role and deadline, then send again." },
    ru: { "owned-title": "Текущие пары", "owned-note": "Выход из пары не отменяет пассы и не меняет платежи.", "owned-first": "Обновить текущие пары", "owned-next": "Следующие пары", ownedEmpty: "На этой странице действующих пар нет.", waitlisted: "В очереди на назначение", assigned: "Пассы назначены", dissolved: "Пара завершена", dissolve: "Выйти из этой пары", dissolveConsent: "Хочу выйти из этой пары; пассы и платежи остаются без изменений.", pairChanged: "Пара изменилась. Проверь текущие пары перед повтором.", title: "Приглашения в пару", note: "Приглашение приостанавливает твою одиночную очередь до истечения срока или закрытия приглашения. Другой человек должен явно согласиться. Принятые приглашения — история, а не подтверждение действующей пары или назначенных пассов.", "code-label": "Мой код партнёра (передай партнёру)", "event-label": "Эвент", "target-label": "Код партнёра", invite: "Отправить приглашение", incoming: "Полученные", outgoing: "Отправленные", refresh: "Обновить приглашения", next: "Следующие приглашения", first: "Первые приглашения", "events-next": "Ещё эвенты", "events-first": "Первые эвенты", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", discard: "Закрыть отклонённый запрос", choose: "Выбери эвент", ready: "Текущие приглашения загружены", empty: "На этой странице приглашений нет.", unavailable: "Не удалось завершить. Обнови список или проверь прежний запрос.", invalid: "Проверь эвент, код партнёра и выбранную роль.", storage: "Хранилище восстановления недоступно. Новый запрос не отправлен.", blocked: "Данные восстановления повреждены. Новые действия на этой странице заблокированы.", unknown: "Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос перед новым действием.", rejected: "Прежний запрос отклонён. Проверь квитанцию или закрой его перед новым действием.", receipt: "Исторический результат запроса: ", pending: "Ожидает ответа", accepted: "Принято", declined: "Отклонено", revoked: "Отозвано", expired: "Срок истёк", accept: "Принять приглашение", decline: "Отклонить приглашение", revoke: "Отозвать приглашение", consent: "Согласен вступить в пару как ", leader: "Лидер", follower: "Фолловер", until: "Срок приглашения: ", role: "Твоя роль: ", context: "Нужна действующая регистрация в очереди ожидания. Если эвент недоступен, проверь /register и данные для регистрации.", from: "От: ", to: "Коду партнёра: ", select: "Выбрать", changed: "Регистрация изменилась. Проверь обновлённую роль и срок, затем отправь заново." }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const kinds = ["invite", "accept", "decline", "revoke", "dissolve"], states = ["pending", "accepted", "declined", "revoked", "expired"];
  const validInvitation = i => i && uuid(i.id) && uuid(i.event_id) && uuid(i.target_user_id) && Number.isSafeInteger(i.version) && i.version > 0 && states.includes(i.state) && Number.isFinite(Date.parse(i.expires_at));
  let owner, language = "en", epoch = 0, locked = false, working = false, blocked = false, pending, rejected = false;
  let context, rows = [], ownedRows = [], ownedNext, next, eventNext, statusKey = "ready", receiptState;
  const w = () => words[language];
  const key = () => "zinusya-pair-pending-v1:" + owner;
  const copy = v => JSON.parse(JSON.stringify(v));
  function status(k) { statusKey = k; el.status.textContent = w()[k]; }
  function labels() {
    for (const id of ["title", "note", "code-label", "event-label", "target-label", "invite", "incoming", "outgoing", "refresh", "next", "first", "events-next", "events-first", "recover", "retry", "discard", "owned-title", "owned-note", "owned-first", "owned-next"]) el[id].textContent = w()[id];
    if (el.event.options?.length) el.event.options[0].textContent = w().choose;
    el.receipt.textContent = receiptState ? w().receipt + w()[receiptState] : "";
    paintContext(); paintRows(); paintOwned(); status(statusKey);
  }
  function controls() {
    const busy = locked || working || !owner;
    for (const id of ["event", "events-next", "events-first", "target", "direction", "refresh", "next", "first", "owned-first", "owned-next"]) el[id].disabled = busy || blocked || !!pending;
    el.invite.disabled = busy || blocked || !!pending || !context;
    el.recover.hidden = el.retry.hidden = !pending; el.discard.hidden = !pending || !rejected;
    el.recover.disabled = el.retry.disabled = el.discard.disabled = busy || blocked;
    for (const button of [...el.list.querySelectorAll("button,input"), ...el["owned-list"].querySelectorAll("button,input")]) button.disabled = busy || blocked || !!pending;
    el["owned-next"].hidden = !ownedNext; el.next.hidden = !next; el["events-next"].hidden = !eventNext;
  }
  function clear() {
    epoch++; owner = pending = context = next = eventNext = receiptState = ownedNext = undefined;
    rows = []; ownedRows = []; el["owned-list"].replaceChildren(); working = blocked = rejected = false;
    el.pairs.hidden = true; el.code.textContent = el.receipt.textContent = el.context.textContent = el.target.value = "";
    el.event.replaceChildren(); el.list.replaceChildren(); el.direction.value = "incoming"; controls();
  }
  async function call(e, path, method, body) {
    try { const value = await env.request(path, method, body); if (e !== epoch) throw { stale: true }; return value; }
    catch (error) { if (e !== epoch) throw { stale: true }; if (error.status === 401) env.denied(); throw error; }
  }
  function onError(error) { if (!error.stale) status(error.local || (pending ? rejected ? "rejected" : "unknown" : "unavailable")); }
  async function run(fn) {
    if (locked || working || blocked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await fn(e); } catch (error) { if (e === epoch) onError(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  function validPending(p) {
    if (!p || p.owner !== owner || !kinds.includes(p.kind) || !uuid(p.body?.operation_id)) return false;
    const a = p.body.arguments;
    if (!a || !uuid(a.event_id) || !Number.isSafeInteger(a.expected_version) || a.expected_version < 1) return false;
    if (p.kind === "dissolve") return uuid(a.pair_id);
    return p.kind === "invite" ? uuid(a.target_user_id) && a.target_user_id !== owner : uuid(a.invitation_id) && (p.kind !== "accept" || ["leader", "follower"].includes(a.role));
  }
  function remember(kind, args) {
    const p = { owner, kind, body: { operation_id: crypto.randomUUID(), arguments: args } };
    try { sessionStorage.setItem(key(), JSON.stringify(p)); } catch { throw { local: "storage" }; }
    pending = p; rejected = false; controls();
  }
  function complete(value) {
    if (pending.kind === "dissolve") {
      if (!validPair(value) || value.state !== "dissolved" || value.id !== pending.body.arguments.pair_id || value.event_id !== pending.body.arguments.event_id) throw { local: "unknown" };
    } else {
    if (!validInvitation(value?.invitation) || value.invitation.event_id !== pending.body.arguments.event_id ||
        (pending.kind !== "invite" && value.invitation.id !== pending.body.arguments.invitation_id) ||
        (pending.kind === "invite" && value.invitation.target_user_id !== pending.body.arguments.target_user_id)) throw { local: "unknown" };
    }
    try { sessionStorage.removeItem(key()); } catch { throw { local: "unknown" }; }
    receiptState = pending.kind === "dissolve" ? value.state : value.invitation.state; pending = undefined; rejected = false; context = undefined;
    el.receipt.textContent = w().receipt + w()[receiptState]; paintContext(); status("ready");
  }
  async function dispatch(e) {
    if (!pending) return;
    rejected = false;
    try { complete(await call(e, "./pairs/" + pending.kind, "POST", copy(pending.body))); }
    catch (error) { if ([400, 403, 404, 409].includes(error.status)) rejected = true; throw error; }
    await refreshAll(e);
  }
  async function recover(e) {
    if (!pending) return;
    try { complete(await call(e, "./pairs/operations/" + encodeURIComponent(pending.body.operation_id) + "?kind=" + pending.kind)); }
    catch (error) { if (error.status === 404) { status(rejected ? "rejected" : "unknown"); return; } throw error; }
    await refreshAll(e);
  }
  function time(value, zone) { return new Intl.DateTimeFormat(language, { timeZone: zone, year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", timeZoneName: "shortOffset" }).format(new Date(value)) + " (" + zone + ")"; }
  function paintContext() {
    el.context.textContent = context ? context.event_name + " · " + w().role + w()[context.role] + " · " + w().until + time(context.expires_at, context.timezone) : w().context;
  }
  async function loadContext(e) {
    context = undefined; paintContext(); controls();
    if (!el.event.value) return;
    const value = await call(e, "./pairs/events/" + el.event.value + "/context");
    if (value?.event_id !== el.event.value || !["leader", "follower"].includes(value.role) || !Number.isSafeInteger(value.expected_version) || value.expected_version < 1 || !Number.isFinite(Date.parse(value.expires_at))) throw { local: "invalid" };
    time(value.expires_at, value.timezone); context = value; paintContext();
  }
  async function events(e, after) {
    context = undefined; paintContext();
    const page = await call(e, "./pairs/events" + (after ? "?after=" + after : ""));
    if (!Array.isArray(page?.events) || page.events.length > 20 || !page.events.every(v => uuid(v.id) && typeof v.name === "string") || (page.next && !uuid(page.next))) throw { local: "invalid" };
    const placeholder = document.createElement("option"); placeholder.value = ""; placeholder.textContent = w().choose;
    el.event.replaceChildren(placeholder, ...page.events.map(v => { const n = document.createElement("option"); n.value = v.id; n.textContent = v.name; return n; }));
    el.event.value = ""; eventNext = page.next;
  }
  async function list(e, after) {
    rows = []; el.list.replaceChildren(); next = undefined;
    const page = await call(e, "./pairs/invitations?state=all&direction=" + el.direction.value + (after ? "&after=" + after : ""));
    if (!Array.isArray(page?.invitations) || page.invitations.length > 20 || !page.invitations.every(v => validInvitation(v.invitation) && ["leader", "follower"].includes(v.required_role) && typeof v.timezone === "string") || (page.next && !uuid(page.next))) throw { local: "invalid" };
    for (const row of page.invitations) time(row.invitation.expires_at, row.timezone);
    rows = page.invitations; next = page.next; paintRows(); status(pending ? rejected ? "rejected" : "unknown" : "ready");
  }
  const validPair = p => p && uuid(p.id) && uuid(p.event_id) && Number.isSafeInteger(p.version) && p.version > 0 && ["waitlisted", "assigned", "dissolved"].includes(p.state);
  async function loadOwned(e, after) {
    ownedRows = []; ownedNext = undefined; el["owned-list"].replaceChildren();
    const page = await call(e, "./pairs/my" + (after ? "?after=" + after : ""));
    if (!Array.isArray(page?.pairs) || page.pairs.length > 20 || !page.pairs.every(row => validPair(row.pair) && row.pair.state !== "dissolved" && typeof row.event_name === "string" && Number.isFinite(Date.parse(row.starts_at)) && Number.isFinite(Date.parse(row.ends_at))) || (page.next && !uuid(page.next))) throw { local: "invalid" };
    for (const row of page.pairs) time(row.starts_at, row.timezone);
    ownedRows = page.pairs; ownedNext = page.next; paintOwned();
  }
  async function refreshAll(e) { await loadOwned(e); await list(e); }
  function paintOwned() {
    el["owned-list"].replaceChildren();
    if (!ownedRows.length) { el["owned-list"].textContent = w().ownedEmpty; return; }
    for (const row of ownedRows) {
      const item = document.createElement("div"), text = document.createElement("p"), consent = document.createElement("input"), label = document.createElement("label"), button = document.createElement("button");
      text.textContent = row.event_name + " · " + w()[row.pair.state] + " · " + time(row.starts_at, row.timezone);
      consent.type = "checkbox"; label.append(consent, document.createTextNode(w().dissolveConsent)); button.type = "button"; button.textContent = w().dissolve;
      button.addEventListener("click", () => run(async e => {
        if (pending || !consent.checked) throw { local: "invalid" };
        const current = await call(e, "./pairs/my?event_id=" + row.pair.event_id);
        if (!Array.isArray(current?.pairs) || !current.pairs.some(v => validPair(v.pair) && v.pair.id === row.pair.id && v.pair.version === row.pair.version && v.pair.state === row.pair.state)) { await loadOwned(e); status("pairChanged"); return; }
        remember("dissolve", { event_id: row.pair.event_id, pair_id: row.pair.id, expected_version: row.pair.version }); await dispatch(e);
      }));
      item.append(text, label, button); el["owned-list"].append(item);
    }
  }
  function paintRows() {
    el.list.replaceChildren();
    if (!rows.length) { el.list.textContent = w().empty; return; }
    for (const row of rows) {
      const i = row.invitation, incoming = el.direction.value === "incoming", item = document.createElement("div"), text = document.createElement("p");
      text.textContent = row.event_name + " · " + w()[i.state] + " · " + (incoming ? w().from + row.inviter_name : w().to + i.target_user_id) + " · " + w().until + time(i.expires_at, row.timezone);
      item.append(text);
      if (i.state === "pending") {
        const consent = document.createElement("input"); consent.type = "checkbox";
        if (incoming) { const label = document.createElement("label"); label.append(consent, document.createTextNode(w().consent + w()[row.required_role])); item.append(label); }
        for (const kind of incoming ? ["accept", "decline"] : ["revoke"]) {
          const button = document.createElement("button"); button.type = "button"; button.textContent = w()[kind];
          button.addEventListener("click", () => run(async e => {
            if (pending || (kind === "accept" && !consent.checked)) throw { local: "invalid" };
            const args = { event_id: i.event_id, invitation_id: i.id, expected_version: i.version };
            if (kind === "accept") args.role = row.required_role;
            remember(kind, args); await dispatch(e);
          })); item.append(button);
        }
      }
      el.list.append(item);
    }
  }
  el.invite.addEventListener("click", () => run(async e => {
    if (pending || !context || !uuid(el.target.value.trim()) || el.target.value.trim() === owner) throw { local: "invalid" };
    const old = context; await loadContext(e);
    if (context.expected_version !== old.expected_version || context.role !== old.role || context.expires_at !== old.expires_at) { status("changed"); return; }
    remember("invite", { event_id: context.event_id, target_user_id: el.target.value.trim(), expected_version: context.expected_version }); await dispatch(e);
  }));
  el.event.addEventListener("change", () => run(loadContext));
  el.direction.addEventListener("change", () => run(e => list(e)));
  el.refresh.addEventListener("click", () => run(refreshAll));
  el["owned-first"].addEventListener("click", () => run(e => loadOwned(e)));
  el["owned-next"].addEventListener("click", () => run(e => loadOwned(e, ownedNext)));
  el.first.addEventListener("click", () => run(e => list(e)));
  el.next.addEventListener("click", () => run(e => list(e, next)));
  el["events-first"].addEventListener("click", () => run(e => events(e)));
  el["events-next"].addEventListener("click", () => run(e => events(e, eventNext)));
  el.recover.addEventListener("click", () => run(recover));
  el.retry.addEventListener("click", () => run(dispatch));
  el.discard.addEventListener("click", () => run(async e => {
    if (!pending || !rejected) return;
    await recover(e); if (!pending) return;
    try { sessionStorage.removeItem(key()); } catch { throw { local: "unknown" }; }
    pending = undefined; rejected = false; context = undefined; paintContext(); await list(e);
  }));
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(locale) { language = locale === "ru" ? "ru" : "en"; labels(); controls(); },
    async start(id, locale) {
      clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); el.code.textContent = owner; el.pairs.hidden = false;
      const e = epoch;
      try { const raw = sessionStorage.getItem(key()); if (raw) { if (raw.length > 2048) throw new Error("Oversized recovery"); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error("Invalid recovery"); } }
      catch { blocked = true; status("blocked"); controls(); return; }
      try { await events(e); await loadOwned(e); if (pending) await recover(e); else await list(e); } catch (error) { if (e === epoch) onError(error); } controls();
    }
  };
};
