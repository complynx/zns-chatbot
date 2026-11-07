"use strict";

window.createZinusyaPaymentReviews = function (options) {
  const ids = ["title", "note", "status", "event-label", "event", "first", "next", "evidence-label", "evidence", "evidence-first", "evidence-next", "detail", "proof", "download", "form", "decision-label", "decision", "amount-label", "amount", "reason-label", "reason", "save", "preview", "receipt", "confirm", "cancel", "reload", "recover", "retry"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("pr-" + id)]));
  const section = document.getElementById("payment-review");
  const words = {
    en: { title: "Payment reviews", note: "Preparing a decision does not confirm money. Confirm only after inspecting the proof.", "event-label": "Event", "evidence-label": "Payment proof", first: "First events", next: "Next events", "evidence-first": "First proofs", "evidence-next": "Next proofs", proof: "Load proof", download: "Download proof", "decision-label": "Decision", "amount-label": "Amount to credit", "reason-label": "Reason", save: "Prepare decision", confirm: "Confirm saved decision", cancel: "Cancel saved decision", reload: "Reload current state", recover: "Check earlier request", retry: "Retry same request", confirmed: "Confirm payment", rejected: "Reject proof", confirmedState: "Confirmed", rejectedState: "Rejected", choose: "Choose an event", chooseProof: "Choose a proof", loading: "Loading…", ready: "Review the proof and enter your decision.", empty: "No events available for payment review.", noProofs: "No pending proofs on this page.", unavailable: "Could not load current state. Retry the request.", unknown: "The earlier result is unknown. Check it or retry the exact same request before making another change.", storage: "Browser session storage is unavailable. No new request was sent.", blocked: "The saved request record is invalid. New requests are blocked on this page.", dirty: "Prepare your edit or reload current state before switching or confirming.", invalid: "Check the amount and reason. A rejection needs a reason and credits zero.", conflict: "Request rejected. Reload current state before making a new decision.", prepared: "Decision prepared. No money credited.", cancelled: "Saved decision cancelled. No money credited.", completed: "Decision recorded", payer: "Payer", reported: "Reported amount", remaining: "Remaining balance", deadline: "Evidence deadline", sent: "Proof sent", late: "Sent after the deadline. Confirmation needs a reason.", timely: "Sent before the deadline", state: "Proof status", pending: "Pending", history: "Recorded decision", saved: "Saved decision", expires: "Decision expires", expired: "The saved decision expired. Prepare a new decision.", reason: "Reason", version: "Balance version", noSaved: "No saved decision.", proofReady: "Proof loaded. Use the download link to inspect it." },
    ru: { title: "Проверка платежей", note: "Подготовка решения не подтверждает оплату. Подтверждай только после проверки файла.", "event-label": "Эвент", "evidence-label": "Подтверждение оплаты", first: "Первые эвенты", next: "Следующие эвенты", "evidence-first": "Первые подтверждения", "evidence-next": "Следующие подтверждения", proof: "Загрузить файл", download: "Скачать файл", "decision-label": "Решение", "amount-label": "Сумма к зачислению", "reason-label": "Причина", save: "Подготовить решение", confirm: "Подтвердить сохранённое решение", cancel: "Отменить сохранённое решение", reload: "Загрузить текущее состояние", recover: "Проверить прежний запрос", retry: "Повторить тот же запрос", confirmed: "Подтвердить оплату", rejected: "Отклонить подтверждение", confirmedState: "Подтверждено", rejectedState: "Отклонено", choose: "Выбери эвент", chooseProof: "Выбери подтверждение", loading: "Загружаю…", ready: "Проверь файл и укажи решение.", empty: "Нет доступных эвентов для проверки платежей.", noProofs: "На этой странице нет ожидающих подтверждений.", unavailable: "Не удалось загрузить состояние. Повтори запрос.", unknown: "Результат прежнего запроса неизвестен. Проверь его или повтори точно тот же запрос перед новым изменением.", storage: "Хранилище сессии браузера недоступно. Новый запрос не отправлен.", blocked: "Запись прежнего запроса повреждена. Новые запросы на этой странице заблокированы.", dirty: "Подготовь изменения или загрузи текущее состояние перед переключением или подтверждением.", invalid: "Проверь сумму и причину. Для отклонения нужна причина, зачисляется ноль.", conflict: "Запрос отклонён. Загрузи текущее состояние перед новым решением.", prepared: "Решение подготовлено. Деньги не зачислены.", cancelled: "Сохранённое решение отменено. Деньги не зачислены.", completed: "Решение записано", payer: "Плательщик", reported: "Заявленная сумма", remaining: "Остаток к оплате", deadline: "Срок отправки подтверждения", sent: "Файл отправлен", late: "Отправлено после дедлайна. Для подтверждения нужна причина.", timely: "Отправлено до дедлайна", state: "Статус подтверждения", pending: "Ожидает проверки", history: "Записанное решение", saved: "Сохранённое решение", expires: "Срок решения", expired: "Срок решения истёк. Подготовь новое решение.", reason: "Причина", version: "Версия баланса", noSaved: "Нет сохранённого решения.", proofReady: "Файл загружен. Открой ссылку скачивания для проверки." }
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const integer = v => Number.isSafeInteger(v) && v >= 0;
  const exact = (v, keys) => v && typeof v === "object" && !Array.isArray(v) && Object.keys(v).sort().join() === keys.split(",").sort().join();
  let owner, language = "en", epoch = 0, working = false, locked = false, blocked = false, dirty = false;
  let eventID, evidenceID, eventAfter, eventNext, evidenceAfter, evidenceNext, detail, pending, proofURL, statusKey;
  let events = new Map();
  const w = () => words[language];
  const key = () => "zinusya-payment-review-pending-v1:" + owner;
  const base = "./payment-reviews/";
  const detailPath = () => base + "events/" + eventID + "/evidence/" + evidenceID;
  function node(tag, text) { const n = document.createElement(tag); n.textContent = text; return n; }
  function status(value) { statusKey = value; el.status.textContent = w()[value]; }
  function money(value, e = detail.evidence) {
    if (!integer(value) || !Number.isInteger(e.currency_scale) || e.currency_scale < 0 || e.currency_scale > 4 || !/^[A-Z]{3}$/.test(e.currency)) throw { local: "invalid" };
    const digits = BigInt(value).toString().padStart(e.currency_scale + 1, "0");
    return e.currency + " " + (e.currency_scale ? digits.slice(0, -e.currency_scale) + "." + digits.slice(-e.currency_scale) : digits);
  }
  function time(value) {
    const zone = events.get(eventID)?.timezone;
    if (!zone) return new Date(value).toISOString() + " · UTC";
    return new Intl.DateTimeFormat(language, { timeZone: zone, dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) + " · " + zone;
  }
  function reviewValid(v) {
    return exact(v, "event_id,evidence_id,expected_version,decision,amount_minor,reason") && uuid(v.event_id) && uuid(v.evidence_id) && integer(v.expected_version) && integer(v.amount_minor) && typeof v.reason === "string" && Array.from(v.reason).length <= 512 &&
      (v.decision === "confirmed" ? v.amount_minor > 0 : v.decision === "rejected" && v.amount_minor === 0 && !!v.reason.trim());
  }
  function recordValid(p) {
    if (!exact(p, "owner,event_id,evidence_id,kind,body") || p.owner !== owner || !uuid(p.event_id) || !uuid(p.evidence_id) || !uuid(p.body?.operation_id)) return false;
    const b = p.body;
    if (p.kind === "prepare") return exact(b,"operation_id,expected_revision,review") && integer(b.expected_revision) && reviewValid(b.review) && b.review.event_id === p.event_id && b.review.evidence_id === p.evidence_id;
    if (p.kind === "cancel") return exact(b,"operation_id,workspace_id,revision") && uuid(b.workspace_id) && integer(b.revision) && b.revision > 0;
    return p.kind === "commit" && exact(b,"operation_id,canonical_proposal_id,confirmed,arguments") && b.confirmed === true && uuid(b.canonical_proposal_id) && exact(b.arguments,"workspace_id,revision,review") && uuid(b.arguments.workspace_id) && integer(b.arguments.revision) && b.arguments.revision > 0 && reviewValid(b.arguments.review) && b.arguments.review.event_id === p.event_id && b.arguments.review.evidence_id === p.evidence_id;
  }
  function usableWorkspace() { const a = detail?.workspace; return a?.state === "prepared" && a.current && Date.parse(a.preparation.expires_at) > Date.now() && detail.evidence.state === "pending"; }
  function controls() {
    const disabled = !owner || locked || working;
    const frozen = disabled || !!pending || blocked;
    for (const name of ["event","first","next","evidence","evidence-first","evidence-next"]) el[name].disabled = frozen;
    for (const name of ["decision","amount","reason","save"]) el[name].disabled = frozen || !detail || detail.evidence.state !== "pending";
    el.amount.disabled ||= el.decision.value === "rejected";
    el.confirm.disabled = frozen || dirty || !usableWorkspace();
    el.cancel.disabled = frozen || !detail?.workspace || detail.workspace.state !== "prepared" || !detail.workspace.current;
    el.proof.disabled = disabled || !detail;
    el.reload.disabled = disabled;
    el.recover.hidden = el.retry.hidden = !pending;
    el.recover.disabled = el.retry.disabled = disabled || blocked;
  }
  function labels() {
    for (const id of ids) if (w()[id]) el[id].textContent = w()[id];
    const choices = el.decision.options; if (choices[0]) choices[0].textContent = w().confirmed; if (choices[1]) choices[1].textContent = w().rejected;
    if (statusKey) status(statusKey);
  }
  function clearProof() { if (proofURL) URL.revokeObjectURL(proofURL); proofURL = undefined; el.download.hidden = true; el.download.removeAttribute("href"); }
  function clearDetail() { clearProof(); detail = undefined; dirty = false; el.form.hidden = true; el.detail.replaceChildren(); el.preview.replaceChildren(); el.receipt.textContent = ""; el.amount.value = el.reason.value = ""; }
  function clear() {
    epoch++; owner = undefined; working = blocked = false; pending = undefined;
    eventID = evidenceID = eventAfter = eventNext = evidenceAfter = evidenceNext = undefined;
    events = new Map(); clearDetail(); el.event.replaceChildren(); el.evidence.replaceChildren(); section.hidden = true; controls();
  }
  function errorStatus(error) { if (!error.stale) status(error.local || (pending ? "unknown" : "unavailable")); }
  async function call(e, path, method, body) {
    try {
      const result = await options.request(path, method, body);
      if (e !== epoch || !owner) throw { stale: true };
      return result;
    } catch (error) {
      if (e !== epoch || !owner) throw { stale: true };
      if (error.status === 401) { options.setBusy(false); options.denied(error); }
      throw error;
    }
  }
  async function run(action) {
    if (!owner || working || locked) return;
    const e = epoch; working = true; options.setBusy(true); controls();
    try { await action(e); } catch (error) { if (e === epoch) errorStatus(error); }
    finally { if (e === epoch) { working = false; options.setBusy(false); controls(); } }
  }
  function option(select, value, text) { const n = node("option", text); n.value = value; select.append(n); }
  async function loadEvents(e, after) {
    const page = await call(e, base + "events" + (after ? "?after=" + after : ""));
    if (!Array.isArray(page.events) || page.events.some(v => !uuid(v.id) || typeof v.name !== "string" || typeof v.timezone !== "string") || (page.next && !uuid(page.next))) throw new Error("Invalid events");
    eventAfter = after; eventNext = page.next; el.event.replaceChildren(); option(el.event,"",w().choose);
    for (const event of page.events) { events.set(event.id,event); option(el.event,event.id,event.name + " · " + event.timezone); }
    el.first.hidden = !after; el.next.hidden = !eventNext;
    status(page.events.length ? "ready" : "empty");
  }
  async function loadEvidence(e, id, after) {
    clearDetail(); eventID = id; evidenceID = undefined; evidenceAfter = after; el.event.value = id;
    const page = await call(e, base + "events/" + id + "/evidence" + (after ? "?after=" + after : ""));
    if (!Array.isArray(page.evidence) || page.evidence.some(v => !uuid(v.id) || v.event_id !== id) || (page.next && !uuid(page.next))) throw new Error("Invalid proofs");
    evidenceNext = page.next; el.evidence.replaceChildren(); option(el.evidence,"",w().chooseProof);
    for (const proof of page.evidence) option(el.evidence,proof.id,proof.user_id + " · " + money(proof.amount_minor,proof) + " · " + time(proof.sent_at));
    el["evidence-first"].hidden = !after; el["evidence-next"].hidden = !evidenceNext;
    status(page.evidence.length ? "ready" : "noProofs");
  }
  function render() {
    el.detail.replaceChildren(); el.preview.replaceChildren(); if (!detail) return;
    const a = detail.evidence, balance = detail.balance, saved = detail.workspace;
    const line = (box, title, value) => box.append(node("p", title + ": " + value));
    line(el.detail,w().payer,detail.payer?.full_name || detail.payer?.user_id || a.user_id);
    line(el.detail,w().reported,money(a.amount_minor)); line(el.detail,w().remaining,money(balance.remaining_minor));
    line(el.detail,w().version,balance.version); line(el.detail,w().sent,time(a.sent_at)); line(el.detail,w().deadline,time(a.due_at));
    el.detail.append(node("p",a.timely ? w().timely : w().late));
    line(el.detail,w().state,a.state === "pending" ? w().pending : w()[a.state + "State"] || a.state);
    if (a.review) { line(el.detail,w().history,w()[a.review.decision] + " · " + money(a.review.amount_minor)); if (a.review.reason) line(el.detail,w().reason,a.review.reason); }
    if (saved) {
      const args = saved.preparation.arguments;
      line(el.preview,w().saved,(w()[args.decision] || args.decision) + " · " + money(args.amount_minor));
      if (args.reason) line(el.preview,w().reason,args.reason);
      line(el.preview,w().expires,time(saved.preparation.expires_at));
      if (saved.state === "prepared" && Date.parse(saved.preparation.expires_at) <= Date.now()) el.preview.append(node("p",w().expired));
    } else el.preview.append(node("p",w().noSaved));
  }
  async function loadDetail(e, id = evidenceID) {
    clearDetail(); evidenceID = id; el.evidence.value = id;
    const result = await call(e,detailPath());
    if (result.evidence?.id !== id || result.evidence.event_id !== eventID || !integer(result.balance?.version)) throw new Error("Invalid payment detail");
    const a = result.workspace;
    if (a && (!uuid(a.workspace_id) || a.event_id !== eventID || a.evidence_id !== id || !integer(a.revision) || a.revision < 1 || !uuid(a.preparation?.proposal_id) || !reviewValid(a.preparation.arguments))) throw new Error("Invalid saved decision");
    detail = result;
    if (!Array.from(el.evidence.options).some(option => option.value === id)) option(el.evidence,id,(result.payer?.full_name || result.evidence.user_id) + " · " + money(result.evidence.amount_minor) + " · " + (w()[result.evidence.state + "State"] || result.evidence.state));
    el.evidence.value = id;
    const args = a?.preparation.arguments;
    el.decision.value = args?.decision || "confirmed";
    el.amount.value = args ? money(args.amount_minor).split(" ")[1] : ""; el.reason.value = args?.reason || "";
    el.form.hidden = result.evidence.state !== "pending"; render(); status("ready");
  }
  function forget() { try { sessionStorage.removeItem(key()); } catch { throw { local: "storage" }; } pending = undefined; }
  async function accepted(e, value) {
    const p = pending;
    if (p.kind === "commit") {
      if (value.review?.evidence?.id !== p.evidence_id || value.review.evidence.event_id !== p.event_id || !uuid(value.canonical_operation_id)) throw new Error("Invalid review receipt");
    } else if (value.event_id !== p.event_id || value.evidence_id !== p.evidence_id || !uuid(value.workspace_id) || (p.kind === "prepare" ? value.revision !== p.body.expected_revision + 1 : value.workspace_id !== p.body.workspace_id || value.revision !== p.body.revision || value.state !== "cancelled")) throw new Error("Invalid decision receipt");
    const kind = p.kind; forget();
    await loadEvidence(e,p.event_id); await loadDetail(e,p.evidence_id); el.receipt.textContent = w()[kind === "prepare" ? "prepared" : kind === "cancel" ? "cancelled" : "completed"];
  }
  async function resolveEvent(e,id) {
    if (!events.has(id)) {
      const event = await call(e,base + "events/" + id);
      if (event.id !== id || typeof event.timezone !== "string" || typeof event.name !== "string") throw new Error("Invalid event");
      events.set(id,event);
    }
    const event = events.get(id);
    if (!Array.from(el.event.options).some(option => option.value === id)) option(el.event,id,event.name + " · " + event.timezone);
    el.event.value = id;
  }
  async function openLaunch(e) {
    const hash = window.location?.hash || "";
    if (!/^#payment-review(?:[?&]|$)/.test(hash)) return;
    const params = new URLSearchParams(hash.includes("?") ? hash.slice(hash.indexOf("?")+1) : "");
    const event = params.get("event_id"), evidence = params.get("evidence_id");
    if (event || evidence) {
      if (!uuid(event) || !uuid(evidence)) throw {local:"invalid"};
      await resolveEvent(e,event); await loadEvidence(e,event); await loadDetail(e,evidence);
    }
    section.scrollIntoView?.({block:"start"});
  }
  async function recover(e) {
    if (!pending || blocked) return;
    eventID = pending.event_id; evidenceID = pending.evidence_id;
    await resolveEvent(e,eventID);
    try {
      const receipt = await call(e,base + "operations/" + pending.body.operation_id);
      if (receipt.kind !== pending.kind || !receipt.completed) { status("unknown"); return; }
      await accepted(e,pending.kind === "commit" ? receipt.review : receipt.workspace);
    } catch (error) { if (error.status === 404) status("unknown"); else throw error; }
  }
  async function dispatch(e) {
    if (!pending || blocked) return;
    eventID = pending.event_id; evidenceID = pending.evidence_id;
    try { await accepted(e,await call(e,base + ({prepare:"workspaces",cancel:"cancellations",commit:"confirmations"})[pending.kind],"POST",pending.body)); }
    catch (error) {
      if (error.status === 400 || error.status === 409) { forget(); clearDetail(); status("conflict"); } else throw error;
    }
  }
  async function send(e, kind, body) {
    if (pending || blocked) return;
    const record = {owner,event_id:eventID,evidence_id:evidenceID,kind,body:{operation_id:crypto.randomUUID(),...body}};
    if (!recordValid(record)) throw {local:"invalid"};
    try { sessionStorage.setItem(key(),JSON.stringify(record)); } catch { throw {local:"storage"}; }
    pending = record; controls(); await dispatch(e);
  }
  function amount() {
    if (el.decision.value === "rejected") return 0;
    const scale = detail.evidence.currency_scale, text = el.amount.value.trim();
    if (!/^\d+(?:[.,]\d+)?$/.test(text) || text.length > 22) throw {local:"invalid"};
    const parts = text.replace(",",".").split("."); if ((parts[1]?.length || 0) > scale) throw {local:"invalid"};
    const v = BigInt(parts[0]) * 10n ** BigInt(scale) + BigInt((parts[1] || "").padEnd(scale,"0") || "0");
    if (v < 1n || v > BigInt(Number.MAX_SAFE_INTEGER)) throw {local:"invalid"}; return Number(v);
  }
  el.form.addEventListener("submit", event => { event.preventDefault(); return run(e => {
    if (!detail || detail.evidence.state !== "pending") return;
    return send(e,"prepare",{expected_revision:detail.workspace?.revision || 0,review:{event_id:eventID,evidence_id:evidenceID,expected_version:detail.balance.version,decision:el.decision.value,amount_minor:amount(),reason:el.reason.value.trim()}});
  }); });
  el.confirm.addEventListener("click",() => run(e => {
    if (dirty || !usableWorkspace()) { status(dirty ? "dirty" : "expired"); return; }
    const a = detail.workspace;
    return send(e,"commit",{canonical_proposal_id:a.preparation.proposal_id,confirmed:true,arguments:{workspace_id:a.workspace_id,revision:a.revision,review:a.preparation.arguments}});
  }));
  el.cancel.addEventListener("click",() => run(e => {
    const a = detail?.workspace; if (!a || !a.current || a.state !== "prepared") return;
    return send(e,"cancel",{workspace_id:a.workspace_id,revision:a.revision});
  }));
  for (const name of ["decision","amount","reason"]) el[name].addEventListener(name === "decision" ? "change" : "input",() => { dirty = true; controls(); });
  el.event.addEventListener("change",() => {
    const id = el.event.value;
    if (!id || dirty || pending || blocked) { el.event.value = eventID || ""; if (dirty) status("dirty"); return; }
    return run(e => loadEvidence(e,id));
  });
  el.evidence.addEventListener("change",() => {
    const id = el.evidence.value;
    if (!id || dirty || pending || blocked) { el.evidence.value = evidenceID || ""; if (dirty) status("dirty"); return; }
    return run(e => loadDetail(e,id));
  });
  for (const [name,cursor] of [["first",()=>undefined],["next",()=>eventNext]]) el[name].addEventListener("click",() => run(async e => {
    if (dirty || pending || blocked) { status("dirty"); return; } clearDetail(); eventID = evidenceID = undefined; el.evidence.replaceChildren(); await loadEvents(e,cursor());
  }));
  for (const [name,cursor] of [["evidence-first",()=>undefined],["evidence-next",()=>evidenceNext]]) el[name].addEventListener("click",() => run(async e => {
    if (dirty || pending || blocked) { status("dirty"); return; } if (eventID) await loadEvidence(e,eventID,cursor());
  }));
  el.reload.addEventListener("click",() => run(e => pending ? recover(e) : evidenceID ? loadDetail(e) : eventID ? loadEvidence(e,eventID,evidenceAfter) : loadEvents(e,eventAfter)));
  el.recover.addEventListener("click",() => run(recover)); el.retry.addEventListener("click",() => run(dispatch));
  el.proof.addEventListener("click",() => run(async e => {
    const blob = await options.readProof(detailPath() + "/content");
    if (e !== epoch || !owner) return;
    clearProof(); proofURL = URL.createObjectURL(blob); el.download.href = proofURL; el.download.hidden = false; status("proofReady");
  }));
  clear();
  return {
    clear, setBusy(value) { locked = value; controls(); },
    setLanguage(value) { language = value === "ru" ? "ru" : "en"; if (owner) { labels(); render(); } },
    async start(id, locale) {
      clear(); if (!uuid(id)) return; owner = id; language = locale === "ru" ? "ru" : "en"; labels(); section.hidden = false;
      const e = epoch;
      try { const raw = sessionStorage.getItem(key()); if (raw) { if (raw.length > 16384) throw new Error("Large request record"); pending = JSON.parse(raw); if (!recordValid(pending)) throw new Error("Invalid request record"); } }
      catch { blocked = true; pending = undefined; status("blocked"); controls(); return; }
      try { await loadEvents(e); if (pending) await recover(e); else await openLaunch(e); } catch (error) { if (e === epoch) errorStatus(error); }
      controls();
    }
  };
};
