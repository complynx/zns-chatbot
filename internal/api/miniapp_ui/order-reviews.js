"use strict";

// Capabilities carry the Mini App session. Recovery stores business intent only.
window.createZinusyaOrderReviews = function (env) {
  const section = document.getElementById("order-review");
  const names = ["title","note","status","event-label","event","event-first","event-next","evidence-label","evidence","evidence-first","evidence-next","detail","proof","download","form","decision-label","decision","reason-label","reason","save","preview","receipt","confirm","cancel","reload","recover","retry"];
  const el = Object.fromEntries(names.map(id => [id,document.getElementById("or-"+id)]));
  const words = {
    en: {title:"Order payment reviews",note:"Inspect the proof and the full saved decision before confirming. Cancelling a saved decision does not cancel the order or its payment claim.","event-label":"Event","evidence-label":"Payment proof","event-first":"First events","event-next":"Next events","evidence-first":"First proofs","evidence-next":"Next proofs",proof:"Load proof",download:"Download loaded proof","decision-label":"Decision","reason-label":"Reason",save:"Prepare decision",confirm:"Confirm saved decision",cancel:"Cancel saved decision",reload:"Reload current state",recover:"Check earlier request",retry:"Retry same request",choose:"Choose an event",chooseProof:"Choose a proof",confirmed:"Confirm payment",rejected:"Reject proof",pending:"Awaiting review",exhausted:"Places unavailable",needs_decision:"Needs ambassador decision",confirmedState:"Payment confirmed",rejectedState:"Proof rejected",prepared:"Saved decision prepared; no money confirmed",cancelled:"Saved decision cancelled; claim unchanged",completed:"Decision recorded",loading:"Loading…",ready:"Select a proof and review it.",empty:"No unresolved proofs on this page.",unknown:"The earlier result is unknown. Check it or retry the same request before making another change.",unavailable:"Could not load current state. Retry without replacing an earlier request.",denied:"Current review access is unavailable.",storage:"Browser session storage is unavailable. No new request was sent.",blocked:"The stored request is invalid. New requests are blocked on this page.",dirty:"Prepare your edit or reload current state before confirming or switching.",invalid:"Choose an allowed decision and enter a reason.",inspect:"Load and inspect this proof before confirming the exact saved decision.",conflict:"Request rejected. Read current state before preparing a new decision.",payer:"Payer",order:"Order",reported:"Reported amount",total:"Frozen order total",sent:"File sent",deadline:"Evidence deadline",state:"Current proof state",saved:"Exact saved decision",noSaved:"No saved decision.",expires:"Decision expires",credited:"Confirmed amount",historical:"Original request result",reason:"Reason",proofReady:"Proof loaded; inspect it using the download link."},
    ru: {title:"Проверка оплаты заказов",note:"Перед подтверждением проверь файл и полное сохранённое решение. Отмена решения не отменяет заказ или подтверждение оплаты.","event-label":"Эвент","evidence-label":"Подтверждение оплаты","event-first":"Первые эвенты","event-next":"Следующие эвенты","evidence-first":"Первые подтверждения","evidence-next":"Следующие подтверждения",proof:"Загрузить файл",download:"Скачать загруженный файл","decision-label":"Решение","reason-label":"Причина",save:"Подготовить решение",confirm:"Подтвердить сохранённое решение",cancel:"Отменить сохранённое решение",reload:"Загрузить текущее состояние",recover:"Проверить прежний запрос",retry:"Повторить тот же запрос",choose:"Выбери эвент",chooseProof:"Выбери подтверждение",confirmed:"Подтвердить оплату",rejected:"Отклонить подтверждение",pending:"Ожидает проверки",exhausted:"Места закончились",needs_decision:"Требуется решение амбассадора",confirmedState:"Оплата подтверждена",rejectedState:"Подтверждение отклонено",prepared:"Решение подготовлено; деньги не подтверждены",cancelled:"Решение отменено; подтверждение оплаты не изменено",completed:"Решение записано",loading:"Загружаю…",ready:"Выбери и проверь подтверждение оплаты.",empty:"На этой странице нет непроверенных подтверждений.",unknown:"Результат прежнего запроса неизвестен. Проверь его или повтори тот же запрос перед изменениями.",unavailable:"Не удалось загрузить состояние. Повтори прежний запрос, не заменяя его.",denied:"Доступ к проверке сейчас недоступен.",storage:"Хранилище сессии браузера недоступно. Новый запрос не отправлен.",blocked:"Запись прежнего запроса повреждена. Новые запросы заблокированы.",dirty:"Подготовь изменение или загрузи текущее состояние перед подтверждением или переключением.",invalid:"Выбери допустимое решение и укажи причину.",inspect:"Загрузи и проверь этот файл перед подтверждением точного сохранённого решения.",conflict:"Запрос отклонён. Проверь текущее состояние перед новым решением.",payer:"Плательщик",order:"Заказ",reported:"Заявлено",total:"Зафиксированная стоимость заказа",sent:"Файл отправлен",deadline:"Срок отправки подтверждения",state:"Текущее состояние подтверждения",saved:"Точное сохранённое решение",noSaved:"Нет сохранённого решения.",expires:"Подтвердить решение до",credited:"Подтверждённая сумма",historical:"Исходный результат запроса",reason:"Причина",proofReady:"Файл загружен; проверь его по ссылке для скачивания."}
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v) && !/^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(v);
  const integer = v => Number.isSafeInteger(v) && v >= 0 && v <= 9000000000000000;
  const positive = v => integer(v) && v > 0;
  const keys = (v,wanted) => v && typeof v === "object" && !Array.isArray(v) && Object.keys(v).sort().join() === wanted;
  const stamp = v => typeof v === "string" && /(Z|[+-]\d{2}:\d{2})$/.test(v) && Number.isFinite(Date.parse(v));
  const unresolved = e => ["pending","exhausted","needs_decision"].includes(e?.state);
  let owner, language="en", epoch=0, working=false, locked=false, blocked=false, dirty=false;
  let eventID,evidenceID,eventAfter,eventNext,evidenceAfter,evidenceNext,detail,pending,lastReceipt,statusKey;
  let events=[],rows=[],eventKnown=false,queueKnown=false,blobURL,loadedProof;
  const w = () => words[language];
  const base = "./order-reviews/";
  const key = () => "zinusya-order-review-v1:"+owner;
  function node(tag,text) { const n=document.createElement(tag);n.textContent=text;return n; }
  function status(name) { statusKey=name;el.status.textContent=w()[name]; }
  function stateName(state) { return w()[state === "confirmed" ? "confirmedState" : state === "rejected" ? "rejectedState" : state]; }
  function money(amount,scale,currency) { const digits=BigInt(amount).toString().padStart(scale+1,"0");return (scale ? digits.slice(0,-scale)+"."+digits.slice(-scale) : digits)+" "+currency; }
  function date(value,zone) { return new Intl.DateTimeFormat(language,{timeZone:zone,dateStyle:"medium",timeStyle:"long"}).format(new Date(value)); }
  function reviewValid(r) { return r && uuid(r.event_id) && uuid(r.evidence_id) && r.expected_version===1 && ["confirmed","rejected"].includes(r.decision) && typeof r.reason === "string" && r.reason.trim()!=="" && Array.from(r.reason).length<=512 && !r.reason.includes("\0"); }
  function sameReview(a,b) { return ["event_id","evidence_id","expected_version","decision","reason"].every(k=>a[k]===b[k]); }
  function eventValid(e) { if (!e || !uuid(e.id) || typeof e.name!=="string" || typeof e.timezone!=="string") throw Error("Invalid event");new Intl.DateTimeFormat(language,{timeZone:e.timezone});return e; }
  function claimValid(c) {
    if (!c || !uuid(c.id) || !uuid(c.order_id) || !uuid(c.user_id) || !uuid(c.file_id) || c.event_id!==eventID || !positive(c.version) || !positive(c.amount_minor) || !integer(c.quoted_amount_minor) || !["pending","exhausted","needs_decision","confirmed","rejected"].includes(c.state) || !Number.isInteger(c.currency_scale) || c.currency_scale<0 || c.currency_scale>6 || !/^[A-Z]{3}$/.test(c.currency) || !stamp(c.sent_at) || !stamp(c.due_at)) throw Error("Invalid proof");
    return c;
  }
  function workspaceValid(v) {
    if (!v || !uuid(v.id) || v.event_id!==eventID || v.evidence_id!==evidenceID || !positive(v.revision) || typeof v.current!=="boolean" || !["prepared","cancelled","completed"].includes(v.state)) throw Error("Invalid saved decision");
    const p=v.preparation;
    if (!p || !uuid(p.proposal_id) || !reviewValid(p.arguments) || p.arguments.event_id!==eventID || p.arguments.evidence_id!==evidenceID || !stamp(p.expires_at) || typeof p.description!=="string" || !p.description.trim() || p.description.length>65536) throw Error("Invalid saved summary");
    const c=claimValid(p.evidence);
    if (c.id!==evidenceID || (detail && ["order_id","user_id","file_id","amount_minor","quoted_amount_minor","currency","currency_scale"].some(k=>c[k]!==detail.evidence[k]))) throw Error("Saved proof mismatch");
    return v;
  }
  function detailValid(v) {
    const c=claimValid(v?.evidence),o=v?.order,q=o?.snapshot?.quote;
    if (c.id!==evidenceID || !o || o.id!==c.order_id || o.event_id!==eventID || !positive(o.version) || !["submitted","cancelled"].includes(o.state) || typeof o.snapshot.event_name!=="string" || !q || q.total_minor!==c.quoted_amount_minor || q.currency!==c.currency || q.currency_scale!==c.currency_scale || typeof q.timezone!=="string" || v.payer?.user_id!==c.user_id || typeof v.payer.full_name!=="string") throw Error("Invalid review detail");
    new Intl.DateTimeFormat(language,{timeZone:q.timezone});
    if (v.workspace!==null) workspaceValid(v.workspace);
    return v;
  }
  function dropProof() { if (blobURL) URL.revokeObjectURL(blobURL);blobURL=loadedProof=undefined;el.download.hidden=true;el.download.removeAttribute("href");el.download.download=""; }
  function clearReceipt() { lastReceipt=undefined;el.receipt.replaceChildren(); }
  function resetTarget() { clearReceipt();dropProof();detail=undefined;dirty=false;evidenceID=undefined;rows=[];queueKnown=false;evidenceAfter=evidenceNext=undefined;el.detail.replaceChildren();el.preview.replaceChildren();el.form.hidden=true;el.reason.value="";el.evidence.replaceChildren(); }
  function clear() {
    epoch++;resetTarget();owner=eventID=eventAfter=eventNext=pending=lastReceipt=statusKey=undefined;
    working=blocked=false;events=[];eventKnown=false;section.hidden=true;el.event.replaceChildren();el.receipt.replaceChildren();el.status.textContent="";controls();
  }
  function usable() { const a=detail?.workspace;return a?.state==="prepared" && a.current && Date.parse(a.preparation.expires_at)>Date.now() && a.preparation.arguments.expected_version===detail.evidence.version && unresolved(detail.evidence) && (a.preparation.arguments.decision!=="confirmed" || detail.evidence.state==="pending"); }
  function inspected() { return loadedProof?.event===eventID && loadedProof.id===evidenceID && loadedProof.file===detail?.evidence.file_id; }
  function controls() {
    const disabled=locked || working || !owner || blocked,frozen=disabled || !!pending;
    for (const id of ["event","event-first","event-next","evidence","evidence-first","evidence-next"]) el[id].disabled=frozen || dirty;
    for (const id of ["decision","reason","save"]) el[id].disabled=frozen || !unresolved(detail?.evidence);
    if (el.decision.options[0]) el.decision.options[0].disabled=detail?.evidence.state!=="pending";
    el.confirm.disabled=frozen || dirty || !usable() || !inspected();
    el.cancel.disabled=frozen || detail?.workspace?.state!=="prepared" || !detail.workspace.current;
    el.proof.disabled=disabled || !detail;el.reload.disabled=disabled;
    el.recover.hidden=el.retry.hidden=!pending;el.recover.disabled=el.retry.disabled=disabled;
    el["event-first"].hidden=eventKnown && !eventAfter;el["event-next"].hidden=!eventNext;
    el["evidence-first"].hidden=!eventID || (queueKnown && !evidenceAfter);el["evidence-next"].hidden=!evidenceNext;
  }
  function labels() { for (const id of names) if(w()[id]) el[id].textContent=w()[id];if (el.decision.options[0]) el.decision.options[0].textContent=w().confirmed;if(el.decision.options[1]) el.decision.options[1].textContent=w().rejected;if(statusKey) status(statusKey); }
  function addOption(select,value,text) { const n=node("option",text);n.value=value;select.append(n); }
  function renderEvents() { el.event.replaceChildren();addOption(el.event,"",w().choose);for (const e of events) addOption(el.event,e.id,e.name+" · "+e.timezone);el.event.value=eventID || ""; }
  function renderRows() {
    el.evidence.replaceChildren();addOption(el.evidence,"",w().chooseProof);
    const visible=new Map(rows.map(c=>[c.id,c]));if(detail) visible.set(detail.evidence.id,detail.evidence);
    const zone=detail?.order.snapshot.quote.timezone || events.find(e=>e.id===eventID)?.timezone;
    for (const c of visible.values()) addOption(el.evidence,c.id,money(c.amount_minor,c.currency_scale,c.currency)+" · "+stateName(c.state)+" · "+(zone?date(c.sent_at,zone):c.sent_at)+" · "+c.id.slice(0,8));
    el.evidence.value=evidenceID || "";
  }
  function renderDetail() {
    el.detail.replaceChildren();el.preview.replaceChildren();el.form.hidden=!detail;
    if (!detail) return;
    const c=detail.evidence,zone=detail.order.snapshot.quote.timezone;
    el.detail.append(node("h2",detail.order.snapshot.event_name),node("p",w().payer+": "+detail.payer.full_name),node("p",w().order+": "+c.order_id),node("p",w().state+": "+stateName(c.state)),node("p",w().reported+": "+money(c.amount_minor,c.currency_scale,c.currency)),node("p",w().total+": "+money(c.quoted_amount_minor,c.currency_scale,c.currency)),node("p",w().sent+": "+date(c.sent_at,zone)),node("p",w().deadline+": "+date(c.due_at,zone)+" · "+zone));
    if (c.review?.reason) el.detail.append(node("p",w().reason+": "+c.review.reason));
    const a=detail.workspace;
    if (!a) { el.preview.append(node("p",w().noSaved));return; }
    el.preview.append(node("h2",w().saved),node("p",w()[a.state]),node("pre",a.preparation.description),node("p",w().expires+": "+date(a.preparation.expires_at,zone)+" · "+zone));
  }
  function renderReceipt() {
    el.receipt.replaceChildren();if(!lastReceipt) return;
    const {kind,result}=lastReceipt;
    el.receipt.append(node("p",w().historical+": "+(kind==="save"?w().prepared:kind==="cancel"?w().cancelled:stateName(result.evidence.state))));
    if(kind==="commit") el.receipt.append(node("p",w().credited+": "+money(result.evidence.review.amount_minor,result.evidence.currency_scale,result.evidence.currency)),node("p",w().reason+": "+result.evidence.review.reason));
  }
  async function call(e,path,method="GET",body) { if(e!==epoch) throw {stale:true};try { const v=await env.request(path,method,body);if(e!==epoch) throw {stale:true};return v; } catch(error) { if(e!==epoch) throw {stale:true};if(error.status===401) {env.setBusy(false);env.denied();}throw error; } }
  function fail(error) { if(error.stale) return;if(error.status===403) {dropProof();detail=undefined;renderDetail();status("denied");}else status(error.local || (pending?"unknown":"unavailable")); }
  async function run(fn) { if(!owner || working || locked || blocked) return;const e=epoch;working=true;env.setBusy(true);controls();try {await fn(e);}catch(error){if(e===epoch) fail(error);}finally {if(e===epoch) {working=false;env.setBusy(false);controls();}} }
  async function loadEvents(e,after) {
    const page=await call(e,base+"events"+(after?"?after="+after:""));
    if(!page || !Array.isArray(page.events) || page.events.length>20 || (page.next!==undefined && !uuid(page.next))) throw Error("Invalid event page");
    events=page.events.map(eventValid);eventKnown=true;eventAfter=after;eventNext=page.next;eventID=undefined;resetTarget();renderEvents();renderRows();status("ready");
  }
  async function ensureEvent(e) {
    if(events.some(v=>v.id===eventID)) return;
    const found=eventValid(await call(e,base+"events/"+eventID));if(found.id!==eventID) throw Error("Wrong event");
    events=[found];eventKnown=false;eventAfter=eventNext=undefined;renderEvents();
  }
  async function loadQueue(e,id,after) {
    eventID=id;resetTarget();await ensureEvent(e);
    const page=await call(e,base+"events/"+id+"/evidence"+(after?"?after="+after:""));
    if(!page || !Array.isArray(page.evidence) || page.evidence.length>20 || (page.next!==undefined && !uuid(page.next))) throw Error("Invalid proof page");
    rows=page.evidence.map(claimValid);queueKnown=true;evidenceAfter=after;evidenceNext=page.next;renderEvents();renderRows();status(rows.length?"ready":"empty");
  }
  async function loadTarget(e) {
    await ensureEvent(e);
    const value=await call(e,base+"events/"+eventID+"/evidence/"+evidenceID),checked=detailValid(value);
    if(detail && (detail.evidence.id!==checked.evidence.id || detail.evidence.file_id!==checked.evidence.file_id)) dropProof();
    detail=checked;dirty=false;
    el.decision.value=detail.workspace?.preparation.arguments.decision || (detail.evidence.state==="pending"?"confirmed":"rejected");
    el.reason.value=detail.workspace?.preparation.arguments.reason || "";
    renderEvents();renderRows();renderDetail();status("ready");
  }
  function validPending(p) {
    if(!keys(p,"body,event_id,evidence_id,kind,owner") || p.owner!==owner || !uuid(p.event_id) || !uuid(p.evidence_id) || !uuid(p.body?.operation_id)) return false;
    const b=p.body;
    if(p.kind==="save") return keys(b,"expected_revision,language,operation_id,review") && integer(b.expected_revision) && ["en","ru"].includes(b.language) && reviewValid(b.review) && b.review.event_id===p.event_id && b.review.evidence_id===p.evidence_id;
    if(p.kind==="cancel") return keys(b,"operation_id,revision,workspace_id") && positive(b.revision) && uuid(b.workspace_id);
    if(p.kind!=="commit" || !keys(b,"arguments,canonical_proposal_id,confirmed,operation_id") || b.confirmed!==true || !uuid(b.canonical_proposal_id)) return false;
    const a=b.arguments;return keys(a,"review,revision,workspace_id") && positive(a.revision) && uuid(a.workspace_id) && reviewValid(a.review) && a.review.event_id===p.event_id && a.review.evidence_id===p.evidence_id;
  }
  function forget() { try {sessionStorage.removeItem(key());}catch{throw {local:"storage"};}pending=undefined; }
  function acceptedResult(result) {
    const b=pending.body;
    if(pending.kind!=="commit") {
      const a=workspaceValid(result);
      if(pending.kind==="save" ? a.revision!==b.expected_revision+1 || !sameReview(a.preparation.arguments,b.review) || a.state!=="prepared" : a.id!==b.workspace_id || a.revision!==b.revision || a.state!=="cancelled") throw Error("Wrong saved result");
      return a;
    }
    const c=claimValid(result?.evidence),r=c.review,a=b.arguments;
    if(result.canonical_operation_id!==a.workspace_id || c.id!==evidenceID || c.state!==a.review.decision || c.version!==a.review.expected_version+1 || !r || r.actor_id!==owner || r.decision!==a.review.decision || r.reason!==a.review.reason || r.amount_minor!==(c.state==="confirmed"?c.amount_minor:0) || (detail && ["order_id","user_id","file_id","amount_minor","quoted_amount_minor","currency","currency_scale"].some(k=>c[k]!==detail.evidence[k]))) throw Error("Wrong decision receipt");
    return result;
  }
  async function accept(e,result) { const record={kind:pending.kind,result:acceptedResult(result)};forget();lastReceipt=record;renderReceipt();await loadTarget(e);status(record.kind==="save"?"prepared":record.kind==="cancel"?"cancelled":"completed"); }
  async function recover(e) {
    eventID=pending.event_id;evidenceID=pending.evidence_id;await loadTarget(e);
    try {
      const receipt=await call(e,base+"operations/"+pending.body.operation_id);
      if(receipt.operation_id!==pending.body.operation_id || receipt.kind!==pending.kind || receipt.source!=="miniapp" || receipt.completed!==true) throw Error("Wrong operation receipt");
      await accept(e,pending.kind==="commit"?receipt.review:receipt.workspace);
    }catch(error){if(error.status===404) status("unknown");else throw error;}
  }
  async function dispatch(e) {
    const action={save:"workspaces",cancel:"cancellations",commit:"confirmations"}[pending.kind];
    try {await accept(e,await call(e,base+action,"POST",pending.body));}
    catch(error){if(error.status===400 || error.status===409) {forget();dropProof();await loadTarget(e);status("conflict");}else throw error;}
  }
  async function begin(e,kind,body) {
    if(pending || blocked) return;
    const record={owner,event_id:eventID,evidence_id:evidenceID,kind,body};
    try {const raw=JSON.stringify(record);sessionStorage.setItem(key(),raw);if(sessionStorage.getItem(key())!==raw) throw Error("Storage unavailable");}catch{throw {local:"storage"};}
    pending=record;controls();await dispatch(e);
  }
  el.form.addEventListener("submit",event=>{event.preventDefault();return run(async e=>{
    if(pending || !unresolved(detail?.evidence)) return;
    const review={event_id:eventID,evidence_id:evidenceID,expected_version:detail.evidence.version,decision:el.decision.value,reason:el.reason.value.trim()};
    if(!reviewValid(review) || (review.decision==="confirmed" && detail.evidence.state!=="pending")) throw {local:"invalid"};
    await begin(e,"save",{operation_id:crypto.randomUUID(),expected_revision:detail.workspace?.revision || 0,language,review});
  });});
  el.confirm.addEventListener("click",()=>run(async e=>{
    if(pending) return;if(dirty) throw {local:"dirty"};if(!usable() || !inspected()) throw {local:"inspect"};
    const a=detail.workspace;
    await begin(e,"commit",{operation_id:crypto.randomUUID(),canonical_proposal_id:a.preparation.proposal_id,confirmed:true,arguments:{workspace_id:a.id,revision:a.revision,review:{...a.preparation.arguments}}});
  }));
  el.cancel.addEventListener("click",()=>run(async e=>{
    const a=detail?.workspace;if(pending || a?.state!=="prepared" || !a.current) return;
    await begin(e,"cancel",{operation_id:crypto.randomUUID(),workspace_id:a.id,revision:a.revision});
  }));
  el.proof.addEventListener("click",()=>run(async e=>{
    if(!detail) return;const id=evidenceID,file=detail.evidence.file_id;dropProof();
    const blob=await env.readProof(base+"events/"+eventID+"/evidence/"+id+"/content");
    if(e!==epoch || detail?.evidence.id!==id || detail.evidence.file_id!==file) return;
    blobURL=URL.createObjectURL(blob);loadedProof={event:eventID,id,file};el.download.href=blobURL;
    el.download.download="order-proof"+({"application/pdf":".pdf","image/jpeg":".jpg","image/png":".png"}[blob.type] || ".bin");el.download.hidden=false;status("proofReady");
  }));
  for(const id of ["reason","decision"]) el[id].addEventListener(id==="reason"?"input":"change",()=>{dirty=true;status("dirty");controls();});
  el.event.addEventListener("change",()=>run(async e=>{if(dirty) throw {local:"dirty"};const id=el.event.value;if(!uuid(id)) {eventID=undefined;resetTarget();status("ready");return;}await loadQueue(e,id);}));
  el.evidence.addEventListener("change",()=>run(async e=>{if(dirty) throw {local:"dirty"};const id=el.evidence.value;clearReceipt();dropProof();detail=undefined;evidenceID=uuid(id)?id:undefined;renderDetail();if(evidenceID) await loadTarget(e);else status("ready");}));
  el["event-first"].addEventListener("click",()=>run(e=>loadEvents(e)));
  el["event-next"].addEventListener("click",()=>run(e=>loadEvents(e,eventNext)));
  el["evidence-first"].addEventListener("click",()=>run(e=>loadQueue(e,eventID)));
  el["evidence-next"].addEventListener("click",()=>run(e=>loadQueue(e,eventID,evidenceNext)));
  el.reload.addEventListener("click",()=>run(e=>pending?recover(e):evidenceID?loadTarget(e):eventID?loadQueue(e,eventID):loadEvents(e)));
  el.recover.addEventListener("click",()=>run(recover));el.retry.addEventListener("click",()=>run(dispatch));
  clear();
  return {
    clear,setBusy(value){locked=value;controls();},
    setLanguage(locale){language=locale==="ru"?"ru":"en";labels();renderEvents();renderRows();renderDetail();renderReceipt();controls();},
    async start(id,locale){
      clear();if(!uuid(id)) return;owner=id;language=locale==="ru"?"ru":"en";labels();section.hidden=false;working=true;const e=epoch;status("loading");
      try {
        let raw;try {raw=sessionStorage.getItem(key());}catch{blocked=true;status("storage");return;}
        if(raw) {try{if(raw.length>16384) throw Error("Oversized recovery record");pending=JSON.parse(raw);if(!validPending(pending)) throw Error("Invalid recovery record");}catch{blocked=true;status("blocked");return;}}
        if(pending) {await recover(e);return;}
        const hash=window.location?.hash || "",params=new URLSearchParams(hash.startsWith("#order-review?")?hash.slice(14):"");
        if(uuid(params.get("event")) && uuid(params.get("evidence"))) {eventID=params.get("event");evidenceID=params.get("evidence");await loadTarget(e);return;}
        await loadEvents(e);if(!events.length) section.hidden=true;
      }catch(error){if(e===epoch) fail(error);}finally{if(e===epoch){working=false;controls();}}
    }
  };
};
