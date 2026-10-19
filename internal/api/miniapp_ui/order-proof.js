"use strict";

// This controller receives a session-bound request capability, never IAM tokens.
// Persist an exact owner-bound request before POST; reads reconcile lost replies.
window.createZinusyaOrderProof = function (env) {
  const section = document.getElementById("order-proof");
  const ids = ["title", "note", "status", "target", "history", "history-next", "file-label", "file", "files-first", "files-next", "preview", "download", "amount-label", "amount", "form", "submit", "reload", "recover", "retry", "receipt", "preview-info"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("op-" + id)]));
  const words = {
    en: {title:"Order payment proof",note:"Send the file to this Telegram bot first, then select it here. Only an ambassador can confirm payment. Once proof is submitted, contact an ambassador to cancel the order.","file-label":"Accepted file", "amount-label":"Reported amount",submit:"Send proof for review",preview:"Load selected file",download:"Download loaded file",readClaim:"Load this proof",reload:"Reload order and proofs",recover:"Check earlier request",retry:"Retry the same request","files-first":"First files","files-next":"Next files","history-next":"More proofs",choose:"Choose a file",empty:"No payment proofs for this order.",noFiles:"No accepted files on this page.",loading:"Loading…",ready:"Check the order, file and reported amount before sending.",unknown:"The earlier result is unknown. Check it or retry the same request before making changes.",conflict:"Request rejected. Current proofs are shown below; read them before a new request.",unavailable:"Could not load current state. Retry without changing the earlier request.",storage:"Browser session storage is unavailable. No new request was sent.",blocked:"The stored request is invalid. New requests are blocked on this page.",invalid:"Choose a file, load it and enter a positive amount using the order currency.",active:"An existing proof is shown below. Do not send another payment for it.",more:"Check the remaining proof pages before sending another claim.",recorded:"Original claim recorded. Current review state is shown below.",historical:"Original request result",current:"Current proof state",pending:"Awaiting review; available places held",exhausted:"Places unavailable; ambassador must arrange resolution",needs_decision:"Ambassador must resolve this claim",confirmed:"Payment confirmed",rejected:"Proof rejected",total:"Order total",deadline:"Evidence deadline",sent:"File sent",order:"Order",reported:"Reported",file:"File",closed:"This order is cancelled; proof history remains available."},
    ru: {title:"Подтверждение оплаты заказа",note:"Сначала отправь файл этому боту в Telegram, затем выбери его здесь. Оплату подтверждает только амбассадор. После отправки подтверждения отмену заказа нужно обсудить с амбассадором.","file-label":"Принятый файл","amount-label":"Заявленная сумма",submit:"Отправить подтверждение на проверку",preview:"Загрузить выбранный файл",download:"Скачать загруженный файл",readClaim:"Загрузить это подтверждение",reload:"Загрузить заказ и подтверждения",recover:"Проверить прежний запрос",retry:"Повторить тот же запрос","files-first":"Первые файлы","files-next":"Следующие файлы","history-next":"Ещё подтверждения",choose:"Выбери файл",empty:"Для этого заказа нет подтверждений оплаты.",noFiles:"На этой странице нет принятых файлов.",loading:"Загружаю…",ready:"Проверь заказ, файл и заявленную сумму перед отправкой.",unknown:"Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос перед изменениями.",conflict:"Запрос отклонён. Ниже показаны текущие подтверждения; проверь их перед новым запросом.",unavailable:"Не удалось загрузить состояние. Повтори прежний запрос, не меняя его.",storage:"Хранилище сессии браузера недоступно. Новый запрос не отправлен.",blocked:"Запись прежнего запроса повреждена. Новые запросы на этой странице заблокированы.",invalid:"Выбери и загрузи файл, укажи положительную сумму в валюте заказа.",active:"Ниже уже есть подтверждение. Не отправляй повторную оплату по нему.",more:"Проверь оставшиеся страницы подтверждений перед новой отправкой.",recorded:"Исходное подтверждение принято. Текущее решение показано ниже.",historical:"Исходный результат запроса",current:"Текущее состояние подтверждения",pending:"Ожидает проверки; доступные места зарезервированы",exhausted:"Места закончились; требуется решение амбассадора",needs_decision:"Требуется решение амбассадора",confirmed:"Оплата подтверждена",rejected:"Подтверждение отклонено",total:"Стоимость заказа",deadline:"Срок отправки подтверждения",sent:"Файл отправлен",order:"Заказ",reported:"Заявлено",file:"Файл",closed:"Этот заказ отменён; история подтверждений доступна."}
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const nonnegative = v => Number.isSafeInteger(v) && v >= 0 && v <= 9000000000000000;
  const positive = v => nonnegative(v) && v > 0;
  const states = ["pending", "exhausted", "needs_decision", "confirmed", "rejected"];
  const w = () => words[language];
  let owner, language = "en", epoch = 0, locked = false, working = false, blocked = false;
  let eventID, orderID, order, pending, fileNext, filesAfter, historyNext, historyActive = false, historyLoaded = false;
  let files = [], claims = [], historyButtons = [], loadedFile, blobURL, statusName, originalReceipt;
  const key = () => "zinusya-order-proof-v1:" + owner;
  const path = () => "./orders/" + eventID;
  function node(tag, text) { const n = document.createElement(tag); n.textContent = text; return n; }
  function status(name) { statusName = name; el.status.textContent = w()[name]; }
  function labels() { for (const id of ["title", "note", "file-label", "amount-label", "submit", "preview", "download", "reload", "recover", "retry", "files-first", "files-next", "history-next"]) el[id].textContent = w()[id]; }
  function dropPreview() { if (blobURL) URL.revokeObjectURL(blobURL); blobURL = loadedFile = undefined; el.download.hidden = true; el.download.removeAttribute("href"); el.download.download = ""; el["preview-info"].textContent = ""; }
  function controls() {
    const disabled = locked || working || !owner || blocked;
    const edit = disabled || !!pending || !order || order.state !== "submitted" || !historyLoaded || historyActive || !!historyNext;
    el.file.disabled = el.amount.disabled = edit;
    el.submit.disabled = edit || !loadedFile || loadedFile !== el.file.value;
    el.preview.disabled = disabled || !eventID || !(pending?.body.file_id || el.file.value);
    el.reload.disabled = disabled || !eventID;
    el.recover.hidden = el.retry.hidden = !pending;
    el.recover.disabled = disabled; el.retry.disabled = disabled || !order;
    el["files-first"].hidden = !filesAfter; el["files-next"].hidden = !fileNext;
    el["files-first"].disabled = el["files-next"].disabled = edit;
    el["history-next"].hidden = !historyNext; el["history-next"].disabled = disabled;
    for (const button of historyButtons) button.disabled = disabled;
  }
  function clear() {
    epoch++; dropPreview(); owner = eventID = orderID = order = pending = fileNext = filesAfter = historyNext = undefined;
    working = blocked = historyActive = historyLoaded = false; files = []; claims = []; historyButtons = []; statusName = originalReceipt = undefined;
    section.hidden = true; for (const id of ["target", "history", "receipt"]) el[id].replaceChildren();
    el.file.replaceChildren(); el.amount.value = ""; el.status.textContent = ""; controls();
  }
  function quote(q) { if (!q || !nonnegative(q.total_minor) || !Number.isInteger(q.currency_scale) || q.currency_scale < 0 || q.currency_scale > 6 || !/^[A-Z]{3}$/.test(q.currency) || typeof q.timezone !== "string") throw new Error("Invalid order quote"); new Intl.DateTimeFormat(language, {timeZone:q.timezone}); return q; }
  function decimal(amount, scale) { const digits = BigInt(amount).toString().padStart(scale + 1, "0"); return scale ? digits.slice(0, -scale) + "." + digits.slice(-scale) : digits; }
  function money(amount, scale, currency) { if (!Number.isSafeInteger(amount) || amount < 0 || scale < 0 || scale > 6 || !/^[A-Z]{3}$/.test(currency)) throw new Error("Invalid money"); return decimal(amount, scale) + " " + currency; }
  function date(value) { if (!Number.isFinite(Date.parse(value))) throw new Error("Invalid timestamp"); return new Intl.DateTimeFormat(language, {timeZone:order.snapshot.quote.timezone, dateStyle:"medium", timeStyle:"long"}).format(new Date(value)); }
  function checkOrder(value) { if (!value || value.id !== orderID || value.event_id !== eventID || !positive(value.version) || !["submitted", "cancelled"].includes(value.state) || !value.snapshot || typeof value.snapshot.event_name !== "string") throw new Error("Invalid order"); quote(value.snapshot.quote); return value; }
  function checkClaim(value) { if (!value || !uuid(value.id) || value.user_id !== owner || value.order_id !== orderID || value.event_id !== eventID || !uuid(value.file_id) || !positive(value.amount_minor) || !nonnegative(value.quoted_amount_minor) || !positive(value.version) || !states.includes(value.state) || !Number.isInteger(value.currency_scale) || value.currency_scale < 0 || value.currency_scale > 6 || !/^[A-Z]{3}$/.test(value.currency)) throw new Error("Invalid proof"); if (!Number.isFinite(Date.parse(value.sent_at)) || !Number.isFinite(Date.parse(value.due_at)) || (order && (value.currency !== order.snapshot.quote.currency || value.currency_scale !== order.snapshot.quote.currency_scale || value.quoted_amount_minor !== order.snapshot.quote.total_minor))) throw new Error("Proof terms mismatch"); return value; }
  function claimView(value, historical) {
    const article = document.createElement("article");
    article.append(node("p", (historical ? w().historical : w().current) + ": " + w()[value.state]));
    article.append(node("p", w().reported + ": " + money(value.amount_minor, value.currency_scale, value.currency)));
    article.append(node("p", w().file + ": " + value.file_id), node("p", "ID: " + value.id));
    if (order) article.append(node("p", w().sent + ": " + date(value.sent_at)), node("p", w().deadline + ": " + date(value.due_at) + " · " + order.snapshot.quote.timezone));
    if (value.review?.reason) article.append(node("p", value.review.reason));
    if (!historical) {
      const button = node("button", w().readClaim); button.type = "button";
      button.addEventListener("click", () => run(e => loadProof(e, value.file_id, false)));
      historyButtons.push(button); article.append(button);
    }
    return article;
  }
  function renderOrder() {
    el.target.replaceChildren(); if (!order) return;
    const q = order.snapshot.quote;
    el.target.append(node("h2", order.snapshot.event_name), node("p", w().order + ": " + orderID), node("p", w().total + ": " + money(q.total_minor, q.currency_scale, q.currency)), node("p", w().deadline + ": " + date(order.snapshot.sales_closes_at) + " · " + q.timezone));
    el["amount-label"].textContent = w()["amount-label"] + " (" + q.currency + ")";
  }
  function renderClaims() { historyButtons = []; el.history.replaceChildren(); if (!claims.length) el.history.append(node("p", w().empty)); for (const c of claims) el.history.append(claimView(c, false)); }
  function renderFiles() {
    const selected = el.file.value;
    const blank = node("option", files.length ? w().choose : w().noFiles); blank.value = ""; el.file.replaceChildren(blank);
    for (const file of files) { const option = node("option", (file.filename || file.kind) + " · " + date(file.sent_at) + " · " + file.id); option.value = file.id; el.file.append(option); }
    el.file.value = files.some(f => f.id === selected) ? selected : "";
  }
  async function call(e, url, method = "GET", body) { if (e !== epoch) throw {stale:true}; const result = await env.request(url, method, body); if (e !== epoch) throw {stale:true}; return result; }
  function fail(error) { if (error.stale) return; if (error.status === 401) { env.setBusy(false); env.denied(); return; } status(error.local || (pending ? "unknown" : "unavailable")); }
  async function run(fn) { if (working || locked || !owner || blocked) return; const e = epoch; working = true; env.setBusy(true); controls(); try { await fn(e); } catch (error) { if (e === epoch) fail(error); } finally { if (e === epoch) { working = false; env.setBusy(false); controls(); } } }
  async function loadClaims(e, cursor) {
    if (!cursor) { historyActive = historyLoaded = false; }
    const page = await call(e, path() + "/payment-evidence?order_id=" + orderID + (cursor ? "&after=" + cursor : ""));
    if (!page || !Array.isArray(page.evidence) || page.evidence.length > 20 || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid proof page");
    claims = page.evidence.map(checkClaim); historyNext = page.next; historyLoaded = true;
    historyActive ||= claims.some(c => c.state !== "rejected"); renderClaims();
    if (!pending) status(order.state !== "submitted" ? "closed" : historyActive ? "active" : historyNext ? "more" : "ready");
  }
  async function loadFiles(e, cursor) {
    const page = await call(e, path() + "/proof-files" + (cursor ? "?after=" + cursor : ""));
    if (!page || !Array.isArray(page.files) || page.files.length > 20 || (page.next !== undefined && !uuid(page.next))) throw new Error("Invalid file page");
    for (const file of page.files) if (!file || !uuid(file.id) || typeof file.filename !== "string" || typeof file.kind !== "string" || !Number.isFinite(Date.parse(file.sent_at))) throw new Error("Invalid accepted file");
    dropPreview(); files = page.files; filesAfter = cursor; fileNext = page.next; renderFiles();
  }
  async function load(e) {
    order = checkOrder(await call(e, path() + "/submitted/" + orderID)); renderOrder();
    if (!pending) el.amount.value = decimal(order.snapshot.quote.total_minor, order.snapshot.quote.currency_scale);
    await loadClaims(e); if (!pending) await loadFiles(e);
    if (pending) status("unknown"); else status(order.state !== "submitted" ? "closed" : historyActive ? "active" : historyNext ? "more" : "ready");
  }
  function validPending(p) {
    if (!p || Object.keys(p).sort().join() !== "body,event_id,owner" || p.owner !== owner || !uuid(p.event_id) || !p.body) return false;
    const b = p.body;
    return Object.keys(b).sort().join() === "amount_minor,expected_order_version,file_id,operation_id,order_id" && uuid(b.operation_id) && uuid(b.order_id) && uuid(b.file_id) && positive(b.expected_order_version) && positive(b.amount_minor);
  }
  function forget() { try { sessionStorage.removeItem(key()); } catch { throw {local:"storage"}; } pending = undefined; }
  function pinPending() {
    eventID = pending.event_id; orderID = pending.body.order_id; section.hidden = false;
    if (order && order.id === orderID && order.event_id === eventID) {
      renderOrder(); const q = order.snapshot.quote;
      el.target.append(node("p", w().reported + ": " + money(pending.body.amount_minor, q.currency_scale, q.currency)));
    } else el.target.replaceChildren(node("p", w().order + ": " + orderID));
    el.target.append(node("p", w().file + ": " + pending.body.file_id));
  }
  async function accept(e, value) {
    const result = checkClaim(value), b = pending.body;
    if (result.file_id !== b.file_id || result.amount_minor !== b.amount_minor) throw new Error("Proof receipt mismatch");
    forget(); originalReceipt = result; el.receipt.replaceChildren(claimView(result, true)); await load(e); status("recorded");
  }
  async function recover(e) {
    if (!pending) return;
    pinPending();
    order = checkOrder(await call(e, path() + "/submitted/" + orderID)); pinPending();
    try { const receipt = await call(e, path() + "/payment-evidence-operations/" + pending.body.operation_id); if (receipt.operation_id !== pending.body.operation_id || receipt.kind !== "submit") throw new Error("Invalid proof receipt"); await accept(e, receipt.result); }
    catch (error) { if (error.status === 404) status("unknown"); else throw error; }
  }
  async function dispatch(e) {
    if (!pending || !order) return;
    pinPending();
    try { await accept(e, await call(e, path() + "/payment-evidence", "POST", pending.body)); }
    catch (error) { if (error.status === 400 || error.status === 409) { forget(); order = undefined; dropPreview(); await load(e); status("conflict"); } else throw error; }
  }
  function amount() {
    const scale = order.snapshot.quote.currency_scale, value = el.amount.value.trim().replace(",", ".");
    if (!/^\d+(?:\.\d+)?$/.test(value) || value.length > 24) throw {local:"invalid"};
    const [whole, fraction = ""] = value.split("."); if (fraction.length > scale) throw {local:"invalid"};
    const result = BigInt(whole) * 10n ** BigInt(scale) + BigInt(fraction.padEnd(scale, "0") || "0");
    if (result < 1n || result > 9000000000000000n) throw {local:"invalid"}; return Number(result);
  }
  el.form.addEventListener("submit", event => { event.preventDefault(); return run(async e => {
    if (pending || !order || order.state !== "submitted" || !historyLoaded || historyActive || historyNext || loadedFile !== el.file.value || !files.some(f => f.id === loadedFile)) throw {local:"invalid"};
    const record = {owner, event_id:eventID, body:{operation_id:crypto.randomUUID(), order_id:orderID, expected_order_version:order.version, file_id:loadedFile, amount_minor:amount()}};
    try { const raw = JSON.stringify(record); sessionStorage.setItem(key(), raw); if (sessionStorage.getItem(key()) !== raw) throw new Error("Storage unavailable"); } catch { throw {local:"storage"}; }
    pending = record; controls(); await dispatch(e);
  }); });
  el.file.addEventListener("change", () => { dropPreview(); controls(); });
  async function loadProof(e, id, fromSelection) {
    if (!uuid(id)) throw {local:"invalid"};
    dropPreview(); const blob = await env.readProof(path() + "/proof-files/" + id + "/content");
    if (e !== epoch || (fromSelection && !pending && el.file.value !== id)) return;
    blobURL = URL.createObjectURL(blob); loadedFile = id; el.download.href = blobURL;
    el.download.download = files.find(f => f.id === id)?.filename || "proof"; el.download.hidden = false;
    el["preview-info"].textContent = w().file + ": " + id;
  }
  el.preview.addEventListener("click", () => run(e => loadProof(e, pending?.body.file_id || el.file.value, true)));
  el.reload.addEventListener("click", () => run(e => pending ? recover(e) : load(e)));
  el.recover.addEventListener("click", () => run(recover)); el.retry.addEventListener("click", () => run(dispatch));
  el["files-first"].addEventListener("click", () => run(e => loadFiles(e)));
  el["files-next"].addEventListener("click", () => run(e => loadFiles(e, fileNext)));
  el["history-next"].addEventListener("click", () => run(e => loadClaims(e, historyNext)));
  clear();
  return {
    clear, setBusy(value) { locked = value; controls(); },
    setLanguage(locale) {
      language = locale === "ru" ? "ru" : "en"; labels();
      if (order) { if (pending) pinPending(); else renderOrder(); renderClaims(); renderFiles(); }
      if (originalReceipt) el.receipt.replaceChildren(claimView(originalReceipt, true));
      if (statusName) status(statusName);
      if (loadedFile) el["preview-info"].textContent = w().file + ": " + loadedFile;
      controls();
    },
    async start(id, locale) {
      clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); const e = epoch;
      try { const raw = sessionStorage.getItem(key()); if (raw) { if (raw.length > 2048) throw new Error("Invalid recovery record"); pending = JSON.parse(raw); if (!validPending(pending)) throw new Error("Invalid recovery record"); pinPending(); await recover(e); } }
      catch (error) { if (e !== epoch) return; if (pending && validPending(pending)) fail(error); else { blocked = true; section.hidden = false; status("blocked"); } }
      controls();
    },
    async open(value, locale) {
      if (!owner || blocked || locked || working) return;
      if (pending) { section.hidden = false; status("unknown"); return; }
      if (!value || !uuid(value.id) || !uuid(value.event_id)) return;
      epoch++; dropPreview(); language = locale === "ru" ? "ru" : "en"; eventID = value.event_id; orderID = value.id;
      order = originalReceipt = statusName = undefined; claims = []; files = []; historyActive = historyLoaded = false; historyNext = filesAfter = fileNext = undefined; el.target.replaceChildren(); el.history.replaceChildren(); el.file.replaceChildren(); el.amount.value = ""; el.receipt.replaceChildren(); section.hidden = false; labels();
      const result = run(load); section.scrollIntoView?.({block:"start"}); return result;
    }
  };
};
