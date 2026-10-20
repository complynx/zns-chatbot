"use strict";
window.createZinusyaKnowledge = env => {
  const ids = ["heading", "status", "collection-label", "collection", "query-label", "query", "search", "next", "new", "new-edit", "discovery-note", "events-next", "recent", "recent-list", "results", "article", "title", "provenance", "body", "sources", "current", "history", "history-next", "correct", "edit", "revisions", "editor", "editor-title", "slug-label", "slug", "name-label", "name", "language-label", "language", "topics-label", "topics", "text-label", "text", "sources-heading", "source-inputs", "source-add", "reason-label", "reason", "prepare", "editor-close", "suggestions-heading", "mine", "pending", "suggestions-next", "suggestions", "suggestion", "review", "review-reason-label", "review-reason", "approve", "reject", "preview", "consent-wrap", "consent", "consent-label", "confirm", "recover", "retry", "close-expired", "receipt"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("kb-" + id)])), section = document.getElementById("knowledge");
  const words = {
    en: { heading: "Knowledge", "collection-label": "Collection", "query-label": "Search", search: "Search / browse", next: "Next page", new: "Suggest a new article", "new-edit": "Create an article", current: "Current revision", history: "Revision history", "history-next": "Older revisions", correct: "Suggest a correction", edit: "Edit article", "editor-title": "Article change", "slug-label": "Short name (letters and hyphens)", "name-label": "Title", "language-label": "Article language", "topics-label": "Topics, one per line", "text-label": "Article text (Markdown, at most 96 KiB)", "sources-heading": "Sources", "source-add": "Add source", "reason-label": "Reason for change", prepare: "Review this change", "editor-close": "Close editor", "suggestions-heading": "Suggested changes", mine: "My suggestions and decisions", pending: "Review pending suggestions", "suggestions-next": "Next suggestions", "review-reason-label": "Decision reason", approve: "Review approval", reject: "Review rejection", "consent-label": "I confirm this exact change", confirm: "Confirm this change", recover: "Check earlier request", retry: "Retry the same preparation", "close-expired": "Close the expired request", all: "All knowledge", general: "General and history", event: "Event", empty: "No results on this page.", read: "Read", revision: "Revision", historical: "Historical event", sourceLabel: "Source name", sourceURL: "Source URL", remove: "Remove source", rejectedPreparation: "Preparation was rejected. The draft remains editable; refresh the article or check access before trying again.", prepared: "Review the exact change below before confirming.", unknown: "The result is unknown. Check this same request before starting another change.", invalid: "Check the fields, reason and 96 KiB text limit.", unavailable: "Could not read data. Retry the same read.", storage: "Recovery could not be saved or read. New changes are blocked.", completed: "This operation completed. Its receipt is historical.", expired: "The server confirms this original request expired without completion. You may explicitly close it.", deadline: "The displayed deadline passed. Check the server result before closing or starting another change.", pendingState: "Pending", approved: "Approved", rejected: "Rejected", based: "Based on revision", currentText: "Current published text", proposedText: "Proposed text", reason: "Reason", id: "Article ID", suggestionID: "Suggestion ID", author: "Proposer", reviewer: "Reviewer", until: "Confirm before (UTC)", limited: "More events or topics exist. Load more events or search all knowledge.", discovery: "You can suggest the first article for any listed event.", "events-next": "More events", recent: "Recent requests", recentNote: "At most five recent requests. Select one to inspect its original terms; this does not approve it.", restored: "An unfinished request was restored. Check its result and review it again before confirming.", noReceipt: "No completed receipt exists yet. Retry only this same request.", conflict: "The content or permissions changed. The saved request remains available for reconciliation." },
    ru: { heading: "База знаний", "collection-label": "Раздел", "query-label": "Поиск", search: "Найти / показать статьи", next: "Следующая страница", new: "Предложить новую статью", "new-edit": "Создать статью", current: "Текущая версия", history: "История изменений", "history-next": "Более ранние версии", correct: "Предложить исправление", edit: "Редактировать статью", "editor-title": "Изменение статьи", "slug-label": "Короткое имя (буквы и дефисы)", "name-label": "Заголовок", "language-label": "Язык статьи", "topics-label": "Темы, по одной в строке", "text-label": "Текст статьи (Markdown, не более 96 КиБ)", "sources-heading": "Источники", "source-add": "Добавить источник", "reason-label": "Причина изменения", prepare: "Проверить изменение", "editor-close": "Закрыть редактор", "suggestions-heading": "Предложения изменений", mine: "Мои предложения и решения", pending: "Проверить ожидающие предложения", "suggestions-next": "Следующие предложения", "review-reason-label": "Причина решения", approve: "Проверить одобрение", reject: "Проверить отклонение", "consent-label": "Подтверждаю именно это изменение", confirm: "Подтвердить изменение", recover: "Проверить прежний запрос", retry: "Повторить ту же подготовку", "close-expired": "Закрыть истёкший запрос", all: "Вся база знаний", general: "Общие знания и история", event: "Эвент", empty: "На этой странице ничего нет.", read: "Читать", revision: "Версия", historical: "Прошедший эвент", sourceLabel: "Название источника", sourceURL: "URL источника", remove: "Убрать источник", rejectedPreparation: "Подготовка отклонена. Черновик можно исправить; обнови статью или проверь доступ перед новой попыткой.", prepared: "Проверь точное изменение ниже перед подтверждением.", unknown: "Результат неизвестен. Проверь этот запрос, прежде чем начинать другое изменение.", invalid: "Проверь поля, причину и ограничение текста 96 КиБ.", unavailable: "Не удалось прочитать данные. Повтори чтение.", storage: "Не удалось сохранить или прочитать данные восстановления. Новые изменения заблокированы.", completed: "Операция завершена. Квитанция показывает исторический результат.", expired: "Сервер подтвердил: исходный запрос истёк и не завершён. Его можно явно закрыть.", deadline: "Показанный срок прошёл. Проверь результат на сервере перед закрытием или новым изменением.", pendingState: "Ожидает решения", approved: "Одобрено", rejected: "Отклонено", based: "На основе версии", currentText: "Текущий опубликованный текст", proposedText: "Предложенный текст", reason: "Причина", id: "ID статьи", suggestionID: "ID предложения", author: "Автор предложения", reviewer: "Проверил", until: "Подтвердить до (UTC)", limited: "Есть другие эвенты или темы. Загрузи следующие эвенты или ищи по всей базе.", discovery: "Для любого показанного эвента можно предложить первую статью.", "events-next": "Другие эвенты", recent: "Недавние запросы", recentNote: "Не более пяти недавних запросов. Выбери запрос для проверки исходных условий; это не подтверждение действия.", restored: "Незавершённый запрос восстановлен. Проверь результат и снова проверь изменение перед подтверждением.", noReceipt: "Завершённой квитанции пока нет. Повторяй только этот же запрос.", conflict: "Материалы или права изменились. Сохранённый запрос доступен для сверки." }
  };
  let owner, locale = "en", epoch = 0, locked = false, busy = false, blocked = false, pending, previewReady = false, terminalExpired = false;
  let article, suggestion, capabilities = {}, editing, next, revisionNext, suggestionNext, suggestionMode = "my_suggestions", pageFilters, eventNext, sourceRows = [];
  const w = () => words[locale], key = () => "zinusya.knowledge." + owner;
  const bytes = value => new TextEncoder().encode(value).length;
  const uuid = value => typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value);
  function node(tag, text) { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; return n; }
  function button(text, action) { const b = node("button", text); b.type = "button"; b.addEventListener("click", () => run(action)); return b; }
  function status(code) { el.status.textContent = w()[code] || code; }
  function filters() { const [collection, event] = el.collection.value.split(":"); return { collection: collection || "all", ...(event ? { event_id: event } : {}), limit: 10 }; }
  function controls() {
    const reviewRecovery = new Set([el.consent, el.confirm, el.recover, el.retry, el["close-expired"]]);
    for (const n of section.querySelectorAll("button, input, select, textarea")) n.disabled = locked || busy || (pending?.kind === "review" && !reviewRecovery.has(n));
    for (const id of ["new", "new-edit", "correct", "edit", "prepare", "approve", "reject", "source-add"]) el[id].disabled ||= blocked || !!pending;
    el["events-next"].hidden = !eventNext; el.next.hidden = !next; el["history-next"].hidden = !revisionNext; el["suggestions-next"].hidden = !suggestionNext;
    el.correct.hidden = !article || article.older_revision; el.edit.hidden = !article || article.older_revision || !(article.event_id ? capabilities.can_edit : capabilities.moderator);
    el["new-edit"].hidden = !(filters().event_id ? capabilities.can_edit : capabilities.moderator);
    el.pending.hidden = !(capabilities.moderator || capabilities.event_administrator || capabilities.global_administrator);
    el.retry.disabled ||= blocked;
    el["close-expired"].hidden = !pending || !terminalExpired;
    el.recover.hidden = !pending; el.retry.hidden = !pending || pending.phase !== "preparing" || terminalExpired;
    el["consent-wrap"].hidden = !pending?.proposal_id;
    el.confirm.hidden = !pending?.proposal_id;
    el.confirm.disabled ||= blocked || !previewReady || terminalExpired || !el.consent.checked || Date.parse(pending?.expires_at) <= Date.now();
  }
  function labels() { for (const id of ids) if (w()[id]) el[id].textContent = w()[id]; controls(); }
  function failure(error) {
    if (error.stale) return;
    if (error.status === 401) { clear(); env.denied(); return; }
    if (error.local) status(error.local);
    else status(pending ? (error.status === 409 || error.status === 403 ? "conflict" : "unknown") : "unavailable");
  }
  async function call(e, path, body = {}) {
    if (e !== epoch) throw { stale: true };
    if (bytes(JSON.stringify(body)) > 1024 * 1024) throw { local: "invalid" };
    let result;
    try { result = await env.request("./knowledge/" + path, "POST", body); }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
    if (e !== epoch) throw { stale: true }; return result;
  }
  async function run(action) {
    if (busy || locked || !owner) return; const e = epoch; busy = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) failure(error); }
    finally { if (e === epoch) { busy = false; env.setBusy(false); controls(); } }
  }
  function save() {
    try { sessionStorage.setItem(key(), JSON.stringify(pending)); }
    catch { blocked = true; throw { local: "storage" }; }
  }
  function consentOff() { el.consent.checked = false; previewReady = false; }
  async function capabilitiesFor(e, eventID) { capabilities = await call(e, "capabilities", eventID ? { event_id: eventID } : {}); controls(); }
  async function overview(e, after) {
    const data = await call(e, "overview", after ? { after } : {});
    if (!after) {
      el.collection.replaceChildren();
      for (const [value, text] of [["all", w().all], ["general", w().general]]) { const n = node("option", text); n.value = value; el.collection.append(n); }
      el.collection.value = "all";
    }
    for (const item of data.items || []) if (item.event_id) {
      const value = item.collection + ":" + item.event_id;
      if (Array.from(el.collection.children).some(n => n.value === value)) continue;
      const n = node("option", item.event_name || w().event); n.value = value; el.collection.append(n);
    }
    eventNext = data.next_event;
    el["discovery-note"].textContent = w()[data.truncated ? "limited" : "discovery"];
  }
  async function recent(e) {
    const data = await call(e, "recent_actions"); el["recent-list"].replaceChildren();
    el["recent-list"].append(node("p", w().recentNote));
    for (const item of data.items || []) {
      const label = item.kind + " · " + item.state + " · " + item.created_at;
      if (!["suggest", "edit", "review"].includes(item.kind)) { el["recent-list"].append(node("p", label)); continue; }
      const b = button(label, async e => {
        if (blocked || pending || !uuid(item.operation_id)) return;
        pending = { owner, kind: item.kind, operation_id: item.operation_id, arguments: {}, phase: "locating" };
        save(); await recover(e);
      }); b.disabled = blocked || !!pending; el["recent-list"].append(b);
    }
  }
  async function list(e, after) {
    if (!after) pageFilters = { ...filters(), ...(el.query.value.trim() ? { queries: [el.query.value.trim()] } : {}) };
    const search = !!pageFilters.queries, request = { ...pageFilters, ...(after ? { [search ? "cursor" : "after"]: after } : {}) };
    const data = await call(e, search ? "search" : "list", request); next = data.next; el.results.replaceChildren();
    if (!data.items?.length) el.results.textContent = w().empty;
    for (const item of data.items || []) { const row = node("article"); row.append(node("h2", item.title), node("p", item.excerpt?.text || ""), button(w().read, e => read(e, item.id))); el.results.append(row); }
  }
  function eventTime(value, zone) {
    if (!value) return ""; const timezone = zone || "UTC";
    return new Intl.DateTimeFormat(locale, { timeZone: timezone, dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) + " · " + timezone;
  }
  function sources(target, items) { target.replaceChildren(); for (const source of items || []) target.append(node("p", source.label + " · " + source.url)); }
  async function read(e, id, revision = 0) {
    const data = await call(e, "read", { article_id: id, revision }); article = data.article; revisionNext = undefined;
    el.article.hidden = false; el.title.textContent = article.title;
    el.provenance.textContent = [w().id + ": " + article.id, w().revision + ": " + article.revision, article.language, article.event_name, article.historical ? w().historical : "", eventTime(article.event_starts_at, article.event_timezone), eventTime(article.event_ends_at, article.event_timezone), article.archived_at ? w().historical + ": " + eventTime(article.archived_at, article.event_timezone) : ""].filter(Boolean).join(" · ");
    el.body.textContent = data.body; sources(el.sources, article.sources); el.revisions.replaceChildren();
    await capabilitiesFor(e, article.event_id); controls();
  }
  async function revisions(e, before) {
    const data = await call(e, "revisions", { article_id: article.id, limit: 10, ...(before ? { before } : {}) }); revisionNext = data.next; el.revisions.replaceChildren();
    for (const r of data.items || []) { const row = node("p"); row.append(button(w().revision + " " + r.revision + " · " + r.change_kind + " · " + r.reason, e => read(e, article.id, r.revision))); el.revisions.append(row); }
  }
  function addSource(value = { label: "", url: "" }) {
    if (sourceRows.length >= 16) throw { local: "invalid" };
    const row = node("div"), label = node("input"), url = node("input"); label.value = value.label; url.value = value.url;
    const labelWrap = node("label", w().sourceLabel), urlWrap = node("label", w().sourceURL); labelWrap.append(label); urlWrap.append(url);
    const entry = { row, label, url }; row.append(labelWrap, urlWrap, button(w().remove, async () => { sourceRows = sourceRows.filter(v => v !== entry); row.remove(); }));
    sourceRows.push(entry); el["source-inputs"].append(row);
  }
  async function editor(e, kind, current) {
    if (pending || blocked) return; const f = filters();
    editing = { kind, article_id: current?.id, expected_revision: current?.revision || 0, event_id: current?.event_id || f.event_id };
    await capabilitiesFor(e, editing.event_id); el.editor.hidden = false;
    for (const [field, value] of Object.entries({ slug: current?.slug || "", name: current?.title || "", language: current?.language || locale, topics: (current?.topics || []).join("\n"), text: current ? el.body.textContent : "", reason: "" })) el[field].value = value;
    sourceRows = []; el["source-inputs"].replaceChildren(); for (const source of current?.sources || []) addSource(source);
  }
  function content() {
    const body = el.text.value;
    if (!body.trim() || bytes(body) > 96 * 1024 || !el.name.value.trim() || !el.slug.value.trim() || !el.reason.value.trim()) throw { local: "invalid" };
    return { slug: el.slug.value, title: el.name.value, language: el.language.value, topics: el.topics.value.split(/\r?\n/).filter(v => v.trim()), body, sources: sourceRows.filter(v => v.label.value || v.url.value).map(v => ({ label: v.label.value, url: v.url.value })) };
  }
  async function begin(e, kind, arguments_) {
    if (blocked || pending) return;
    pending = { owner, kind, operation_id: crypto.randomUUID(), arguments: arguments_, phase: "preparing" }; save(); consentOff(); await prepare(e);
  }
  async function prepare(e) {
    consentOff();
    const firstAttempt = !pending.preparation_attempted && !pending.preparation_uncertain;
    pending.preparation_attempted = true; save();
    let data;
    try { data = await call(e, pending.kind + "/prepare", { operation_id: pending.operation_id, arguments: pending.arguments }); }
    catch (error) {
      if (error.stale) throw error;
      // A first, definitive preparation rejection created no preparation. Earlier
      // uncertain attempts must keep their original recovery identity.
      if ([400, 403, 404, 409, 413, 422].includes(error.status) && firstAttempt) {
        try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
        pending = undefined; consentOff(); throw { local: error.status === 400 || error.status === 422 || error.status === 413 ? "invalid" : "rejectedPreparation" };
      }
      pending.preparation_uncertain = true; save(); throw error;
    }
    pending = { ...pending, arguments: data.arguments, proposal_id: data.preconditions.proposal_id, expires_at: data.expires_at, description: data.description, phase: "prepared" }; save(); paintPreview();
  }
  function paintPreview() {
    consentOff(); el.preview.replaceChildren();
    if (!pending?.proposal_id) return;
    el.preview.append(node("p", pending.description), node("p", w().until + ": " + pending.expires_at), node("p", w().reason + ": " + pending.arguments.reason));
    const args = pending.arguments;
    if (args.content) {
      el.preview.append(node("p", w().event + ": " + (args.event_id || w().general)),
        node("p", w().id + ": " + args.article_id + " · " + w().based + ": " + args.expected_revision),
        node("p", w()["slug-label"] + ": " + args.content.slug));
      el.preview.append(node("h2", args.content.title), node("p", args.content.language + " · " + args.content.topics.join(", ")), node("pre", args.content.body));
      for (const source of args.content.sources) el.preview.append(node("p", source.label + " · " + source.url));
    } else {
      el.preview.append(node("p", w().suggestionID + ": " + args.suggestion_id + " · " + (w()[args.decision === "approve" ? "approved" : "rejected"]) + " · " + w().based + ": " + args.expected_revision));
    }
    previewReady = !terminalExpired && Date.parse(pending.expires_at) > Date.now(); status(terminalExpired ? "expired" : previewReady ? "prepared" : "deadline");
  }
  function receipt(result) {
    el.receipt.replaceChildren(node("p", w().completed));
    if (result.article) el.receipt.append(node("p", (result.article.title || w().id) + ": " + result.article.id + " · " + w().revision + ": " + result.article.revision));
    if (result.suggestion) el.receipt.append(node("p", w().suggestionID + ": " + result.suggestion.id + " · " + (w()[result.suggestion.state === "pending" ? "pendingState" : result.suggestion.state] || result.suggestion.state)));
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
    pending = undefined; consentOff(); el.preview.replaceChildren(); el.editor.hidden = true; status("completed");
  }
  function adoptPreparation(data, phase) {
    const proposal = data?.preconditions?.proposal_id;
    if (!uuid(proposal) || !data.arguments || typeof data.arguments !== "object" || !Number.isFinite(Date.parse(data.expires_at)) || (pending.proposal_id && pending.proposal_id !== proposal)) throw { local: "unknown" };
    pending = { ...pending, arguments: data.arguments, proposal_id: proposal, description: data.description, expires_at: data.expires_at, phase }; save();
  }
  async function recover(e) {
    consentOff(); terminalExpired = false;
    const result = await call(e, pending.kind + "/status", { operation_id: pending.operation_id });
    if (result.operation_id !== pending.operation_id) throw { local: "unknown" };
    if (result.status === "completed" && result.result) { receipt(result.result); return; }
    if (result.status === "prepared" || result.status === "expired") {
      adoptPreparation(result.preparation, result.status);
      terminalExpired = result.status === "expired";
      if (pending.kind === "review" && !terminalExpired) await readSuggestion(e, pending.arguments.suggestion_id);
      paintPreview(); return;
    }
    // An unknown result does not authorize another mutation or trust an old preview.
    status("noReceipt");
  }
  async function confirm(e) {
    if (!pending?.proposal_id || !previewReady || terminalExpired || !el.consent.checked || Date.parse(pending.expires_at) <= Date.now()) return;
    pending.phase = "confirming"; save(); consentOff();
    const result = await call(e, pending.kind + "/confirm", { operation_id: pending.operation_id, arguments: pending.arguments, proposal_id: pending.proposal_id, confirmed: true }); receipt(result);
  }
  async function suggestions(e, mode, after) {
    suggestionMode = mode; const data = await call(e, mode, { limit: 10, ...(filters().event_id ? { event_id: filters().event_id } : {}), ...(after ? { after } : {}) });
    suggestionNext = data.next; el.suggestions.replaceChildren(); if (!data.items?.length) el.suggestions.textContent = w().empty;
    for (const s of data.items || []) el.suggestions.append(button(s.content.title + " · " + (w()[s.state === "pending" ? "pendingState" : s.state] || s.state), e => readSuggestion(e, s.id)));
  }
  async function readSuggestion(e, id) {
    const data = await call(e, "suggestion", { suggestion_id: id }); suggestion = data.suggestion; el.suggestion.hidden = false; el.review.hidden = true;
    el.suggestion.replaceChildren(node("h2", suggestion.content.title), node("p", [w().suggestionID + ": " + suggestion.id, w().author + ": " + suggestion.proposer_id, w().based + ": " + suggestion.base_revision, suggestion.state, suggestion.reason, suggestion.decision_reason, suggestion.reviewer_id ? w().reviewer + ": " + suggestion.reviewer_id : ""].filter(Boolean).join(" · ")), node("h2", w().proposedText), node("p", suggestion.content.language + " · " + (suggestion.content.topics || []).join(", ")), node("pre", data.body));
    el.suggestion.append(node("p", w().event + ": " + (suggestion.event_id || w().general)),
      node("p", w().id + ": " + suggestion.article_id),
      node("p", w()["slug-label"] + ": " + suggestion.content.slug));
    for (const source of suggestion.content.sources || []) el.suggestion.append(node("p", source.label + " · " + source.url));
    let current;
    try { current = await call(e, "read", { article_id: suggestion.article_id }); }
    catch (error) { if (error.status !== 404 || suggestion.base_revision !== 0) throw error; }
    suggestion.expected_revision = current?.article.revision || 0;
    if (current) {
      const c = current.article;
      el.suggestion.append(node("h2", w().currentText + " · " + w().revision + " " + c.revision),
        node("p", c.title + " · " + c.language + " · " + (c.topics || []).join(", ")),
        node("p", w().event + ": " + (c.event_id || w().general)),
        node("p", w().id + ": " + c.id), node("p", w()["slug-label"] + ": " + c.slug), node("pre", current.body));
      for (const source of c.sources || []) el.suggestion.append(node("p", source.label + " · " + source.url));
    }
    await capabilitiesFor(e, suggestion.event_id);
    el.review.hidden = suggestion.state !== "pending" || suggestion.proposer_id === owner || !(suggestion.event_id ? capabilities.can_edit : capabilities.moderator);
    el["review-reason"].value = "";
  }
  function clear() {
    epoch++; owner = undefined; busy = false; blocked = false; pending = undefined; terminalExpired = false; article = undefined; suggestion = undefined; editing = undefined; capabilities = {}; next = undefined; revisionNext = undefined; suggestionNext = undefined; sourceRows = []; pageFilters = undefined; eventNext = undefined;
    consentOff(); section.hidden = true;
    for (const id of ["results", "body", "title", "provenance", "sources", "revisions", "suggestions", "suggestion", "preview", "receipt", "status", "source-inputs", "recent-list"]) el[id].replaceChildren();
    for (const id of ["article", "editor", "review"]) el[id].hidden = true;
    for (const id of ["query", "slug", "name", "topics", "text", "reason", "review-reason"]) el[id].value = "";
  }
  el["events-next"].addEventListener("click", () => run(e => overview(e, eventNext)));
  el.recent.addEventListener("click", () => run(recent));
  el.search.addEventListener("click", () => run(e => list(e)));
  el.next.addEventListener("click", () => run(e => list(e, next)));
  el.collection.addEventListener("change", () => run(async e => { next = undefined; suggestionNext = undefined; el.suggestions.replaceChildren(); article = undefined; suggestion = undefined; el.article.hidden = true; el.editor.hidden = true; el.suggestion.hidden = true; el.review.hidden = true; await capabilitiesFor(e, filters().event_id); await list(e); }));
  el.new.addEventListener("click", () => run(e => editor(e, "suggest")));
  el["new-edit"].addEventListener("click", () => run(e => editor(e, "edit")));
  el.correct.addEventListener("click", () => run(e => editor(e, "suggest", article)));
  el.edit.addEventListener("click", () => run(e => editor(e, "edit", article)));
  el.current.addEventListener("click", () => run(e => read(e, article.id)));
  el.history.addEventListener("click", () => run(e => revisions(e)));
  el["history-next"].addEventListener("click", () => run(e => revisions(e, revisionNext)));
  el["source-add"].addEventListener("click", () => run(async () => addSource()));
  el["editor-close"].addEventListener("click", () => { if (!busy && !locked) el.editor.hidden = true; });
  el.prepare.addEventListener("click", () => run(e => { const { kind, ...args } = editing; return begin(e, kind, { ...args, content: content(), reason: el.reason.value }); }));
  el.mine.addEventListener("click", () => run(e => suggestions(e, "my_suggestions")));
  el.pending.addEventListener("click", () => run(e => suggestions(e, "pending_suggestions")));
  el["suggestions-next"].addEventListener("click", () => run(e => suggestions(e, suggestionMode, suggestionNext)));
  for (const [id, decision] of [["approve", "approve"], ["reject", "reject"]]) el[id].addEventListener("click", () => run(e => { if (!el["review-reason"].value.trim()) throw { local: "invalid" }; return begin(e, "review", { suggestion_id: suggestion.id, expected_revision: suggestion.expected_revision, decision, reason: el["review-reason"].value }); }));
  el.consent.addEventListener("change", controls);
  el["close-expired"].addEventListener("click", () => run(async () => {
    if (!pending || !terminalExpired) return;
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "storage" }; }
    pending = undefined; terminalExpired = false; consentOff(); el.preview.replaceChildren(); el.status.textContent = "";
  }));
  el.confirm.addEventListener("click", () => run(confirm)); el.recover.addEventListener("click", () => run(recover)); el.retry.addEventListener("click", () => run(prepare));
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(value) { locale = /^(ru|be|uk)(-|$)/i.test(value) ? "ru" : "en"; consentOff(); labels(); },
    async start(id, language) {
      clear(); if (!uuid(id)) return; owner = id; locale = /^(ru|be|uk)(-|$)/i.test(language) ? "ru" : "en"; labels(); section.hidden = false; const e = epoch;
      try {
        const raw = sessionStorage.getItem(key());
        if (raw) { if (bytes(raw) > 1024 * 1024) throw { local: "storage" }; pending = JSON.parse(raw); if (pending.owner !== owner || !["suggest", "edit", "review"].includes(pending.kind) || !uuid(pending.operation_id) || !pending.arguments || !["preparing", "locating", "prepared", "confirming", "expired"].includes(pending.phase) || (!["preparing", "locating"].includes(pending.phase) && (!uuid(pending.proposal_id) || !Number.isFinite(Date.parse(pending.expires_at))))) throw { local: "storage" }; }
      } catch { blocked = true; status("storage"); }
      try { await overview(e); await capabilitiesFor(e); await list(e); if (pending && !blocked) { await recover(e); if (pending && !terminalExpired && previewReady) status("restored"); } }
      catch (error) { if (e === epoch) failure(error); } controls();
    }
  };
};
