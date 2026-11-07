"use strict";

window.createZinusyaMassageStaff = env => {
  const ids = ["title", "status", "event-label", "event", "events-first", "events-next", "window-label", "window", "windows-first", "windows-next", "filter-label", "filter", "range", "range-prev", "range-next", "rows", "rows-first", "rows-next", "detail", "reload", "preference-note", "notify-label", "notify", "notify-next-label", "notify-next", "preference-save", "preference-reload", "preference-recover", "preference-retry", "preference-status", "instant-title", "instant-note", "instant-specialist-label", "instant-specialist", "instant-service-label", "instant-service", "instant-prepare", "own-bookings"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("ms-" + id)]));
  const section = document.getElementById("massage-staff");
  const words = {
    en: { title: "Specialist timetable", "event-label": "Event", "window-label": "Party", "filter-label": "Show", all: "Whole timetable", own: "My clients", choose: "Choose an event", "events-first": "First events", "events-next": "Next events", "windows-first": "First parties", "windows-next": "Next parties", "range-prev": "Earlier times", "range-next": "Later times", "rows-first": "First bookings", "rows-next": "Next bookings", reload: "Reload specialist access", empty: "No bookings in this time range.", none: "No specialist events are available for this account.", denied: "Specialist access has changed. Reload the available events.", unavailable: "Could not load the timetable. Reload to try again.", client: "Client", details: "Client details", contact: "Contact in Telegram", price: "Price", reserved: "Reserved until" },
    ru: { title: "Расписание специалиста", "event-label": "Эвент", "window-label": "Вечеринка", "filter-label": "Показать", all: "Всё расписание", own: "Мои клиенты", choose: "Выбери эвент", "events-first": "Первые эвенты", "events-next": "Следующие эвенты", "windows-first": "Первые вечеринки", "windows-next": "Следующие вечеринки", "range-prev": "Раньше", "range-next": "Позже", "rows-first": "Первые записи", "rows-next": "Следующие записи", reload: "Обновить доступ специалиста", empty: "В этом интервале записей нет.", none: "Для этого аккаунта нет доступных эвентов специалиста.", denied: "Доступ специалиста изменился. Обнови список доступных эвентов.", unavailable: "Не удалось загрузить расписание. Обнови список и попробуй снова.", client: "Клиент", details: "Данные клиента", contact: "Связаться в Telegram", price: "Стоимость", reserved: "Время занято до" }
  };
  Object.assign(words.en, { "preference-note": "Notification settings apply to ALL your specialist events in this bot.", "notify-label": "Notify me about bookings and cancellations", "notify-next-label": "Remind me about upcoming sessions", "preference-save": "Save notifications", "preference-reload": "Reload notification setting", "preference-recover": "Check earlier save", "preference-retry": "Retry the same save", preferenceUnknown: "The earlier save is unresolved. Check it or retry the same request before making another change.", preferenceStorage: "Recovery data cannot be saved or read. Changes are blocked; reload to check the earlier request.", preferenceUnavailable: "The notification setting is unavailable. The timetable remains separate.", preferenceConflict: "The setting changed before this save. Reload it before choosing again.", preferenceSaved: "Earlier save confirmed. Select an available event to reload the current setting." });
  Object.assign(words.ru, { "preference-note": "Настройки уведомлений действуют для ВСЕХ твоих эвентов специалиста в этом боте.", "notify-label": "Сообщать о записях и отменах", "notify-next-label": "Напоминать о предстоящих сессиях", "preference-save": "Сохранить уведомления", "preference-reload": "Обновить настройку уведомлений", "preference-recover": "Проверить сохранение", "preference-retry": "Повторить тот же запрос", preferenceUnknown: "Результат сохранения неизвестен. Проверь его или повтори тот же запрос до нового изменения.", preferenceStorage: "Не удалось сохранить или прочитать данные восстановления. Изменения заблокированы; перезагрузи страницу для проверки прежнего запроса.", preferenceUnavailable: "Настройка уведомлений недоступна. Расписание работает отдельно.", preferenceConflict: "Настройка изменилась до сохранения. Обнови её перед новым выбором.", preferenceSaved: "Прежнее сохранение подтверждено. Выбери доступный эвент для загрузки текущей настройки." });
  Object.assign(words.en, { "instant-title": "My specialist time", "instant-note": "Reserve your own time for the selected current party, then check the exact preview before confirming.", "instant-specialist-label": "Specialist", "instant-service-label": "Service", "instant-prepare": "Review my time reservation", "own-bookings": "My bookings and unfinished choices" });
  Object.assign(words.ru, { "instant-title": "Моё время специалиста", "instant-note": "Выбери текущую вечеринку и услугу для себя, затем проверь точное время перед подтверждением.", "instant-specialist-label": "Специалист", "instant-service-label": "Услуга", "instant-prepare": "Проверить резерв своего времени", "own-bookings": "Мои записи и незавершённые действия" });
  const day = 86400000;
  const uuid = value => typeof value === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(value) && value !== "00000000-0000-0000-0000-000000000000";
  const identifier = value => typeof value === "string" && /^[a-z][a-z0-9_-]{0,31}$/.test(value);
  const instant = value => typeof value === "string" && /(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value));
  const contact = value => typeof value === "string" && /^tg:\/\/user\?id=[1-9][0-9]{0,18}$/.test(value);
  let owner, language = "en", epoch = 0, locked = false, working = false;
  let events = [], eventID, eventAfter, eventNext, schedule, windowAfter, windowNext, viewTimezone, rangeIndex = 0, rows = [], rowAfter, rowNext, detail;
  let preference, preferenceRecord, preferenceBlocked = false, preferenceMessage, configuration;
  const w = () => words[language];
  const preferenceKey = () => "zinusya.massage-staff-preference." + owner;
  const preferencePath = id => "./massage-staff/" + id + "/preferences";
  function validPreference(value) {
    return value && value.user_id === owner && typeof value.notify_bookings === "boolean" && typeof value.notify_next === "boolean" && Number.isSafeInteger(value.version) && value.version >= 0;
  }
  function validPreferenceRecord(value) {
    const args = value?.arguments;
    if (!args) return false;
    const flags = ["notify_bookings", "notify_next"];
    const validFlags = flags.some(key => typeof args[key] === "boolean") &&
      flags.every(key => !(key in args) || typeof args[key] === "boolean");
    return value.owner === owner && uuid(value.operation_id) && uuid(args.event_id) && validFlags &&
      Number.isSafeInteger(args.expected_version) && args.expected_version >= 0 &&
      Object.keys(value).sort().join() === "arguments,operation_id,owner" &&
      Object.keys(args).every(key => ["event_id", "expected_version", ...flags].includes(key));
  }
  function preferenceStatus(message) { preferenceMessage = message; el["preference-status"].textContent = message ? w()[message] : ""; }
  function writePreferenceRecord(record) {
    try { sessionStorage.setItem(preferenceKey(), JSON.stringify(record)); }
    catch { preferenceBlocked = true; throw { preferenceStorage: true }; }
    preferenceRecord = record;
  }
  function removePreferenceRecord() {
    try { sessionStorage.removeItem(preferenceKey()); }
    catch { preferenceBlocked = true; throw { preferenceStorage: true }; }
    preferenceRecord = undefined;
  }
  function preferenceError(error) {
    if (error.stale) return;
    if (error.status === 401) { env.setBusy(false); env.denied(); return; }
    preferenceStatus(preferenceBlocked || error.preferenceStorage ? "preferenceStorage" : preferenceRecord ? "preferenceUnknown" : "preferenceUnavailable");
  }
  async function loadPreference(e) {
    preference = undefined; el.notify.checked = false; el["notify-next"].checked = false;
    if (!eventID) return;
    try {
      const reply = await call(e, preferencePath(eventID));
      if (!validPreference(reply)) throw new Error("Invalid specialist preference");
      preference = reply; el.notify.checked = reply.notify_bookings; el["notify-next"].checked = reply.notify_next;
      if (!preferenceBlocked && !preferenceRecord) preferenceStatus();
    } catch (error) { preferenceError(error); }
  }
  async function preferenceResult(e, reply) {
    if (!validPreference(reply)) throw new Error("Invalid preference receipt");
    removePreferenceRecord();
    preferenceStatus("preferenceSaved");
    await loadPreference(e);
    if (e === epoch && preference) preferenceStatus("preferenceSaved");
  }
  async function savePreference(e) {
    const record = preferenceRecord;
    try {
      const reply = await call(e, preferencePath(record.arguments.event_id), "POST", { operation_id: record.operation_id, arguments: record.arguments });
      await preferenceResult(e, reply);
    } catch (error) {
      if (!error.stale && error.status === 409) {
        // An exact replay returns its immutable receipt. A 409 is a rejected
        // request, so a new choice must use a freshly loaded current version.
        try { removePreferenceRecord(); preference = undefined; preferenceStatus("preferenceConflict"); }
        catch (storageError) { preferenceError(storageError); }
      } else preferenceError(error);
    }
  }
  const label = (value, fallback) => typeof value?.[language] === "string" ? value[language] : typeof value?.en === "string" ? value.en : fallback;
  function node(tag, text) { const value = document.createElement(tag); if (text !== undefined) value.textContent = text; return value; }
  function options(select, values, chosen) {
    select.replaceChildren(...values.map(value => { const option = node("option", value.name); option.value = value.id; return option; }));
    select.value = chosen || "";
  }
  function time(value, timezone) {
    if (typeof timezone !== "string" || !timezone) throw new Error("Missing event timezone");
    return new Intl.DateTimeFormat(language, { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", timeZoneName: "longOffset" }).format(new Date(value));
  }
  function money(terms) {
    if (!Number.isSafeInteger(terms.price_minor) || terms.price_minor < 0 || !Number.isInteger(terms.currency_scale) || terms.currency_scale < 0 || terms.currency_scale > 6 || !/^[A-Z]{3}$/.test(terms.currency)) throw new Error("Invalid booking price");
    const digits = String(terms.price_minor).padStart(terms.currency_scale + 1, "0");
    return (terms.currency_scale ? digits.slice(0, -terms.currency_scale) + (language === "ru" ? "," : ".") + digits.slice(-terms.currency_scale) : digits) + " " + terms.currency;
  }
  function range() {
    const window = schedule?.windows[Number(el.window.value)];
    if (!window) return undefined;
    const from = Date.parse(window.starts_at) + rangeIndex * day;
    if (from >= Date.parse(window.ends_at)) return undefined;
    return { from: new Date(from).toISOString(), until: new Date(Math.min(from + day, Date.parse(window.ends_at))).toISOString(), window };
  }
  function currentInstantWindow() {
    const selected = range()?.window;
    return configuration?.document?.windows.find(value => value.id === selected?.id && Date.parse(value.starts_at) === Date.parse(selected.starts_at) && Date.parse(value.ends_at) === Date.parse(selected.ends_at));
  }
  function renderInstantSelectors() {
    const party = currentInstantWindow();
    const specialists = (configuration?.document?.specialists || []).filter(value => configuration.own_specialist_ids.includes(value.id) && value.work_spans.some(span => span.window_id === party?.id));
    const selected = specialists.find(value => value.id === el["instant-specialist"].value) || specialists[0];
    options(el["instant-specialist"], specialists.map(value => ({ id: value.id, name: label(value.name, value.id) })), selected?.id);
    const services = (configuration?.document?.services || []).filter(value => selected?.service_ids.includes(value.id));
    const service = services.find(value => value.id === el["instant-service"].value) || services[0];
    options(el["instant-service"], services.map(value => ({ id: value.id, name: label(value.name, value.id) })), service?.id);
  }
  function ownBookingContext() {
    return { event_id: eventID, event_name: configuration?.event_name || events.find(value => value.id === eventID)?.name, timezone: configuration?.timezone || viewTimezone };
  }
  function controls() {
    const busy = locked || working || !owner;
    for (const id of ["event", "events-first", "events-next", "reload"]) el[id].disabled = busy;
    for (const id of ["window", "windows-first", "windows-next", "filter", "range-prev", "range-next", "rows-first", "rows-next"]) el[id].disabled = busy || !schedule;
    for (const button of el.rows.querySelectorAll("button")) button.disabled = busy;
    el["events-first"].hidden = !eventAfter; el["events-next"].hidden = !eventNext;
    el["windows-first"].hidden = !windowAfter; el["windows-next"].hidden = !windowNext;
    el["rows-first"].hidden = !rowAfter; el["rows-next"].hidden = !rowNext;
    el["instant-specialist"].disabled = busy || !configuration || !currentInstantWindow();
    el["instant-service"].disabled = el["instant-specialist"].disabled || !el["instant-specialist"].value;
    el["instant-prepare"].disabled = el["instant-service"].disabled || !el["instant-service"].value || typeof env.prepareOwnTime !== "function";
    el["own-bookings"].disabled = busy || !eventID || !viewTimezone || typeof env.openOwnBookings !== "function";
    el.notify.disabled = busy || preferenceBlocked || !!preferenceRecord || !preference;
    el["notify-next"].disabled = el.notify.disabled;
    el["preference-save"].disabled = el.notify.disabled || (el.notify.checked === preference?.notify_bookings && el["notify-next"].checked === preference?.notify_next);
    el["preference-reload"].disabled = busy || !eventID;
    for (const id of ["preference-recover", "preference-retry"]) { el[id].hidden = !preferenceRecord; el[id].disabled = busy || preferenceBlocked; }
    const span = range();
    el["range-prev"].hidden = !span || rangeIndex === 0;
    el["range-next"].hidden = !span || Date.parse(span.until) >= Date.parse(span.window.ends_at);
  }
  function labels() {
    for (const id of ["title", "event-label", "window-label", "windows-first", "windows-next", "filter-label", "events-first", "events-next", "range-prev", "range-next", "rows-first", "rows-next", "reload"]) el[id].textContent = w()[id];
    for (const id of ["preference-note", "notify-label", "notify-next-label", "preference-save", "preference-reload", "preference-recover", "preference-retry"]) el[id].textContent = w()[id];
    for (const id of ["instant-title", "instant-note", "instant-specialist-label", "instant-service-label", "instant-prepare", "own-bookings"]) el[id].textContent = w()[id];
    preferenceStatus(preferenceMessage);
    options(el.filter, [{ id: "all", name: w().all }, { id: "own", name: w().own }], el.filter.value || "all");
    options(el.event, [{ id: "", name: w().choose }, ...events], eventID);
    options(el.window, (schedule?.windows || []).map((value, index) => ({ id: String(index), name: label(value.name, value.id) + " · " + time(value.starts_at, viewTimezone) + " — " + time(value.ends_at, viewTimezone) })), el.window.value || (schedule?.windows.length ? "0" : ""));
    renderInstantSelectors();
  }
  function clearRows() {
    rows = []; detail = rowAfter = rowNext = undefined;
    el.rows.replaceChildren(); el.detail.replaceChildren(); controls();
  }
  function clear() {
    preference = preferenceRecord = preferenceMessage = undefined; preferenceBlocked = false; el.notify.checked = false; el["notify-next"].checked = false; el["preference-status"].textContent = "";
    epoch++; owner = eventID = eventAfter = eventNext = configuration = schedule = windowAfter = windowNext = viewTimezone = undefined; working = false; rangeIndex = 0; events = [];
    clearRows(); for (const id of ["event", "window", "filter", "instant-specialist", "instant-service"]) el[id].replaceChildren();
    el.range.textContent = el.status.textContent = ""; section.hidden = true; controls();
  }
  function errorStatus(error) {
    if (error.stale) return;
    configuration = undefined; renderInstantSelectors(); clearRows();
    if (error.status === 401) { env.setBusy(false); env.denied(); return; }
    if (error.status === 403 || error.status === 404 || error.status === 409) {
      configuration = schedule = windowAfter = windowNext = viewTimezone = undefined; el.window.replaceChildren(); el.range.textContent = ""; el.status.textContent = w().denied;
    } else el.status.textContent = w().unavailable;
    controls();
  }
  async function call(e, path, method = "GET", body) {
    try { const result = await env.request(path, method, body); if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  async function run(action) {
    if (working || locked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  async function loadEvents(e, after) {
    preference = undefined; el.notify.checked = false; el["notify-next"].checked = false;
    clearRows(); configuration = schedule = windowAfter = windowNext = viewTimezone = eventID = undefined; rangeIndex = 0; el.range.textContent = ""; el.window.replaceChildren();
    const page = await call(e, "./massage-staff/events" + (after ? "?after=" + encodeURIComponent(after) : ""));
    if (!Array.isArray(page.events) || page.events.length > 20 || page.events.some(value => !uuid(value.id) || typeof value.name !== "string") || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid specialist events");
    events = page.events; eventAfter = after; eventNext = page.next; labels(); el.status.textContent = events.length ? "" : w().none; controls();
  }
  function validBooking(value) {
    const terms = value?.terms;
    return uuid(value?.id) && Number.isSafeInteger(value.version) && value.version > 0 && typeof value.client_name === "string" && value.client_name.length <= 1024 && terms && identifier(terms.specialist_id) && identifier(terms.service_id) && instant(terms.starts_at) && instant(terms.treatment_ends_at) && instant(terms.occupancy_ends_at);
  }
  function bookingNodes(value, withContact) {
    const terms = value.terms, timezone = viewTimezone;
    const nodes = [node("h2", w().client + ": " + value.client_name), node("p", label(terms.specialist_name, terms.specialist_id) + " · " + label(terms.service_name, terms.service_id)), node("p", time(terms.starts_at, timezone) + " — " + time(terms.treatment_ends_at, timezone)), node("p", w().price + ": " + money(terms))];
    if (withContact) {
      nodes.push(node("p", w().reserved + ": " + time(terms.occupancy_ends_at, timezone)));
      if (contact(value.contact)) { const link = node("a", w().contact); link.href = value.contact; link.rel = "noopener noreferrer"; nodes.push(link); }
    }
    return nodes;
  }
  function renderRows() {
    el.rows.replaceChildren(...rows.map(value => {
      const item = node("article"); item.append(...bookingNodes(value, false));
      if (contact(value.contact)) {
        const button = node("button", w().details); button.type = "button";
        button.addEventListener("click", () => run(async e => {
          detail = undefined; el.detail.replaceChildren();
          const reply = await call(e, "./massage-staff/" + eventID + "/clients/" + value.id);
          if (!validBooking(reply) || reply.id !== value.id) throw new Error("Invalid specialist client");
          detail = reply; el.detail.replaceChildren(...bookingNodes(detail, true));
        })); item.append(button);
      }
      return item;
    }));
    if (!rows.length && schedule) el.rows.append(node("p", w().empty));
    if (detail) el.detail.replaceChildren(...bookingNodes(detail, true));
    controls();
  }
  async function loadRows(e, after) {
    clearRows(); const span = range(); if (!span) return;
    const args = { event_id: eventID, window_id: span.window.id, from: span.from, until: span.until, own_only: el.filter.value === "own", limit: 20, ...(after ? { after } : {}) };
    const page = await call(e, "./massage-staff/" + eventID + "/timetable", "POST", args);
    const next = page.next;
    if (page.event_id !== eventID || !Array.isArray(page.bookings) || page.bookings.length > 20 || page.bookings.some(value => !validBooking(value)) || (next && (!uuid(next.id) || next.event_id !== eventID || next.window_id !== args.window_id || next.own_only !== args.own_only || Date.parse(next.from) !== Date.parse(args.from) || Date.parse(next.until) !== Date.parse(args.until) || !instant(next.starts_at)))) throw new Error("Invalid specialist timetable");
    time(span.from, page.timezone);
    viewTimezone = page.timezone; rows = page.bookings; rowAfter = after; rowNext = next;
    el.range.textContent = time(span.from, viewTimezone) + " — " + time(span.until, viewTimezone) + " · " + viewTimezone;
    el.status.textContent = ""; renderRows();
  }
  async function loadSchedule(e, after) {
    clearRows(); configuration = schedule = windowAfter = windowNext = viewTimezone = undefined; el.range.textContent = ""; el.window.replaceChildren();
    if (!eventID) { controls(); return; }
    const reply = await call(e, "./massage-staff/" + eventID + "/windows", "POST", { event_id: eventID, ...(after ? { after } : {}) });
    const validWindow = value => identifier(value?.id) && instant(value.starts_at) && instant(value.ends_at) && Date.parse(value.starts_at) < Date.parse(value.ends_at);
    if (reply.event_id !== eventID || !Array.isArray(reply.windows) || reply.windows.length > 20 || reply.windows.some(value => !validWindow(value)) || (reply.next && (reply.next.event_id !== eventID || !validWindow({ ...reply.next, id: reply.next.window_id })))) throw new Error("Invalid specialist windows");
    time(reply.windows[0]?.starts_at || new Date().toISOString(), reply.timezone);
    // Current configuration is needed for a new reservation, not historical reads.
    try {
      const config = await call(e, "./massage-staff/" + eventID + "/schedule");
      if (config.event_id !== eventID || !Number.isSafeInteger(config.event_version) || config.event_version < 1 || !Number.isSafeInteger(config.version) || config.version < 1 || typeof config.event_name !== "string" || !config.document || !Array.isArray(config.document.windows) || !Array.isArray(config.document.specialists) || !Array.isArray(config.document.services) || !Array.isArray(config.own_specialist_ids) || config.own_specialist_ids.some(value => !identifier(value))) throw new Error("Invalid specialist configuration");
      time(config.observed_at, config.timezone);
      configuration = config;
    } catch (error) {
      if (error.stale || error.status === 401) throw error;
      configuration = undefined;
    }
    schedule = reply; windowAfter = after; windowNext = reply.next; viewTimezone = reply.timezone; rangeIndex = 0; labels();
    if (!reply.windows.length) { el.status.textContent = w().empty; controls(); return; }
    await loadRows(e);
  }
  el.event.addEventListener("change", () => run(async e => { eventID = el.event.value || undefined; preference = undefined; el.notify.checked = false; el["notify-next"].checked = false; await loadSchedule(e); await loadPreference(e); }));
  el.notify.addEventListener("change", controls);
  el["notify-next"].addEventListener("change", controls);
  el["preference-reload"].addEventListener("click", () => run(e => loadPreference(e)));
  el["preference-save"].addEventListener("click", () => run(async e => {
    if (!preference || preferenceBlocked || preferenceRecord) return;
    try {
      writePreferenceRecord({ owner, operation_id: crypto.randomUUID(), arguments: { event_id: eventID, expected_version: preference.version, notify_bookings: el.notify.checked, notify_next: el["notify-next"].checked } });
      preferenceStatus("preferenceUnknown"); await savePreference(e);
    } catch (error) { preferenceError(error); }
  }));
  el["preference-retry"].addEventListener("click", () => run(e => savePreference(e)));
  el["preference-recover"].addEventListener("click", () => run(async e => {
    try {
      const record = preferenceRecord;
      const reply = await call(e, "./massage-staff/" + record.arguments.event_id + "/preference-operations/" + record.operation_id);
      await preferenceResult(e, reply);
    } catch (error) { preferenceError(error); }
  }));
  el.window.addEventListener("change", () => run(e => { rangeIndex = 0; renderInstantSelectors(); return loadRows(e); }));
  el["instant-specialist"].addEventListener("change", () => { renderInstantSelectors(); controls(); });
  el["instant-service"].addEventListener("change", controls);
  el["instant-prepare"].addEventListener("click", () => {
    if (el["instant-prepare"].disabled) return;
    const party = currentInstantWindow();
    void env.prepareOwnTime(ownBookingContext(), { event_id: eventID, expected_event_version: configuration.event_version, expected_schedule_version: configuration.version, selection: { window_id: party.id, specialist_id: el["instant-specialist"].value, service_id: el["instant-service"].value } });
  });
  el["own-bookings"].addEventListener("click", () => { if (!el["own-bookings"].disabled) void env.openOwnBookings(ownBookingContext()); });
  el.filter.addEventListener("change", () => run(e => loadRows(e)));
  for (const [id, step] of [["range-prev", -1], ["range-next", 1]]) el[id].addEventListener("click", () => run(e => { rangeIndex += step; return loadRows(e); }));
  for (const [id, cursor] of [["events-first", () => undefined], ["events-next", () => eventNext]]) el[id].addEventListener("click", () => run(e => loadEvents(e, cursor())));
  for (const [id, cursor] of [["rows-first", () => undefined], ["rows-next", () => rowNext]]) el[id].addEventListener("click", () => run(e => loadRows(e, cursor())));
  for (const [id, cursor] of [["windows-first", () => undefined], ["windows-next", () => windowNext]]) el[id].addEventListener("click", () => run(e => loadSchedule(e, cursor())));
  el.reload.addEventListener("click", () => run(e => loadEvents(e)));
  clear();
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(locale) {
    language = locale === "ru" ? "ru" : "en"; if (owner) { labels(); renderRows(); const span = range(); if (span) el.range.textContent = time(span.from, viewTimezone) + " — " + time(span.until, viewTimezone) + " · " + viewTimezone; }
  }, async start(id, locale) {
    clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en";
    try {
      const saved = sessionStorage.getItem(preferenceKey());
      if (saved !== null) { const parsed = JSON.parse(saved); if (!validPreferenceRecord(parsed)) throw new Error("Invalid preference recovery record"); preferenceRecord = parsed; preferenceMessage = "preferenceUnknown"; }
    } catch { preferenceBlocked = true; preferenceMessage = "preferenceStorage"; }
    labels(); section.hidden = false;
    if (/^#massage-staff(?:[?&]|$)/.test(window.location?.hash || "")) section.scrollIntoView?.({ block: "start" });
    const e = epoch; try { await loadEvents(e); } catch (error) { if (e === epoch) errorStatus(error); } controls();
  } };
};
