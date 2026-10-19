"use strict";

// The parent owns authentication. This controller shares the server draft with
// agent tools; pending bodies remain owner-bound until their outcome is known.
window.createZinusyaMeals = function (env) {
  const el = Object.fromEntries(["meals", "title", "note", "status", "event-label", "event", "first", "next", "saved", "form", "items", "save", "clear", "reload", "latest", "recover", "retry"].map(id => [id, document.getElementById(id === "meals" ? id : "meals-" + id)]));
  const words = {
    en: { title: "Meals", note: "Save a draft here or continue with Zinusya. Saving does not order food, reserve items or confirm payment.", event: "Event", choose: "Choose an event", first: "First page", next: "Next page", save: "Save meal draft", clear: "Clear saved meal draft", reload: "Reload saved draft", latest: "Start a new draft with the current menu", recover: "Check earlier save", retry: "Retry the same save", empty: "No events with meals access.", ready: "Draft loaded", saved: "Saved draft", unsaved: "No saved draft", complete: "Complete draft total", partial: "Known partial amount — not payable", missing: "Still to choose", lunch: "Lunch", dinner: "Dinner", soup: "Soup", main: "Main", side: "Side", salad: "Salad", unchosen: "Not chosen yet", "no-lunch": "No lunch", "individual-items": "Individual items (empty selection is allowed)", "combo-no-soup": "Combo without soup", "combo-with-soup": "Combo with soup", none: "None", service: "Service", closes: "Sales close", opens: "Sales open", menu: "Menu version", historical: "This draft uses an older menu. Its choices and total are historical. To edit with the current menu, explicitly start a new draft below; the saved draft stays unchanged until you save.", unknown: "The earlier save outcome is unknown. Check it or retry the same save before editing.", conflict: "The save was rejected or sales changed. Your edits remain visible. Reload the saved draft before another save.", unavailable: "Could not finish. An uncertain save keeps its original request; check it or retry the same save.", blocked: "Recovery data is unavailable or invalid. No new save will be sent from this page.", storage: "Could not store recovery data. No new request was created.", invalid: "Check the meal choices.", newDraft: "New draft with the current menu; the saved draft stays unchanged until you save.", savedReload: "Save confirmed. Reload to view the current draft.", receipt: "Save confirmed", revision: "Draft version" },
    ru: { title: "Питание", note: "Сохрани черновик здесь или продолжи с Зинусей. Сохранение не оформляет заказ, не резервирует блюда и не подтверждает оплату.", event: "Эвент", choose: "Выбери эвент", first: "Первая страница", next: "Следующая страница", save: "Сохранить питание", clear: "Очистить черновик питания", reload: "Загрузить сохранённый черновик", latest: "Начать новый черновик с текущим меню", recover: "Проверить прежнее сохранение", retry: "Повторить то же сохранение", empty: "Нет эвентов с доступным питанием.", ready: "Черновик загружен", saved: "Сохранённый черновик", unsaved: "Нет сохранённого черновика", complete: "Итог полного черновика", partial: "Известная часть суммы — не к оплате", missing: "Осталось выбрать", lunch: "Обед", dinner: "Ужин", soup: "Суп", main: "Горячее", side: "Гарнир", salad: "Салат", unchosen: "Ещё не выбрано", "no-lunch": "Без обеда", "individual-items": "Отдельные блюда (можно ничего не выбирать)", "combo-no-soup": "Комплекс без супа", "combo-with-soup": "Комплекс с супом", none: "Нет", service: "Подача", closes: "Продажи до", opens: "Продажи с", menu: "Версия меню", historical: "Черновик сохранён с прежним меню. Его состав и сумма исторические. Для изменений явно начни новый черновик с текущим меню ниже; сохранённый черновик не изменится до сохранения.", unknown: "Результат прежнего сохранения неизвестен. Проверь его или повтори тот же запрос перед изменениями.", conflict: "Сохранение отклонено или условия продаж изменились. Твои изменения остались на экране. Загрузи сохранённый черновик перед новой попыткой.", unavailable: "Не удалось завершить. Запрос с неизвестным результатом сохранён; проверь его или повтори тот же запрос.", blocked: "Данные восстановления недоступны или повреждены. Эта страница не отправит новое сохранение.", storage: "Не удалось сохранить данные восстановления. Новый запрос не создан.", invalid: "Проверь выбор питания.", newDraft: "Новый черновик с текущим меню; сохранённый не изменится до сохранения.", savedReload: "Сохранение подтверждено. Загрузи текущий черновик.", receipt: "Сохранение подтверждено", revision: "Версия черновика" }
  };
  const copy = value => JSON.parse(JSON.stringify(value));
  const uuid = value => typeof value === "string" && /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(value) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(value);
  const id = value => typeof value === "string" && /^[a-z][a-z0-9_-]{0,31}$/.test(value);
  const revision = value => Number.isSafeInteger(value) && value >= 0;
  const modes = ["no-lunch", "individual-items", "combo-no-soup", "combo-with-soup"];
  let owner, language = "en", epoch = 0, locked = false, working = false, blocked = false, conflict = false;
  let eventID, after, next, catalog, savedCatalog, draft, pending, historical = false, dirty = false;
  let choices = [], inputs = [], statusKey = "choose", statusSuffix = "";
  const w = () => words[language];
  const key = () => "zinusya-meals-pending-v1:" + owner;
  const path = () => "./meals/" + eventID;
  const node = (tag, text) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; };
  const label = value => value?.[language] || value?.en || value?.ru || "";
  const date = (value, zone) => new Intl.DateTimeFormat(language, { timeZone: zone, dateStyle: "medium", timeStyle: "long" }).format(new Date(value));
  function money(amount, doc) {
    if (!Number.isSafeInteger(amount) || amount < 0 || !Number.isInteger(doc.currency_scale) || doc.currency_scale < 0 || doc.currency_scale > 6 || !/^[A-Z]{3}$/.test(doc.currency)) throw { local: "invalid" };
    const digits = BigInt(amount).toString().padStart(doc.currency_scale + 1, "0");
    return doc.currency + " " + (doc.currency_scale ? digits.slice(0, -doc.currency_scale) + "." + digits.slice(-doc.currency_scale) : digits);
  }
  function validChoices(rows) {
    const set = rows => Array.isArray(rows) && rows.length <= 100 && rows.every(id) && new Set(rows).size === rows.length;
    return Array.isArray(rows) && rows.length <= 14 && new Set(rows.map(c => c?.day_id)).size === rows.length && rows.every(c => {
      if (!c || !id(c.day_id) || Object.keys(c).some(k => !["day_id", "lunch", "dinner_ids"].includes(k)) || (c.dinner_ids !== undefined && c.dinner_ids !== null && !set(c.dinner_ids))) return false;
      const l = c.lunch;
      return l === undefined || l === null || (modes.includes(l.mode) && Object.keys(l).every(k => ["mode", "individual_ids", "soup_id", "main_id", "side_id", "salad_id"].includes(k)) && (l.individual_ids === undefined || set(l.individual_ids)) && ["soup_id", "main_id", "side_id", "salad_id"].every(k => l[k] === undefined || id(l[k])));
    });
  }
  function validPending(p) {
    const b = p?.body;
    return p?.owner === owner && uuid(p.event_id) && Object.keys(p).sort().join() === "body,event_id,owner" && b && Object.keys(b).sort().join() === "choices,expected_catalog_version,expected_draft_version,expected_event_version,operation_id" && uuid(b.operation_id) && revision(b.expected_event_version) && b.expected_event_version > 0 && revision(b.expected_catalog_version) && b.expected_catalog_version > 0 && revision(b.expected_draft_version) && validChoices(b.choices);
  }
  function controls() {
    const busy = locked || working, editing = busy || blocked || !!pending || !catalog || !draft || historical || conflict;
    for (const input of inputs) input.disabled = editing;
    el.save.disabled = el.clear.disabled = editing;
    el.event.disabled = el.first.disabled = el.next.disabled = busy || blocked || !!pending || dirty;
    el.reload.disabled = busy || blocked || !!pending;
    el.latest.hidden = !historical; el.latest.disabled = busy || blocked || !!pending;
    el.recover.hidden = el.retry.hidden = !pending || blocked;
    el.recover.disabled = el.retry.disabled = busy;
  }
  function showStatus(key, suffix = "") { statusKey = key; statusSuffix = suffix; el.status.textContent = w()[key] + suffix; }
  function labels() {
    for (const name of ["title", "note", "first", "next", "save", "clear", "reload", "latest", "recover", "retry"]) el[name].textContent = w()[name];
    el["event-label"].textContent = w().event;
    if (el.event.options?.[0]?.value === "") el.event.options[0].textContent = w().choose;
  }
  function clear() {
    epoch++; owner = eventID = after = next = catalog = savedCatalog = draft = pending = undefined;
    working = blocked = conflict = historical = dirty = false; choices = []; inputs = [];
    el.meals.hidden = true;
    for (const name of ["items", "saved", "event"]) el[name].replaceChildren();
    statusKey = "choose"; statusSuffix = ""; el.status.textContent = ""; controls();
  }
  async function call(e, url, method = "GET", body) {
    try { const result = await env.request(url, method, body); if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  function errorStatus(error) {
    if (error.status === 401 || error.status === 403) { env.setBusy(false); env.denied(); return; }
    showStatus(w()[error.local] ? error.local : "unavailable");
  }
  async function run(action) {
    if (working || locked || !owner) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  function checkCatalog(c, version) {
    if (c?.event_id !== eventID || !revision(c.version) || c.version < 1 || (version && c.version !== version) || !revision(c.event_version) || typeof c.timezone !== "string" || !Array.isArray(c.document?.days) || c.document.days.length > 14) throw { local: "invalid" };
  }
  function checkDraft(d) {
    if (d?.event_id !== eventID || !revision(d.version) || (d.version && (!revision(d.catalog_version) || !d.catalog_version || !validChoices(d.evaluation?.choices)))) throw { local: "invalid" };
  }
  async function loadPage(e, cursor) {
    const page = await call(e, "./meals/events" + (cursor ? "?after=" + encodeURIComponent(cursor) : ""));
    if (!Array.isArray(page?.events) || page.events.length > 20 || (page.next !== undefined && !uuid(page.next))) throw { local: "invalid" };
    const options = [node("option", w().choose)]; options[0].value = "";
    for (const row of page.events) { if (!uuid(row.id) || typeof row.name !== "string") throw { local: "invalid" }; const option = node("option", row.name + " · " + row.timezone); option.value = row.id; options.push(option); }
    eventID = catalog = savedCatalog = draft = undefined; historical = conflict = dirty = false; choices = []; inputs = [];
    el.items.replaceChildren(); el.saved.replaceChildren();
    el.event.replaceChildren(...options); el.event.value = ""; after = cursor; next = page.next;
    el.first.hidden = !after; el.next.hidden = !next;
    if (!eventID) showStatus(page.events.length ? "choose" : "empty");
  }
  function summary() {
    el.saved.replaceChildren();
    if (!draft?.version) { el.saved.append(node("p", w().unsaved)); return; }
    const doc = savedCatalog.document;
    el.saved.append(node("p", w().saved + " · " + draft.version + " · " + w().menu + " " + draft.catalog_version));
    for (const choice of draft.evaluation.choices) {
      const day = doc.days.find(d => d.id === choice.day_id); if (!day) throw { local: "invalid" };
      const names = (service, ids) => (ids || []).map(value => { const item = service?.items.find(i => i.id === value); if (!item) throw { local: "invalid" }; return label(item.name); }).join(", ") || w().none;
      if (day.lunch) { const l = choice.lunch; const ids = l?.mode === "individual-items" ? l.individual_ids : [l?.soup_id, l?.main_id, l?.side_id, l?.salad_id].filter(Boolean); el.saved.append(node("p", label(day.name) + " · " + w().lunch + ": " + (l ? w()[l.mode] : w().unchosen) + " · " + names(day.lunch.service, ids))); }
      if (day.dinner) el.saved.append(node("p", label(day.name) + " · " + w().dinner + ": " + names(day.dinner, choice.dinner_ids)));
    }
    const ev = draft.evaluation;
    el.saved.append(node("p", (ev.complete ? w().complete : w().partial) + ": " + money(ev.complete ? ev.quote.total_minor : ev.known_amount_minor, doc)));
    if (!ev.complete) el.saved.append(node("p", w().missing + ": " + ev.missing.map(m => label(doc.days.find(d => d.id === m.day_id)?.name) + " — " + (w()[m.field] || m.field)).join(", ")));
    if (draft.saved_at) el.saved.append(node("p", date(draft.saved_at, draft.timezone) + " · " + draft.timezone));
    if (historical) el.saved.append(node("p", w().historical));
  }
  function input(tag, name, value, parent, changed) {
    const control = node(tag); control.id = name; control.value = value;
    const caption = node("label"); caption.htmlFor = name; caption.append(control); parent.append(caption); inputs.push(control);
    control.addEventListener("change", () => { changed(control); dirty = true; controls(); }); return { control, caption };
  }
  function items(service, selected, parent, name, changed) {
    for (const item of service.items) {
      const row = input("input", name + ":" + item.id, "", parent, c => changed(item.id, c.checked));
      row.control.type = "checkbox"; row.control.checked = selected.includes(item.id);
      row.caption.append(document.createTextNode(label(item.name) + " · " + money(item.price_minor, catalog.document)));
    }
  }
  function serviceTimes(service, parent, zone) {
    parent.append(node("p", w().service + ": " + date(service.starts_at, zone) + " – " + date(service.ends_at, zone) + " · " + zone), node("p", w().opens + ": " + date(service.sales_opens_at, zone) + " · " + w().closes + ": " + date(service.sales_closes_at, zone)));
  }
  function editor() {
    el.items.replaceChildren(); inputs = [];
    if (!catalog || !draft) return;
    el.items.append(node("p", w().menu + " " + catalog.version));
    for (const day of catalog.document.days) {
      let choice = choices.find(c => c.day_id === day.id);
      if (!choice) { choice = { day_id: day.id, lunch: null, dinner_ids: [] }; choices.push(choice); }
      const section = node("fieldset"), legend = node("legend", label(day.name)); section.append(legend); el.items.append(section);
      if (day.lunch) {
        const lunch = day.lunch; section.append(node("p", w().lunch)); serviceTimes(lunch.service, section, catalog.timezone);
        const row = input("select", "meal-mode:" + day.id, choice.lunch?.mode || "", section, c => { choice.lunch = c.value ? { mode: c.value } : null; editor(); document.getElementById("meal-mode:" + day.id)?.focus?.(); });
        row.caption.replaceChildren(document.createTextNode(w().lunch), row.control);
        for (const mode of ["", "no-lunch", "individual-items", ...(lunch.combos ? ["combo-no-soup", "combo-with-soup"] : [])]) { const option = node("option", mode ? w()[mode] + (mode.startsWith("combo-") ? " · " + money(mode === "combo-with-soup" ? lunch.combos.with_soup_minor : lunch.combos.without_soup_minor, catalog.document) : "") : w().unchosen); option.value = mode; row.control.append(option); }
        row.control.value = choice.lunch?.mode || "";
        if (choice.lunch?.mode === "individual-items") {
          const selected = choice.lunch.individual_ids || [];
          items(lunch.service, selected, section, "meal-lunch:" + day.id, (id, checked) => { choice.lunch.individual_ids = checked ? [...(choice.lunch.individual_ids || []), id] : (choice.lunch.individual_ids || []).filter(i => i !== id); });
        } else if (choice.lunch?.mode?.startsWith("combo-")) {
          for (const category of [ ...(choice.lunch.mode === "combo-with-soup" ? ["soup"] : []), "main", "side", "salad" ]) {
            const field = category + "_id", part = input("select", "meal-" + category + ":" + day.id, choice.lunch[field] || "", section, c => { if (c.value) choice.lunch[field] = c.value; else delete choice.lunch[field]; });
            part.caption.replaceChildren(document.createTextNode(w()[category]), part.control);
            const empty = node("option", w().unchosen); empty.value = ""; part.control.append(empty);
            for (const item of lunch.service.items.filter(i => i.category === category)) { const option = node("option", label(item.name)); option.value = item.id; part.control.append(option); }
            part.control.value = choice.lunch[field] || "";
          }
        }
      }
      if (day.dinner) { section.append(node("p", w().dinner)); serviceTimes(day.dinner, section, catalog.timezone); items(day.dinner, choice.dinner_ids || [], section, "meal-dinner:" + day.id, (id, checked) => { choice.dinner_ids = checked ? [...(choice.dinner_ids || []), id] : (choice.dinner_ids || []).filter(i => i !== id); }); }
    }
    controls();
  }
  async function loadEvent(e, value) {
    if (!uuid(value)) throw { local: "invalid" };
    eventID = value; catalog = savedCatalog = draft = undefined; historical = conflict = dirty = false; el.items.replaceChildren(); el.saved.replaceChildren();
    const d = await call(e, path() + "/draft"); checkDraft(d);
    const current = await call(e, path() + "/catalog"); checkCatalog(current);
    const old = d.version && d.catalog_version !== current.version ? await call(e, path() + "/catalog?version=" + d.catalog_version) : current;
    checkCatalog(old, d.version ? d.catalog_version : current.version);
    if (!Array.from(el.event.options).some(o => o.value === value)) { const option = node("option", current.event_name + " · " + current.timezone); option.value = value; el.event.append(option); }
    el.event.value = value;
    draft = d; savedCatalog = old; catalog = old; historical = old.version !== current.version;
    choices = copy(d.evaluation?.choices || []); summary(); editor(); showStatus("ready");
  }
  function forget() { try { sessionStorage.removeItem(key()); } catch { throw { local: "storage" }; } pending = undefined; }
  async function accepted(e, receipt) {
    checkDraft(receipt);
    if (receipt.version !== pending.body.expected_draft_version + 1 || receipt.catalog_version !== pending.body.expected_catalog_version || receipt.event_version !== pending.body.expected_event_version) throw { local: "invalid" };
    forget();
    try { await loadEvent(e, eventID); showStatus("receipt", " · " + receipt.version); }
    catch (error) { if (error.stale) throw error; if (error.status === 401 || error.status === 403) errorStatus(error); else showStatus("savedReload"); }
  }
  async function recover(e) {
    eventID = pending.event_id; el.event.value = eventID;
    try { await accepted(e, await call(e, path() + "/draft/operations/" + pending.body.operation_id)); }
    catch (error) { if (error.status === 404) showStatus("unknown"); else throw error; }
  }
  async function dispatch(e) {
    eventID = pending.event_id; el.event.value = eventID;
    try { await accepted(e, await call(e, path() + "/draft", "POST", pending.body)); }
    catch (error) { if (error.status === 400 || error.status === 409) { forget(); conflict = true; showStatus("conflict"); } else throw error; }
  }
  async function save(e, empty) {
    if (pending || blocked || conflict || !catalog || !draft || (!empty && historical)) return;
    const body = { operation_id: crypto.randomUUID(), expected_event_version: catalog.event_version, expected_catalog_version: catalog.version, expected_draft_version: draft.version, choices: empty ? [] : copy(choices) };
    if (!validChoices(body.choices)) throw { local: "invalid" };
    const record = { owner, event_id: eventID, body }, raw = JSON.stringify(record);
    if (raw.length > 60000) throw { local: "invalid" };
    try { sessionStorage.setItem(key(), raw); } catch { throw { local: "storage" }; }
    pending = record; controls(); await dispatch(e);
  }
  el.form.addEventListener("submit", event => { event.preventDefault(); return run(e => save(e, false)); });
  el.clear.addEventListener("click", () => run(e => save(e, true)));
  el.recover.addEventListener("click", () => run(recover)); el.retry.addEventListener("click", () => run(dispatch));
  el.reload.addEventListener("click", () => run(e => eventID ? loadEvent(e, eventID) : loadPage(e, after)));
  el.event.addEventListener("change", () => {
    if (pending || blocked || dirty) return;
    if (!el.event.value) {
      eventID = catalog = savedCatalog = draft = undefined; choices = []; inputs = []; historical = conflict = false;
      el.items.replaceChildren(); el.saved.replaceChildren(); showStatus("choose"); controls(); return;
    }
    return run(e => loadEvent(e, el.event.value));
  });
  el.first.addEventListener("click", () => run(e => loadPage(e))); el.next.addEventListener("click", () => run(e => loadPage(e, next)));
  el.latest.addEventListener("click", () => run(async e => { const current = await call(e, path() + "/catalog"); checkCatalog(current); catalog = current; historical = false; conflict = false; choices = []; dirty = true; editor(); showStatus("newDraft"); }));
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(locale) { language = locale === "ru" ? "ru" : "en"; labels(); showStatus(statusKey, statusSuffix); if (draft) { summary(); editor(); } },
    async start(id, locale) {
      clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); el.meals.hidden = false;
      const e = epoch;
      try { const raw = sessionStorage.getItem(key()); if (raw) { if (raw.length > 65536) throw new Error("Oversized recovery record"); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error("Invalid recovery record"); } }
      catch { blocked = true; pending = undefined; showStatus("blocked"); controls(); return; }
      try { await loadPage(e); if (pending) await recover(e); if ((window.location?.hash || "") === "#meals") el.meals.scrollIntoView?.({ block: "start" }); }
      catch (error) { if (e === epoch) errorStatus(error); } controls();
    }
  };
};
