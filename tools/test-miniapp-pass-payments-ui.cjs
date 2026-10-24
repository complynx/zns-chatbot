"use strict";
const assert=require("node:assert/strict");
const {webcrypto,randomUUID}=require("node:crypto");
const {readFileSync}=require("node:fs");
const {resolve}=require("node:path");
const {runInNewContext}=require("node:vm");
const {test}=require("node:test");
const source=readFileSync(resolve(__dirname,"../internal/api/miniapp_ui/pass-payments.js"),"utf8");
const owner=randomUUID(),other=randomUUID(),eventID=randomUUID(),passID=randomUUID(),fileID=randomUUID();
const clone=v=>JSON.parse(JSON.stringify(v));
const key=id=>"zinusya-pass-payments-v1:"+id;
function backend(){
  const pass={id:passID,price_minor:10000,currency:"BYN",currency_scale:2,payment_due_at:"2026-10-25T01:30:00Z",tier:{id:"early",number:1},deposit:{minimum_minor:1000,minimum_bps:3000,required_minor:3000,final_due_at:"2026-10-27T01:30:00Z"}};
  const api={target:{event_id:eventID,event_name:"Event <script>",timezone:"Europe/Berlin",registration:{event_id:eventID,user_id:owner,state:"assigned",pass},balance:{pass_id:passID,price_minor:10000,currency:"BYN",currency_scale:2,confirmed_minor:0,remaining_minor:10000,deposit_required_minor:3000,deposit_satisfied:false,fully_paid:false},pending_evidence:0},files:[{id:fileID,filename:"proof <img>.pdf",kind:"document",sent_at:"2026-10-25T00:30:00Z"}],claims:[],receipts:new Map(),actions:[],hook:undefined};
  api.result=b=>({id:randomUUID(),user_id:owner,event_id:eventID,pass_id:b.pass_id,file_id:b.file_id,amount_minor:b.amount_minor,purpose:b.purpose,currency:"BYN",currency_scale:2,sent_at:api.files[0].sent_at,submitted_at:"2026-10-25T01:00:00Z",due_at:pass.payment_due_at,timely:true,state:"pending"});
  api.base=(path,method,body)=>{
    if(path==="./pass-payments/targets")return {targets:api.target.registration.state==="assigned"?[api.target]:[]};
    if(path==="./pass-payments/recent")return {actions:api.actions};
    if(path.includes("/passes/")){if(api.target.registration.state!=="assigned")throw {status:404};return api.target;}
    if(path.includes("/operations/")){const op=decodeURIComponent(path.split("/").at(-1)),result=api.receipts.get(op);if(!result)throw {status:404};return {operation_id:op,kind:"submit",timezone:api.target.timezone,result};}
    if(method==="POST"){if(api.receipts.has(body.operation_id))return api.receipts.get(body.operation_id);const result=api.result(body);api.receipts.set(body.operation_id,clone(result));api.claims.push(result);api.target.pending_evidence++;api.actions.push({operation_id:body.operation_id,event_id:eventID,pass_id:passID,event_name:api.target.event_name});return result;}
    if(path.includes("/files"))return {files:api.files};
    if(path.includes("/evidence?"))return {evidence:api.claims};
    throw Error("Unexpected request "+path);
  };
  api.request=async(p,m="GET",b)=>clone(await (api.hook?api.hook(p,m,b,api.base):api.base(p,m,b)));
  return api;
}
function browser(api,{storage=new Map(),failGet=false,failSet=false}={}){
  const elements=new Map(),calls=[],revoked=[];
  function element(tag="div"){const e={tag,children:[],hidden:false,disabled:false,value:"",listeners:{},append(...n){this.children.push(...n);},replaceChildren(...n){this.children=n;},addEventListener(n,f){this.listeners[n]=f;},removeAttribute(n){delete this[n];}};let text="";Object.defineProperties(e,{textContent:{get:()=>text,set:v=>{text=String(v);e.children=[];}},innerHTML:{set(){throw Error("Unsafe HTML");}}});return e;}
  const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const context={document:{getElementById:get,createElement:element},window:{},crypto:webcrypto,Intl,URL:{createObjectURL:()=>"blob:owned",revokeObjectURL:v=>revoked.push(v)},sessionStorage:{getItem:k=>{if(failGet)throw Error("Unreadable storage");return storage.get(k)??null;},setItem:(k,v)=>{if(failSet)throw Error("Unwritable storage");storage.set(k,v);},removeItem:k=>storage.delete(k)}};
  let ui;runInNewContext(source,context);
  ui=context.window.createZinusyaPassPayments({request:async(path,method,body)=>{calls.push({path,method,body:body?clone(body):undefined,saved:storage.get(key(owner))});return api.request(path,method,body);},readProof:async path=>{calls.push({path,proof:true});return {};},setBusy:v=>ui.setBusy(v),denied:()=>ui.clear()});
  const fire=async(id,name="click")=>{const n=get("pp-"+id);assert.equal(n.disabled,false,id+" disabled");await n.listeners[name]({preventDefault(){}});};
  const text=n=>(n.textContent||"")+n.children.map(text).join(" ");
  return {ui,calls,storage,revoked,get:id=>get("pp-"+id),fire,text:()=>[...elements.values()].map(text).join(" "),start:(id=owner,locale="en")=>ui.start(id,locale),open:()=>ui.show(eventID,passID),async select(){get("pp-file").value=fileID;await fire("file","change");await fire("preview");},async submit(amount="30.00",purpose="initial"){get("pp-amount").value=amount;get("pp-purpose").value=purpose;await fire("form","submit");}};
}
const writes=b=>b.calls.filter(c=>c.method==="POST");

test("explicit deposit shares exact persisted evidence without crediting money; review then top-up",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();assert.equal(b.get("purpose").value,"");await b.submit();
 const first=writes(b)[0];assert.ok(first.saved);assert.deepEqual(JSON.parse(first.saved).body,first.body);assert.equal(first.body.amount_minor,3000);assert.equal(first.body.purpose,"initial");assert.deepEqual(Object.keys(first.body).sort(),["amount_minor","file_id","operation_id","pass_id","purpose"]);assert.equal(api.target.balance.confirmed_minor,0);assert.match(b.text(),/Pending review; no money credited/);assert.match(b.text(),/Confirmed paid: 0.00 BYN/);assert.match(b.text(),/Deposit share: 30.00%/);assert.match(b.text(),/<script>/);
 api.claims[0]={...api.claims[0],state:"confirmed",review:{reason:"Deposit checked"}};api.target.balance={...api.target.balance,confirmed_minor:3000,remaining_minor:7000,deposit_satisfied:true};api.target.pending_evidence=0;await b.fire("reload");assert.match(b.text(),/Confirmed paid: 30.00 BYN/);await b.select();await b.submit("70.00","top_up");assert.equal(writes(b)[1].body.purpose,"top_up");assert.equal(writes(b)[1].body.amount_minor,7000);
});
test("lost committed reply reload recovers immutable pending receipt and current review by GET only",async()=>{
 const api=backend(),storage=new Map();api.hook=(p,m,b,base)=>{const r=base(p,m,b);if(m==="POST")throw {status:503};return r;};const b=browser(api,{storage});await b.start();await b.open();await b.select();await b.submit();assert.equal(storage.size,1);api.hook=undefined;api.claims[0]={...api.claims[0],state:"confirmed"};api.target.balance.confirmed_minor=3000;api.target.balance.remaining_minor=7000;
 const reload=browser(api,{storage});await reload.start();assert.equal(writes(reload).length,0);assert.equal(storage.size,0);assert.match(reload.get("receipt").children[0].children[0].textContent,/Original operation result: Pending review/);assert.match(reload.get("history").children[0].children[0].textContent,/Current evidence: Confirmed/);
});
test("unknown result freezes selection; explicit retry preserves exact operation and purpose after balance changes",async()=>{
 const api=backend(),storage=new Map();api.hook=(p,m,b,base)=>{if(m==="POST")throw {status:503};return base(p,m,b);};const b=browser(api,{storage});await b.start();await b.open();await b.select();await b.submit("30.00","initial");const original=writes(b)[0].body;
 api.target.balance.confirmed_minor=4000;api.target.balance.remaining_minor=6000;api.target.balance.deposit_satisfied=true;api.hook=undefined;
 const reload=browser(api,{storage});await reload.start();assert.equal(reload.get("targets").disabled,true);assert.equal(reload.get("purpose").disabled,true);assert.equal(writes(reload).length,0);await reload.fire("retry");assert.deepEqual(writes(reload)[0].body,original);assert.equal(api.claims.length,1);
});
test("fresh context recent locator reads original submission even for fully paid target, with no POST",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();await b.submit();api.target.balance={...api.target.balance,confirmed_minor:10000,remaining_minor:0,fully_paid:true};const fresh=browser(api);await fresh.start();assert.equal(fresh.get("targets").children.length,2);const button=fresh.get("recent-list").children[0];await button.listeners.click();assert.match(fresh.text(),/Original operation result/);assert.match(fresh.text(),/Remaining: 0.00 BYN/);assert.equal(writes(fresh).length,0);
});
test("unknown currency and invalid scale never produce POST",async()=>{
 for(const change of [{currency:""},{currency_scale:undefined},{currency_scale:5}]){const api=backend();Object.assign(api.target.registration.pass,change);Object.assign(api.target.balance,change);const b=browser(api);await b.start();await b.open();assert.equal(b.get("submit").disabled,true);assert.equal(writes(b).length,0);}
});
test("late evidence displays sent-time deadline and timezone while overdue leaves pass assigned",async()=>{
 const api=backend();api.target.registration.pass.payment_due_at="2020-10-25T01:30:00Z";const v=api.result({pass_id:passID,file_id:fileID,amount_minor:3000,purpose:"initial"});v.timely=false;api.claims=[v];const b=browser(api);await b.start();await b.open();assert.match(b.text(),/Late evidence/);assert.match(b.text(),/Europe\/Berlin/);assert.match(b.text(),/pass remains assigned/);b.ui.setLanguage("ru");assert.match(b.text(),/Подтверждение с опозданием/);assert.equal(writes(b).length,0);
});
test("malformed or foreign-owner journal blocks writes while server history and receipts remain readable",async()=>{
 for(const raw of ["{",JSON.stringify({owner:other,event_id:eventID,body:{}})]){const api=backend();const r=api.result({pass_id:passID,file_id:fileID,amount_minor:3000,purpose:"initial"});api.claims=[r];api.receipts.set("old",r);api.actions=[{event_id:eventID,pass_id:passID,operation_id:"old",event_name:"Event"}];const storage=new Map([[key(owner),raw]]),b=browser(api,{storage});await b.start();await b.get("recent-list").children[0].listeners.click();assert.match(b.text(),/Current evidence/);assert.match(b.text(),/Original operation result/);assert.equal(b.get("submit").disabled,true);assert.equal(storage.get(key(owner)),raw);assert.equal(writes(b).length,0);}
});
test("unreadable storage preserves read-only server history",async()=>{
 const api=backend(),b=browser(api,{failGet:true});await b.start();await b.open();assert.match(b.text(),/Session storage is unavailable/);assert.equal(b.get("submit").disabled,true);assert.ok(b.calls.some(c=>c.path.includes("/evidence?")));assert.equal(writes(b).length,0);
});
test("unwritable journal prevents first POST and leaves server reads available",async()=>{
 const api=backend(),b=browser(api,{failSet:true});await b.start();await b.open();await b.select();await b.submit();assert.equal(writes(b).length,0);assert.equal(b.get("submit").disabled,true);await b.fire("reload");assert.ok(b.calls.some(c=>c.path.includes("/evidence?")));
});
test("logout or changed owner fences late target responses and clears loaded proof URLs",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();b.ui.clear();assert.deepEqual(b.revoked,["blob:owned"]);assert.equal(b.get("download").hidden,true);
 let release;api.hook=(p,m,v,base)=>p.includes("/passes/")?new Promise(r=>{release=()=>r(base(p,m,v));}):base(p,m,v);await b.start();const pending=b.open();b.ui.clear();release();await pending;assert.doesNotMatch(b.text(),new RegExp(passID));api.hook=undefined;await b.start(other);assert.doesNotMatch(b.text(),new RegExp(passID));assert.equal(writes(b).length,0);
});
test("concurrent pending submissions are not added to confirmed money",async()=>{
 const api=backend();api.claims=[api.result({pass_id:passID,file_id:fileID,amount_minor:3000,purpose:"initial"}),api.result({pass_id:passID,file_id:fileID,amount_minor:7000,purpose:"top_up"})];api.target.pending_evidence=2;const b=browser(api);await b.start();await b.open();assert.match(b.text(),/awaiting review \(not credited\): 2/);assert.match(b.text(),/Confirmed paid: 0.00 BYN/);assert.match(b.text(),/Remaining: 100.00 BYN/);assert.equal(writes(b).length,0);
});
test("exact locale decimal parsing rejects exponent, grouping, zero, excess precision and unsafe integers",async()=>{
 for(const amount of ["1e3","1,000.00","0","1.001","90071992547409.92","-1"]){const b=browser(backend());await b.start();await b.open();await b.select();await b.submit(amount);assert.equal(writes(b).length,0,amount);}
 const b=browser(backend());await b.start(owner,"ru");await b.open();await b.select();await b.submit("30,01");assert.equal(writes(b)[0].body.amount_minor,3001);
});
test("unknown journal cannot be replaced by a new target or recent action",async()=>{
 const api=backend();api.hook=(p,m,b,base)=>{if(m==="POST")throw {status:503};return base(p,m,b);};const b=browser(api);await b.start();await b.open();await b.select();await b.submit();const saved=b.storage.get(key(owner));await b.ui.show(randomUUID(),randomUUID());assert.equal(b.storage.get(key(owner)),saved);assert.equal(writes(b).length,1);assert.equal(b.get("retry").hidden,false);
});
test("history pagination and accepted file loading are required before a new submission",async()=>{
 const api=backend(),cursor=randomUUID();api.hook=(p,m,b,base)=>p.includes("/evidence?")&&!p.includes("after=")?{evidence:[],next:cursor}:base(p,m,b);const b=browser(api);await b.start();await b.open();assert.equal(b.get("submit").disabled,true);await b.fire("history-next");assert.equal(b.get("submit").disabled,true);await b.select();assert.equal(b.get("submit").disabled,false);await b.submit();assert.equal(writes(b).length,1);
});
test("source file from agent-created history loads without selecting or submitting a new claim",async()=>{
 const api=backend();api.claims=[api.result({pass_id:passID,file_id:fileID,amount_minor:3000,purpose:"initial"})];const b=browser(api);await b.start();await b.open();const button=b.get("history").children[0].children.find(n=>n.tag==="button");await button.listeners.click();assert.equal(b.calls.filter(c=>c.proof)[0].path,"./pass-payments/events/"+eventID+"/files/"+fileID+"/content");assert.equal(b.get("download").hidden,false);assert.equal(b.get("submit").disabled,true);assert.equal(writes(b).length,0);
});


test("lost reply recovers original receipt when registration returns to waitlisted before any assigned-target read",async()=>{
 const api=backend(),storage=new Map();
 api.hook=(p,m,b,base)=>{const result=base(p,m,b);if(m==="POST")throw {status:503};return result;};
 const first=browser(api,{storage});await first.start();await first.open();await first.select();await first.submit();assert.equal(storage.size,1);
 api.claims[0]={...api.claims[0],state:"confirmed"};
 api.target.registration.state="waitlisted";api.hook=undefined;
 const reload=browser(api,{storage});await reload.start();
 assert.equal(storage.size,0);assert.equal(writes(reload).length,0);assert.equal(reload.get("submit").disabled,true);
 const receiptIndex=reload.calls.findIndex(c=>c.path.includes("/operations/")),targetIndex=reload.calls.findIndex(c=>c.path.includes("/passes/"));assert.ok(receiptIndex>=0&&receiptIndex<targetIndex);
 assert.match(reload.get("receipt").children[0].children[0].textContent,/Original operation result: Pending review/);
 assert.match(reload.get("history").children[0].children[0].textContent,/Current evidence: Confirmed/);
 assert.match(reload.text(),/Europe\/Berlin/);assert.match(reload.get("target").children[0].textContent,/assigned pass is unavailable/);
 reload.ui.setLanguage("ru");assert.match(reload.get("receipt").children[0].children[0].textContent,/Исходный результат операции/);
 await reload.fire("reload");assert.equal(writes(reload).length,0);assert.match(reload.text(),/Europe\/Berlin/);
});

test("fresh context recent submission remains readable when assigned target is unavailable with receipt timezone",async()=>{
 const api=backend(),first=browser(api);await first.start();await first.open();await first.select();await first.submit();
 api.target.registration.state="waitlisted";api.hook=undefined;
 const fresh=browser(api);await fresh.start();await fresh.get("recent-list").children[0].listeners.click();
 assert.match(fresh.text(),/Original operation result/);assert.match(fresh.text(),/Current evidence/);assert.match(fresh.text(),/Europe\/Berlin/);assert.equal(writes(fresh).length,0);assert.equal(fresh.get("submit").disabled,true);
 const receiptIndex=fresh.calls.findIndex(c=>c.path.includes("/operations/")),targetIndex=fresh.calls.findIndex(c=>c.path.includes("/passes/"));assert.ok(receiptIndex>=0&&receiptIndex<targetIndex);
});

test("receipt mismatch preserves exact journal and cannot be masked by target reads",async()=>{
 for(const change of [{user_id:other},{event_id:randomUUID()},{pass_id:randomUUID()},{file_id:randomUUID()},{amount_minor:999},{purpose:"top_up"}]){
  const api=backend(),storage=new Map();api.hook=(p,m,b,base)=>{const r=base(p,m,b);if(m==="POST")throw {status:503};return r;};
  const first=browser(api,{storage});await first.start();await first.open();await first.select();await first.submit();const saved=storage.get(key(owner));
  api.hook=(p,m,b,base)=>{const r=base(p,m,b);return p.includes("/operations/")?{...r,result:{...r.result,...change}}:r;};
  const reload=browser(api,{storage});await reload.start();assert.equal(storage.get(key(owner)),saved);assert.equal(writes(reload).length,0);assert.equal(reload.calls.some(c=>c.path.includes("/passes/")),false);
 }
});

test("authorization failure during optional current-target read clears historical content",async()=>{
 const api=backend(),first=browser(api);await first.start();await first.open();await first.select();await first.submit();
 api.hook=(p,m,b,base)=>{if(p.includes("/passes/"))throw {status:403};return base(p,m,b);};
 const fresh=browser(api);await fresh.start();await fresh.get("recent-list").children[0].listeners.click();assert.doesNotMatch(fresh.text(),/Original operation result/);assert.equal(fresh.get("receipt").children.length,0);assert.equal(writes(fresh).length,0);
});


test("deselecting the assigned pass cannot submit evidence against the old target",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();b.get("targets").value=passID;await b.fire("targets","change");await b.select();b.get("targets").value="";await b.fire("targets","change");await b.submit();assert.equal(writes(b).length,0);assert.equal(b.get("submit").disabled,true);
});

test("pass pagination keeps the active pass visibly selected while paging other targets",async()=>{
 const api=backend(),cursor=randomUUID(),otherTarget=clone(api.target);otherTarget.registration.pass.id=randomUUID();otherTarget.balance.pass_id=otherTarget.registration.pass.id;
 api.hook=(p,m,b,base)=>p==="./pass-payments/targets"?{targets:[api.target],next:cursor}:p.startsWith("./pass-payments/targets?after=")?{targets:[otherTarget]}:base(p,m,b);
 const b=browser(api);await b.start();await b.open();await b.select();await b.fire("targets-next");assert.equal(b.get("targets").value,passID);assert.equal(b.get("targets").children.length,3);await b.submit();assert.equal(writes(b)[0].body.pass_id,passID);
});
