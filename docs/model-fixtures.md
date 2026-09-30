# Programmable sandbox model

The fixture provider returns operator-written plans. It tests real host tools,
authorization, persistence, retries, and Telegram rendering without external AI.
It does not test model reasoning, skill selection, or language inference.

Start a separate product sandbox with the optional overlay:

```sh
docker compose -f compose.product.yaml -f compose.product.fixture.yaml up -d --build
```

Do not rebuild a stand during Functional QA. The overlay sets `model.provider`
to `fixture`, `model.url` to `http://fake:8080/lab/model`, and `synthetic_only`
to `true`. Configuration rejects fixture mode without an explicit HTTP URL or
outside synthetic app/bot mode. The normal product configuration is unchanged.

## Install and send a message

POST `/lab/model/fixtures` on the fake Telegram server with `X-Sandbox: 1` and
JSON. This example atomically installs a sequence and sends Alice's message:

```json
{
  "input": { "user": 101, "text": "Calculate 2 plus 3", "language_code": "en" },
  "steps": [
    {
      "expect": { "text": "Calculate 2 plus 3", "script": { "remaining": 2 } },
      "plan": {
        "view": "workflow",
        "script_action": {
          "code": "return input.a + input.b",
          "input_json": "{\"a\":2,\"b\":3}"
        }
      }
    },
    {
      "expect": {
        "text": "Calculate 2 plus 3",
        "script": { "remaining": 1, "runs": [{ "result": 5 }] }
      },
      "plan": { "view": "workflow", "text": "The result is 5." }
    }
  ]
}
```

The response contains `installed: true` and the assigned `update_id`. The
fixture exists before polling can receive the update. The message appears in
the normal Telegram-like UI. Synthetic users are Alice 101, Bob 202, and visitor 303. This input mode cannot also specify `owner` or `update_id`.

For direct integration tests, omit `input` and supply `owner` and `update_id`.
Install before delivering that exact update. Do not predict a live update ID
or install after `/lab/input`; bot polling can race setup.

Each `expect` is a typed partial `agent.Input` object and must include exact
`text`. Objects match the supplied subset; arrays match length and element
order exactly. Numbers use their JSON representation. Unknown typed fields,
duplicate JSON keys, trailing JSON, and invalid plans are rejected. All plans
use the existing `agent.Plan` contract, including KB, history, registration,
and JS actions. Unmentioned fields do not constrain input. Avoid unstable IDs
unless a prior API read supplied them.

## Scope and evidence

### Synthetic knowledge assessments

An optional `assessment` object programs the classifier for one synthetic
owner/update case. Combine it with the ordinary suggestion plan and input in
the same installation envelope:

```json
{
  "assessment": {
    "expect": {
      "event": "",
      "topic": "travel",
      "fact_key": "arrival",
      "text": "The synthetic shuttle leaves at 18:00."
    },
    "result": {
      "worthwhile": true,
      "reason": "Programmed synthetic verdict for manual review."
    }
  }
}
```

This fragment is not a complete installation. Supply either `input` with plan
steps or a known `owner` and positive `update_id`. Assessment-only cases are
allowed for an explicit later retry. A case must contain at least one plan step
or assessment. Install-and-enqueue validates the complete case before publishing
the input update.

Assessment field names are case-sensitive; aliases such as `Worthwhile` are
rejected, including when the canonical spelling is also present.
All four expected strings and both result fields are required, even when an
identifier is empty or `worthwhile` is false. Expected strings match exactly;
there are no subset or wildcard matches. Text must be nonblank. Identifiers
are limited to 100 Unicode characters, text to 2,000, and reason to 512.
NUL, invalid UTF-8, null fields, duplicate or unknown keys, and wrong types are
rejected. Assessment requests are limited to 16 KiB and encoded results to
4,096 bytes. Plan steps and assessments share the retained 32-entry and 256-KiB
global capacity; consumed entries still count.

`POST /lab/model/knowledge-assessment` uses host-supplied actor/update scope and
the same synthetic lab boundary. Assessment does not consume a planning turn.
Each case permits one successful assessment. Missing configuration or changed
input fails closed. An identical second request is rejected rather than returning
a cached verdict. This one-assessment limit belongs only to the fixture: it does
not restrict the product classifier. Multiple script assessments in one update
are outside this fixture's scope.

`GET /lab/model/state` includes separate `assessment` fields: `configured`,
`accepted`, `rejected`, and `last_status`. It exposes no expected text or reason.
The existing plan counters retain their meaning. Assessment counters are
in-memory test evidence, not durable application receipts.

A positive synthetic verdict prepares only the author's private draft. Manual
submission and current authorized review are still required for publication.
Provider guards still check source authority immediately before HTTP I/O. This
tests application behavior, not factual correctness or real-model quality.

The application checks its persisted verdict before calling the fixture again.
A response lost before durable storage is not silently replayed by the fixture;
use a separately programmed explicit retry update through normal application
controls. Do not reset Telegram cursors or replay business effects to rearm a
fixture. Drain pending work before restarting the fake; restarting clears fixture
definitions, not application history.

### Pair registration plans

Use `view: "passes"` for these plans. Each read needs a following fixture step;
a mutation finishes the interaction. Replace the example event and target with
values returned by the operator API. The host supplies versions and identity.

Invite: the user's current message must explicitly identify the partner, for
example `Invite Telegram user 202 to sandbox-passport-pair.` The plans are:

```json
[
  {
    "view": "passes",
    "registration_action": {
      "name": "read",
      "event": "sandbox-passport-pair",
      "view": "home"
    }
  },
  {
    "view": "passes",
    "registration_action": {
      "name": "invite",
      "event": "sandbox-passport-pair",
      "invite_telegram_id": 202
    }
  }
]
```

For acceptance, read `home`, then read `invitations` for the same event. The
final action is `{"name":"accept","event":"sandbox-passport-pair","target":"alice"}`,
where `alice` is the sender owner returned by the invitation read. `decline`
uses the same fields. A manual invitation button can also finish the flow.

For the actor's own cancellation, read `home`, then use
`{"name":"cancel","event":"sandbox-passport-pair"}`. Do not supply a target.
All actions still enforce profile requirements, current permissions and booking
state. An operator-written plan does not override these checks.

A denied read still needs a terminal step. Assert the error in
`registration.reads`, then return a text plan such as
`{"view":"passes","text":"Access denied."}`. A one-step read-only sequence
exhausts the fixture when the host asks for the next plan; that is not a complete
denial test. The fixture read budget is three registration reads per interaction.

Agent pass export is not yet exposed as a plan action. Use the manual
`/passes_table` command for the export acceptance scope.

### Receipt media plans

An image upload and its interpretation are separate operations. The model input
contains `attachment.id` for the current upload and may contain
`media_context.pending` and `media_context.candidates`. Pending entries include
`id`, `kind`, optional `question`, and current `choices`; candidates include
`order_id` or `registration_event`, version, and amounts. Use IDs from the actual
input or an already observed pending attachment. Do not invent or predict them.

The plan field is `media_action`, with `view: "media"`. For a neutral uploaded
image, an operator provider can ask the user to choose its purpose:

```json
{
  "view": "media",
  "media_action": {"media_id": "ACTUAL_ATTACHMENT_ID", "intent": "clarify"}
}
```

To interpret a synthetic receipt for a pass, use the current event and amount:

```json
{
  "view": "media",
  "media_action": {
    "media_id": "ACTUAL_ATTACHMENT_ID",
    "intent": "receipt",
    "registration_event": "ACTUAL_PASS_EVENT",
    "amount": "200.00",
    "currency": "RUB"
  }
}
```

For a modern order use `order_id` instead of `registration_event`, never both.
Amount is a positive decimal string; currency is `RUB` or `BYN`. Omit a target
when the user has not identified one. The host validates ownership, current
state and ambiguity, and may show clarification buttons. Interpretation does
not approve payment. The existing human review flow remains necessary.

`intent: "cancel"` uses only `media_id` and `intent`. For a later text reply to
a clarification, use the pending ID and an actual current choice, not a previous
message's unrelated attachment. Fixture `expect` may match exact `text` and a
partial `media_context` object; any supplied arrays must match fully as usual.

The atomic fixture `input` helper sends text, not file uploads. Do not install
a fixture after a live upload and assume polling has not started. For upload
interpretation use a QA-owned synthetic Remote provider: POST `/plan` receives
the current model input, and HTTP 200 returns the plan JSON above with the
observed attachment ID. Alternatively, install an atomic text fixture for an
already pending attachment. Keep actual upload and button interaction in the
rendered UI. Synthetic provider plans exercise host behavior, not OCR or real
model interpretation; do not log attachment bytes as ordinary diagnostics.

### Other operator plan examples

Each object below is the `plan` of a fixture step. A read is followed by another
step; its `expect` can check the remaining budget or returned data. Mutations
finish the interaction. These examples define public test inputs, not model
reasoning expectations.

```json
{
  "view": "knowledge",
  "knowledge_action": {
    "name": "memo_set",
    "fact_key": "fqa_note",
    "text": "I prefer morning workshops"
  }
}
```

```json
{
  "view": "knowledge",
  "knowledge_action": { "name": "memo_read", "fact_key": "fqa_note" }
}
```

After a memo read, the next input includes `knowledge.remaining` and
`knowledge.reads`. The read object contains `memo` with `key`, `text`, `version`
and `active`. Omitted data is marked explicitly. `/memo` shows the acting user's
private notes; select another synthetic user to check separation.

```json
{ "view": "workflow", "history_action": { "before": 0 } }
```

History returns up to four newest private events to the next step in
`conversation.reads`; `conversation.remaining` decreases. A zero `before`
starts at the newest event. Follow `next_before` for older pages.

```json
{ "view": "workflow", "action": { "name": "select", "slot_id": "massage-1" } }
```

This last example exercises the generic booking fixture from `/start`, not
the production massage schedule. A viewing-only user still cannot book it.
For a final textual response, use `{"view":"workflow","text":"Done."}`.
Typed plans cannot name a different actor or supply permission flags.

The host attaches owner, update ID, and zero-based plan-loop turn through Go
context. `FixtureRemote` sends those values to POST `/lab/model/plan`; scripts,
message text, and fixture plans cannot set the actor used by business APIs.
Every action still passes normal validation and current API permissions. A
fixture is never an authorization grant. HTTP redirects are rejected and each
model call has a ten-second deadline.

GET `/lab/model/state?owner=alice&update_id=ID`, with `X-Sandbox: 1`, returns
counts, next turn, and last status. It never returns prompts, plans, private
tool results, or another owner's case. Inspect the real domain API and rendered
messages to establish outcome; consumed plans alone do not prove completion.

Wrong owner, update, turn, missing fixture, mismatched input, and exhausted
sequences fail closed. A mismatch does not advance the queue. Concurrent
requests for the same turn permit only one consumption. There is no Scripted
fallback. Manual commands that bypass the model continue normally.

## Bounds and limitations

The process retains at most 32 cases and 32 total entries, counting plan steps
and assessments together, including consumed entries. Each entry is at most
64 KiB; stored serialized entries total at most 256 KiB.
Installation bodies are at most 256 KiB and model input at most 512 KiB. JSON
nesting is bounded. Input injection retains the existing 5,000-byte message and
pending-update limits. Reinstalling the same scope is rejected.

Fixture queues are ephemeral and have no reset endpoint. Restart only between
QA runs to clear them; this also resets consumed evidence. Bot/domain durable
replay still applies when a completed update already has a saved result, but
the fixture does not resume an interrupted model sequence across fake-server
restarts. A failed injected-message persistence operation publishes no update;
its fixture may retain capacity until restart.

The fixture implements planning and explicitly programmed knowledge assessment.
It does not implement semantic history summaries. Programmed responses do not
prove real-model reasoning, classification quality or summary acceptance.

`X-Sandbox` is a lab marker, not production authentication. These endpoints are
for a private, loopback-exposed synthetic stand. Lab operators can install
fixtures and inspect synthetic users; do not expose the fake service publicly
or send production data. No model credentials or external model calls are used.

## Profile and registration operator examples

These are programmed Plan scenarios, not interpretation tests. Use the URLs
supplied with the frozen stand. Product Compose defaults are fake/UI
`http://127.0.0.1:8118` and API `http://127.0.0.1:8117`. Installing fixtures does
not require restarting or rebuilding the stand.

POST the following to `/lab/model/fixtures` with `X-Sandbox: 1` to set Alice's own
role. Her profile must be editable; a profile action has no target user.

```json
{
  "input": {
    "user": 101,
    "text": "Set my dance role to leader.",
    "language_code": "en"
  },
  "steps": [
    {
      "expect": { "text": "Set my dance role to leader." },
      "plan": {
        "view": "profile",
        "profile_action": { "name": "set", "field": "role", "value": "leader" }
      }
    }
  ]
}
```

Profile fields are `role` (`leader` or `follower`), `legal_name`, and `passport`.
For identity fields, include the exact requested value in the current message.
For example, text `Set my passport to AB1234567.` can use
`{"view":"profile","profile_action":{"name":"set","field":"passport","value":"AB1234567"}}`.
Readiness is already available as `profile` in fixture input; there is no
`profile_action: read`. A display-only Plan is
`{"view":"profile","text":"Here is your profile."}`. Verify saved values through
the owner's read-only profile endpoint below.

This operator-side JavaScript constructs complete registration proposals and
fixture requests. Run it in a client with `fetch`, not through the agent's JS tool:

```js
const fake = "http://127.0.0.1:8118";
const event = "sandbox-festival";
const registration = (name, view = "home", options = {}) => ({
  view: "passes",
  registration_action: {
    assignment: null,
    name,
    event,
    target: "",
    invite_telegram_id: 0,
    payment_admin: "",
    view,
    cursor: "",
    ...options,
  },
});
const step = (text, plan, expected = {}) => ({
  expect: { text, ...expected },
  plan,
});
async function install(user, text, steps) {
  const response = await fetch(`${fake}/lab/model/fixtures`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Sandbox": "1" },
    body: JSON.stringify({ input: { user, text, language_code: "en" }, steps }),
  });
  if (!response.ok)
    throw new Error(`Fixture installation failed: ${response.status}`);
  return response.json();
}
```

**Own registration: read, then act.** Check Alice's profile readiness and event
eligibility first. Confirm that the event's payment-admin list contains `bob`.
The product fixture seeds Bob as a payment contact; current state remains decisive.

```js
const text =
  "Register me solo for sandbox-festival with Bob as payment contact.";
await install(101, text, [
  step(text, registration("read", "home")),
  step(text, registration("solo", "home", { payment_admin: "bob" }), {
    registration: { remaining: 2, reads: [{ booking: { owner: "alice" } }] },
  }),
]);
```

A `home` read supplies current registration and payment contacts. For a display-only
scenario, replace the second Plan with `registration('show', 'home')`. Other
read/show views are `events`, `invitations`, `queue`, `admins`, `profile`, `payment`,
`payment_queue`, and `admin_target`. `events` can use `event: ''`; other views require
a known event. At most three registration reads are available per update. Use only
a returned cursor for another page. Expected arrays match their full length; omit
an array expectation when its length is not part of the scenario.

**Administrator target: Telegram ID on read, returned owner on action.** The
message must identify the target: an exact current numeric ID, shared contact,
current editor target, or authorized queue result. Booking-administrator rights
are required; payment-review rights alone are insufficient. This example assumes
Alice already has a registration and assignment is permitted:

```js
const text =
  "Assign the existing pass for Telegram user 101 at sandbox-festival for 150 RUB total.";
await install(202, text, [
  step(text, registration("read", "admin_target", { target: "101" })),
  step(
    text,
    registration("admin_assign", "admin_target", {
      target: "alice",
      assignment: {
        total_price: 150,
        kind: null,
        comment: null,
        skip_balance: null,
        append_tier: null,
        create: false,
        from_profile: false,
        role: "",
        legal_name: null,
      },
    }),
    {
      registration: {
        reads: [
          { admin_target: { booking: { owner: "alice" }, can_assign: true } },
        ],
      },
    },
  ),
]);
```

To show the target editor without assignment, use
`registration('show', 'admin_target', {target: 'alice'})` after the same read.
Use the returned owner; do not put Telegram ID `101` in the final action.
For explicitly requested creation, use `create: true, from_profile: true` only
when `can_create_from_profile` is true. Otherwise provide `from_profile: false`,
explicit `role` and `legal_name` from the current request. Unchanged optional
assignment fields are `null`. `total_price` is total RUB, including zero for free;
`append_tier` is one-based and cannot accompany non-null `skip_balance`.

**Payment.** A participant can read then show `payment`. Changing their payment
contact uses a `home` read followed by
`registration('payment_admin', 'home', {payment_admin: 'bob'})`. Receipt upload and
submission remain separate GUI/media operations. An authorized reviewer can use
this sequence after a pending receipt for Alice exists:

```js
const text = "Accept Alice?s pending pass receipt for sandbox-festival.";
await install(202, text, [
  step(text, registration("read", "payment_queue")),
  step(
    text,
    registration("proof_accept", "payment_queue", { target: "alice" }),
  ),
]);
```

Use `proof_reject` for explicit rejection, or `show` for a non-mutating review menu.
The target must occur in the current authorized pending queue. Do not supply actor,
version, attempt or idempotency fields in a Plan. Fixtures do not grant permissions.

## Read-only state for these scenarios

To exercise a stale or foreign button, replay an actual captured callback through
POST `/lab/input` with `X-Sandbox: 1`:

```json
{"user":101,"data":"<captured callback data>","message_id":132}
```

The field is `data`, not `callback_data`. The server allocates a fresh update ID;
the selected synthetic user remains the actor. This tests callback validation,
not replay of an identical transport update. Record the before/after domain state
as well as the rendered response. Never invent a callback token as positive-path
evidence.

`GET /lab/state?user=101` on the fake service returns Alice's visible messages,
edits and interaction evidence; use 202 for Bob or 303 for Visitor. Fixture counters
are at `/lab/model/state?owner=alice&update_id=ID` with `X-Sandbox: 1`. Neither
endpoint alone proves that a business action succeeded.

The product Compose sandbox uses the public synthetic signing key below. Other
stands may provide different credentials. Its bearer token has two parts, not a
three-part JWT. This Node example makes a read-only request with a one-minute token:

```js
import { createHmac } from "node:crypto";
const api = "http://127.0.0.1:8117";
function syntheticToken(subject) {
  const body = Buffer.from(
    JSON.stringify({
      sub: subject,
      actor: "sandbox-bot",
      aud: "zns-core",
      exp: Math.floor(Date.now() / 1000) + 60,
    }),
  ).toString("base64url");
  const signature = createHmac(
    "sha256",
    "sandbox-product-only-not-for-production-123456789",
  )
    .update(body)
    .digest("base64url");
  return `${body}.${signature}`;
}
const response = await fetch(`${api}/v1/me/pass-profile`, {
  headers: { Authorization: `Bearer ${syntheticToken("alice")}` },
});
console.log(response.status, await response.json());
```

Confirmed GET routes, relative to the API base:

| Route                                                    | Evidence / access                                                      |
| -------------------------------------------------------- | ---------------------------------------------------------------------- |
| `/v1/me/pass-profile`                                    | Own profile values, role, version and editable/frozen state.           |
| `/v1/me/pass-profile/history`                            | Own profile changes.                                                   |
| `/v1/passes/events`                                      | Available event IDs and capabilities.                                  |
| `/v1/passes/events/{event}/me`                           | Own registration state and version.                                    |
| `/v1/passes/events/{event}/invitations`                  | Own invitations; optional `after` cursor.                              |
| `/v1/passes/events/{event}/payment-admins`               | Event payment contacts.                                                |
| `/v1/passes/events/{event}/queue`                        | Authorized registration queue; optional `after` cursor.                |
| `/v1/passes/events/{event}/admin/targets/{telegram_id}`  | Booking administrator's exact target and permitted assignment options. |
| `/v1/passes/events/{event}/me/payment-quote`             | Own current payment quote.                                             |
| `/v1/passes/events/{event}/participants/{owner}/payment` | Payment state under participant/reviewer permissions.                  |
| `/v1/passes/events/{event}/payment-queue`                | Authorized pending reviews; optional `after` cursor.                   |

Record actor, update ID, before/after state and version, fixture consumption, and
rendered message/button evidence. Check allowed and denied actors, stale actions,
and replay without duplicate mutation. Exercise EN/RU and mouse/touch where
relevant. Installation, consumed plans or optimistic replies are not acceptance.
Keep credentials and private profile values out of published screenshots/reports.
