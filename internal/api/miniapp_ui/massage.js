"use strict";

window.createZinusyaMassage = env => {
  const ids = ["title", "note", "status", "event-label", "event", "events-first", "events-next", "choices", "choices-first", "choices-next", "selection", "window-label", "window", "specialist-label", "specialist", "service-label", "service", "range", "range-prev", "range-next", "slots", "slots-first", "slots-next", "reload", "history", "history-first", "history-next", "preview", "receipt", "confirm", "discard", "recover", "retry"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("massage-" + id)]));
  el.section = document.getElementById("massage");
  const words = {
    en: { choices: "Unfinished choices", "choices-first": "First choices", "choices-next": "Next choices", choices_none: "No unfinished choices on this page.", continue_book: "Review saved booking choice", continue_cancel: "Review saved cancellation", choice_changed: "The saved choice could not be revalidated. Discard this preview and reload, or retry the same request.", title: "Massage", note: "Check the service and time before confirming. A preview does not reserve a slot or confirm payment.", event: "Event", window: "Party", specialist: "Specialist", service: "Service", choose: "Choose an event", empty: "No massage events or booking history.", "events-first": "First events", "events-next": "Next events", "range-prev": "Earlier times", "range-next": "Later times", "slots-first": "First slots", "slots-next": "Next slots", reload: "Reload current state", "history-first": "First bookings", "history-next": "Next bookings", confirm: "Confirm this action", discard: "Discard preview", recover: "Check earlier request", retry: "Retry the same request", prepare: "Review this slot", cancel: "Review cancellation", history: "My bookings", none: "No bookings on this page.", unavailable: "Could not load current data. Retry.", disabled: "Time selection is unavailable in this form. Your booking history remains below.", unknown: "The earlier request has no confirmed result. Check it or retry the same request before starting another action.", storage: "Browser session storage is required to recover this request. Nothing new was sent.", blocked: "The saved request cannot be read. New actions are blocked.", prepared: "Check this exact preview, then confirm or discard it.", expired: "The preview has expired. Discard it and choose again.", rejected: "The request was rejected and has no operation receipt. Discard the preview and reload before choosing again.", booked: "Booked", cancelled: "Cancelled", completed: "Earlier action completed; current booking history is below.", occupied: "Occupied", party_limit: "Party booking limit reached", price: "Price", start: "Start", end: "Treatment ends", occupancy: "Occupied until", expires: "Confirm before", cancellation: "Cancel this booking", zone: "Event timezone" },
    ru: { choices: "Незавершённые действия", "choices-first": "Первые действия", "choices-next": "Следующие действия", choices_none: "На этой странице нет незавершённых действий.", continue_book: "Проверить сохранённый выбор", continue_cancel: "Проверить сохранённую отмену", choice_changed: "Не удалось заново проверить сохранённый выбор. Отбрось предпросмотр и обнови данные или повтори тот же запрос.", title: "Массаж", note: "Проверь услугу и время перед подтверждением. Предпросмотр не занимает место и не подтверждает оплату.", event: "Эвент", window: "Вечеринка", specialist: "Специалист", service: "Услуга", choose: "Выбери эвент", empty: "Нет доступных эвентов или истории записей.", "events-first": "Первые эвенты", "events-next": "Следующие эвенты", "range-prev": "Раньше", "range-next": "Позже", "slots-first": "Первые места", "slots-next": "Следующие места", reload: "Обновить состояние", "history-first": "Первые записи", "history-next": "Следующие записи", confirm: "Подтвердить это действие", discard: "Отбросить предпросмотр", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", prepare: "Проверить это время", cancel: "Проверить отмену", history: "Мои записи", none: "На этой странице записей нет.", unavailable: "Не удалось загрузить данные. Повтори запрос.", disabled: "Выбор нового времени здесь недоступен. История твоих записей остаётся ниже.", unknown: "Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос перед новым действием.", storage: "Для восстановления запроса нужно хранилище сессии браузера. Новый запрос не отправлен.", blocked: "Не удалось прочитать сохранённый запрос. Новые действия заблокированы.", prepared: "Проверь точный предпросмотр, затем подтверди или отбрось его.", expired: "Срок подтверждения истёк. Отбрось предпросмотр и выбери заново.", rejected: "Запрос отклонён, квитанции операции нет. Отбрось предпросмотр и обнови состояние перед новым выбором.", booked: "Записано", cancelled: "Отменено", completed: "Прежнее действие выполнено; текущее состояние записей показано ниже.", occupied: "Занято", party_limit: "Достигнут лимит записей на вечеринку", price: "Цена", start: "Начало", end: "Окончание массажа", occupancy: "Место занято до", expires: "Подтвердить до", cancellation: "Отменить эту запись", zone: "Таймзона эвента" }
  };
  Object.assign(words.en, { self_time: "My specialist time", self_note: "You are both the client and the specialist for this reservation.", continue_instant: "Review saved self reservation" });
  Object.assign(words.ru, { self_time: "Моё время специалиста", self_note: "В этой записи ты одновременно участник и специалист.", continue_instant: "Проверить сохранённый резерв своего времени" });
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const version = v => Number.isSafeInteger(v) && v > 0;
  const identifier = v => typeof v === "string" && /^[a-z][a-z0-9_-]{0,31}$/.test(v);
  const instant = v => typeof v === "string" && Number.isFinite(Date.parse(v));
  const day = 86400000;
  let owner, language = "en", epoch = 0, locked = false, working = false, blocked = false;
  let eventID, eventsAfter, eventsNext, schedule, rangeIndex = 0, slotAfter, slotNext, historyAfter, historyNext, choicesAfter, choicesNext;
  let pending, prepared, slots, history = [], choices = [], actionButtons = [];
  const w = () => words[language];
  const key = () => "zinusya-massage-operation-v1:" + owner;
  const path = () => "./massage/" + eventID;
  const node = (tag, text) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; };
  const label = v => v?.[language] || v?.en || "";
  function time(value, timezone) {
    return new Intl.DateTimeFormat(language, { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", timeZoneName: "longOffset" }).format(new Date(value));
  }
  function money(terms) {
    if (!Number.isSafeInteger(terms.price_minor) || terms.price_minor < 0 || !Number.isInteger(terms.currency_scale) || terms.currency_scale < 0 || terms.currency_scale > 4 || !/^[A-Z]{3}$/.test(terms.currency)) throw new Error("Invalid price");
    const factor = 10 ** terms.currency_scale;
    return Math.floor(terms.price_minor / factor) + (terms.currency_scale ? "." + String(terms.price_minor % factor).padStart(terms.currency_scale, "0") : "") + " " + terms.currency;
  }
  function validPending(p) {
    if (!p || p.owner !== owner || !["book", "cancel", "instant"].includes(p.kind) || !["prepare", "prepared", "apply", "rejected"].includes(p.phase) || !p.body || !uuid(p.body.operation_id)) return false;
    const a = p.body.arguments;
    if (!a || !uuid(a.event_id)) return false;
    if (p.phase !== "prepare" && (!p.display || typeof p.display.event_name !== "string" || !p.display.event_name.length || p.display.event_name.length > 2048 || typeof p.display.timezone !== "string" || !p.display.timezone.length || p.display.timezone.length > 128)) return false;
    if (["apply", "rejected"].includes(p.phase) && (!uuid(p.body.proposal_id) || p.body.confirmed !== true)) return false;
    if (!["apply", "rejected"].includes(p.phase) && ((p.body.proposal_id !== undefined && !uuid(p.body.proposal_id)) || p.body.confirmed !== undefined)) return false;
    return p.kind === "cancel" ? uuid(a.booking_id) && version(a.expected_version) : version(a.expected_event_version) && version(a.expected_schedule_version) && a.selection && identifier(a.selection.window_id) && identifier(a.selection.specialist_id) && identifier(a.selection.service_id) && (p.kind === "instant" ? a.selection.starts_at === undefined : instant(a.selection.starts_at));
  }
  function persist(value) {
    try { sessionStorage.setItem(key(), JSON.stringify(value)); } catch { throw { local: "storage" }; }
    pending = value;
  }
  function removePending() {
    try { sessionStorage.removeItem(key()); } catch { throw { local: "storage" }; }
    pending = prepared = undefined; el.preview.replaceChildren();
  }
  function controls() {
    const busy = locked || working || !owner;
    const editing = busy || blocked || !!pending;
    el.selection.hidden = !schedule;
    for (const id of ["event", "events-first", "events-next", "window", "specialist", "service", "range-prev", "range-next", "slots-first", "slots-next", "history-first", "history-next", "choices-first", "choices-next"]) el[id].disabled = editing;
    for (const button of actionButtons) button.disabled = editing || button.unavailable;
    el.reload.disabled = busy || blocked;
    el.confirm.hidden = !prepared;
    el.confirm.disabled = busy || blocked || pending?.phase !== "prepared" || !prepared || Date.parse(prepared.expires_at) <= Date.now();
    el.discard.hidden = !pending || pending.phase === "apply";
    el.discard.disabled = busy || blocked;
    el.recover.hidden = !pending; el.retry.hidden = !pending || pending.phase === "rejected";
    el.recover.disabled = el.retry.disabled = busy || blocked;
  }
  function labels() {
    for (const id of ["title", "note", "events-first", "events-next", "range-prev", "range-next", "slots-first", "slots-next", "reload", "history-first", "history-next", "choices-first", "choices-next", "confirm", "discard", "recover", "retry"]) el[id].textContent = w()[id];
    for (const id of ["event", "window", "specialist", "service"]) el[id + "-label"].textContent = w()[id];
  }
  function clear() {
    epoch++; owner = undefined; working = blocked = false;
    eventID = eventsAfter = eventsNext = schedule = slotAfter = slotNext = historyAfter = historyNext = choicesAfter = choicesNext = pending = prepared = slots = undefined;
    rangeIndex = 0; history = []; choices = []; actionButtons = [];
    for (const id of ["event", "window", "specialist", "service", "slots", "history", "choices", "preview", "receipt"]) el[id].replaceChildren();
    el.status.textContent = el.range.textContent = ""; el.section.hidden = true; renderChoices(); controls();
  }
  async function call(e, url, method = "GET", body) {
    try { const result = await env.request(url, method, body); if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.stale) return;
    if (error.status === 401) { env.denied(); return; }
    el.status.textContent = w()[error.local || (pending ? "unknown" : "unavailable")];
  }
  async function run(action) {
    if (working || locked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  async function loadEvents(e, after) {
    const page = await call(e, "./massage/events" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.events) || page.events.length > 20 || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid event page");
    const retained = Array.from(el.event.options).find(option => option.value === eventID);
    const options = [node("option", w().choose)]; options[0].value = "";
    for (const event of page.events) { if (!uuid(event.id)) throw new Error("Invalid event"); const option = node("option", event.name + " · " + event.timezone); option.value = event.id; option.dataset.timezone = event.timezone; options.push(option); }
    if (retained && !page.events.some(event => event.id === eventID)) options.push(retained);
    el.event.replaceChildren(...options); el.event.value = eventID || "";
    if (pending?.display) showHistoricalEvent({ event_name: pending.display.event_name, terms: { timezone: pending.display.timezone } });
    eventsAfter = after; eventsNext = page.next;
    el["events-first"].hidden = !eventsAfter; el["events-next"].hidden = !eventsNext;
    if (!eventID) el.status.textContent = page.events.length ? w().choose : w().empty;
  }
  function choose(select, rows, current) {
    select.replaceChildren(...rows.map(row => { const option = node("option", label(row.name)); option.value = row.id; return option; }));
    select.value = rows.some(row => row.id === current) ? current : rows[0]?.id || "";
  }
  function renderSelectors() {
    choose(el.window, schedule?.document.windows || [], el.window.value);
    renderSpecialists();
  }
  function renderSpecialists() {
    choose(el.specialist, schedule?.document.specialists.filter(s => s.work_spans.some(span => span.window_id === el.window.value)) || [], el.specialist.value);
    const specialist = schedule?.document.specialists.find(s => s.id === el.specialist.value);
    choose(el.service, schedule?.document.services.filter(s => specialist?.service_ids.includes(s.id) && [1, 2, 3, 5].includes(s.blocks)) || [], el.service.value);
  }
  function range() {
    const window = schedule?.document.windows.find(v => v.id === el.window.value);
    if (!window) return undefined;
    const from = Date.parse(window.starts_at) + rangeIndex * day;
    return { from: new Date(from).toISOString(), until: new Date(Math.min(from + day, Date.parse(window.ends_at))).toISOString(), window };
  }
  async function loadSlots(e, after) {
    slots = undefined; slotAfter = after; slotNext = undefined; renderSlots();
    const span = range();
    if (!span || !el.service.value || !el.specialist.value) return;
    slots = await call(e, path() + "/availability", "POST", { event_id: eventID, expected_event_version: schedule.event_version, expected_schedule_version: schedule.version,
      query: { window_id: el.window.value, specialist_id: el.specialist.value, service_id: el.service.value, from: span.from, until: span.until, limit: 40, ...(after ? { after_start: after } : {}) } });
    if (slots.event_id !== eventID || !Array.isArray(slots.candidates) || slots.candidates.length > 40 || (slots.next_after !== undefined && !instant(slots.next_after))) throw new Error("Invalid availability page");
    slotNext = slots.next_after; renderSlots();
  }
  function renderSlots() {
    el.slots.replaceChildren(); actionButtons = actionButtons.filter(b => b.cancelBooking || b.savedChoice);
    const span = range();
    el.range.textContent = span ? time(span.from, schedule.timezone) + " — " + time(span.until, schedule.timezone) : "";
    el["range-prev"].hidden = !span || rangeIndex === 0;
    el["range-next"].hidden = !span || Date.parse(span.until) >= Date.parse(span.window.ends_at);
    el["slots-first"].hidden = !slotAfter; el["slots-next"].hidden = !slotNext;
    if (!slots) return;
    el.slots.append(node("p", money(slots)));
    for (const slot of slots.candidates) {
      const article = node("article");
      article.append(node("p", time(slot.starts_at, slots.timezone) + " — " + time(slot.treatment_ends_at, slots.timezone)));
      const button = node("button", slot.available ? w().prepare : w()[slot.unavailable_reason] || w().occupied); button.type = "button"; button.unavailable = !slot.available;
      button.addEventListener("click", () => run(e => begin(e, "book", { event_id: eventID, expected_event_version: schedule.event_version, expected_schedule_version: schedule.version, selection: { window_id: slots.window_id, specialist_id: slots.specialist_id, service_id: slots.service_id, starts_at: slot.starts_at } })));
      article.append(button); actionButtons.push(button); el.slots.append(article);
    }
    controls();
  }
  function renderTerms(container, snapshot) {
    const t = snapshot.terms;
    container.append(node("p", snapshot.event_name), node("p", label(t.window_name)), node("p", label(t.specialist_name) + " · " + label(t.service_name)), node("p", w().price + ": " + money(t)), node("p", w().start + ": " + time(t.starts_at, t.timezone)), node("p", w().end + ": " + time(t.treatment_ends_at, t.timezone)), node("p", w().occupancy + ": " + time(t.occupancy_ends_at, t.timezone)), node("p", w().zone + ": " + t.timezone));
  }
  function showHistoricalEvent(snapshot) {
    if (!eventID || !snapshot || Array.from(el.event.options).some(option => option.value === eventID)) return;
    const option = node("option", snapshot.event_name + " · " + snapshot.terms.timezone);
    option.value = eventID; option.dataset.timezone = snapshot.terms.timezone; el.event.append(option); el.event.value = eventID;
  }
  function renderPreparation() {
    el.preview.replaceChildren();
    if (!prepared) return;
    if (pending.kind === "cancel") el.preview.append(node("h2", w().cancellation));
    if (pending.kind === "instant") el.preview.append(node("h2", w().self_time), node("p", w().self_note));
    const snapshot = pending.kind !== "cancel" ? prepared.snapshot : prepared.booking.snapshot;
    showHistoricalEvent(snapshot); renderTerms(el.preview, snapshot); el.preview.append(node("p", w().expires + ": " + time(prepared.expires_at, snapshot.terms.timezone)));
  }
  async function loadHistory(e, after) {
    const page = await call(e, path() + "/bookings" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.bookings) || page.bookings.length > 20 || (page.next_after !== undefined && !uuid(page.next_after))) throw new Error("Invalid booking history");
    history = page.bookings; historyAfter = after; historyNext = page.next_after; renderHistory();
  }
  function renderHistory() {
    el.history.replaceChildren(node("h2", w().history)); actionButtons = actionButtons.filter(b => !b.cancelBooking);
    el["history-first"].hidden = !historyAfter; el["history-next"].hidden = !historyNext;
    showHistoricalEvent(history[0]?.snapshot);
    if (!history.length) el.history.append(node("p", w().none));
    for (const booking of history) {
      if (!uuid(booking.id) || !version(booking.version) || !["booked", "cancelled"].includes(booking.state)) throw new Error("Invalid booking");
      const article = node("article"); article.append(node("p", w()[booking.state]));
      if (booking.snapshot?.purpose === "staff_self") article.append(node("p", w().self_time));
      renderTerms(article, booking.snapshot);
      if (booking.state === "booked") {
        const button = node("button", w().cancel); button.type = "button"; button.cancelBooking = true;
        button.addEventListener("click", () => run(e => begin(e, "cancel", { event_id: eventID, booking_id: booking.id, expected_version: booking.version })));
        actionButtons.push(button); article.append(button);
      }
      el.history.append(article);
    }
    controls();
  }
  async function loadChoices(e, after) {
    const page = await call(e, path() + "/pending" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.proposals) || page.proposals.length > 20 || (page.next_after !== undefined && !uuid(page.next_after))) throw new Error("Invalid unfinished choices");
    for (const choice of page.proposals) {
      const args = choice.kind === "instant" ? choice.instant : choice.kind === "book" ? choice.booking : choice.cancellation;
      if (!uuid(choice.proposal_id) || choice.event_id !== eventID || !instant(choice.expires_at) || args?.event_id !== eventID || !validPending({ owner, kind: choice.kind, phase: "prepare", body: { operation_id: choice.proposal_id, arguments: args } })) throw new Error("Invalid saved choice");
    }
    choices = page.proposals; choicesAfter = after; choicesNext = page.next_after; renderChoices();
  }
  function renderChoices() {
    el.choices.replaceChildren(); actionButtons = actionButtons.filter(button => !button.savedChoice);
    el["choices-first"].hidden = !choicesAfter; el["choices-next"].hidden = !choicesNext;
    if (!eventID) return;
    el.choices.append(node("h2", w().choices));
    if (!choices.length) el.choices.append(node("p", w().choices_none));
    const timezone = Array.from(el.event.options).find(option => option.value === eventID)?.dataset.timezone;
    if (choices.some(choice => choice.kind === "book") && (typeof timezone !== "string" || !timezone)) throw new Error("Missing event timezone");
    for (const choice of choices) {
      const article = node("article");
      if (choice.kind === "book") article.append(node("p", w().start + ": " + time(choice.booking.selection.starts_at, timezone)));
      const button = node("button", w()[choice.kind === "instant" ? "continue_instant" : choice.kind === "book" ? "continue_book" : "continue_cancel"]);
      button.type = "button"; button.savedChoice = true; button.unavailable = Date.parse(choice.expires_at) <= Date.now();
      button.addEventListener("click", () => run(e => begin(e, choice.kind, choice.kind === "instant" ? choice.instant : choice.kind === "book" ? choice.booking : choice.cancellation, choice.proposal_id)));
      article.append(button); el.choices.append(article); actionButtons.push(button);
    }
    controls();
  }
  async function loadCurrent(e) {
    if (!eventID) return;
    await loadHistory(e, historyAfter);
    schedule = slots = undefined; renderSelectors(); renderSlots();
    await loadChoices(e, choicesAfter);
    try {
      schedule = await call(e, path() + "/schedule");
      if (schedule.event_id !== eventID || !version(schedule.version) || !version(schedule.event_version)) throw new Error("Invalid schedule");
      renderSelectors(); rangeIndex = 0; await loadSlots(e); el.status.textContent = "";
    } catch (error) { if (error.status !== 404) throw error; el.status.textContent = w().disabled; }
  }
  async function begin(e, kind, args, proposalID) {
    if (pending || blocked) return;
    const choice = { owner, kind, phase: "prepare", body: { operation_id: crypto.randomUUID(), arguments: args, ...(proposalID ? { proposal_id: proposalID } : {}) } };
    if (args.event_id !== eventID || !validPending(choice)) throw new Error("Invalid selection");
    persist(choice); el.receipt.replaceChildren();
    await dispatch(e);
  }
  async function completed(e, booking) {
    if (!uuid(booking.id) || !["booked", "cancelled"].includes(booking.state)) throw new Error("Invalid receipt");
    removePending(); el.receipt.textContent = w()[booking.state]; el.status.textContent = w().completed;
    await loadCurrent(e);
  }
  async function dispatch(e) {
    if (!pending || blocked) return;
    const applying = pending.phase === "apply";
    if (pending.phase === "rejected") { el.status.textContent = w().rejected; return; }
    if (!applying) { prepared = undefined; el.preview.replaceChildren(); }
    let response;
    try { response = await call(e, (pending.kind === "instant" ? "./massage-staff/" + eventID + "/instant" : path() + "/" + pending.kind) + (applying ? "" : "/prepare"), "POST", pending.body); }
    catch (error) {
      if (!applying && pending.body.proposal_id && [400, 404, 409].includes(error.status)) throw { local: "choice_changed" };
      if (!applying || ![400, 404, 409].includes(error.status)) throw error;
      // Validation/access/conflict errors reject the apply without committing it.
      // Reconcile its original receipt before permitting a new selection.
      let receipt;
      try { receipt = await operationReceipt(e); }
      catch (recoveryError) {
        if (recoveryError.status !== 404) throw recoveryError;
        persist({ ...pending, phase: "rejected" }); el.status.textContent = w().rejected; return;
      }
      return completed(e, receipt.result);
    }
    if (applying) return completed(e, response);
    if (!uuid(response.proposal_id) || !instant(response.expires_at) || response.arguments?.event_id !== eventID) throw new Error("Invalid preparation");
    if (pending.body.proposal_id && response.proposal_id !== pending.body.proposal_id) throw { local: "choice_changed" };
    const snapshot = pending.kind !== "cancel" ? response.snapshot : response.booking.snapshot;
    if (pending.kind === "instant" && (snapshot?.purpose !== "staff_self" || !version(snapshot.staff_binding_version))) throw new Error("Invalid self reservation");
    persist({ ...pending, phase: "prepared", display: { event_name: snapshot.event_name, timezone: snapshot.terms.timezone } });
    prepared = response; renderPreparation(); el.status.textContent = w()[Date.parse(response.expires_at) <= Date.now() ? "expired" : "prepared"];
  }
  async function recover(e) {
    if (!pending || blocked) return;
    if (pending.phase !== "apply") return dispatch(e);
    const receipt = await operationReceipt(e);
    return completed(e, receipt.result);
  }
  async function operationReceipt(e) {
    const receipt = await call(e, path() + "/operations/" + pending.body.operation_id);
    if (receipt.operation_id !== pending.body.operation_id || receipt.event_id !== eventID || receipt.kind !== pending.kind) throw new Error("Invalid operation receipt");
    return receipt;
  }
  function selectOwnEvent(context) {
    if (!uuid(context?.event_id) || typeof context.event_name !== "string" || typeof context.timezone !== "string") throw new Error("Invalid own booking event");
    eventID = context.event_id; schedule = slots = undefined; history = []; choices = [];
    historyAfter = historyNext = choicesAfter = choicesNext = undefined;
    showHistoricalEvent({ event_name: context.event_name, terms: { timezone: context.timezone } });
    el.event.value = eventID; el.preview.replaceChildren(); el.receipt.replaceChildren(); renderSelectors(); renderSlots(); renderHistory(); renderChoices();
  }
  async function openOwnEvent(context, args) {
    el.section.scrollIntoView?.({ block: "start" });
    await run(async e => {
      if (pending) return recover(e);
      if (blocked) { el.status.textContent = w().blocked; return; }
      selectOwnEvent(context);
      if (args) { await begin(e, "instant", args); }
      else { await loadCurrent(e); }
    });
  }
  el.event.addEventListener("change", () => run(async e => { if (pending || blocked) return; eventID = el.event.value || undefined; schedule = slots = undefined; history = []; choices = []; historyAfter = historyNext = choicesAfter = choicesNext = undefined; el.preview.replaceChildren(); el.receipt.textContent = ""; renderSelectors(); renderSlots(); renderHistory(); renderChoices(); if (eventID) await loadCurrent(e); }));
  el.window.addEventListener("change", () => run(async e => { rangeIndex = 0; renderSpecialists(); await loadSlots(e); }));
  el.specialist.addEventListener("change", () => run(async e => { renderSpecialists(); await loadSlots(e); }));
  el.service.addEventListener("change", () => run(e => loadSlots(e)));
  for (const [id, step] of [["range-prev", -1], ["range-next", 1]]) el[id].addEventListener("click", () => run(e => { rangeIndex += step; return loadSlots(e); }));
  for (const [id, cursor] of [["events-first", () => undefined], ["events-next", () => eventsNext]]) el[id].addEventListener("click", () => run(e => loadEvents(e, cursor())));
  for (const [id, cursor] of [["slots-first", () => undefined], ["slots-next", () => slotNext]]) el[id].addEventListener("click", () => run(e => loadSlots(e, cursor())));
  for (const [id, cursor] of [["history-first", () => undefined], ["history-next", () => historyNext]]) el[id].addEventListener("click", () => run(e => loadHistory(e, cursor())));
  for (const [id, cursor] of [["choices-first", () => undefined], ["choices-next", () => choicesNext]]) el[id].addEventListener("click", () => run(e => loadChoices(e, cursor())));
  el.reload.addEventListener("click", () => run(async e => { if (pending) await recover(e); else { await loadEvents(e, eventsAfter); await loadCurrent(e); } }));
  el.confirm.addEventListener("click", () => run(async e => {
    if (pending?.phase !== "prepared" || !prepared) return;
    if (Date.parse(prepared.expires_at) <= Date.now()) { el.status.textContent = w().expired; return; }
    persist({ ...pending, phase: "apply", body: { operation_id: pending.body.operation_id, arguments: prepared.arguments, proposal_id: prepared.proposal_id, confirmed: true } });
    prepared = undefined; el.preview.replaceChildren(); await dispatch(e);
  }));
  el.discard.addEventListener("click", () => run(async e => { if (!pending || pending.phase === "apply" || blocked) return; removePending(); await loadCurrent(e); }));
  el.recover.addEventListener("click", () => run(recover)); el.retry.addEventListener("click", () => run(dispatch));
  clear();
  return { clear, openOwnEvent(context) { return openOwnEvent(context); }, prepareInstant(context, args) { return openOwnEvent(context, args); }, setBusy(value) { locked = value; controls(); }, setLanguage(locale) {
    language = locale === "ru" ? "ru" : "en";
    if (!owner) return;
    labels(); renderSelectors(); renderSlots(); renderHistory(); renderChoices(); renderPreparation(); controls();
  }, async start(id, locale) {
    clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); el.section.hidden = false;
    if (/^#massage(?:[?&]|$)/.test(window.location?.hash || "")) el.section.scrollIntoView?.({ block: "start" });
    try { const raw = sessionStorage.getItem(key()); if (raw) { if (raw.length > 16384) throw new Error("Invalid recovery record"); const saved = JSON.parse(raw); if (!validPending(saved)) throw new Error("Invalid recovery record"); pending = saved; eventID = pending.body.arguments.event_id; } }
    catch { blocked = true; el.status.textContent = w().blocked; controls(); return; }
    const e = epoch;
    try { await loadEvents(e); if (pending) await recover(e); }
    catch (error) { if (e === epoch) errorStatus(error); }
    controls();
  } };
};
