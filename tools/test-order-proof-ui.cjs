"use strict";
const assert = require("node:assert/strict");
const {webcrypto, randomUUID} = require("node:crypto");
const {readFileSync} = require("node:fs");
const {resolve} = require("node:path");
const {runInNewContext} = require("node:vm");
const {test} = require("node:test");
const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/order-proof.js"), "utf8");
const owner = randomUUID(), other = randomUUID(), eventID = randomUUID(), orderID = randomUUID(), fileID = randomUUID();
const clone = value => JSON.parse(JSON.stringify(value));
const key = user => "zinusya-order-proof-v1:" + user;
function backend() {
  const api = {order:{id:orderID,event_id:eventID,version:1,state:"submitted",snapshot:{event_name:"Event <script>",sales_closes_at:"2026-10-25T01:30:00Z",quote:{total_minor:2568,currency_scale:2,currency:"BYN",timezone:"Europe/Berlin"}}},claims:[],receipts:new Map(),hook:undefined};
  api.file = {id:fileID,filename:"proof <img>.pdf",kind:"document",sent_at:"2026-10-25T00:30:00Z"};
  api.result = body => ({id:randomUUID(),event_id:eventID,order_id:body.order_id,user_id:owner,file_id:body.file_id,amount_minor:body.amount_minor,quoted_amount_minor:2568,currency_scale:2,currency:"BYN",version:1,state:"pending",sent_at:api.file.sent_at,due_at:api.order.snapshot.sales_closes_at});
  api.base = (path, method, body) => {
    if (path.includes("/submitted/")) return api.order;
    if (path.includes("/proof-files")) return {files:[api.file]};
    if (path.includes("/payment-evidence-operations/")) {const result = api.receipts.get(path.split("/").at(-1)); if (!result) throw {status:404}; return {operation_id:path.split("/").at(-1),kind:"submit",result};}
    if (method === "POST") {const old = api.receipts.get(body.operation_id); if (old) return old; const result = api.result(body); api.receipts.set(body.operation_id,result); api.claims.push(result); return result;}
    if (path.includes("/payment-evidence?")) return {evidence:api.claims};
    throw new Error("Unexpected request " + path);
  };
  api.request = async (path,method="GET",body) => clone(await (api.hook ? api.hook(path,method,body,api.base) : api.base(path,method,body)));
  return api;
}
function browser(api, {storage=new Map(),failSet=false}={}) {
  const elements = new Map(), calls=[], revoked=[];
  function element(tag="div") {
    const e={tag,children:[],hidden:false,disabled:false,value:"",listeners:{},append(...n){this.children.push(...n);},replaceChildren(...n){this.children=n;},addEventListener(name,fn){this.listeners[name]=fn;},removeAttribute(name){delete this[name];}};
    let text="";Object.defineProperties(e,{textContent:{get:()=>text,set:v=>{text=String(v);e.children=[];}},innerHTML:{set(){throw new Error("Unsafe HTML");}}});return e;
  }
  const get=id=>{if(!elements.has(id))elements.set(id,element());return elements.get(id);};
  const context={document:{getElementById:get,createElement:element},window:{},crypto:webcrypto,Intl,URL:{createObjectURL:()=>"blob:owned",revokeObjectURL:url=>revoked.push(url)},sessionStorage:{getItem:k=>storage.get(k)??null,setItem:(k,v)=>{if(failSet)throw new Error("Storage unavailable");storage.set(k,v);},removeItem:k=>storage.delete(k)}};
  let ui;
  runInNewContext(source,context);
  const env={request:async(path,method,body)=>{calls.push({path,method,body:body?clone(body):undefined,saved:storage.get(key(owner))});return api.request(path,method,body);},readProof:async path=>{calls.push({path,proof:true});return {};},setBusy:value=>ui.setBusy(value),denied:()=>ui.clear()};
  ui=context.window.createZinusyaOrderProof(env);
  const fire=async(id,name="click")=>{const e=get("op-"+id);assert.equal(e.disabled,false,id+" disabled");await e.listeners[name]({preventDefault(){}});};
  const text=e=>(e.textContent||"")+e.children.map(text).join(" ");
  return {ui,get:id=>get("op-"+id),calls,storage,revoked,fire,text:()=>[...elements.values()].map(text).join(" "),start:()=>ui.start(owner,"en"),open:()=>ui.open(api.order,"en"),async select(){get("op-file").value=fileID;await fire("file","change");await fire("preview");},submit:()=>fire("form","submit")};
}

test("explicit order and accepted file produce an exact persisted claim, not paid status",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();b.get("amount").value="25,68";await b.submit();
 const write=b.calls.find(c=>c.method==="POST");assert.ok(write.saved);assert.deepEqual(JSON.parse(write.saved).body,write.body);assert.equal(write.body.order_id,orderID);assert.equal(write.body.file_id,fileID);assert.equal(write.body.amount_minor,2568);assert.equal(api.claims.length,1);assert.equal(b.storage.size,0);assert.match(b.text(),/available places held/);assert.doesNotMatch(b.text(),/Payment confirmed/);assert.match(b.text(),/<script>/);
});
test("commit with lost reply recovers original receipt after reload without another POST",async()=>{
 const api=backend(),storage=new Map();api.hook=(path,method,body,base)=>{const result=base(path,method,body);if(method==="POST")throw {status:503};return result;};
 const b=browser(api,{storage});await b.start();await b.open();await b.select();await b.submit();assert.equal(storage.size,1);assert.equal(api.claims.length,1);
 api.claims[0]={...api.claims[0],state:"confirmed",version:2};api.hook=undefined;
 const reload=browser(api,{storage});await reload.start();assert.equal(storage.size,0);assert.equal(reload.calls.filter(c=>c.method==="POST").length,0);assert.match(reload.text(),/Payment confirmed/);assert.match(reload.text(),/Awaiting review/);
});
test("unknown receipt blocks a new selection and retries exact operation/body",async()=>{
 const api=backend(),storage=new Map();api.hook=(path,method,body,base)=>{if(method==="POST")throw {status:503};return base(path,method,body);};
 const b=browser(api,{storage});await b.start();await b.open();await b.select();await b.submit();const first=b.calls.find(c=>c.method==="POST");
 const reload=browser(api,{storage});await reload.start();assert.equal(reload.get("file").disabled,true);assert.equal(reload.get("retry").hidden,false);api.hook=undefined;await reload.fire("retry");assert.deepEqual(reload.calls.find(c=>c.method==="POST").body,first.body);assert.equal(api.claims.length,1);
});
test("storage failure prevents POST",async()=>{
 const api=backend(),b=browser(api,{failSet:true});await b.start();await b.open();await b.select();await b.submit();assert.equal(b.calls.filter(c=>c.method==="POST").length,0);assert.equal(api.claims.length,0);assert.match(b.text(),/storage is unavailable/);
});
test("a file must be loaded and decimal amounts are exact through six digits",async()=>{
 const api=backend();api.order.snapshot.quote.currency_scale=6;api.order.snapshot.quote.total_minor=1234567;
 const b=browser(api);await b.start();await b.open();assert.equal(b.get("submit").disabled,true);await b.select();b.get("amount").value="1.234567";await b.submit();assert.equal(b.calls.find(c=>c.method==="POST").body.amount_minor,1234567);
});
test("excess precision, exponent, negative and zero amounts produce no POST",async()=>{
 for(const amount of ["1.234","1e3","-1","0","Infinity"]){const api=backend(),b=browser(api);await b.start();await b.open();await b.select();b.get("amount").value=amount;await b.submit();assert.equal(b.calls.filter(c=>c.method==="POST").length,0,amount);}
});
test("agent-created claim is visible and prevents another claim",async()=>{
 const api=backend();api.claims=[api.result({order_id:orderID,file_id:fileID,amount_minor:2568})];const b=browser(api);await b.start();await b.open();assert.equal(b.get("file").disabled,true);assert.equal(b.get("submit").disabled,true);assert.match(b.text(),/existing proof/);
});
test("unseen history pages block new claims; active proof on later page remains blocking",async()=>{
 const api=backend();let pages=0;api.hook=(path,method,body,base)=>{if(path.includes("/payment-evidence?")){pages++;return pages===1?{evidence:[],next:randomUUID()}:{evidence:[api.result({order_id:orderID,file_id:fileID,amount_minor:2568})]};}return base(path,method,body);};
 const b=browser(api);await b.start();await b.open();assert.equal(b.get("file").disabled,true);await b.fire("history-next");assert.equal(b.get("file").disabled,true);
});
test("clear/logout and owner switch revoke proof URLs and hide prior content",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();assert.equal(b.get("download").hidden,false);b.ui.clear();assert.deepEqual(b.revoked,["blob:owned"]);assert.equal(b.get("download").hidden,true);await b.ui.start(other,"ru");assert.doesNotMatch(b.text(),new RegExp(orderID));
});
test("late order response cannot render after logout",async()=>{
 const api=backend();let release;api.hook=(path,method,body,base)=>path.includes("/submitted/")?new Promise(resolve=>{release=()=>resolve(base(path,method,body));}):base(path,method,body);
 const b=browser(api);await b.start();const opening=b.open();b.ui.clear();release();await opening;assert.doesNotMatch(b.text(),new RegExp(orderID));
});
test("foreign-owner or malformed recovery record blocks rather than starting a request",async()=>{
 const storage=new Map([[key(owner),JSON.stringify({owner:other,event_id:eventID,body:{}})]]),b=browser(backend(),{storage});await b.start();assert.match(b.text(),/stored request is invalid/);assert.equal(b.calls.length,0);
});
test("changing the file revokes old preview and requires another load",async()=>{
 const b=browser(backend());await b.start();await b.open();await b.select();await b.fire("file","change");assert.equal(b.get("download").hidden,true);assert.equal(b.get("submit").disabled,true);assert.deepEqual(b.revoked,["blob:owned"]);
});
test("Russian labels and event timezone survive locale change",async()=>{
 const b=browser(backend());await b.start();await b.open();b.ui.setLanguage("ru");assert.match(b.text(),/Подтверждение оплаты заказа/);assert.match(b.text(),/Europe\/Berlin/);assert.match(b.get("amount-label").textContent,/BYN/);
});

test("malformed proof terms do not discard a recoverable operation",async()=>{
 for(const change of [{currency:"EUR"},{sent_at:"invalid"},{quoted_amount_minor:1}]) {
  const api=backend(),storage=new Map();api.hook=(path,method,body,base)=>{const result=base(path,method,body);return method==="POST"?{...result,...change}:result;};
  const b=browser(api,{storage});await b.start();await b.open();await b.select();await b.submit();assert.equal(storage.size,1);api.hook=undefined;const reload=browser(api,{storage});await reload.start();assert.equal(storage.size,0);assert.equal(api.claims.length,1);
 }
});

test("agent-created active claim can load its exact file independently of disabled new-proof fields",async()=>{
 const api=backend();api.claims=[api.result({order_id:orderID,file_id:fileID,amount_minor:2568})];const b=browser(api);await b.start();await b.open();assert.equal(b.get("file").disabled,true);
 const button=b.get("history").children[0].children.find(n=>n.tag==="button");assert.ok(button);await button.listeners.click();
 assert.equal(b.get("download").hidden,false);assert.equal(b.calls.filter(c=>c.proof).at(-1).path,"./orders/"+eventID+"/proof-files/"+fileID+"/content");assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
 b.ui.clear();assert.equal(b.get("download").download,"");
});

test("changing language translates original receipt, current proof and loaded-file status without resubmitting",async()=>{
 const api=backend(),b=browser(api);await b.start();await b.open();await b.select();await b.submit();
 const button=b.get("history").children[0].children.find(n=>n.tag==="button");await button.listeners.click();
 const writes=b.calls.filter(c=>c.method==="POST").length;b.ui.setLanguage("ru");
 assert.match(b.get("receipt").children[0].children[0].textContent,/Исходный результат запроса: Ожидает проверки/);
 assert.match(b.get("history").children[0].children[0].textContent,/Текущее состояние подтверждения: Ожидает проверки/);
 assert.match(b.get("status").textContent,/Исходное подтверждение принято/);
 assert.equal(b.get("preview-info").textContent,"Файл: "+fileID);
 assert.equal(b.calls.filter(c=>c.method==="POST").length,writes);
 b.ui.setLanguage("en");assert.match(b.get("receipt").children[0].children[0].textContent,/Original request result/);
 b.ui.clear();assert.equal(b.get("receipt").children.length,0);assert.equal(b.get("status").textContent,"");
});

test("zero-total order and an exceptional positive-payment claim remain readable and recoverable",async()=>{
 const api=backend(),storage=new Map();api.order.snapshot.quote.total_minor=0;
 const result=api.result;api.result=body=>({...result(body),quoted_amount_minor:0,state:"needs_decision"});
 api.hook=(path,method,body,base)=>{const result=base(path,method,body);if(method==="POST")throw {status:503};return result;};
 const b=browser(api,{storage});await b.start();await b.open();assert.match(b.text(),/Order total: 0.00 BYN/);
 await b.select();await b.submit();assert.equal(b.calls.filter(c=>c.method==="POST").length,0);
 b.get("amount").value="1.00";await b.submit();assert.equal(storage.size,1);assert.equal(api.claims.length,1);
 api.hook=undefined;const reload=browser(api,{storage});await reload.start();
 assert.equal(storage.size,0);assert.equal(reload.calls.filter(c=>c.method==="POST").length,0);
 assert.match(reload.text(),/Order total: 0.00 BYN/);assert.match(reload.text(),/Ambassador must resolve this claim/);
 assert.equal(reload.get("submit").disabled,true);
});
