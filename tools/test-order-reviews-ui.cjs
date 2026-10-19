"use strict";
const assert = require("node:assert/strict");
const {webcrypto,randomUUID} = require("node:crypto");
const {readFileSync} = require("node:fs");
const {resolve} = require("node:path");
const {runInNewContext} = require("node:vm");
const {test} = require("node:test");
const source=readFileSync(resolve(__dirname,"../internal/api/miniapp_ui/order-reviews.js"),"utf8");
const clone=v=>JSON.parse(JSON.stringify(v));
const owner=randomUUID(),payer=randomUUID(),eventID=randomUUID(),evidenceID=randomUUID(),orderID=randomUUID(),fileID=randomUUID();
const key="zinusya-order-review-v1:"+owner;
function backend(state="pending") {
 const api={event:{id:eventID,name:"Sold event <script>",timezone:"Europe/Berlin"},receipts:new Map(),effects:0};
 api.claim={id:evidenceID,event_id:eventID,order_id:orderID,user_id:payer,file_id:fileID,version:1,state,amount_minor:2568,quoted_amount_minor:2568,currency:"BYN",currency_scale:2,sent_at:"2026-10-25T00:30:00Z",due_at:"2026-10-25T01:30:00Z"};
 api.detail={evidence:api.claim,payer:{user_id:payer,full_name:"Payer <img>"},workspace:null,order:{id:orderID,event_id:eventID,state:"submitted",version:1,snapshot:{event_name:api.event.name,quote:{total_minor:2568,currency:"BYN",currency_scale:2,timezone:"Europe/Berlin"}}}};
 api.prepare=(review,revision=1)=>api.detail.workspace={id:randomUUID(),event_id:eventID,evidence_id:evidenceID,state:"prepared",current:true,revision,preparation:{proposal_id:randomUUID(),arguments:clone(review),evidence:clone(api.claim),expires_at:"2099-10-25T00:00:00Z",description:"Exact decision <script>\nSoup × 2: 24.68 BYN\nService: 1.00 BYN\nTotal: 25.68 BYN\n"+review.reason}};
 api.args=(decision="confirmed",reason="Checked <img>")=>({event_id:eventID,evidence_id:evidenceID,expected_version:1,decision,reason});
 api.complete=(args=api.detail.workspace.preparation.arguments)=>{
   if(api.claim.version===1){api.effects++;api.claim.state=args.decision;api.claim.version=2;api.claim.review={actor_id:owner,decision:args.decision,reason:args.reason,amount_minor:args.decision==="confirmed"?2568:0};api.detail.workspace.state="completed";}
   return {canonical_operation_id:api.detail.workspace.id,evidence:clone(api.claim)};
 };
 api.base=(path,method,body)=>{
  if(path==="./order-reviews/events")return {events:[api.event]};
  if(path==="./order-reviews/events/"+eventID)return api.event;
  if(path==="./order-reviews/events/"+eventID+"/evidence")return {evidence:api.claim.version===1?[api.claim]:[]};
  if(path==="./order-reviews/events/"+eventID+"/evidence/"+evidenceID)return api.detail;
  if(path.includes("/operations/")){const v=api.receipts.get(path.split("/").at(-1));if(!v)throw {status:404};return v;}
  if(method==="POST") {
   const old=api.receipts.get(body.operation_id);if(old)return old.review||old.workspace;
   let kind,result;
   if(path.endsWith("/workspaces")){kind="save";assert.equal(body.expected_revision,api.detail.workspace?.revision||0);result=api.prepare(body.review,body.expected_revision+1);}
   else if(path.endsWith("/cancellations")){kind="cancel";assert.equal(body.workspace_id,api.detail.workspace.id);api.detail.workspace.state="cancelled";result=api.detail.workspace;}
   else if(path.endsWith("/confirmations")){kind="commit";assert.equal(body.confirmed,true);assert.equal(body.canonical_proposal_id,api.detail.workspace.preparation.proposal_id);assert.deepEqual(body.arguments.review,api.detail.workspace.preparation.arguments);result=api.complete(body.arguments.review);}
   else throw Error("Unexpected write "+path);
   api.receipts.set(body.operation_id,clone({operation_id:body.operation_id,kind,source:"miniapp",completed:true,[kind==="commit"?"review":"workspace"]:result}));return result;
  }
  throw Error("Unexpected request "+path);
 };
 api.request=async(path,method,body)=>clone(await(api.hook?api.hook(path,method,body,api.base):api.base(path,method,body)));
 return api;
}
function browser(api,{storage=new Map(),failGet=false,failSet=false,hash="",app=false}={}) {
 const elements=new Map(),calls=[],revoked=[];
 function element(tag="div") {
  const e={tag,children:[],hidden:false,disabled:false,value:"",listeners:{},append(...n){this.children.push(...n);},replaceChildren(...n){this.children=n;},addEventListener(k,v){this.listeners[k]=v;},removeAttribute(k){delete this[k];}};
  let text="";Object.defineProperties(e,{textContent:{get:()=>text,set:v=>{text=String(v);e.children=[];}},options:{get:()=>e.children},innerHTML:{set(){throw Error("Unsafe HTML");}}});return e;
 }
 const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
 get("or-decision").append(element("option"),element("option"));
 const context={document:{getElementById:get,createElement:element,documentElement:{}},window:{location:{hash},Telegram:{WebApp:{initData:"non-authorizing-fixture",ready(){}}}},navigator:{language:"en"},crypto:webcrypto,TextEncoder,Uint8Array,btoa,Intl,URLSearchParams,URL:{createObjectURL:()=>"blob:proof",revokeObjectURL:v=>revoked.push(v)},sessionStorage:{getItem:k=>{if(failGet)throw Error("Storage unavailable");return storage.get(k)??null;},setItem:(k,v)=>{if(failSet)throw Error("Storage unavailable");storage.set(k,v);},removeItem:k=>storage.delete(k)}};
 let ui;
 context.fetch=async(path,options)=>{
  const body=options.body?JSON.parse(options.body):undefined;calls.push({path,method:options.method,body,authorization:options.headers.Authorization,credentials:options.credentials,cache:options.cache,persisted:storage.get(key)});
  try {
   if(path.endsWith("/content"))return {ok:true,blob:async()=>({type:"application/pdf"})};
   let result;
   if(path.startsWith("./order-reviews/"))result=await api.request(path,options.method,body);
   else if(path==="./session/bootstrap"||path==="./me")result={user_id:owner,session_token:"a".repeat(43),full_name:"Owner",bot_language:"en"};
   else if(path==="./profile")result={version:0,full_name:"Owner",bot_language:"en"};
   else if(path==="./account")result={events:[],has_more:false,observed_at:"2026-10-17T00:00:00Z"};
   else if(path==="./registration-details")return {ok:false,status:404};
   else if(path==="./session/logout")return {ok:true,status:204};
   else throw Error("Unexpected app path "+path);
   return {ok:true,status:200,json:async()=>clone(result)};
  }catch(error){return {ok:false,status:error.status||503};}
 };
 runInNewContext(source,context);
 if(app){get("profile-language").append(element("option"),element("option"),element("option"));runInNewContext(readFileSync(resolve(__dirname,"../internal/api/miniapp_ui/app.js"),"utf8"),context);}
 else ui=context.window.createZinusyaOrderReviews({request:async(path,method,body)=>{calls.push({path,method,body:body?clone(body):undefined,persisted:storage.get(key)});return api.request(path,method,body?clone(body):body);},readProof:async path=>{calls.push({path,proof:true});if(api.proof)await api.proof();return {type:"application/pdf"};},setBusy:v=>ui.setBusy(v),denied:()=>ui.clear()});
 const text=e=>(e.textContent||"")+e.children.map(text).join(" ");
 const fire=async(id,event="click",force=false)=>{const el=get("or-"+id);if(!force)assert.equal(el.disabled,false,id+" disabled");await el.listeners[event]({preventDefault(){}});};
 return {ui,calls,storage,revoked,async settled(){for(let i=0;i<100;i++){if(!get("retry").disabled && get("or-event").children.length>1)return;await new Promise(r=>setTimeout(r,5));}assert.fail("App did not connect reviewer UI");},async logout(){await get("logout").listeners.click();},get:id=>get("or-"+id),text:id=>text(get("or-"+id)),fire,start:()=>ui.start(owner,"en"),async select(){get("or-event").value=eventID;await fire("event","change");get("or-evidence").value=evidenceID;await fire("evidence","change");},async save(reason="Checked <img>",decision="confirmed"){get("or-reason").value=reason;await fire("reason","input");get("or-decision").value=decision;await fire("decision","change");await fire("form","submit");}};
}
const writes=b=>b.calls.filter(c=>c.method==="POST");

test("prepare saves intent only; proof inspection and explicit confirmation record one decision",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();await b.save();assert.equal(api.effects,0);assert.equal(b.get("confirm").disabled,true);assert.match(b.text("preview"),/Soup × 2: 24.68 BYN/);assert.match(b.text("preview"),/<script>/);
 await b.fire("proof");assert.equal(b.get("download").download,"order-proof.pdf");await b.fire("confirm");assert.equal(api.effects,1);assert.equal(api.claim.state,"confirmed");assert.equal(b.get("confirm").disabled,true);assert.equal(b.storage.size,0);
 for(const c of writes(b)){assert.deepEqual(JSON.parse(c.persisted).body,c.body);assert.equal(c.body.review?.amount_minor,undefined);}
 assert.match(b.text("detail"),/Payment confirmed/);assert.match(b.text("receipt"),/Confirmed amount: 25.68 BYN/);
});
test("agent prepared decision opens natively; edits invalidate old consent and cancel keeps claim",async()=>{
 const api=backend();api.prepare(api.args());const b=browser(api);await b.start();await b.select();assert.equal(b.get("reason").value,"Checked <img>");await b.fire("proof");assert.equal(b.get("confirm").disabled,false);
 b.get("reason").value="Clarified";await b.fire("reason","input");assert.equal(b.get("confirm").disabled,true);await b.fire("confirm","click",true);assert.equal(api.effects,0);
 await b.fire("form","submit");assert.equal(api.detail.workspace.revision,2);await b.fire("cancel");assert.equal(api.detail.workspace.state,"cancelled");assert.equal(api.claim.state,"pending");assert.equal(api.effects,0);
});
test("lost successful commit recovers original receipt after reload without another POST",async()=>{
 const api=backend(),storage=new Map();api.prepare(api.args());const b=browser(api,{storage});await b.start();await b.select();await b.fire("proof");
 api.hook=(p,m,v,base)=>{const result=base(p,m,v);if(m==="POST")throw {status:503};return result;};await b.fire("confirm");assert.equal(storage.size,1);assert.equal(api.effects,1);
 api.hook=(p,m,v,base)=>p==="./order-reviews/events"?{events:[]}:base(p,m,v);const reload=browser(api,{storage});await reload.start();assert.equal(storage.size,0);assert.equal(writes(reload).length,0);assert.match(reload.text("receipt"),/Payment confirmed/);assert.equal(reload.get("event").value,eventID);
});
test("unknown outcome keeps exact request and permits only explicit same-key retry",async()=>{
 const api=backend(),storage=new Map(),b=browser(api,{storage});await b.start();await b.select();api.hook=(p,m,v,base)=>{if(m==="POST")throw {status:503};return base(p,m,v);};await b.save();const first=writes(b)[0].body;
 const reload=browser(api,{storage});await reload.start();assert.match(reload.text("status"),/unknown/);assert.equal(reload.get("event").disabled,true);assert.equal(reload.get("save").disabled,true);assert.equal(writes(reload).length,0);
 api.hook=undefined;await reload.fire("retry");assert.deepEqual(writes(reload)[0].body,first);assert.equal(storage.size,0);assert.equal(api.effects,0);
});
test("an agent can finish native prepared intent; current reload cannot confirm it twice",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();await b.save();await b.fire("proof");api.complete();await b.fire("reload");assert.equal(b.get("confirm").disabled,true);assert.match(b.text("detail"),/Payment confirmed/);assert.match(b.text("receipt"),/Saved decision prepared/);assert.equal(api.effects,1);
});
test("exhausted and exceptional claims can be rejected but cannot confirm money",async()=>{
 for(const state of ["exhausted","needs_decision"]){const api=backend(state),b=browser(api);await b.start();await b.select();assert.equal(b.get("decision").options[0].disabled,true);await b.save("No matching capacity","confirmed");assert.equal(writes(b).length,0);await b.save("No matching capacity","rejected");await b.fire("proof");await b.fire("confirm");assert.equal(api.claim.review.amount_minor,0);assert.equal(api.claim.state,"rejected");}
});
test("failed storage writes prevent POST",async()=>{
 const api=backend(),b=browser(api,{failSet:true});await b.start();await b.select();await b.save();assert.equal(writes(b).length,0);assert.match(b.text("status"),/storage is unavailable/);
});
test("unreadable recovery storage blocks all new writes",async()=>{
 const api=backend(),b=browser(api,{failGet:true});await b.start();assert.equal(b.get("event").disabled,true);assert.equal(b.get("save").disabled,true);assert.equal(writes(b).length,0);
});
test("malformed or foreign-owner recovery intent remains blocked and is not deleted",async()=>{
 for(const raw of ["{",JSON.stringify({owner:randomUUID(),kind:"commit"})]){const storage=new Map([[key,raw]]),b=browser(backend(),{storage});await b.start();assert.equal(b.get("save").disabled,true);assert.equal(b.storage.get(key),raw);assert.equal(b.calls.length,0);}
});
test("logout clears sensitive presentation and ignores a late proof response",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();let release;api.proof=()=>new Promise(r=>{release=r;});const loading=b.fire("proof");b.ui.clear();release();await loading;assert.equal(b.get("download").hidden,true);assert.equal(b.text("detail"),"");assert.equal(b.text("preview"),"");assert.equal(b.get("reason").value,"");assert.equal(writes(b).length,0);
});
test("locale updates current labels and original receipt while frozen decision stays literal",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();await b.save();const description=b.text("preview");assert.match(b.text("detail"),/GMT\+2/);assert.match(b.text("detail"),/GMT\+1/);b.ui.setLanguage("ru");assert.match(b.text("receipt"),/Исходный результат запроса/);assert.match(b.text("status"),/Решение подготовлено/);assert.match(b.text("detail"),/Плательщик: Payer <img>/);assert.ok(b.text("preview").includes(api.detail.workspace.preparation.description));assert.ok(description.includes(api.detail.workspace.preparation.description));assert.equal(writes(b).length,1);
});
test("deep link reads an off-page reviewed proof without depending on unresolved queue",async()=>{
 const api=backend();api.prepare(api.args());api.complete();const b=browser(api,{hash:"#order-review?event="+eventID+"&evidence="+evidenceID});await b.start();assert.match(b.text("detail"),/Payment confirmed/);assert.equal(b.calls.some(c=>c.path==="./order-reviews/events"),false);await b.fire("proof");assert.equal(b.get("download").hidden,false);assert.equal(b.get("confirm").disabled,true);
});
test("event and evidence pagination use explicit cursors and empty pages keep writes disabled",async()=>{
 const api=backend(),cursor=randomUUID();api.hook=(p,m,v,base)=>p==="./order-reviews/events"?{events:[api.event],next:cursor}:p==="./order-reviews/events?after="+cursor?{events:[]}:base(p,m,v);const b=browser(api);await b.start();assert.equal(b.get("event-next").hidden,false);await b.fire("event-next");assert.equal(b.get("event-first").hidden,false);assert.equal(b.get("save").disabled,true);await b.fire("event-first");await b.select();assert.equal(b.get("save").disabled,false);
});

test("real app wires reviewer API and file reads to current session and clears on logout",async()=>{
 const api=backend(),b=browser(api,{app:true});await b.settled();await b.select();await b.save();await b.fire("proof");await b.fire("confirm");assert.equal(api.effects,1);
 for(const c of b.calls.filter(c=>c.path.startsWith("./order-reviews/"))){assert.equal(c.authorization,"ZinusyaSession "+"a".repeat(43));assert.equal(c.credentials,"omit");assert.equal(c.cache,"no-store");}
 await b.logout();assert.equal(b.text("detail"),"");assert.equal(b.text("preview"),"");assert.equal(b.get("download").hidden,true);assert.equal(b.get("reason").value,"");
});

test("switching away from a reviewed target clears its original receipt",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();await b.save();await b.fire("proof");await b.fire("confirm");assert.match(b.text("receipt"),/Payment confirmed/);
 b.get("event").value="";await b.fire("event","change");assert.equal(b.text("receipt"),"");assert.equal(b.text("detail"),"");assert.equal(b.text("status"),"Select a proof and review it.");
});

test("agent completion with no native receipt recovers by explicit same-key retry without another decision",async()=>{
 const api=backend(),storage=new Map();api.prepare(api.args());const b=browser(api,{storage});await b.start();await b.select();await b.fire("proof");
 api.hook=(p,m,v,base)=>{if(m==="POST")throw {status:503};return base(p,m,v);};await b.fire("confirm");const original=writes(b)[0].body;api.complete();api.hook=undefined;
 const reload=browser(api,{storage});await reload.start();assert.match(reload.text("status"),/unknown/);assert.equal(reload.get("confirm").disabled,true);assert.equal(storage.size,1);assert.equal(writes(reload).length,0);
 await reload.fire("retry");assert.deepEqual(writes(reload)[0].body,original);assert.equal(api.effects,1);assert.equal(storage.size,0);assert.match(reload.text("receipt"),/Payment confirmed/);
});
test("a late successful decision cannot repaint logout or discard its recovery record",async()=>{
 const api=backend(),b=browser(api);api.prepare(api.args());await b.start();await b.select();await b.fire("proof");let release;
 api.hook=(p,m,v,base)=>{const result=base(p,m,v);return m==="POST"?new Promise(r=>{release=()=>r(result);}):result;};
 const request=b.fire("confirm");b.ui.clear();release();await request;assert.equal(api.effects,1);assert.equal(b.text("receipt"),"");assert.equal(b.text("detail"),"");assert.equal(b.storage.size,1);assert.equal(b.get("download").hidden,true);
});
