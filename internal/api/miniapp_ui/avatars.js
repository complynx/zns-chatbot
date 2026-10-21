"use strict";

// Draft state is shared with the agent. Only operation metadata survives reload;
// image bytes stay in memory or on the authenticated server.
window.createZinusyaAvatars = function (env) {
  const names = ["avatars", "title", "note", "status", "file-label", "file", "upload", "history-note", "history-list", "history-label", "history", "history-preview", "history-image", "history-import", "history-next", "draft-label", "draft", "editor", "local", "canvas", "form", "x-label", "x", "y-label", "y", "zoom-label", "zoom", "angle-label", "angle", "reset", "save", "preview", "saved-preview", "reload", "recover", "retry", "generate", "documents-label", "documents", "document-status", "refresh-documents", "download"];
  const el = Object.fromEntries(names.map(id => [id, document.getElementById(id === "avatars" ? id : "avatars-" + id)]));
  const words = {
    en: { title: "Avatar", note: "Save a draft here or continue with Zinusya. Saving does not send an avatar. Up to five drafts are kept. Telegram photos are downloaded on demand; browser uploads use up to 50 MiB total. Automatic deletion is not available yet.", "file-label": "JPEG or PNG, up to 20 MiB", upload: "Upload a new original", "draft-label": "Saved draft", local: "Local approximation — drag the photo or use the fields. Preview the saved version to check the server result.", "x-label": "Horizontal offset (%)", "y-label": "Vertical offset (%)", "zoom-label": "Scale (%)", "angle-label": "Clockwise rotation (degrees)", reset: "Reset crop", save: "Save edits", preview: "Preview saved version", reload: "Reload saved state (discard local edits)", recover: "Check earlier request", retry: "Retry the same request", choose: "Choose a draft", empty: "No saved drafts. Upload an original to begin.", ready: "Saved draft loaded", edited: "Unsaved edits", saved: "Request confirmed; current saved state loaded", unknown: "The earlier request outcome is unknown. Check it or retry the same request before continuing. For an upload, reselect the same original if needed.", conflict: "The draft changed or the request was rejected. Your edits remain here. Reload saved state before continuing.", sourceUnavailable: "The source photo is unavailable. Send it again in Telegram or upload another photo here. Created documents remain available below.", unavailable: "Could not finish. Check any earlier request before retrying.", invalid: "Check the image and transform values.", mismatch: "Reselect the exact original used for the earlier upload.", storage: "Recovery storage is unavailable. No new request was sent.", blocked: "Recovery data is invalid. New changes are blocked on this page.", previewed: "Saved-version server preview loaded", canvas: "Local avatar preview", image: "Saved avatar preview", version: "Version" },
    ru: { title: "Аватарка", note: "Сохрани черновик здесь или продолжи с Зинусей. Сохранение не отправляет аватарку. Хранятся до пяти черновиков. Фото из Telegram скачиваются по запросу; загрузки через браузер занимают до 50 МиБ. Автоматического удаления ещё нет.", "file-label": "JPEG или PNG, до 20 МиБ", upload: "Загрузить новый оригинал", "draft-label": "Сохранённый черновик", local: "Локальное приближение — перетаскивай фото или используй поля. Проверь результат сервера через просмотр сохранённой версии.", "x-label": "Сдвиг по горизонтали (%)", "y-label": "Сдвиг по вертикали (%)", "zoom-label": "Масштаб (%)", "angle-label": "Поворот по часовой стрелке (градусы)", reset: "Сбросить кадрирование", save: "Сохранить изменения", preview: "Посмотреть сохранённую версию", reload: "Загрузить сохранённое (сбросить местные правки)", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", choose: "Выбери черновик", empty: "Нет черновиков. Загрузи оригинал, чтобы начать.", ready: "Сохранённый черновик загружен", edited: "Есть несохранённые изменения", saved: "Запрос подтверждён; загружено текущее сохранённое состояние", unknown: "Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос. При необходимости снова выбери тот же оригинал для загрузки.", conflict: "Черновик изменился или запрос отклонён. Твои правки остались здесь. Загрузи сохранённое состояние для продолжения.", sourceUnavailable: "Исходное фото недоступно. Пришли его заново в Telegram или загрузи другое фото здесь. Готовые документы доступны ниже.", unavailable: "Не удалось завершить. Проверь прежний запрос перед повтором.", invalid: "Проверь изображение и параметры кадрирования.", mismatch: "Выбери точно тот же оригинал, что и для прежней загрузки.", storage: "Хранилище восстановления недоступно. Новый запрос не отправлен.", blocked: "Данные восстановления повреждены. Новые изменения на этой странице заблокированы.", previewed: "Загружен серверный просмотр сохранённой версии", canvas: "Локальный просмотр аватарки", image: "Сохранённая версия аватарки", version: "Версия" }
  };
  Object.assign(words.en, { generate: "Generate and send", "documents-label": "Created documents", "refresh-documents": "Refresh delivery status", download: "Download JPEG", finalized: "Document created. Its Telegram delivery status is shown below.", already: "This saved version already has a document. Download it or check its delivery status below.", finalizeRejected: "Creation was rejected: the draft may have changed or the twenty-document limit was reached. Reload saved state and check created documents.", downloaded: "JPEG download started. This does not change Telegram delivery.", noDocuments: "No created documents.", pending: "Created; waiting to send to Telegram.", retryable: "Waiting for another safe Telegram send attempt.", send_inflight: "Sending to Telegram. Do not generate a replacement to retry.", delivered: "Sent to Telegram.", delivery_unknown: "Telegram delivery is uncertain. Check the chat; ask an administrator if a resend is needed.", delivery_failed: "Telegram delivery failed. You can download the document; ask an administrator about retrying.", delivery_blocked: "Telegram delivery is blocked. You can download the document; an administrator can inspect the cause.", dismissed: "Telegram delivery was closed. The document is still available to download." });
  Object.assign(words.ru, { generate: "Сформировать и отправить", "documents-label": "Готовые документы", "refresh-documents": "Обновить статус отправки", download: "Скачать JPEG", finalized: "Документ создан. Статус отправки в Telegram показан ниже.", already: "Для этой сохранённой версии уже есть документ. Скачай его или проверь статус отправки ниже.", finalizeRejected: "Создание отклонено: черновик мог измениться или достигнут лимит двадцати документов. Загрузи сохранённое состояние и проверь готовые документы.", downloaded: "Скачивание JPEG началось. Это не меняет отправку в Telegram.", noDocuments: "Готовых документов пока нет.", pending: "Документ создан и ожидает отправки в Telegram.", retryable: "Ожидается безопасная повторная попытка отправки в Telegram.", send_inflight: "Отправляется в Telegram. Не создавай новый документ для повтора.", delivered: "Отправлено в Telegram.", delivery_unknown: "Результат отправки в Telegram неизвестен. Проверь чат; если нужен повтор, обратись к администратору.", delivery_failed: "Не удалось отправить в Telegram. Документ можно скачать; о повторе спроси администратора.", delivery_blocked: "Отправка в Telegram заблокирована. Документ можно скачать; причину может проверить администратор.", dismissed: "Отправка в Telegram закрыта. Документ по-прежнему можно скачать." });
  Object.assign(words.en, { "history-note": "Imported attachments are checked when selected. Some may be unavailable or unsupported.", "history-list": "Find imported attachments", "history-label": "Imported attachment", "history-preview": "Preview photo", "history-import": "Use this photo", historyImage: "Imported photo preview", "history-next": "Next attachments", historyItem: "Imported file", historyEmpty: "No imported attachments on this page.", historyReady: "Preview an imported photo before creating a draft.", historyPreviewed: "Preview only; no draft has been created. Use this photo to start editing.", historyRejected: "Could not use this attachment. It may be unsupported or unavailable; send a photo again or upload one here." });
  Object.assign(words.ru, { "history-note": "Импортированные вложения проверяются при выборе. Некоторые могут быть недоступны или иметь неподдерживаемый формат.", "history-list": "Найти импортированные вложения", "history-label": "Импортированное вложение", "history-preview": "Посмотреть фото", "history-import": "Использовать это фото", historyImage: "Просмотр импортированного фото", "history-next": "Следующие вложения", historyItem: "Импортированный файл", historyEmpty: "На этой странице нет импортированных вложений.", historyReady: "Посмотри импортированное фото перед созданием черновика.", historyPreviewed: "Это только просмотр, черновик ещё не создан. Используй это фото для редактирования.", historyRejected: "Не удалось использовать вложение: формат не поддерживается или файл недоступен. Пришли фото заново или загрузи его здесь." });
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const digest = v => typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
  const identity = () => ({ offset_x_ppm: 0, offset_y_ppm: 0, zoom_ppm: 1000000, angle_millidegrees: 0 });
  const copy = x => JSON.parse(JSON.stringify(x));
  const validTransform = t => t && Object.keys(t).sort().join() === "angle_millidegrees,offset_x_ppm,offset_y_ppm,zoom_ppm" && Object.values(t).every(Number.isSafeInteger) && Math.abs(t.offset_x_ppm) <= 65536000000 && Math.abs(t.offset_y_ppm) <= 65536000000 && t.zoom_ppm >= 250000 && t.zoom_ppm <= 8000000 && t.angle_millidegrees >= -180000 && t.angle_millidegrees <= 180000;
  const validDraft = d => d && uuid(d.id) && Number.isSafeInteger(d.version) && d.version > 0 && digest(d.source_sha256) && digest(d.frame_sha256) && validTransform(d.transform);
  let owner, language = "en", epoch = 0, locked = false, working = false, blocked = false, conflict = false, dirty = false;
  let draft, pending, original, overlay, previewURL, drag, statusKey = "empty", documents = [], documentsLoaded = false, documentURL, history = [], historyNext, historyPreviewURL, historyPreviewSource;
  const w = () => words[language];
  const key = () => "zinusya-avatar-pending-v1:" + owner;
  const path = id => "./avatars/drafts/" + id;
  function status(k) { statusKey = k; el.status.textContent = w()[k]; }
  function labels() {
    for (const id of ["title", "note", "file-label", "upload", "history-note", "history-list", "history-label", "history-preview", "history-import", "history-next", "draft-label", "local", "x-label", "y-label", "zoom-label", "angle-label", "reset", "save", "preview", "reload", "recover", "retry", "generate", "documents-label", "refresh-documents", "download"]) el[id].textContent = w()[id];
    for (const option of el.draft.options || []) option.textContent = option.textContent.replace(/ · (Version|Версия) /, " · " + w().version + " ");
    el.canvas.setAttribute("aria-label", w().canvas); el["saved-preview"].alt = w().image;
    el["history-image"].alt = w().historyImage; paintDocuments(); paintHistory(); status(statusKey);
  }
  function dropPreview() {
    if (previewURL) URL.revokeObjectURL(previewURL); previewURL = undefined;
    el["saved-preview"].hidden = true; el["saved-preview"].removeAttribute("src");
  }
  function dropHistoryPreview() {
    if (historyPreviewURL) URL.revokeObjectURL(historyPreviewURL);
    historyPreviewURL = historyPreviewSource = undefined;
    el["history-image"].hidden = true; el["history-image"].removeAttribute("src");
  }
  const historyLocator = source => source && source.archive_id + ":" + source.record_id;
  const selectedHistory = () => history.find(item => item.record_id === el.history.value);
  function dropDocumentURL() { if (documentURL) URL.revokeObjectURL(documentURL); documentURL = undefined; }
  const validReceipt = r => r && uuid(r.artifact_id) && uuid(r.delivery_id) && uuid(r.draft_id) && Number.isSafeInteger(r.draft_version) && r.draft_version > 0 && digest(r.source_sha256) && digest(r.frame_sha256) && digest(r.sha256) && validTransform(r.transform) && r.width === 1024 && r.height === 1024 && Number.isSafeInteger(r.size_bytes) && r.size_bytes > 0 && r.size_bytes <= 4 * 1024 * 1024 && Number.isFinite(Date.parse(r.created_at));
  const states = ["pending", "retryable", "send_inflight", "delivered", "delivery_unknown", "delivery_failed", "delivery_blocked", "dismissed"];
  const selectedDocument = () => documents.find(v => v.avatar.artifact_id === el.documents.value);
  const generated = () => draft && documents.some(v => v.avatar.draft_id === draft.id && v.avatar.draft_version === draft.version);
  function documentStatus() { el["document-status"].textContent = w()[selectedDocument()?.delivery_state] || w().noDocuments; }
  function paintDocuments(selected = el.documents.value) {
    const options = documents.map(v => { const n = document.createElement("option"); n.value = v.avatar.artifact_id; n.textContent = new Date(v.avatar.created_at).toLocaleString(language, { dateStyle: "medium", timeStyle: "short" }) + " · " + w().version + " " + v.avatar.draft_version; return n; });
    el.documents.replaceChildren(...options); el.documents.value = documents.some(v => v.avatar.artifact_id === selected) ? selected : documents[0]?.avatar.artifact_id || ""; documentStatus();
  }
  async function refreshDocuments(e, selected) {
    documentsLoaded = false; controls();
    const page = await call(e, "./avatars/finalized");
    if (!Array.isArray(page?.avatars) || page.avatars.length > 20 || !page.avatars.every(v => validReceipt(v.avatar) && states.includes(v.delivery_state) && Number.isSafeInteger(v.delivery_version) && v.delivery_version > 0)) throw { local: "invalid" };
    dropDocumentURL(); documents = page.avatars; documentsLoaded = true; paintDocuments(selected);
  }
  function dropImages() { original?.close(); overlay?.close(); original = overlay = undefined; dropPreview(); }
  function controls() {
    const busy = locked || working || !owner, edit = busy || blocked || !!pending || conflict || !draft || !original;
    for (const id of ["x", "y", "zoom", "angle", "reset"]) el[id].disabled = edit;
    el.save.disabled = edit || !dirty; el.preview.disabled = edit || dirty;
    el.generate.disabled = edit || dirty || !previewURL || !documentsLoaded || !!generated();
    el.documents.disabled = busy || !documents.length;
    el["refresh-documents"].disabled = busy || blocked;
    el.download.disabled = busy || !selectedDocument();
    el.file.disabled = busy || blocked || dirty || (!!pending && pending.kind !== "upload");
    el.upload.disabled = busy || blocked || !!pending || dirty || !el.file.files?.length;
    const historyBusy = busy || blocked || !!pending || dirty;
    el["history-list"].disabled = historyBusy;
    el.history.disabled = el["history-preview"].disabled = historyBusy || !history.length;
    el["history-import"].disabled = historyBusy || !historyPreviewSource || historyPreviewSource !== historyLocator(selectedHistory());
    el["history-next"].hidden = !historyNext; el["history-next"].disabled = historyBusy;
    el.draft.disabled = busy || blocked || !!pending || dirty;
    el.reload.disabled = busy || blocked || !!pending;
    el.recover.hidden = el.retry.hidden = !pending;
    el.recover.disabled = el.retry.disabled = busy || blocked;
  }
  function clear() {
    epoch++; dropImages(); dropDocumentURL(); dropHistoryPreview(); documents = []; documentsLoaded = false; el.documents.replaceChildren(); documentStatus(); history = []; historyNext = undefined; el.history.replaceChildren(); draft = pending = owner = drag = undefined;
    blocked = conflict = dirty = working = false;
    el.avatars.hidden = el.editor.hidden = true; el.draft.replaceChildren(); el.file.value = "";
    el.canvas.getContext("2d").clearRect(0, 0, 1024, 1024); status("empty"); controls();
  }
  async function check(e, promise) {
    try { const result = await promise; if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  async function call(e, url, method = "GET", body) { return check(e, env.request(url, method, body)); }
  async function sha(e, blob) {
    const bytes = await check(e, blob.arrayBuffer());
    const hash = await check(e, crypto.subtle.digest("SHA-256", bytes));
    return Array.from(new Uint8Array(hash), b => b.toString(16).padStart(2, "0")).join("");
  }
  function onError(error) {
    el.draft.value = draft?.id || "";
    if (error.status === 401 || error.status === 403) { env.setBusy(false); env.denied(); return; }
    status(w()[error.local] ? error.local : error.status === 409 ? "conflict" : "unavailable");
  }
  async function run(action) {
    if (working || locked || !owner || blocked) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) onError(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  function transform() {
    const values = ["x", "y", "zoom", "angle"].map(id => el[id].value === "" ? NaN : Number(el[id].value));
    const t = { offset_x_ppm: Math.round(values[0] * 10000), offset_y_ppm: Math.round(values[1] * 10000), zoom_ppm: Math.round(values[2] * 10000), angle_millidegrees: Math.round(values[3] * 1000) };
    if (!validTransform(t)) throw { local: "invalid" };
    if (t.angle_millidegrees === 180000) t.angle_millidegrees = -180000;
    return t;
  }
  function fields(t) { el.x.value = t.offset_x_ppm / 10000; el.y.value = t.offset_y_ppm / 10000; el.zoom.value = t.zoom_ppm / 10000; el.angle.value = t.angle_millidegrees / 1000; }
  function paint() {
    if (!original || !overlay) return;
    const t = transform(), ctx = el.canvas.getContext("2d"), size = 1024;
    ctx.fillStyle = "white"; ctx.fillRect(0, 0, size, size); ctx.save();
    ctx.translate(size / 2 + size * t.offset_x_ppm / 1000000, size / 2 + size * t.offset_y_ppm / 1000000);
    ctx.rotate(t.angle_millidegrees * Math.PI / 180000);
    const scale = Math.max(size / original.width, size / original.height) * t.zoom_ppm / 1000000;
    ctx.scale(scale, scale); ctx.drawImage(original, -original.width / 2, -original.height / 2); ctx.restore();
    const side = Math.min(overlay.width, overlay.height);
    ctx.drawImage(overlay, (overlay.width - side) / 2, (overlay.height - side) / 2, side, side, 0, 0, size, size);
  }
  function edited() {
    if (el.x.disabled) return;
    dirty = true; dropPreview(); status("edited");
    try { paint(); } catch (error) { onError(error); } controls();
  }
  async function bitmap(e, blob) {
    const image = await createImageBitmap(blob, { imageOrientation: "from-image" });
    if (e !== epoch) { image.close(); throw { stale: true }; }
    return image;
  }
  function unavailableSource(next = draft) {
    dropImages(); draft = next; dirty = conflict = false;
    if (next) { fields(next.transform); el.draft.value = next.id; }
    el.editor.hidden = true; status("sourceUnavailable"); return false;
  }
  async function load(e, id) {
    const next = await call(e, path(id)); if (!validDraft(next)) throw { local: "invalid" };
    let raw;
    try { raw = await check(e, env.readBlob(path(id) + "/original")); }
    catch (error) {
      if (error.status !== 422) throw error;
      return unavailableSource(next);
    }
    if (await sha(e, raw) !== next.source_sha256) throw { local: "invalid" };
    const frame = await check(e, env.readBlob("./avatars/frame/" + next.frame_sha256));
    if (await sha(e, frame) !== next.frame_sha256) throw { local: "invalid" };
    let photo, border;
    try { photo = await bitmap(e, raw); border = await bitmap(e, frame); }
    catch (error) { photo?.close(); border?.close(); throw error; }
    dropImages(); original = photo; overlay = border; draft = next;
    fields(next.transform); dirty = conflict = false; el.draft.value = next.id; el.editor.hidden = false; paint(); status("ready"); return true;
  }
  async function list(e, selected) {
    const page = await call(e, "./avatars/drafts");
    if (!Array.isArray(page?.drafts) || page.drafts.length > 5 || !page.drafts.every(validDraft)) throw { local: "invalid" };
    const options = page.drafts.map((d, i) => { const n = document.createElement("option"); n.value = d.id; n.textContent = (i + 1) + " · " + w().version + " " + d.version; return n; });
    const id = page.drafts.find(d => d.id === selected)?.id || page.drafts[0]?.id;
    el.draft.replaceChildren(...options); el.draft.value = id || "";
    if (id) return load(e, id);
    dropImages(); draft = undefined; el.editor.hidden = true; status("empty"); return true;
  }
  function paintHistory() {
    const selected = el.history.value;
    const options = history.map((item, i) => { const option = document.createElement("option"); option.value = item.record_id; option.textContent = w().historyItem + " " + (i + 1); return option; });
    el.history.replaceChildren(...options); el.history.value = history.some(item => item.record_id === selected) ? selected : history[0]?.record_id || "";
  }
  async function listHistory(e, after) {
    dropHistoryPreview();
    const page = await call(e, "./avatars/history" + (after ? "?after=" + after : ""));
    if (!Array.isArray(page?.items) || page.items.length > 20 || !page.items.every(item => uuid(item.record_id) && uuid(item.archive_id)) || typeof page.has_more !== "boolean" || (page.has_more && (!page.items.length || !uuid(page.next_id) || page.next_id !== page.items.at(-1).record_id || page.next_id === after))) throw { local: "invalid" };
    history = page.items; historyNext = page.has_more ? page.next_id : undefined; paintHistory(); status(history.length ? "historyReady" : "historyEmpty");
  }
  function validPending(p) {
    if (!p || p.owner !== owner || !uuid(p.operation_id)) return false;
    if (p.kind === "history") return Object.keys(p).sort().join() === "body,kind,operation_id,owner" && p.body && Object.keys(p.body).sort().join() === "history_source,operation_id" && p.body.operation_id === p.operation_id && p.body.history_source && Object.keys(p.body.history_source).sort().join() === "archive_id,record_id" && uuid(p.body.history_source.record_id) && uuid(p.body.history_source.archive_id);
    if (p.kind === "upload") return Object.keys(p).sort().join() === "kind,operation_id,owner,sha256,size" && digest(p.sha256) && Number.isSafeInteger(p.size) && p.size > 0 && p.size <= 20 * 1024 * 1024;
    if (p.kind === "finalize") return Object.keys(p).sort().join() === "body,draft_id,kind,operation_id,owner" && uuid(p.draft_id) && p.body && Object.keys(p.body).sort().join() === "expected_version,operation_id" && p.body.operation_id === p.operation_id && Number.isSafeInteger(p.body.expected_version) && p.body.expected_version > 0;
    return p.kind === "save" && Object.keys(p).sort().join() === "body,draft_id,kind,operation_id,owner" && uuid(p.draft_id) && p.body && Object.keys(p.body).sort().join() === "expected_version,operation_id,transform" && p.body.operation_id === p.operation_id && Number.isSafeInteger(p.body.expected_version) && p.body.expected_version > 0 && validTransform(p.body.transform);
  }
  function remember(record) {
    try { sessionStorage.setItem(key(), JSON.stringify(record)); } catch { throw { local: "storage" }; }
    pending = record; controls();
  }
  function forget() {
    try { sessionStorage.removeItem(key()); } catch { blocked = true; throw { local: "blocked" }; }
    pending = undefined;
  }
  async function confirmed(e, receipt) {
    if (pending.kind === "finalize") {
      if (!validReceipt(receipt) || receipt.draft_id !== pending.draft_id || receipt.draft_version !== pending.body.expected_version) throw { local: "invalid" };
      const id = receipt.artifact_id; forget(); await refreshDocuments(e, id); if (!draft) await list(e, receipt.draft_id); status("finalized"); return;
    }
    if (!validDraft(receipt) || (pending.kind === "save" && receipt.id !== pending.draft_id)) throw { local: "invalid" };
    if (pending.kind === "history") dropHistoryPreview();
    const id = receipt.id; forget(); el.file.value = "";
    if (await list(e, id)) status("saved");
  }
  async function recover(e) {
    if (!pending) return;
    try { const receipt = await call(e, (pending.kind === "finalize" ? "./avatars/finalizations/operations/" : "./avatars/operations/") + pending.operation_id); await confirmed(e, receipt); }
    catch (error) { if (error.status === 404) { status("unknown"); return; } throw error; }
  }
  async function dispatch(e) {
    const p = pending; if (!p) return;
    let result;
    try {
      if (p.kind === "upload") {
        const file = el.file.files?.[0];
        if (!file || file.size !== p.size || await sha(e, file) !== p.sha256) throw { local: "mismatch" };
        result = await check(e, env.upload(file, p.operation_id));
      } else if (p.kind === "history") result = await call(e, "./avatars/drafts", "POST", p.body);
      else result = await call(e, path(p.draft_id) + (p.kind === "finalize" ? "/finalize" : ""), "POST", p.body);
    } catch (error) {
      if (p.kind === "finalize" && error.status === 422) { forget(); unavailableSource(); return; }
      if ([400, 404, 409, 413, 422].includes(error.status)) { forget(); conflict = true; status(p.kind === "finalize" ? "finalizeRejected" : p.kind === "history" ? "historyRejected" : "conflict"); return; }
      throw error;
    }
    await confirmed(e, result);
  }
  el["history-list"].addEventListener("click", () => run(e => listHistory(e)));
  el["history-next"].addEventListener("click", () => run(e => listHistory(e, historyNext)));
  el.history.addEventListener("change", () => { dropHistoryPreview(); status("historyReady"); controls(); });
  el["history-preview"].addEventListener("click", () => run(async e => {
    dropHistoryPreview();
    const source = selectedHistory(); if (!source) return;
    let raw;
    try { raw = await check(e, env.readBlob("./avatars/history/" + source.archive_id + "/" + source.record_id + "/preview")); }
    catch (error) { if ([400, 404, 413, 422].includes(error.status)) { status("historyRejected"); return; } throw error; }
    if (raw.type !== "image/jpeg" || !raw.size || raw.size > 4 * 1024 * 1024) throw { local: "invalid" };
    const image = await bitmap(e, raw); image.close();
    historyPreviewURL = URL.createObjectURL(raw); historyPreviewSource = historyLocator(source);
    el["history-image"].src = historyPreviewURL; el["history-image"].hidden = false; status("historyPreviewed");
  }));
  el["history-import"].addEventListener("click", () => run(async e => {
    if (pending || dirty) return;
    const source = selectedHistory(); if (!source || historyPreviewSource !== historyLocator(source)) return;
    const body = { operation_id: crypto.randomUUID(), history_source: { record_id: source.record_id, archive_id: source.archive_id } };
    remember({ owner, kind: "history", operation_id: body.operation_id, body }); await dispatch(e);
  }));
  el.file.addEventListener("change", controls);
  el.upload.addEventListener("click", () => run(async e => {
    if (pending || dirty) return;
    const file = el.file.files?.[0];
    if (!file || !file.size || file.size > 20 * 1024 * 1024 || !["image/jpeg", "image/png", ""].includes(file.type)) throw { local: "invalid" };
    const hash = await sha(e, file);
    remember({ owner, kind: "upload", operation_id: crypto.randomUUID(), sha256: hash, size: file.size });
    await dispatch(e);
  }));
  el.form.addEventListener("submit", event => { event.preventDefault(); return run(async e => {
    if (!draft || pending || conflict || !dirty) return;
    const body = { operation_id: crypto.randomUUID(), expected_version: draft.version, transform: transform() };
    remember({ owner, kind: "save", operation_id: body.operation_id, draft_id: draft.id, body });
    await dispatch(e);
  }); });
  el.recover.addEventListener("click", () => run(recover)); el.retry.addEventListener("click", () => run(dispatch));
  el.reload.addEventListener("click", () => run(async e => { await list(e, draft?.id); await refreshDocuments(e); }));
  el.draft.addEventListener("change", () => { if (!pending && !dirty) return run(e => load(e, el.draft.value)); });
  el.reset.addEventListener("click", () => { if (!el.reset.disabled) { fields(identity()); edited(); } });
  for (const id of ["x", "y", "zoom", "angle"]) el[id].addEventListener("input", edited);
  el.preview.addEventListener("click", () => run(async e => {
    if (!draft || dirty || pending) return;
    let blob;
    try { blob = await check(e, env.readBlob(path(draft.id) + "/preview/" + draft.version)); }
    catch (error) { if (error.status === 422) { unavailableSource(); return; } throw error; }
    dropPreview(); previewURL = URL.createObjectURL(blob); el["saved-preview"].src = previewURL; el["saved-preview"].hidden = false; status("previewed");
  }));
  el.generate.addEventListener("click", () => run(async e => {
    if (!draft || pending || dirty || conflict || !previewURL || !documentsLoaded) return;
    await refreshDocuments(e);
    if (generated()) { status("already"); return; }
    const body = { operation_id: crypto.randomUUID(), expected_version: draft.version };
    remember({ owner, kind: "finalize", operation_id: body.operation_id, draft_id: draft.id, body });
    await dispatch(e);
  }));
  el["refresh-documents"].addEventListener("click", () => run(e => refreshDocuments(e)));
  el.documents.addEventListener("change", () => { if (!el.documents.disabled) { dropDocumentURL(); documentStatus(); controls(); } });
  el.download.addEventListener("click", () => run(async e => {
    const view = selectedDocument(); if (!view) return;
    const blob = await check(e, env.readBlob("./avatars/finalized/" + view.avatar.artifact_id + "/content"));
    if (blob.size !== view.avatar.size_bytes || await sha(e, blob) !== view.avatar.sha256) throw { local: "invalid" };
    dropDocumentURL(); documentURL = URL.createObjectURL(blob);
    const link = document.createElement("a"); link.href = documentURL; link.download = "zinusya-avatar.jpg"; link.click(); status("downloaded");
  }));
  el.canvas.addEventListener("pointerdown", event => {
    if (el.x.disabled) return;
    try { drag = { id: event.pointerId, x: event.clientX, y: event.clientY, transform: transform() }; el.canvas.setPointerCapture(event.pointerId); } catch { drag = undefined; }
  });
  el.canvas.addEventListener("pointermove", event => {
    if (!drag || event.pointerId !== drag.id || el.x.disabled) return;
    const rect = el.canvas.getBoundingClientRect(); if (!rect.width || !rect.height) return;
    const t = copy(drag.transform);
    t.offset_x_ppm = Math.max(-65536000000, Math.min(65536000000, t.offset_x_ppm + Math.round((event.clientX - drag.x) * 1000000 / rect.width)));
    t.offset_y_ppm = Math.max(-65536000000, Math.min(65536000000, t.offset_y_ppm + Math.round((event.clientY - drag.y) * 1000000 / rect.height)));
    fields(t); edited();
  });
  for (const name of ["pointerup", "pointercancel", "lostpointercapture"]) el.canvas.addEventListener(name, () => { drag = undefined; });
  return { clear, setBusy(value) { locked = value; controls(); }, setLanguage(locale) { language = locale === "ru" ? "ru" : "en"; labels(); },
    async start(id, locale) {
      clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); el.avatars.hidden = false;
      const e = epoch;
      try {
        const raw = sessionStorage.getItem(key());
        if (raw) { if (raw.length > 2048) throw new Error("Oversized recovery"); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error("Invalid recovery"); }
      } catch { blocked = true; status("blocked"); controls(); return; }
      try { if (pending) await recover(e); else await list(e); if (!documentsLoaded) await refreshDocuments(e); }
      catch (error) { if (e === epoch) onError(error); } controls();
    }
  };
};
