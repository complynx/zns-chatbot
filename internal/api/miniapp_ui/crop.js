"use strict";

// Canvas rendering and mouse/touch gestures are transferred from static/fit_frame.mjs.
// The adapter saves the transform; authenticated Go endpoints render and queue the JPEG.
window.createZinusyaAvatarCrop = function (env) {
  const el = Object.fromEntries(["avatar-crop", "crop-canvas", "crop-help", "crop-status", "crop-finish", "crop-back", "crop-recover", "crop-retry"].map(id => [id, document.getElementById(id)]));
  const canvas = el["crop-canvas"], ctx = canvas.getContext("2d"), app = window.Telegram?.WebApp;
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const digest = v => typeof v === "string" && /^[0-9a-f]{64}$/.test(v);
  const validTransform = t => t && Object.keys(t).sort().join() === "angle_millidegrees,offset_x_ppm,offset_y_ppm,zoom_ppm" && Object.values(t).every(Number.isSafeInteger) && Math.abs(t.offset_x_ppm) <= 65536000000 && Math.abs(t.offset_y_ppm) <= 65536000000 && t.zoom_ppm >= 250000 && t.zoom_ppm <= 8000000 && t.angle_millidegrees >= -180000 && t.angle_millidegrees <= 180000;
  const validDraft = d => d && uuid(d.id) && Number.isSafeInteger(d.version) && d.version > 0 && digest(d.source_sha256) && digest(d.frame_sha256) && validTransform(d.transform);
  const words = {
    en: { help: "Drag to move. Pinch with two fingers to zoom and rotate. Mouse: scroll to zoom; hold Shift for fine zoom or drag with Shift to rotate.", finish: "Finish", back: "Back", recover: "Check earlier request", retry: "Retry the same request", loading: "Loading photo…", ready: "Adjust the photo inside the frame.", saving: "Saving crop…", rendering: "Checking the saved crop…", queued: "Document created and queued for Telegram.", unknown: "The earlier request is uncertain. Check it before continuing.", conflict: "The saved photo changed. Close and reopen Crop to load its current version.", unavailable: "Could not finish. Check the earlier request before retrying.", missing: "This photo is unavailable. Send it again in Telegram.", blocked: "Earlier recovery data is unavailable or invalid. Open Avatar from the bot menu to check it.", invalid: "This crop is outside the supported zoom or movement range.", storage: "Could not store recovery data. Nothing was sent.", edited: "Adjust the crop, then press Finish." },
    ru: { help: "Перетаскивай фото. Двумя пальцами меняй масштаб и поворот. Мышь: колесо меняет масштаб; Shift — точный масштаб или поворот при перетаскивании.", finish: "Готово", back: "Назад", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", loading: "Загружаю фото…", ready: "Расположи фото внутри рамки.", saving: "Сохраняю кадрирование…", rendering: "Проверяю сохранённое кадрирование…", queued: "Документ создан и поставлен в очередь отправки в Telegram.", unknown: "Результат прежнего запроса неизвестен. Сначала проверь его.", conflict: "Сохранённое фото изменилось. Закрой и открой кадрирование заново.", unavailable: "Не удалось завершить. Проверь прежний запрос перед повтором.", missing: "Фото недоступно. Пришли его заново в Telegram.", blocked: "Данные прежнего запроса недоступны или повреждены. Открой аватарку из меню бота для проверки.", invalid: "Масштаб или сдвиг выходят за допустимые пределы.", storage: "Не удалось сохранить данные восстановления. Запрос не отправлен.", edited: "Настрой кадрирование и нажми «Готово»." }
  };
  let owner, draft, original, frame, pending, openPending, completed = false, blocked = false, conflict = false, generated = false;
  let epoch = 0, working = false, locked = false, locale = "en", phase = "loading";
  let W = 0, H = 0, frameSize = 0, left = 0, top = 0, matrix = [[1, 0, 0], [0, 1, 0]];
  let mouse, touches;
  const key = () => "zinusya-avatar-pending-v1:" + owner;
  const path = id => "./avatars/drafts/" + id;
  const w = () => words[locale];
  function status(value) { phase = value; el["crop-status"].textContent = w()[value]; }
  function labels() {
    el["crop-help"].textContent = w().help;
    for (const name of ["finish", "back", "recover", "retry"]) el["crop-" + name].textContent = w()[name];
    canvas.setAttribute("aria-label", locale === "ru" ? "Кадрирование аватарки" : "Avatar crop");
    app?.MainButton?.setText(w().finish); status(phase);
  }
  const editable = () => !!owner && !!original && !!frame && !!draft && !pending && !openPending && !blocked && !conflict && !working && !locked && !completed;
  function controls() {
    const enabled = editable() && (!generated || changed()); el["crop-finish"].disabled = !enabled;
    if (enabled) app?.MainButton?.enable(); else app?.MainButton?.disable();
    el["crop-recover"].hidden = !pending && !openPending;
    el["crop-retry"].hidden = !pending;
    el["crop-recover"].disabled = working || locked || blocked;
    el["crop-retry"].disabled = working || locked || blocked || !["save", "finalize"].includes(pending?.kind);
  }
  function clear() {
    epoch++; original?.close(); frame?.close(); original = frame = draft = owner = pending = openPending = mouse = touches = undefined;
    completed = blocked = conflict = working = generated = false;
    el["avatar-crop"].hidden = true; document.body.classList.remove("avatar-crop-active", "avatar-crop-telegram");
    app?.MainButton?.offClick?.(finishClick); app?.BackButton?.offClick?.(back);
    app?.MainButton?.hide(); app?.BackButton?.hide(); ctx.clearRect(0, 0, canvas.width, canvas.height);
  }
  async function check(e, promise) {
    try { const result = await promise; if (e !== epoch) throw { stale: true }; return result; }
    catch (error) { if (e !== epoch) throw { stale: true }; throw error; }
  }
  const call = (e, url, method = "GET", body) => check(e, env.request(url, method, body));
  async function sha(e, blob) {
    const raw = await check(e, blob.arrayBuffer());
    const hash = await check(e, crypto.subtle.digest("SHA-256", raw));
    return Array.from(new Uint8Array(hash), b => b.toString(16).padStart(2, "0")).join("");
  }
  function errorStatus(error) {
    if ([401, 403].includes(error.status)) { env.denied(); return; }
    if ([404, 410, 422].includes(error.status) && !pending) status("missing");
    else status(error.local || (error.status === 409 ? "conflict" : "unavailable"));
  }
  async function run(action) {
    if (working || locked || !owner || blocked) return;
    const e = epoch; working = true; env.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } }
  }
  // Same affine convention as Python: [[a,c,e],[b,d,f]], in 1024-square units.
  function multiply(A, B) {
    return [[A[0][0] * B[0][0] + A[0][1] * B[1][0], A[0][0] * B[0][1] + A[0][1] * B[1][1], A[0][0] * B[0][2] + A[0][1] * B[1][2] + A[0][2]],
      [A[1][0] * B[0][0] + A[1][1] * B[1][0], A[1][0] * B[0][1] + A[1][1] * B[1][1], A[1][0] * B[0][2] + A[1][1] * B[1][2] + A[1][2]]];
  }
  function toTransform(value = matrix) {
    const cover = 1024 / Math.min(original.width, original.height);
    const angle = Math.atan2(value[1][0], value[0][0]);
    const t = { offset_x_ppm: Math.round(value[0][2] * 1000000 / 1024), offset_y_ppm: Math.round(value[1][2] * 1000000 / 1024), zoom_ppm: Math.round(Math.hypot(value[0][0], value[1][0]) / cover * 1000000), angle_millidegrees: Math.round(angle * 180000 / Math.PI) };
    if (t.angle_millidegrees === 180000) t.angle_millidegrees = -180000;
    return t;
  }
  function fromTransform(t) {
    const scale = 1024 / Math.min(original.width, original.height) * t.zoom_ppm / 1000000;
    const angle = t.angle_millidegrees * Math.PI / 180000, c = Math.cos(angle) * scale, s = Math.sin(angle) * scale;
    return [[c, -s, t.offset_x_ppm * 1024 / 1000000], [s, c, t.offset_y_ppm * 1024 / 1000000]];
  }
  function layout() {
    if (!owner) return;
    const bounds = canvas.getBoundingClientRect(); W = bounds.width; H = bounds.height;
    frameSize = Math.min(W, H); left = (W - frameSize) / 2; top = (H - frameSize) / 2;
    const ratio = Math.max(1, window.devicePixelRatio || 1);
    canvas.width = Math.max(1, Math.round(W * ratio)); canvas.height = Math.max(1, Math.round(H * ratio)); draw();
  }
  function draw() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    if (!original || !frame || !frameSize) return;
    const ratio = canvas.width / W, size = frameSize * ratio, factor = size / 1024;
    ctx.save(); ctx.beginPath(); ctx.rect(left * ratio, top * ratio, size, size); ctx.clip();
    ctx.fillStyle = "white"; ctx.fillRect(left * ratio, top * ratio, size, size);
    ctx.translate((left + frameSize / 2) * ratio, (top + frameSize / 2) * ratio);
    ctx.transform(matrix[0][0] * factor, matrix[1][0] * factor, matrix[0][1] * factor, matrix[1][1] * factor, matrix[0][2] * factor, matrix[1][2] * factor);
    ctx.drawImage(original, -original.width / 2, -original.height / 2); ctx.restore();
    const side = Math.min(frame.width, frame.height);
    ctx.drawImage(frame, (frame.width - side) / 2, (frame.height - side) / 2, side, side, left * ratio, top * ratio, size, size);
  }
  function change(value) {
    if (!editable() || !validTransform(toTransform(value))) return;
    matrix = value; status("edited"); draw(); controls();
  }
  function move(dx, dy) { change(multiply([[1, 0, dx * 1024 / frameSize], [0, 1, dy * 1024 / frameSize]], matrix)); }
  function around(x, y, scale, angle) {
    const px = (x - left - frameSize / 2) * 1024 / frameSize, py = (y - top - frameSize / 2) * 1024 / frameSize;
    const c = Math.cos(angle) * scale, s = Math.sin(angle) * scale;
    change(multiply([[c, -s, px - c * px + s * py], [s, c, py - s * px - c * py]], matrix));
  }
  canvas.addEventListener("mousedown", e => { if (!editable()) return; e.preventDefault(); mouse = { x: e.clientX, y: e.clientY }; });
  window.addEventListener("mousemove", e => {
    if (!mouse || !editable()) return; e.preventDefault();
    if (e.shiftKey) {
      const cx = left + frameSize / 2, cy = top + frameSize / 2;
      around(cx, cy, 1, Math.atan2(e.clientY - cy, e.clientX - cx) - Math.atan2(mouse.y - cy, mouse.x - cx));
    } else move(e.clientX - mouse.x, e.clientY - mouse.y);
    mouse = { x: e.clientX, y: e.clientY };
  });
  window.addEventListener("mouseup", () => { mouse = undefined; });
  canvas.addEventListener("wheel", e => { e.preventDefault(); if (!editable()) return; const step = e.shiftKey ? .005 : .1; around(e.clientX, e.clientY, e.deltaY < 0 ? 1 + step : 1 - step, 0); }, { passive: false });
  const points = e => Array.from(e.touches).slice(0, 2).map(t => ({ x: t.clientX, y: t.clientY }));
  canvas.addEventListener("touchstart", e => { if (!editable()) return; e.preventDefault(); touches = points(e); }, { passive: false });
  canvas.addEventListener("touchmove", e => {
    if (!touches || !editable()) return; e.preventDefault(); const next = points(e);
    if (touches.length === 1 && next.length === 1) move(next[0].x - touches[0].x, next[0].y - touches[0].y);
    if (touches.length === 2 && next.length === 2) {
      const oldCenter = { x: (touches[0].x + touches[1].x) / 2, y: (touches[0].y + touches[1].y) / 2 };
      const center = { x: (next[0].x + next[1].x) / 2, y: (next[0].y + next[1].y) / 2 };
      const distance = a => Math.hypot(a[1].x - a[0].x, a[1].y - a[0].y), angle = a => Math.atan2(a[1].y - a[0].y, a[1].x - a[0].x);
      move(center.x - oldCenter.x, center.y - oldCenter.y);
      around(center.x, center.y, distance(next) / Math.max(1e-6, distance(touches)), angle(next) - angle(touches));
    }
    touches = next;
  }, { passive: false });
  canvas.addEventListener("touchend", e => { touches = points(e); }, { passive: false });
  canvas.addEventListener("touchcancel", () => { touches = undefined; });
  window.addEventListener("resize", layout);
  // Telegram can update the stable-height CSS variable without a window resize.
  const viewportObserver = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(layout);
  viewportObserver?.observe(canvas);
  async function load(e, id) {
    if (!uuid(id)) throw { local: "missing" };
    const next = await call(e, path(id)); if (!validDraft(next) || next.id !== id) throw { local: "missing" };
    const raw = await check(e, env.readBlob(path(id) + "/original"));
    if (await sha(e, raw) !== next.source_sha256) throw { local: "invalid" };
    const border = await check(e, env.readBlob("./avatars/frame/" + next.frame_sha256));
    if (await sha(e, border) !== next.frame_sha256) throw { local: "invalid" };
    const documents = await call(e, "./avatars/finalized");
    if (!Array.isArray(documents?.avatars) || documents.avatars.length > 20) throw { local: "invalid" };
    let photo, overlay;
    try { photo = await createImageBitmap(raw, { imageOrientation: "from-image" }); overlay = await createImageBitmap(border); if (e !== epoch) throw { stale: true }; }
    catch (error) { photo?.close(); overlay?.close(); throw error; }
    generated = documents.avatars.some(v => v.avatar?.draft_id === id && v.avatar.draft_version === next.version);
    original?.close(); frame?.close(); original = photo; frame = overlay; draft = next; matrix = fromTransform(next.transform); layout(); status("ready");
  }
  function validPending(p) {
    if (p?.owner !== owner || !uuid(p.operation_id)) return false;
    if (["save", "finalize"].includes(p.kind)) return Object.keys(p).sort().join() === "body,draft_id,kind,operation_id,owner" && uuid(p.draft_id) &&
      p.body?.operation_id === p.operation_id && Number.isSafeInteger(p.body.expected_version) && p.body.expected_version > 0 &&
      Object.keys(p.body).sort().join() === (p.kind === "save" ? "expected_version,operation_id,transform" : "expected_version,operation_id") && (p.kind !== "save" || validTransform(p.body.transform));
    if (p.kind === "upload") return Object.keys(p).sort().join() === "kind,operation_id,owner,sha256,size" && digest(p.sha256) && Number.isSafeInteger(p.size) && p.size > 0 && p.size <= 20 * 1024 * 1024;
    return p.kind === "history" && Object.keys(p).sort().join() === "body,kind,operation_id,owner" && p.body?.operation_id === p.operation_id && Object.keys(p.body).sort().join() === "history_source,operation_id" &&
      Object.keys(p.body.history_source || {}).sort().join() === "archive_id,record_id" && uuid(p.body.history_source.record_id) && uuid(p.body.history_source.archive_id);
  }
  async function recordOpen(e) {
    const storageKey = "zinusya-avatar-crop-open-v1:" + owner + ":" + draft.id;
    let operation;
    try {
      operation = sessionStorage.getItem(storageKey) || crypto.randomUUID();
      if (!uuid(operation)) throw new Error();
      sessionStorage.setItem(storageKey, operation);
      if (sessionStorage.getItem(storageKey) !== operation) throw new Error();
    } catch { blocked = true; throw { local: "storage" }; }
    openPending = operation; controls();
    const receipt = await call(e, path(draft.id) + "/open", "POST", { operation_id: operation });
    if (receipt?.draft_id !== draft.id || !Number.isFinite(Date.parse(receipt.opened_at))) throw { local: "invalid" };
    try { sessionStorage.removeItem(storageKey); } catch { throw { local: "storage" }; }
    openPending = undefined; status("ready");
  }
  function remember(p) {
    try { const raw = JSON.stringify(p); sessionStorage.setItem(key(), raw); if (sessionStorage.getItem(key()) !== raw) throw new Error(); }
    catch { throw { local: "storage" }; }
    pending = p; controls();
  }
  function forget() { try { sessionStorage.removeItem(key()); } catch { throw { local: "storage" }; } pending = undefined; }
  async function accept(e, result) {
    const p = pending;
    if (p.kind === "finalize") {
      if (!uuid(result?.artifact_id) || !uuid(result.delivery_id) || result.draft_id !== p.draft_id || result.draft_version !== p.body.expected_version) throw { local: "invalid" };
      forget(); completed = true; status("queued"); return;
    }
    if (!validDraft(result) || (p.kind === "save" && (result.id !== p.draft_id || result.version !== p.body.expected_version + 1))) throw { local: "invalid" };
    forget(); await load(e, result.id);
  }
  async function recover(e) {
    if (!pending) return;
    try { await accept(e, await call(e, (pending.kind === "finalize" ? "./avatars/finalizations/operations/" : "./avatars/operations/") + pending.operation_id)); }
    catch (error) { if (error.status !== 404) throw error; status("unknown"); }
  }
  async function dispatch(e) {
    if (!pending || !["save", "finalize"].includes(pending.kind)) return;
    const p = pending;
    try { await accept(e, await call(e, path(p.draft_id) + (p.kind === "finalize" ? "/finalize" : ""), "POST", p.body)); }
    catch (error) {
      if ([400, 404, 409, 410, 413, 422].includes(error.status)) { forget(); conflict = true; status("conflict"); }
      else throw error;
    }
  }
  async function finish(e) {
    if (!original || !draft || pending || openPending || conflict || completed) return;
    const transform = toTransform(); if (!validTransform(transform)) throw { local: "invalid" };
    if (!changed() && generated) return;
    if (changed()) {
      status("saving"); const body = { operation_id: crypto.randomUUID(), expected_version: draft.version, transform };
      remember({ owner, kind: "save", operation_id: body.operation_id, draft_id: draft.id, body }); await dispatch(e);
      if (pending || conflict) return;
    }
    status("rendering");
    await check(e, env.readBlob(path(draft.id) + "/preview/" + draft.version));
    const body = { operation_id: crypto.randomUUID(), expected_version: draft.version };
    remember({ owner, kind: "finalize", operation_id: body.operation_id, draft_id: draft.id, body }); await dispatch(e);
  }
  function changed() { const t = toTransform(); return Object.keys(t).some(k => t[k] !== draft.transform[k]); }
  const finishClick = () => run(finish);
  const back = () => { app?.close?.(); };
  el["crop-finish"].addEventListener("click", finishClick); el["crop-back"].addEventListener("click", back);
  el["crop-recover"].addEventListener("click", () => run(e => pending ? recover(e) : recordOpen(e))); el["crop-retry"].addEventListener("click", () => run(dispatch));
  return { clear, setBusy(value) { locked = value; controls(); },
    async start(id, language) {
      clear(); if (!uuid(id)) return; owner = id; locale = language === "ru" ? "ru" : "en"; labels(); status("loading");
      el["avatar-crop"].hidden = false; document.body.classList.add("avatar-crop-active");
      if (app?.MainButton) { document.body.classList.add("avatar-crop-telegram"); app.MainButton.onClick(finishClick); app.MainButton.show(); app.BackButton?.onClick(back); app.BackButton?.show(); app.expand?.(); }
      const e = epoch;
      try {
        const raw = sessionStorage.getItem(key());
        if (raw) { if (raw.length > 2048) throw new Error(); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error(); }
      } catch { blocked = true; status("blocked"); controls(); return; }
      try {
        if (pending) await recover(e);
        else await load(e, env.draftID);
        if (!pending && !completed) {
          if (draft?.id !== env.draftID) await load(e, env.draftID);
          await recordOpen(e);
        }
      }
      catch (error) { if (e === epoch) errorStatus(error); }
      controls();
    }
  };
};
