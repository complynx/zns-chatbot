"use strict";
const assert = require("node:assert/strict");
const {webcrypto} = require("node:crypto");
const {readFileSync} = require("node:fs");
const {resolve} = require("node:path");
const {test} = require("node:test");
const {runInNewContext} = require("node:vm");
const source = readFileSync(resolve(__dirname,"../internal/api/miniapp_ui/payment-reviews.js"),"utf8");
const id = () => webcrypto.randomUUID();
const owner=id(), other=id(), eventID=id(), evidenceID=id();
const clone = v => JSON.parse(JSON.stringify(v));
const key = user => "zinusya-payment-review-pending-v1:"+user;
function backend() {
 const event={id:eventID,name:"Event <script>",timezone:"Europe/Minsk"};
 const api={event,receipts:new Map(),credits:0,hook:undefined};
 api.detail={evidence:{id:evidenceID,event_id:eventID,user_id:owner,pass_id:id(),amount_minor:4000,currency:"BYN",currency_scale:2,sent_at:"2026-10-16T10:00:00Z",due_at:"2026-10-16T11:00:00Z",timely:true,state:"pending"},payer:{user_id:owner,full_name:"Payer <img>"},balance:{version:0,remaining_minor:10000},workspace:null};
 api.base=(path,method,body)=>{
  if (path.endsWith("/events")) return {events:[event]};
  if (path.endsWith("/events/"+eventID)) return event;
  if (path.includes("/operations/")) { const r=api.receipts.get(path.split("/").at(-1));if(!r)throw {status:404};return r; }
  if (path.endsWith("/evidence")) return {evidence:[api.detail.evidence]};
  if (path.endsWith("/evidence/"+evidenceID)) return api.detail;
  if (method!=="POST") throw Error("Unexpected request "+path);
  if (api.receipts.has(body.operation_id)) {const r=api.receipts.get(body.operation_id);return r.kind==="commit"?r.review:r.workspace;}
  let kind,result;
  if(path.endsWith("/workspaces")) {
   kind="prepare";if(body.expected_revision!==(api.detail.workspace?.revision||0))throw {status:409};
   result={workspace_id:id(),event_id:eventID,evidence_id:evidenceID,revision:body.expected_revision+1,state:"prepared",current:true,preparation:{proposal_id:id(),arguments:clone(body.review),expires_at:"2035-01-01T00:00:00Z"}};api.detail.workspace=clone(result);
  } else if(path.endsWith("/cancellations")) { kind="cancel";api.detail.workspace.state="cancelled";result=clone(api.detail.workspace); }
  else if(path.endsWith("/confirmations")) {
   kind="commit";const w=api.detail.workspace;
   if(!body.confirmed||w.state!=="prepared"||body.canonical_proposal_id!==w.preparation.proposal_id||JSON.stringify(body.arguments.review)!==JSON.stringify(w.preparation.arguments))throw {status:409};
   api.credits++;w.state="completed";api.detail.evidence.state=body.arguments.review.decision;
   api.detail.evidence.review={...body.arguments.review};api.detail.balance.version++;
   result={canonical_operation_id:w.workspace_id,review:{evidence:clone(api.detail.evidence),review:clone(api.detail.evidence.review),balance:clone(api.detail.balance)}};
  } else throw Error("Unexpected write "+path);
  api.receipts.set(body.operation_id,{kind,completed:true,[kind==="commit"?"review":"workspace"]:clone(result)});return result;
 };
 api.request=async(path,method="GET",body)=>clone(await(api.hook?api.hook(path,method,body,api.base):api.base(path,method,body)));
 return api;
}
function browser(api,{storage=new Map(),failSet=false,app=false,launch=""}={}) {
 const elements=new Map(),calls=[];let controller,created=0,revoked=0;
 function element(tag="div") {
  let text="";const e={tag,children:[],listeners:{},hidden:false,disabled:false,value:"",append(...nodes){this.children.push(...nodes);},replaceChildren(...nodes){this.children=nodes;},addEventListener(k,f){this.listeners[k]=f;},removeAttribute(k){delete this[k];}};
  Object.defineProperties(e,{textContent:{get:()=>text,set:v=>{text=String(v);e.children=[];}},options:{get:()=>e.children},innerHTML:{set(){throw Error("Unsafe HTML");}}});return e;
 }
 const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
 get("pr-decision").append(element("option"),element("option"));
 get("profile-language").append(element("option"),element("option"),element("option"));
 const context={window:{location:{hash:launch},Telegram:{WebApp:{initData:"non-authorizing-fixture",ready(){}}}},document:{getElementById:get,createElement:element,documentElement:{}},navigator:{language:"en"},crypto:webcrypto,TextEncoder,Uint8Array,btoa,Intl,URLSearchParams,console,URL:{createObjectURL(){created++;return "blob:fixture";},revokeObjectURL(){revoked++;}},sessionStorage:{getItem:k=>storage.get(k)??null,setItem(k,v){if(failSet)throw Error("Storage unavailable");storage.set(k,v);},removeItem:k=>storage.delete(k)}};
 runInNewContext(source,context);
 const factory=context.window.createZinusyaPaymentReviews;
 if(app) {
 context.window.createZinusyaPaymentReviews=options=>{controller=factory(options);return controller;};
 context.fetch=async(path,options)=>{
  let result;const body=options.body?JSON.parse(options.body):undefined;
  try {
   if(path.startsWith("./payment-reviews/")){calls.push({path,method:options.method,body,authorization:options.headers.Authorization,credentials:options.credentials});if(path.endsWith("/content"))return {ok:true,blob:async()=>({type:"application/pdf"})};result=await api.request(path,options.method,body);}
   else if(path==="./session/bootstrap"||path==="./me")result={user_id:owner,session_token:"a".repeat(43),full_name:"Owner",bot_language:"en"};
   else if(path==="./profile")result={version:0,full_name:"Owner",bot_language:"en"};
   else if(path==="./account")result={events:[],has_more:false,observed_at:"2026-10-16T14:00:00Z"};
   else if(path==="./registration-details")return {ok:false,status:404};
   else if(path==="./session/logout")return {ok:true,status:204};
   else throw Error("Unexpected app request "+path);
   return {ok:true,status:200,json:async()=>result};
  } catch(error){return {ok:false,status:error.status||503};}
 };
 runInNewContext(readFileSync(resolve(__dirname,"../internal/api/miniapp_ui/app.js"),"utf8"),context);
 } else controller=factory({request:async(path,method,body)=>{calls.push({path,method,body:body?clone(body):undefined,stored:storage.get(key(owner))});return api.request(path,method,body);},setBusy:v=>controller?.setBusy(v),denied(){controller.clear();},readProof:async path=>{calls.push({path});return {type:"application/pdf"};}});
 const b={get controller(){return controller;},storage,calls,get,start:async(user=owner,locale="en")=>controller.start(user,locale),async click(name,type="click"){assert.ok(get("pr-"+name).listeners[type],name);await get("pr-"+name).listeners[type]({preventDefault(){}});},async select(){get("pr-event").value=eventID;await this.click("event","change");get("pr-evidence").value=evidenceID;await this.click("evidence","change");},async edit(value,reason=""){get("pr-amount").value=value;get("pr-reason").value=reason;await this.click("amount","input");},text(name){const walk=e=>[e.textContent,...e.children.map(walk)].join("\n");return walk(get("pr-"+name));},objects:()=>({created,revoked})};return b;
}
const posts=b=>b.calls.filter(c=>c.method==="POST");

test("prepare shows exact money and reason, explicit confirmation is the only credit",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.select();await b.edit("30,87","Reviewed <script>");await b.click("form","submit");
 assert.equal(api.credits,0);assert.equal(posts(b).length,1);assert.ok(posts(b)[0].stored);assert.match(b.text("preview"),/BYN 30\.87/);assert.match(b.text("preview"),/Reviewed <script>/);assert.match(b.text("detail"),/Payer <img>/);assert.match(b.text("detail"),/Europe\/Minsk/);
 assert.equal(b.get("pr-confirm").disabled,false);await b.click("confirm");assert.equal(api.credits,1);assert.match(b.text("detail"),/Proof status: Confirmed/);assert.equal(b.get("pr-confirm").disabled,true);assert.equal(b.storage.has(key(owner)),false);
});

test("an agent-prepared decision opens in the form; editing blocks its old confirmation",async()=>{
 const api=backend();api.base("./payment-reviews/workspaces","POST",{operation_id:id(),expected_revision:0,review:{event_id:eventID,evidence_id:evidenceID,expected_version:0,decision:"confirmed",amount_minor:1200,reason:"Agent draft"}});
 const b=browser(api);await b.start();await b.select();assert.equal(b.get("pr-amount").value,"12.00");assert.equal(b.get("pr-confirm").disabled,false);
 await b.edit("14.00");await b.click("confirm");assert.equal(posts(b).length,0);assert.equal(api.credits,0);
 await b.click("form","submit");assert.equal(api.detail.workspace.revision,2);assert.match(b.text("preview"),/BYN 14\.00/);
 await b.click("cancel");assert.equal(api.detail.workspace.state,"cancelled");assert.equal(api.credits,0);assert.equal(b.get("pr-confirm").disabled,true);
});

test("lost successful confirmation recovers after reload without another credit, including off-page events",async()=>{
 const api=backend(),storage=new Map(),b=browser(api,{storage});await b.start();await b.select();await b.edit("10.00");await b.click("form","submit");
 api.hook=(path,method,body,normal)=>{const result=normal(path,method,body);if(path.endsWith("/confirmations"))throw {status:503};return result;};await b.click("confirm");assert.equal(api.credits,1);const pending=JSON.parse(storage.get(key(owner)));
 api.hook=(path,method,body,normal)=>path.endsWith("/events")?{events:[]}:normal(path,method,body);
 const reload=browser(api,{storage});await reload.start();assert.equal(api.credits,1);assert.equal(posts(reload).length,0);assert.equal(storage.has(key(owner)),false);assert.match(reload.text("detail"),/Europe\/Minsk/);assert.match(reload.text("receipt"),/recorded/);assert.equal(reload.get("pr-event").value,eventID);assert.equal(pending.kind,"commit");
});

test("unknown status preserves the exact pending request until an explicit retry",async()=>{
 const api=backend(),storage=new Map(),b=browser(api,{storage});await b.start();await b.select();await b.edit("12.34");api.hook=(path,method,body,normal)=>{if(method==="POST")throw {status:503};return normal(path,method,body);};await b.click("form","submit");const original=posts(b)[0].body;
 api.hook=undefined;const reload=browser(api,{storage});await reload.start();assert.match(reload.text("status"),/unknown/);assert.equal(reload.get("pr-save").disabled,true);assert.equal(posts(reload).length,0);
 await reload.click("retry");assert.deepEqual(posts(reload)[0].body,original);assert.equal(api.credits,0);assert.equal(storage.has(key(owner)),false);
});

test("failed storage and malformed records block new writes, and another owner cannot reuse them",async()=>{
 const api=backend(),b=browser(api,{failSet:true});await b.start();await b.select();await b.edit("1.00");await b.click("form","submit");assert.equal(posts(b).length,0);assert.match(b.text("status"),/storage/);
 const storage=new Map([[key(owner),'{"owner":"invalid"}']]);const bad=browser(api,{storage});await bad.start();assert.equal(bad.get("pr-save").disabled,true);assert.match(bad.text("status"),/invalid/);
 const foreign=browser(api,{storage});await foreign.start(other);assert.equal(posts(foreign).length,0);assert.equal(foreign.get("pr-retry").hidden,true);assert.equal(storage.has(key(owner)),true);
});

test("expired proposals cannot confirm; rejection uses zero and explicit reason; proof URLs are discarded",async()=>{
 const api=backend(),b=browser(api);await b.start(owner,"ru");await b.select();await b.click("proof");assert.equal(b.get("pr-download").hidden,false);assert.equal(b.objects().created,1);
 b.get("pr-decision").value="rejected";await b.click("decision","change");await b.edit("999","Не тот платёж");await b.click("form","submit");assert.equal(posts(b)[0].body.review.amount_minor,0);assert.equal(b.objects().revoked,1);assert.match(b.text("preview"),/Не тот платёж/);
 api.detail.workspace.preparation.expires_at="2000-01-01T00:00:00Z";await b.click("reload");assert.equal(b.get("pr-confirm").disabled,true);await b.click("confirm");assert.equal(api.credits,0);assert.equal(posts(b).length,1);
});

test("logout clears values and ignores a late detail response",async()=>{
 const api=backend(),b=browser(api);await b.start();b.get("pr-event").value=eventID;await b.click("event","change");let finish;
 api.hook=(path,method,body,normal)=>path.endsWith("/evidence/"+evidenceID)?new Promise(r=>{finish=()=>r(normal(path,method,body));}):normal(path,method,body);
 b.get("pr-evidence").value=evidenceID;const request=b.click("evidence","change");await Promise.resolve();b.controller.clear();finish();await request;
 assert.equal(b.get("payment-review").hidden,true);assert.equal(b.text("detail"),"");assert.equal(b.text("preview"),"");assert.equal(b.get("pr-save").disabled,true);
});


test("real app wiring uses its session for review and proof; logout removes reviewer data",async()=>{
 const api=backend(),b=browser(api,{app:true});
 for(let n=0;n<100&&(!b.controller||b.get("logout").disabled||b.get("status").textContent!=="Signed in");n++)await new Promise(r=>setTimeout(r,5));
 assert.ok(b.controller);assert.equal(b.get("status").textContent,"Signed in");assert.equal(b.get("payment-review").hidden,false);
 await b.select();await b.edit("10.00");await b.click("form","submit");await b.click("proof");
 const calls=b.calls.filter(c=>c.path.startsWith("./payment-reviews/"));assert.ok(calls.length>3);
 for(const call of calls){assert.equal(call.authorization,"ZinusyaSession "+"a".repeat(43));assert.equal(call.credentials,"omit");}
 assert.equal(b.get("pr-download").hidden,false);await b.get("logout").listeners.click();
 assert.equal(b.get("payment-review").hidden,true);assert.equal(b.text("detail"),"");assert.equal(b.get("pr-download").hidden,true);
});


test("recovery restores the event's proof list and allows reviewing another proof",async()=>{
 const api=backend(),storage=new Map(),first=browser(api,{storage});await first.start();await first.select();await first.edit("12.00");
 api.hook=(path,method,body,normal)=>{const result=normal(path,method,body);if(method==="POST")throw {status:503};return result;};await first.click("form","submit");
 const second=id();
 api.hook=(path,method,body,normal)=>{
  if(path.endsWith("/evidence"))return {evidence:[api.detail.evidence,{...api.detail.evidence,id:second}]};
  if(path.endsWith("/evidence/"+second))return {...api.detail,evidence:{...api.detail.evidence,id:second},workspace:null};
  return normal(path,method,body);
 };
 const reload=browser(api,{storage});await reload.start();assert.equal(storage.has(key(owner)),false);
 assert.ok(reload.get("pr-evidence").options.some(option=>option.value===second));assert.equal(reload.get("pr-evidence").value,evidenceID);
 reload.get("pr-evidence").value=second;await reload.click("evidence","change");
 assert.equal(reload.get("pr-evidence").value,second);assert.equal(reload.text("preview").trim(),"No saved decision.");assert.equal(reload.get("pr-save").disabled,false);
});


test("a notification link resolves its event and evidence without treating the click as consent",async()=>{
 const api=backend();api.hook=(path,method,body,normal)=>path.endsWith("/events")?{events:[]}:normal(path,method,body);
 const b=browser(api,{launch:"#payment-review?event_id="+eventID+"&evidence_id="+evidenceID+"&tgWebAppVersion=9.0"});await b.start();
 assert.match(b.text("detail"),/Payer <img>/);assert.match(b.text("detail"),/Europe\/Minsk/);assert.equal(b.get("pr-event").value,eventID);assert.equal(b.get("pr-evidence").value,evidenceID);assert.equal(posts(b).length,0);assert.equal(api.credits,0);
});

test("an uncertain earlier request takes priority over a different launch target",async()=>{
 const api=backend(),storage=new Map(),first=browser(api,{storage});await first.start();await first.select();await first.edit("10.00");
 api.hook=(path,method,body,normal)=>{const result=normal(path,method,body);if(method==="POST")throw {status:503};return result;};await first.click("form","submit");api.hook=undefined;
 const foreign=id(),reload=browser(api,{storage,launch:"#payment-review?event_id="+foreign+"&evidence_id="+id()});await reload.start();
 assert.equal(reload.get("pr-event").value,eventID);assert.ok(reload.calls.every(call=>!call.path.includes(foreign)));assert.equal(posts(reload).length,0);assert.equal(storage.has(key(owner)),false);
});
