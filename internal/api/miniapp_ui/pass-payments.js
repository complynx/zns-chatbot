"use strict";

// Exact owner-bound journals reconcile uncertain writes; receipts never imply money.
window.createZinusyaPassPayments = function (env) {
  const section = document.getElementById("pass-payments");
  const ids = ["title","note","status","targets","targets-first","targets-next","target","history","history-next","recent","recent-list","file-label","file","files-first","files-next","preview","download","preview-info","amount-label","amount","purpose-label","purpose","initial","top_up","form","submit","reload","recover","retry","receipt"];
  const el = Object.fromEntries(ids.map(id => [id, document.getElementById("pp-" + id)]));
  const words = {
    en:{title:"Pass payment evidence",note:"First send the file to this Telegram bot, then select it here. Sending shares this file with the event payment reviewers. It remains pending and adds no confirmed money until an ambassador reviews it.",targets:"Choose an assigned pass",recent:"Recent form submissions", "targets-first":"First passes","targets-next":"Next passes","file-label":"Accepted Telegram file","files-first":"First files","files-next":"Next files",preview:"Load selected file",download:"Download loaded file","amount-label":"Reported amount","purpose-label":"Purpose",initial:"Initial payment / deposit",top_up:"Top-up",submit:"Share evidence for review",reload:"Reload pass and evidence",recover:"Check earlier request",retry:"Retry the exact same request","history-next":"More evidence",loading:"Loading…",ready:"Choose the file, amount and purpose explicitly before sharing.",unknown:"Earlier result unknown. Check it or retry the exact same request before creating another.",blocked:"Session storage is unavailable or invalid. New submissions are blocked; server history remains readable.",invalid:"Choose and load an accepted file, enter a positive exact amount, and choose the purpose.",unavailable:"Could not load current state. Retry the read.",recorded:"Original submission recorded. Current evidence and confirmed balance are shown separately.",conflict:"Request rejected. Reload current evidence before submitting again.",empty:"No evidence on this page.",noFiles:"No accepted files on this page.",choose:"Choose…",targetUnavailable:"This assigned pass is unavailable. Historical evidence remains readable; new submissions are disabled.",price:"Frozen pass price",tier:"Frozen tier",minimum:"Deposit minimum",share:"Deposit share",required:"Required deposit",confirmed:"Confirmed paid",remaining:"Remaining",pending:"Evidence awaiting review (not credited)",due:"Initial evidence deadline",final:"Final payment deadline",overdue:"Deadline passed. Your pass remains assigned; contact an event ambassador.",original:"Original operation result",current:"Current evidence",sent:"File sent",late:"Late evidence; requires human resolution",timely:"Sent by the deadline",read:"Load this evidence file",open:"Read original submission",pending_state:"Pending review; no money credited",confirmed_state:"Confirmed",rejected_state:"Rejected",file:"File",purpose:"Purpose",reported:"Reported",pass:"Pass"},
    ru:{title:"Подтверждение оплаты пасса",note:"Сначала отправь файл этому боту в Telegram, затем выбери его здесь. Отправка открывает файл проверяющим оплату этого эвента. До проверки амбассадором это только ожидающее подтверждение, без зачисления денег.",targets:"Выбери назначенный пасс",recent:"Недавние отправки через форму","targets-first":"Первые пассы","targets-next":"Следующие пассы","file-label":"Принятый файл Telegram","files-first":"Первые файлы","files-next":"Следующие файлы",preview:"Загрузить выбранный файл",download:"Скачать загруженный файл","amount-label":"Заявленная сумма","purpose-label":"Назначение",initial:"Первый платёж / предоплата",top_up:"Доплата",submit:"Передать подтверждение на проверку",reload:"Обновить пасс и подтверждения",recover:"Проверить прежний запрос",retry:"Повторить точно тот же запрос","history-next":"Ещё подтверждения",loading:"Загружаю…",ready:"Перед отправкой явно выбери файл, сумму и назначение.",unknown:"Результат прежнего запроса неизвестен. Проверь его или повтори точно тот же запрос перед новой отправкой.",blocked:"Хранилище сессии недоступно или повреждено. Новые отправки заблокированы; история на сервере доступна.",invalid:"Выбери и загрузи принятый файл, укажи положительную точную сумму и назначение.",unavailable:"Не удалось загрузить состояние. Повтори чтение.",recorded:"Исходная отправка записана. Текущие подтверждения и подтверждённый баланс показаны отдельно.",conflict:"Запрос отклонён. Обнови подтверждения перед новой отправкой.",empty:"На этой странице нет подтверждений.",noFiles:"На этой странице нет принятых файлов.",choose:"Выбери…",targetUnavailable:"Назначенный пасс недоступен. История подтверждений доступна; новые отправки отключены.",price:"Зафиксированная цена пасса",tier:"Зафиксированный тариф",minimum:"Минимальная предоплата",share:"Доля предоплаты",required:"Требуемая предоплата",confirmed:"Подтверждено оплачено",remaining:"Осталось",pending:"Ожидают проверки (не зачислены)",due:"Срок первого подтверждения",final:"Срок доплаты",overdue:"Срок прошёл. Пасс остаётся назначенным; свяжись с амбассадором эвента.",original:"Исходный результат операции",current:"Текущее подтверждение",sent:"Файл отправлен",late:"Подтверждение с опозданием; требуется решение человека",timely:"Отправлено в срок",read:"Загрузить файл подтверждения",open:"Прочитать исходную отправку",pending_state:"Ожидает проверки; деньги не зачислены",confirmed_state:"Подтверждено",rejected_state:"Отклонено",file:"Файл",purpose:"Назначение",reported:"Заявлено",pass:"Пасс"}
  };
  const uuid = v => typeof v === "string" && /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(v) && v !== "00000000-0000-0000-0000-000000000000";
  const natural = v => Number.isSafeInteger(v) && v >= 0;
  const positive = v => natural(v) && v > 0;
  const validMoney = v => v && natural(v.price_minor) && /^[A-Z]{3}$/.test(v.currency) && Number.isInteger(v.currency_scale) && v.currency_scale >= 0 && v.currency_scale <= 4;
  let owner, language="en", epoch=0, working=false, locked=false, blocked=false, pending, target, eventID, passID, original, originalTimezone, statusName;
  let targets=[], files=[], evidence=[], actions=[], buttons=[], targetsNext, targetsAfter, filesNext, filesAfter, historyNext, historyReady=false, loadedFile, blobURL;
  const w = () => words[language];
  const key = () => "zinusya-pass-payments-v1:" + owner;
  const base = () => "./pass-payments/events/" + eventID;
  function node(tag, text) { const n=document.createElement(tag); n.textContent=text; return n; }
  function status(name) { statusName=name; el.status.textContent=w()[name]; }
  function decimal(amount, scale) { const s=BigInt(amount).toString().padStart(scale+1,"0"); return scale ? s.slice(0,-scale)+"."+s.slice(-scale) : s; }
  function money(amount, m) { if (!natural(amount) || !validMoney(m)) throw Error("Invalid money"); return decimal(amount,m.currency_scale)+" "+m.currency; }
  function date(value, timezone=target?.timezone||originalTimezone) { if (!Number.isFinite(Date.parse(value)) || typeof timezone!=="string" || !timezone) throw Error("Invalid date"); return new Intl.DateTimeFormat(language,{timeZone:timezone,dateStyle:"medium",timeStyle:"long"}).format(new Date(value))+" · "+timezone; }
  function labels() { for (const id of ["title","note","recent","targets-first","targets-next","file-label","files-first","files-next","preview","download","amount-label","purpose-label","initial","top_up","submit","reload","recover","retry","history-next"]) el[id].textContent=w()[id]; }
  function controls() {
    const busy=locked||working||!owner, edit=busy||blocked||!!pending||!target||!historyReady||!!historyNext;
    el.file.disabled=el.amount.disabled=el.purpose.disabled=edit; el.submit.disabled=edit||!loadedFile||loadedFile!==el.file.value;
    el.targets.disabled=busy||!!pending; el["targets-first"].disabled=el["targets-next"].disabled=busy||!!pending;
    el["targets-first"].hidden=!targetsAfter; el["targets-next"].hidden=!targetsNext;
    el["files-first"].hidden=!filesAfter; el["files-next"].hidden=!filesNext; el["files-first"].disabled=el["files-next"].disabled=edit;
    el.preview.disabled=busy||!target||!(pending?.body.file_id||el.file.value); el.reload.disabled=busy||!eventID;
    el.recover.hidden=el.retry.hidden=!pending; el.recover.disabled=busy; el.retry.disabled=busy||blocked;
    el.recent.disabled=busy; el["history-next"].hidden=!historyNext; el["history-next"].disabled=busy;
    for (const b of buttons) b.disabled=busy||!!pending;
  }
  function dropPreview() { if(blobURL) URL.revokeObjectURL(blobURL); blobURL=loadedFile=undefined; el.download.hidden=true; el.download.removeAttribute("href"); el.download.download=""; el["preview-info"].textContent=""; }
  function clear() { epoch++; dropPreview(); owner=pending=target=eventID=passID=original=originalTimezone=statusName=undefined; working=blocked=historyReady=false; targets=[]; files=[]; evidence=[]; actions=[]; buttons=[]; targetsNext=targetsAfter=filesNext=filesAfter=historyNext=undefined; for(const id of ["target","history","receipt","recent-list","targets","file"]) el[id].replaceChildren(); el.amount.value=el.purpose.value=""; el.status.textContent=""; section.hidden=true; controls(); }
  async function call(e,url,method="GET",body) { if(e!==epoch) throw {stale:true}; const result=await env.request(url,method,body); if(e!==epoch) throw {stale:true}; return result; }
  function fail(error) { if(error.status===401||error.status===403) { env.setBusy(false); clear(); if(error.status===401) env.denied(); return; } status(error.local||(pending?"unknown":blocked?"blocked":"unavailable")); }
  async function run(fn, startup=false) { if(working||(locked&&!startup)||!owner) return; const e=epoch; working=true; env.setBusy(true); controls(); try { await fn(e); } catch(error) { if(e===epoch&&!error.stale) fail(error); } finally { if(e===epoch) {working=false;env.setBusy(false);controls();} } }
  function checkTarget(v) { const r=v?.registration,p=r?.pass,b=v?.balance; if(!v||!uuid(v.event_id)||r.user_id!==owner||r.event_id!==v.event_id||!uuid(p?.id)||!validMoney(p)||!validMoney(b)||b.pass_id!==p.id||p.price_minor!==b.price_minor||p.currency!==b.currency||p.currency_scale!==b.currency_scale||!natural(b.confirmed_minor)||!natural(b.remaining_minor)||!natural(b.deposit_required_minor)||!natural(v.pending_evidence)||typeof v.event_name!=="string") throw Error("Invalid pass"); new Intl.DateTimeFormat(language,{timeZone:v.timezone}); return v; }
  function checkEvidence(v) { if(!v||!uuid(v.id)||v.user_id!==owner||v.event_id!==eventID||v.pass_id!==passID||!uuid(v.file_id)||!positive(v.amount_minor)||!validMoney({price_minor:0,...v})||!["initial","top_up"].includes(v.purpose)||!["pending","confirmed","rejected"].includes(v.state)||typeof v.timely!=="boolean") throw Error("Invalid evidence"); if(target&&(v.currency!==target.registration.pass.currency||v.currency_scale!==target.registration.pass.currency_scale)) throw Error("Evidence currency mismatch"); for(const d of [v.sent_at,v.due_at,v.submitted_at]) if(!Number.isFinite(Date.parse(d))) throw Error("Invalid evidence date"); return v; }
  function renderTarget() {
    el.target.replaceChildren(); if(!target) return; const p=target.registration.pass,b=target.balance;
    el.target.append(node("h2",target.event_name),node("p",w().pass+": "+p.id),node("p",w().price+": "+money(p.price_minor,p)));
    if(p.tier) el.target.append(node("p",w().tier+": "+p.tier.id+" / "+p.tier.number));
    if(p.deposit) el.target.append(node("p",w().minimum+": "+money(p.deposit.minimum_minor,p)),node("p",w().share+": "+decimal(p.deposit.minimum_bps,2)+"%"));
    for(const [label,value] of [["required",b.deposit_required_minor],["confirmed",b.confirmed_minor],["remaining",b.remaining_minor]]) el.target.append(node("p",w()[label]+": "+money(value,p)));
    el.target.append(node("p",w().pending+": "+target.pending_evidence),node("p",w().due+": "+date(p.payment_due_at)));
    const final=p.deposit?.final_due_at||p.payment_due_at; el.target.append(node("p",w().final+": "+date(final)));
    if(b.remaining_minor>0&&Date.parse(b.deposit_satisfied?final:p.payment_due_at)<Date.now()) el.target.append(node("p",w().overdue));
    el["amount-label"].textContent=w()["amount-label"]+" ("+p.currency+")";
  }
  function evidenceView(v,historical) { const n=node("article",""); n.append(node("p",w()[historical?"original":"current"]+": "+w()[v.state+"_state"]),node("p",w().reported+": "+money(v.amount_minor,{price_minor:0,...v})),node("p",w().purpose+": "+w()[v.purpose]),node("p",w().file+": "+v.file_id),node("p","ID: "+v.id),node("p",w().sent+": "+date(v.sent_at,historical?originalTimezone:target?.timezone||originalTimezone)),node("p",date(v.due_at,historical?originalTimezone:target?.timezone||originalTimezone)+" · "+w()[v.timely?"timely":"late"])); if(v.review?.reason) n.append(node("p",v.review.reason)); if(!historical) { const b=node("button",w().read); b.type="button"; b.addEventListener("click",()=>run(e=>loadProof(e,v.file_id,false))); buttons.push(b); n.append(b); } return n; }
  function renderHistory() { el.history.replaceChildren(); if(!evidence.length) el.history.append(node("p",w().empty)); for(const v of evidence) el.history.append(evidenceView(v,false)); }
  function renderFiles() { const selected=el.file.value; const blank=node("option",files.length?w().choose:w().noFiles); blank.value=""; el.file.replaceChildren(blank); for(const f of files) {const n=node("option",(f.filename||f.kind)+" · "+date(f.sent_at)+" · "+f.id); n.value=f.id; el.file.append(n);} el.file.value=files.some(f=>f.id===selected)?selected:""; }
  async function loadTargets(e,cursor) { const page=await call(e,"./pass-payments/targets"+(cursor?"?after="+cursor:"")); if(!Array.isArray(page.targets)||page.targets.length>20||(page.next!==undefined&&!uuid(page.next))) throw Error("Invalid targets"); targets=page.targets.map(checkTarget); targetsNext=page.next; targetsAfter=cursor; const blank=node("option",w().targets); blank.value=""; el.targets.replaceChildren(blank); for(const t of targets) {const n=node("option",t.event_name+" · "+t.registration.pass.id); n.value=t.registration.pass.id; el.targets.append(n);} el.targets.value=targets.some(t=>t.registration.pass.id===passID)?passID:""; }
  async function loadFiles(e,cursor) { const page=await call(e,base()+"/files"+(cursor?"?after="+cursor:"")); if(!Array.isArray(page.files)||page.files.length>20||(page.next!==undefined&&!uuid(page.next))) throw Error("Invalid files"); for(const f of page.files) if(!uuid(f.id)||typeof f.filename!=="string"||!Number.isFinite(Date.parse(f.sent_at))) throw Error("Invalid file"); dropPreview(); files=page.files; filesNext=page.next; filesAfter=cursor; renderFiles(); }
  async function loadHistory(e,cursor) { if(!cursor) historyReady=false; const page=await call(e,base()+"/evidence?pass_id="+passID+(cursor?"&after="+cursor:"")); if(!Array.isArray(page.evidence)||page.evidence.length>20||(page.next!==undefined&&!uuid(page.next))) throw Error("Invalid evidence page"); evidence=page.evidence.map(checkEvidence); historyNext=page.next; historyReady=true; renderHistory(); }
  async function load(e, historical=false) {
    target=undefined; el.target.replaceChildren();
    try {
      const v=checkTarget(await call(e,base()+"/passes/"+passID));
      if(v.event_id!==eventID||v.registration.pass.id!==passID) throw Error("Wrong target");
      target=v; renderTarget();
    } catch(error) { if(!historical||error.status!==404) throw error; dropPreview(); el.target.replaceChildren(node("p",w().targetUnavailable)); }
    await loadHistory(e);
    if(target&&!pending) await loadFiles(e);
    status(pending?"unknown":blocked?"blocked":historical?"recorded":"ready");
  }
  // The existing receipt wrapper supplies event timezone independently of assignment.
  function receipt(value, operation) {
    if(value.operation_id!==operation||value.kind!=="submit"||typeof value.timezone!=="string"||!value.timezone) throw Error("Wrong receipt");
    new Intl.DateTimeFormat(language,{timeZone:value.timezone});
    return {result:checkEvidence(value.result),timezone:value.timezone};
  }
  async function recent(e) {
    const page=await call(e,"./pass-payments/recent");
    if(!Array.isArray(page.actions)||page.actions.length>20) throw Error("Invalid recent actions");
    actions=page.actions; el["recent-list"].replaceChildren();
    for(const a of actions) {
      if(!uuid(a.event_id)||!uuid(a.pass_id)||typeof a.operation_id!=="string"||!a.operation_id||a.operation_id.length>128) throw Error("Invalid action");
      const n=node("button",a.event_name+" · "+a.pass_id+" · "+w().open); n.type="button";
      n.addEventListener("click",()=>run(async e=>{
        if(pending)return;
        resetTarget(a.event_id,a.pass_id);
        const r=receipt(await call(e,base()+"/operations/"+encodeURIComponent(a.operation_id)),a.operation_id);
        original=r.result; originalTimezone=r.timezone;
        el.receipt.replaceChildren(evidenceView(original,true));
        await load(e,true);
      }));
      buttons.push(n); el["recent-list"].append(n);
    }
  }
  function validPending(p) { const b=p?.body; return p&&Object.keys(p).sort().join()==="body,event_id,owner"&&p.owner===owner&&uuid(p.event_id)&&b&&Object.keys(b).sort().join()==="amount_minor,file_id,operation_id,pass_id,purpose"&&uuid(b.operation_id)&&uuid(b.pass_id)&&uuid(b.file_id)&&positive(b.amount_minor)&&["initial","top_up"].includes(b.purpose); }
  function resetTarget(event,pass) { dropPreview(); eventID=event; passID=pass; target=original=originalTimezone=undefined; historyReady=false; historyNext=filesNext=filesAfter=undefined; evidence=[]; files=[]; el.amount.value=el.purpose.value=""; for(const id of ["target","history","receipt","file"]) el[id].replaceChildren(); }
  function forget() { try {sessionStorage.removeItem(key()); if(sessionStorage.getItem(key())!==null) throw Error();} catch { blocked=true; throw {local:"blocked"};} pending=undefined; }
  async function accept(e,value,timezone=target?.timezone) {
    const v=checkEvidence(value),b=pending.body;
    if(v.file_id!==b.file_id||v.amount_minor!==b.amount_minor||v.purpose!==b.purpose) throw Error("Wrong receipt");
    if(typeof timezone!=="string"||!timezone) throw Error("Missing receipt timezone");
    new Intl.DateTimeFormat(language,{timeZone:timezone});
    original=v; originalTimezone=timezone;
    el.receipt.replaceChildren(evidenceView(v,true));
    forget();
    await load(e,true);
    status("recorded");
  }
  async function recover(e) {
    if(!pending) return false;
    let value;
    try { value=await call(e,base()+"/operations/"+encodeURIComponent(pending.body.operation_id)); }
    catch(error) { if(error.status!==404) throw error; status("unknown"); return false; }
    const r=receipt(value,pending.body.operation_id);
    await accept(e,r.result,r.timezone);
    return true;
  }
  async function dispatch(e) {
    if(!pending||blocked) return;
    if(!target) {
      if(await recover(e)) return;
      await load(e);
    }
    try { await accept(e,await call(e,base()+"/evidence","POST",pending.body)); }
    catch(error) { if(error.status===400||error.status===409) {forget(); await load(e); status("conflict");} else throw error; }
  }
  function amount() { const scale=target.registration.pass.currency_scale,value=el.amount.value.trim(); const separator=new Intl.NumberFormat(language).formatToParts(1.1).find(p=>p.type==="decimal").value; const normalized=value.split(separator).join("."); if(!/^\d+(?:\.\d+)?$/.test(normalized)||value.length>24) throw {local:"invalid"}; const [whole,fraction=""]=normalized.split("."); if(fraction.length>scale) throw {local:"invalid"}; const result=BigInt(whole)*10n**BigInt(scale)+BigInt(fraction.padEnd(scale,"0")||"0"); if(result<1n||result>BigInt(Number.MAX_SAFE_INTEGER)) throw {local:"invalid"}; return Number(result); }
  async function loadProof(e,id,selection) { if(!uuid(id)) throw {local:"invalid"}; const blob=await env.readProof(base()+"/files/"+id+"/content"); if(e!==epoch) return; dropPreview(); blobURL=URL.createObjectURL(blob); loadedFile=selection?id:undefined; el.download.href=blobURL; el.download.download=files.find(f=>f.id===id)?.filename||"payment-evidence"; el.download.hidden=false; el["preview-info"].textContent=w().file+": "+id; }
  el.form.addEventListener("submit",event=>{event.preventDefault();return run(async e=>{if(blocked||pending||!target||!historyReady||historyNext||loadedFile!==el.file.value||!files.some(f=>f.id===loadedFile)||!["initial","top_up"].includes(el.purpose.value)) throw {local:"invalid"}; const record={owner,event_id:eventID,body:{operation_id:crypto.randomUUID(),pass_id:passID,file_id:loadedFile,amount_minor:amount(),purpose:el.purpose.value}}; try {const raw=JSON.stringify(record);sessionStorage.setItem(key(),raw);if(sessionStorage.getItem(key())!==raw) throw Error();} catch {blocked=true;throw {local:"blocked"};} pending=record; await dispatch(e);});});
  el.file.addEventListener("change",()=>{dropPreview();controls();});
  el.targets.addEventListener("change",()=>run(async e=>{if(pending)return;const t=targets.find(t=>t.registration.pass.id===el.targets.value);if(t){resetTarget(t.event_id,t.registration.pass.id);await load(e);}}));
  el.preview.addEventListener("click",()=>run(e=>loadProof(e,pending?.body.file_id||el.file.value,true)));
  for(const [id,fn] of [["reload",e=>pending?recover(e):load(e,!!original)],["recover",recover],["retry",dispatch],["recent",recent],["targets-first",e=>loadTargets(e)],["targets-next",e=>loadTargets(e,targetsNext)],["files-first",e=>loadFiles(e)],["files-next",e=>loadFiles(e,filesNext)],["history-next",e=>loadHistory(e,historyNext)]]) el[id].addEventListener("click",()=>run(fn));
  clear();
  return {clear,setBusy(value){locked=value;controls();},setLocked(value){locked=value;controls();},setLanguage(locale){language=locale==="ru"?"ru":"en";labels();if(target)renderTarget();else if(originalTimezone)el.target.replaceChildren(node("p",w().targetUnavailable));if(target||originalTimezone){renderHistory();renderFiles();}if(original)el.receipt.replaceChildren(evidenceView(original,true));if(statusName)status(statusName);controls();},
    async start(id,locale){clear();if(!uuid(id))return;owner=id;language=locale==="ru"?"ru":"en";labels();section.hidden=false;try{const raw=sessionStorage.getItem(key());if(raw){if(raw.length>2048)throw Error();const p=JSON.parse(raw);if(!validPending(p))throw Error();pending=p;eventID=p.event_id;passID=p.body.pass_id;}}catch{blocked=true;status("blocked");}return run(async e=>{if(pending)await recover(e);await loadTargets(e);await recent(e);if(pending)status("unknown");else if(blocked)status("blocked");else if(!original)status("ready");},true);},
    async show(event,pass){if(!owner||locked||working||!uuid(event)||!uuid(pass))return;if(pending){status("unknown");return;}resetTarget(event,pass);section.hidden=false;const result=run(load);section.scrollIntoView?.({block:"start"});return result;}
  };
};




