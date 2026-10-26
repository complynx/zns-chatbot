"use strict";

const assert = require("node:assert/strict");
const { createHash, webcrypto } = require("node:crypto");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");

const source = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/app.js"), "utf8");
const token = "a".repeat(43);
const nonce = "b".repeat(43);
const launch = proof => createHash("sha256").update(proof).digest("hex");

function browser(proof, stored, replies, accountReplies = [], profileReplies = [], detailsReplies = [], options = {}) {
  function element() {
    return {
    hidden: false, disabled: false, textContent: "", value: "", options: [{textContent:""}], listeners: {},
      children: [], append(...children) { this.children.push(...children); },
      replaceChildren(...children) { this.children = children; },
      removeAttribute(name) { delete this[name]; },
      addEventListener(event, callback) { this.listeners[event] = callback; }
    };
  }
  const elements = Object.fromEntries(["status", "name", "retry", "account", "account-title", "account-status",
    "account-events", "account-retry", "account-first", "account-next", "profile", "profile-title", "profile-status", "profile-first-name", "profile-last-name", "profile-language", "profile-first-name-label", "profile-last-name-label", "profile-language-label", "profile-save-name", "profile-save-language", "profile-refresh", "profile-form", "details", "details-title", "details-note", "details-status", "details-name", "details-passport", "details-name-label", "details-passport-label", "details-name-presence", "details-passport-presence", "details-save-name", "details-save-passport", "details-refresh", "details-name-form", "details-passport-form"].map(id => [id, element()]));
  const scrolls = [];
  const launchFactories = { knowledge: "createZinusyaKnowledge", pairs: "createZinusyaPairs", avatars: "createZinusyaAvatars", orders: "createZinusyaOrders", meals: "createZinusyaMeals", massage: "createZinusyaMassage", "payment-review": "createZinusyaPaymentReviews" };
  for (const id of Object.keys(launchFactories)) {
    elements[id] = element();
    elements[id].hidden = true;
    elements[id].scrollIntoView = () => scrolls.push({ id, visible: !elements[id].hidden });
  }
  const window = { location: { hash: options.hash || "" }, Telegram: { WebApp: { initData: proof, ready() {} } } };
  if (options.launchSection) {
    const section = elements[options.launchSection];
    window[launchFactories[options.launchSection]] = () => ({
      setBusy() {}, setLanguage() {}, clear() { section.hidden = true; },
      async start() { section.hidden = !!options.hideLaunchSection; }
    });
  }
  options.configureWindow?.(window);
  const calls = [];
  const accountCalls = [];
  const profileCalls = [];
  const detailsCalls = [];
  const storageWrites = [];
  let saved = JSON.stringify(stored || {});
  const context = {
    document: { getElementById: id => elements[id] || (options.passPayments ? elements[id] = element() : undefined), createElement: element, documentElement: {} },
    navigator: { language: "en" }, window,
    sessionStorage: { getItem: key => key === "zinusya-miniapp-v1" ? saved : null, setItem: (_, value) => {
      if (options.storageUnavailable) throw new Error("Storage unavailable");
      storageWrites.push(value); saved = value;
    } },
    crypto: webcrypto, Uint8Array, TextEncoder, btoa,
    fetch: async (path, requestOptions) => {
      const accountRead = path.startsWith("./account");
      const profileRead = path.startsWith("./profile");
      const detailsRead = path.startsWith("./registration-details");
      (detailsRead ? detailsCalls : profileRead ? profileCalls : accountRead ? accountCalls : calls).push({ path, ...requestOptions });
      const reply = options.extraReplies?.[path] || (detailsRead ? (detailsReplies.shift() || {status:404}) : profileRead ? (profileReplies.shift() || {status:200,body:{version:0,full_name:account.full_name,first_name:account.first_name,last_name:account.last_name,bot_language:account.bot_language}}) : accountRead ? (accountReplies.shift() || { status: 200, body: emptyPage }) : replies.shift());
      assert.ok(reply, "Unexpected HTTP request");
      if (reply.error) throw reply.error;
      return { ok: reply.status < 400, status: reply.status, json: async () => reply.body };
    }
  };
  if (options.passPayments) {
    runInNewContext(readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/pass-payments.js"), "utf8"), context);
  }
  runInNewContext(source, context);
  return {
    elements, calls, accountCalls, profileCalls, detailsCalls, storageWrites, scrolls, stored: () => JSON.parse(saved),
    async settle() {
      for (let i = 0; i < 100; i++) {
        if (!elements.retry.disabled) return;
        await new Promise(resolve => setTimeout(resolve, 5));
      }
      assert.fail("Client did not finish its request");
    }
  };
}

const account = { user_id: "510e58dd-e547-48d4-b9fa-571d24953218", full_name: "Daniel <test>", first_name: "Daniel", last_name: "<test>", bot_language: "en", session_token: token };
const emptyPage = { observed_at: "2026-10-15T00:00:00Z", events: [], has_more: false };

test("Telegram entry bootstraps automatically without sign-in or sign-out controls", async () => {
  const html = readFileSync(resolve(__dirname, "../internal/api/miniapp_ui/index.html"), "utf8");
  assert.doesNotMatch(html, /id="(?:login|logout)"/);
  const app = browser("proof", {}, [{ status: 200, body: account }]);
  await app.settle();
  assert.equal(app.calls[0].path, "./session/bootstrap");
  assert.equal(app.elements.retry.hidden, true);
  assert.equal(app.elements.status.textContent, "Connected");
});

test("missing Telegram proof gives launch instructions without a login or retry action", async () => {
  const app = browser("", {}, []);
  await app.settle();
  assert.equal(app.calls.length, 0);
  assert.equal(app.elements.retry.hidden, true);
  assert.match(app.elements.status.textContent, /from the Зинуся Telegram bot/);
});

test("only transient bootstrap failures offer connection retry", async () => {
  for (const status of [400, 401, 403, 409, 422]) {
    const app = browser("proof", {}, [{ status }]);
    await app.settle();
    assert.equal(app.elements.retry.hidden, true, String(status));
    assert.equal(app.stored().token, undefined);
  }
  for (const status of [408, 429, 500, 503]) {
    const app = browser("proof", {}, [{ status }]);
    await app.settle();
    assert.equal(app.elements.retry.hidden, false, String(status));
  }
  const malformed = browser("proof", {}, [{ status: 200, body: {} }]);
  await malformed.settle();
  assert.equal(malformed.elements.retry.hidden, true);
});

test("startup keeps controls locked after pass payments load until the last module is ready", { timeout: 5000 }, async () => {
  let entered, release;
  const started = new Promise(resolve => { entered = resolve; });
  const ready = new Promise(resolve => { release = resolve; });
  const app = browser("proof", {}, [{ status: 200, body: account }], [], [], [], {
    passPayments: true,
    extraReplies: {
      "./pass-payments/targets": { status: 200, body: { targets: [] } },
      "./pass-payments/recent": { status: 200, body: { actions: [] } }
    },
    configureWindow(window) {
      window.createZinusyaKnowledge = () => ({
        clear() {}, setBusy() {}, setLanguage() {},
        async start() { entered(); await ready; }
      });
    }
  });
  try {
    await started;
    assert.deepEqual(app.calls.filter(call => call.path.startsWith("./pass-payments/")).map(call => call.path),
      ["./pass-payments/targets", "./pass-payments/recent"]);
    assert.equal(app.elements["pp-status"].textContent, "Choose the file, amount and purpose explicitly before sharing.");
    assert.equal(app.elements.retry.disabled, true);
    assert.equal(app.elements["profile-save-name"].disabled, true);
    assert.equal(app.elements["pp-recent"].disabled, true);
  } finally {
    release();
    await app.settle();
  }
  assert.equal(app.elements.retry.disabled, false);
  assert.equal(app.elements["profile-save-name"].disabled, false);
  assert.equal(app.elements["pp-recent"].disabled, false);
});

function visibleText(element) {
  return [element.textContent, ...element.children.map(visibleText)].join("\n");
}

test("a fresh launch replaces another account's stored capability", async () => {
  const app = browser("new signed proof", { launch: launch("old signed proof"), token, nonce },
    [{ status: 200, body: account }]);
  await app.settle();
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].path, "./session/bootstrap");
  assert.equal(JSON.parse(app.calls[0].body).init_data, "new signed proof");
  assert.notEqual(app.stored().nonce, nonce);
  assert.equal(app.elements.name.textContent, "Daniel <test>");
  assert.equal(app.elements.status.textContent, "Connected");
  assert.ok(!JSON.stringify(app.stored()).includes("signed proof"));
});

test("same-launch reload resumes its capability without bootstrapping", async () => {
  const app = browser("proof", { launch: launch("proof"), token, nonce }, [{ status: 200, body: account }]);
  await app.settle();
  assert.equal(app.calls.length, 1);
  assert.equal(app.calls[0].path, "./me");
  assert.equal(app.calls[0].headers.Authorization, "ZinusyaSession " + token);
});

test("an invalid stored capability gets one proof attempt, then requests a new launch", async () => {
  const app = browser("proof", { launch: launch("proof"), token, nonce }, [{ status: 401 }, { status: 409 }]);
  await app.settle();
  assert.deepEqual(app.calls.map(call => call.path), ["./me", "./session/bootstrap"]);
  assert.equal(JSON.parse(app.calls[1].body).bootstrap_nonce, nonce);
  assert.equal(app.stored().token, undefined);
  assert.match(app.elements.status.textContent, /again from Telegram/);
  assert.equal(app.elements.retry.hidden, true);
});

test("a lost bootstrap reply retries the same proof and nonce", async () => {
  const app = browser("proof", {}, [{ error: new Error("Connection closed") }, { status: 200, body: account }]);
  await app.settle();
  assert.equal(app.elements.retry.hidden, false);
  await app.elements.retry.listeners.click();
  assert.equal(app.calls[0].body, app.calls[1].body);
  assert.equal(app.elements.status.textContent, "Connected");
});

test("an IAM outage retains the capability and retries the owner read", async () => {
  const app = browser("proof", { launch: launch("proof"), token, nonce }, [{ status: 503 }, { status: 200, body: account }]);
  await app.settle();
  assert.equal(app.stored().token, token);
  await app.elements.retry.listeners.click();
  assert.deepEqual(app.calls.map(call => call.path), ["./me", "./me"]);
});

test("profile refresh authentication failure clears capability and private presentation", async () => {
  const app = browser("proof", { launch: launch("proof"), token, nonce }, [{ status: 200, body: account }], [],
    [{ status: 200, body: { version: 0, first_name: account.first_name, full_name: account.full_name } }, { status: 401 }]);
  await app.settle();
  app.elements["profile-first-name"].value = "Unsent private name";
  await app.elements["profile-refresh"].listeners.click();
  assert.equal(app.stored().token, undefined);
  assert.equal(app.elements["profile-first-name"].value, "");
  assert.equal(app.elements.name.textContent, "");
  assert.equal(app.elements.name.hidden, true);
  assert.equal(app.elements.account.hidden, true);
  assert.equal(app.elements["account-events"].children.length, 0);
  assert.match(app.elements.status.textContent, /again from Telegram/);
  assert.equal(app.elements.retry.hidden, true);
});

const event = {
  event_id: "00000000-0000-4000-8000-000000000001", event_name: "Event <script>", timezone: "Asia/Yekaterinburg",
  starts_at: "2026-10-15T00:00:00Z", ends_at: "2026-10-16T00:00:00Z",
  registration_enabled: true, registration: { role: "leader", state: "assigned" },
  pass: { balance: { price_minor: 12345, confirmed_minor: 3087, remaining_minor: 9258,
    deposit_required_minor: 3087, currency: "BYN", currency_scale: 2, deposit_satisfied: true, fully_paid: false },
    due_at: "2026-10-15T12:00:00Z", final_due_at: "2026-10-15T12:00:00Z", overdue: false, pending_evidence: 1 }
};

test("account renders confirmed exact money separately from pending proofs in the event timezone", async () => {
  const app = browser("proof", {}, [{ status: 200, body: account }],
    [{ status: 200, body: { ...emptyPage, events: [event] } }]);
  await app.settle();
  const text = visibleText(app.elements["account-events"]);
  assert.match(text, /Event <script>/);
  assert.match(text, /123\.45 BYN/);
  assert.match(text, /30\.87 BYN/);
  assert.match(text, /92\.58 BYN/);
  assert.match(text, /Proofs awaiting human confirmation: 1/);
  assert.match(text, /5:00:00\s*AM/);
  assert.match(text, /Asia\/Yekaterinburg/);
  assert.equal(app.accountCalls[0].headers.Authorization, "ZinusyaSession " + token);
});

test("disabled registration retains its state without displaying an invented unpaid balance", async () => {
  const disabled = { ...event, registration_enabled: false, pass: undefined };
  const app = browser("proof", {}, [{ status: 200, body: account }],
    [{ status: 200, body: { ...emptyPage, events: [disabled] } }]);
  await app.settle();
  const text = visibleText(app.elements["account-events"]);
  assert.match(text, /Pass assigned/);
  assert.match(text, /module unavailable/);
  assert.doesNotMatch(text, /BYN|Remaining|not fully confirmed/);
});

test("repeated local clock times at DST fallback display distinct UTC offsets", async () => {
  const fallback = { ...event, timezone: "Europe/Berlin", starts_at: "2026-10-25T00:30:00Z", ends_at: "2026-10-25T01:30:00Z" };
  const app = browser("proof", {}, [{ status: 200, body: account }],
    [{ status: 200, body: { ...emptyPage, events: [fallback] } }]);
  await app.settle();
  const text = visibleText(app.elements["account-events"]);
  assert.match(text, /GMT\+2/);
  assert.match(text, /GMT\+1/);
});

test("account pagination retries the same failed cursor and replaces the page", async () => {
  const cursor = event.event_id;
  const app = browser("proof", {}, [{ status: 200, body: account }], [
    { status: 200, body: { ...emptyPage, events: [event], has_more: true, next: cursor } },
    { status: 503 }, { status: 200, body: emptyPage }
  ]);
  await app.settle();
  await app.elements["account-next"].listeners.click();
  assert.match(app.elements["account-status"].textContent, /Retry this page/);
  await app.elements["account-retry"].listeners.click();
  assert.equal(app.accountCalls[1].path, "./account?after=" + cursor);
  assert.equal(app.accountCalls[2].path, app.accountCalls[1].path);
  assert.equal(app.elements["account-events"].children.length, 0);
  assert.equal(app.elements["account-status"].textContent, "No registrations.");
  assert.equal(app.stored().token, token);
});

test("account revocation clears rendered data and the saved capability", async () => {
  const app = browser("proof", {}, [{ status: 200, body: account }], [
    { status: 200, body: { ...emptyPage, events: [event] } }, { status: 401 }
  ]);
  await app.settle();
  await app.elements["account-retry"].listeners.click();
  assert.equal(app.stored().token, undefined);
  assert.equal(app.elements.account.hidden, true);
  assert.equal(app.elements["account-events"].children.length, 0);
  assert.match(app.elements.status.textContent, /again from Telegram/);
});

test("a page outside Telegram does not resume a stored capability", async () => {
  const app = browser("", { launch: launch("proof"), token, nonce }, []);
  await app.settle();
  assert.equal(app.calls.length, 0);
  assert.match(app.elements.status.textContent, /from the Зинуся Telegram bot/);
});


test("an uncertain profile write retries the same operation after reload", async () => {
  const first = browser("proof", {launch:launch("proof"),token,nonce}, [{status:200,body:account}], [],
    [{status:200,body:{version:4,full_name:"Old name",first_name:"Old",last_name:"name",bot_language:"en"}}, {status:503}]);
  await first.settle();
  first.elements["profile-first-name"].value = "New";
  first.elements["profile-last-name"].value = "name";
  await first.elements["profile-form"].listeners.submit({preventDefault(){}});
  await first.settle();
  assert.equal(first.profileCalls.length,2);
  const pending = first.stored().profileChange;
  assert.equal(pending.body.first_name,"New");
  assert.equal(pending.body.expected_version,4);
  assert.equal(first.elements["profile-first-name"].disabled,true);
  const second = browser("proof", first.stored(), [{status:200,body:account}], [],
    [{status:200,body:{version:5,full_name:"New name",first_name:"New",last_name:"name",bot_language:"en"}}, {status:200,body:{}}, {status:200,body:{version:5,full_name:"New name",first_name:"New",last_name:"name",bot_language:"en"}}]);
  await second.settle();
  await second.elements["profile-form"].listeners.submit({preventDefault(){}});
  await second.settle();
  assert.equal(second.profileCalls[1].body,first.profileCalls[1].body);
  assert.equal(second.stored().profileChange,undefined);
});

test("a conflicting profile write requires refresh before another save", async () => {
  const app = browser("proof", {launch:launch("proof"),token,nonce}, [{status:200,body:account}], [],
    [{status:200,body:{version:1,full_name:"Old name",first_name:"Old",last_name:"name",bot_language:"en"}}, {status:409}]);
  await app.settle();
  app.elements["profile-first-name"].value = "New";
  app.elements["profile-last-name"].value = "name";
  await app.elements["profile-form"].listeners.submit({preventDefault(){}});
  await app.settle();
  assert.equal(app.elements["profile-save-name"].disabled,true);
  assert.equal(app.elements["profile-refresh"].disabled,false);
  assert.match(app.elements["profile-status"].textContent,/changed elsewhere/);
  assert.equal(app.stored().profileChange,undefined);
});

test("name form keeps explicit compound components and leaves legacy full names unsplit", async () => {
  const app = browser("proof", {launch:launch("proof"),token,nonce}, [{status:200,body:account}], [], [
    {status:200,body:{version:4,full_name:"Old unsplit full name",bot_language:"ru"}},
    {status:200,body:{}},
    {status:200,body:{version:5,first_name:"Анна Мария",last_name:"де ла Крус",full_name:"Анна Мария де ла Крус",bot_language:"ru"}}
  ]);
  await app.settle();
  assert.equal(app.elements["profile-first-name"].value, "");
  assert.equal(app.elements["profile-last-name"].value, "");
  assert.equal(app.elements.name.textContent, "Old unsplit full name");
  assert.equal(app.elements["profile-first-name-label"].textContent, "Имя");
  assert.equal(app.elements["profile-last-name-label"].textContent, "Фамилия");
  app.elements["profile-first-name"].value = "Анна Мария";
  app.elements["profile-last-name"].value = "де ла Крус";
  await app.elements["profile-form"].listeners.submit({preventDefault(){}});
  await app.settle();
  const body = JSON.parse(app.profileCalls[1].body);
  assert.equal(body.first_name, "Анна Мария");
  assert.equal(body.last_name, "де ла Крус");
  assert.equal(body.full_name, undefined);
  assert.equal(app.elements.name.textContent, "Анна Мария де ла Крус");
  assert.equal(app.elements["details-name"].value, "");
});


test("pending write survives failed read and refresh without changing its operation", async () => {
  const pending={kind:"name",body:{operation_id:webcrypto.randomUUID(),expected_version:2,first_name:"New",last_name:"name"}};
  const app=browser("proof",{launch:launch("proof"),token,nonce,profileChange:pending},[{status:200,body:account}],[],
    [{status:503},{status:200,body:{version:3,full_name:"New name",first_name:"New",last_name:"name",bot_language:"en"}},{status:200,body:{}},{status:200,body:{version:3,full_name:"New name",first_name:"New",last_name:"name",bot_language:"en"}}]);
  await app.settle();
  assert.equal(app.elements["profile-refresh"].disabled,false);
  await app.elements["profile-refresh"].listeners.click();
  await app.settle();
  await app.elements["profile-form"].listeners.submit({preventDefault(){}});
  await app.settle();
  assert.deepEqual(JSON.parse(app.profileCalls[2].body),pending.body);
  assert.equal(app.stored().profileChange,undefined);
});

test("automatic profile language returns to the browser fallback", async () => {
  const app=browser("proof",{launch:launch("proof"),token,nonce},[{status:200,body:account}],[],
    [{status:200,body:{version:1,full_name:"Name",bot_language:"ru"}},{status:200,body:{}},{status:200,body:{version:2,full_name:"Name",bot_language:"auto"}}]);
  await app.settle();
  assert.equal(app.elements["profile-title"].textContent,"Мой профиль");
  app.elements["profile-language"].value="auto";
  await app.elements["profile-save-language"].listeners.click();
  await app.settle();
  assert.equal(app.elements["profile-title"].textContent,"My profile");
});


const detailsState = (version, name = false, passport = false) => ({version,legal_name_present:name,passport_number_present:passport});
const detailsPending = (field="legal-name", version=0) => ({owner:account.user_id,operation_id:webcrypto.randomUUID(),expected_version:version,field});
const signedSession = change => ({launch:launch("proof"),token,nonce,detailsChange:change});
const submit = (app, field) => app.elements["details-" + field + "-form"].listeners.submit({preventDefault(){}});

function assertNoPrivateStorage(app, ...values) {
  for (const raw of app.storageWrites) for (const value of values) assert.ok(!raw.includes(value),"Private form value entered browser storage");
}

test("private form saves one field without storing or prefilling legal values",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)}]);
 await app.settle();
 assert.equal(app.elements.details.hidden,false);
 assert.equal(app.elements["details-name"].value,"");
 app.elements["details-name"].value="Synthetic Private Legal Name";
 await submit(app,"name");await app.settle();
 assert.equal(app.detailsCalls[1].method,"POST");
 assert.equal(JSON.parse(app.detailsCalls[1].body).value,"Synthetic Private Legal Name");
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.stored().detailsChange,undefined);
 assert.match(app.elements["details-name-presence"].textContent,/Saved; value hidden/);
 assert.equal(app.elements["profile-first-name"].value,account.first_name);
 assertNoPrivateStorage(app,"Synthetic Private Legal Name");
});

test("unknown private write retries identical body and operation from memory",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{error:new Error("Network lost")},{status:200,body:detailsState(1,false,true)},{status:200,body:detailsState(1,false,true)}]);
 await app.settle();app.elements["details-passport"].value="SYNTHETIC-PRIVATE-PASSPORT";
 await submit(app,"passport");await app.settle();
 assert.equal(app.elements["details-passport"].disabled,true);
 assert.equal(app.elements["details-save-passport"].disabled,false);
 assert.equal(app.elements["details-save-name"].disabled,true);
 assert.deepEqual(Object.keys(app.stored().detailsChange).sort(),["expected_version","field","operation_id","owner"]);
 // Even a DOM edit cannot change an already submitted uncertain body.
 app.elements["details-passport"].value="Unsubmitted different value";
 await submit(app,"passport");await app.settle();
 assert.equal(app.detailsCalls[1].body,app.detailsCalls[2].body);
 assertNoPrivateStorage(app,"SYNTHETIC-PRIVATE-PASSPORT","Unsubmitted different value");
 assert.equal(app.elements["details-passport"].value,"");
});

test("reload recovers historical receipt then separately refreshes current presence",async()=>{
 const pending=detailsPending();
 const app=browser("proof",signedSession(pending),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(2,true,true)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(3,true,true)}]);
 await app.settle();
 assert.equal(app.detailsCalls.length,3);
 assert.equal(app.detailsCalls[1].path,"./registration-details/operations/"+pending.operation_id);
 assert.ok(app.detailsCalls.every(c=>c.method==="GET"));
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.elements["details-passport"].value,"");
 assert.match(app.elements["details-passport-presence"].textContent,/Saved; value hidden/);
 assert.match(app.elements["details-status"].textContent,/earlier request was saved/);
 assert.equal(app.stored().detailsChange,undefined);
});

test("missing receipt after reload needs explicit reentry using the original operation",async()=>{
 const pending=detailsPending("passport-number",4);
 const app=browser("proof",signedSession(pending),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(4,true)},{status:404},{status:200,body:detailsState(5,true,true)},{status:200,body:detailsState(5,true,true)}]);
 await app.settle();
 assert.equal(app.detailsCalls.length,2);
 assert.match(app.elements["details-status"].textContent,/Re-enter its original value/);
 assert.equal(app.elements["details-save-name"].disabled,true);
 assert.equal(app.elements["details-save-passport"].disabled,false);
 app.elements["details-passport"].value="SYNTHETIC-REENTERED-PASSPORT";
 await submit(app,"passport");await app.settle();
 const body=JSON.parse(app.detailsCalls[2].body);
 assert.equal(body.operation_id,pending.operation_id);
 assert.equal(body.expected_version,4);
 assertNoPrivateStorage(app,"SYNTHETIC-REENTERED-PASSPORT");
});

test("missing receipt plus a newer version closes old CAS without submitting a replacement",async()=>{
 const pending=detailsPending("legal-name",1);
 const app=browser("proof",signedSession(pending),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(2,true)},{status:404}]);
 await app.settle();
 assert.ok(app.detailsCalls.every(c=>c.method==="GET"));
 assert.equal(app.stored().detailsChange,undefined);
 assert.equal(app.elements["details-save-name"].disabled,false);
 assert.match(app.elements["details-status"].textContent,/can no longer apply at its old version/);
});

test("unavailable receipt lookup preserves metadata and blocks changed writes until refresh",async()=>{
 const pending=detailsPending();
 const app=browser("proof",signedSession(pending),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{status:503},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)}]);
 await app.settle();
 assert.equal(app.stored().detailsChange.operation_id,pending.operation_id);
 assert.equal(app.elements["details-save-name"].disabled,true);
 await app.elements["details-refresh"].listeners.click();await app.settle();
 assert.equal(app.stored().detailsChange,undefined);
 assert.ok(app.detailsCalls.every(c=>c.method==="GET"));
});

test("denied private write clears inputs and retains only metadata for reauthentication",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{status:401}]);
 await app.settle();app.elements["details-name"].value="Synthetic Denied Private Name";
 await submit(app,"name");await app.settle();
 assert.equal(app.elements.details.hidden,true);
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.stored().token,undefined);
 assert.ok(app.stored().detailsChange.operation_id);
 assertNoPrivateStorage(app,"Synthetic Denied Private Name");
});

test("private form refuses submission when recovery metadata cannot be persisted",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)}],{storageUnavailable:true});
 await app.settle();app.elements["details-name"].value="Synthetic Unsent Private Name";
 await submit(app,"name");await app.settle();
 assert.equal(app.detailsCalls.length,1);
 assert.match(app.elements["details-status"].textContent,/Nothing was submitted/);
});

test("new Telegram launch recovers the same owner's pending private operation",async()=>{
 const pending=detailsPending();
 const app=browser("new proof",signedSession(pending),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)}]);
 await app.settle();
 assert.equal(app.calls[0].path,"./session/bootstrap");
 assert.equal(app.detailsCalls[1].path,"./registration-details/operations/"+pending.operation_id);
 assert.equal(app.stored().detailsChange,undefined);
});

test("disabled private module does not discard a pending operation or allow writes",async()=>{
 const pending=detailsPending();
 const app=browser("proof",signedSession(pending),[{status:200,body:account}]);
 await app.settle();
 assert.equal(app.elements.details.hidden,false);
 assert.equal(app.elements["details-save-name"].disabled,true);
 assert.equal(app.stored().detailsChange.operation_id,pending.operation_id);
 assert.match(app.elements["details-status"].textContent,/has not been discarded/);
});


test("saving one private field preserves the other unsubmitted draft",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)},
  {status:200,body:detailsState(2,true,true)},{status:200,body:detailsState(2,true,true)}]);
 await app.settle();
 app.elements["details-name"].value="Synthetic Both Fields Name";
 app.elements["details-passport"].value="SYNTHETIC-UNSUBMITTED-PASSPORT";
 await submit(app,"name");await app.settle();
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.elements["details-passport"].value,"SYNTHETIC-UNSUBMITTED-PASSPORT");
 await submit(app,"passport");await app.settle();
 assert.equal(JSON.parse(app.detailsCalls[3].body).value,"SYNTHETIC-UNSUBMITTED-PASSPORT");
 assert.equal(app.elements["details-passport"].value,"");
 assertNoPrivateStorage(app,"Synthetic Both Fields Name","SYNTHETIC-UNSUBMITTED-PASSPORT");
});

test("recovering an uncertain field keeps the other unsent draft",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[],[
  {status:200,body:detailsState(0)},{error:new Error("Reply lost")},
  {status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)},{status:200,body:detailsState(1,true)}]);
 await app.settle();
 app.elements["details-name"].value="Synthetic Uncertain Fields Name";
 app.elements["details-passport"].value="SYNTHETIC-KEPT-PASSPORT";
 await submit(app,"name");await app.settle();
 await app.elements["details-refresh"].listeners.click();await app.settle();
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.elements["details-passport"].value,"SYNTHETIC-KEPT-PASSPORT");
 assertNoPrivateStorage(app,"Synthetic Uncertain Fields Name","SYNTHETIC-KEPT-PASSPORT");
});


test("authentication failure clears private drafts and retains only recovery metadata",async()=>{
 const app=browser("proof",signedSession(),[{status:200,body:account}],[],[
  {status:200,body:{version:0,first_name:account.first_name,full_name:account.full_name}}, {status:401}], [
  {status:200,body:detailsState(0)},{error:new Error("Reply lost")}]);
 await app.settle();
 app.elements["details-name"].value="Synthetic Logout Name";
 app.elements["details-passport"].value="SYNTHETIC-LOGOUT-PASSPORT";
 await submit(app,"name");await app.settle();
 const operation=app.stored().detailsChange.operation_id;
 await app.elements["profile-refresh"].listeners.click();await app.settle();
 assert.equal(app.elements["details-name"].value,"");
 assert.equal(app.elements["details-passport"].value,"");
 assert.equal(app.elements.details.hidden,true);
 assert.equal(app.elements.retry.hidden,true);
 assert.equal(app.stored().token,undefined);
 assert.equal(app.stored().detailsChange.operation_id,operation);
 assertNoPrivateStorage(app,"Synthetic Logout Name","SYNTHETIC-LOGOUT-PASSPORT");
});

test("another verified owner never receives the previous owner's recovery request",async()=>{
 const pending=detailsPending();
 const other={...account,user_id:webcrypto.randomUUID()};
 const app=browser("another proof",signedSession(pending),[{status:200,body:other}],[],[],[{status:200,body:detailsState(0)}]);
 await app.settle();
 assert.equal(app.stored().detailsChange,undefined);
 assert.equal(app.detailsCalls.length,1);
 assert.equal(app.elements["details-name"].value,"");
});


test("native launch targets restore their section after Telegram appends auth parameters", async () => {
  for (const section of ["knowledge", "avatars", "orders", "meals", "massage", "payment-review"]) {
    for (const hash of ["#" + section, "#" + section + "?tgWebAppData=fixture&tgWebAppVersion=9.6"]) {
      const app = browser("proof", {}, [{ status: 200, body: account }], [], [], [], { hash, launchSection: section });
      await app.settle();
      assert.equal(app.elements.status.textContent, "Connected");
      assert.deepEqual(app.scrolls, [{ id: section, visible: true }]);
      assert.equal(app.calls.length, 1);
    }
  }
});

test("launch targets cannot jump to unrelated controls or unauthenticated content", async () => {
  for (const hash of ["", "#tgWebAppData=fixture", "#logout?tgWebAppData=fixture", "#unknown"]) {
    const app = browser("proof", {}, [{ status: 200, body: account }], [], [], [], { hash });
    await app.settle();
    assert.deepEqual(app.scrolls, []);
  }
  const denied = browser("proof", {}, [{ status: 401 }], [], [], [], { hash: "#avatars?tgWebAppData=fixture", launchSection: "avatars" });
  await denied.settle();
  assert.deepEqual(denied.scrolls, []);
  const hidden = browser("proof", {}, [{ status: 200, body: account }], [], [], [], { hash: "#avatars", launchSection: "avatars", hideLaunchSection: true });
  await hidden.settle();
  assert.deepEqual(hidden.scrolls, []);
});


test("verified meal and order forms receive only their original operation fragment", async () => {
  const event = webcrypto.randomUUID(), operation = webcrypto.randomUUID();
  for (const section of ["meals", "orders"]) {
    let continuation;
    const factory = section === "meals" ? "createZinusyaMeals" : "createZinusyaOrders";
    const app = browser("proof", {}, [{ status: 200, body: account }], [], [], [], {
      hash: "#" + section + "?event=" + event + "&operation=" + operation + "&tgWebAppVersion=9.6", launchSection: section,
      configureWindow(window) { const create = window[factory]; window[factory] = env => { continuation = env.continuation; return create(env); }; }
    });
    await app.settle();
    assert.equal(continuation.event_id, event); assert.equal(continuation.operation_id, operation);
    assert.equal(app.elements.status.textContent, "Connected"); assert.equal(app.calls.length, 1);
  }
});

test("crop launch passes only its draft locator after authentication and does not open other forms", async () => {
  const draft = webcrypto.randomUUID(); let continuation, verifiedOwner;
  const app = browser("proof", {}, [{ status: 200, body: account }], [], [], [], {
    hash: "#avatar-crop?draft=" + draft + "&tgWebAppVersion=9.6",
    configureWindow(window) {
      window.createZinusyaAvatarCrop = env => {
        continuation = env;
        return { clear() {}, setBusy() {}, async start(owner) { verifiedOwner = owner; } };
      };
    }
  });
  await app.settle();
  assert.equal(continuation.draftID, draft); assert.equal(verifiedOwner, account.user_id);
  assert.equal(app.calls.length, 1); assert.equal(app.accountCalls.length, 0); assert.equal(app.profileCalls.length, 0);
});
