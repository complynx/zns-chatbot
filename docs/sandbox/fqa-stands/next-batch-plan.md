# Next-batch Functional QA stand proposal

Planning base: 00ad10e3a513bd98856157c849418f4e4483b95e (schema through089).

Current flows/recovery ce427 evidence and state remain preserved.
Both reviewers have released their scenario ownership to FQA lead. Root approved only the three ACK source paths below. Other sections remain
planning; no seed, configuration, image or resource mutation is authorized.
Root chooses a new Git task owner and reviewed build epoch before implementation.

## 1. Actual callback acknowledgement observation

Scope proposal: existing sandbox/fake.go endpoint routing; new
sandbox/callback_receipts.go and callback_receipts_internal_test.go.
No app.js edit is approved. Evidence is explicitly current-provider-process only,
not durable across provider restart; no persistence change is in this task. No bot/business source changes and no synthetic ack injection.

Provide a bounded read-only GET /lab/callback-receipts?user=<synthetic user>
with X-Sandbox:1. Capture only actual authenticated fake Bot API
answerCallbackQuery calls and the actual generated response outcome. Bind the
callback_query_id to the real callback delivered by that stand when available.
A card edit, ingress cursor, update completion or UI click never creates a receipt.

Return bounded sequence, callback ID, matched synthetic user/message ID, actual
HTTP/protocol result and time. No callback data, message body, token or provider
secret in receipts. Scope by user and cap retained events; report truncation rather
than implying completeness. Preserve ordinary fake behavior outside observation.
Do not invent acknowledgement expiry or Telegram policy. Current observation is explicitly process-local; it never claims durability or
client receipt. Provider restart loses these observations.

Acceptance: a real UI click causes an actual endpoint call and one correlated
receipt; editing a card alone yields no acknowledgement; malformed/mismatched
requests do not produce a falsely matched successful receipt; users do not see
another user's records; EN/RU UI displays acknowledgement separately from card
mutation. Native focused tests, lint/format and independent Code QA precede image
build. Functional QA verifies actual click/response/readback on the next epoch.

## 2. External fake outcome controls

Existing current public controls, not new claimed capabilities:
- POST /lab/fault with X-Sandbox:1, {"mode":"none"|"transient"|"edit_missing"}.
  transient returns a one-shot actual429 from a matching ordinary message mutation;
  edit_missing removes the matching fake card and returns actual400 on an edit.
- POST /lab/blocked controls the selected fake destination's blocked state.
- Exact edit delay control supports before_apply/after_apply_loss and release.

Publish these actual scopes explicitly. A global one-shot mutation fault can be
consumed by unrelated work; tests must isolate the case or add a bounded exact
method/chat/message selector in a separate reviewed task. Do not claim current
controls already cover actual retry_after, forced500, connection refusal, SQL
failure, arbitrary model delay, or callback-ack refusal.

If required by a selected case, add only that bounded outcome selector and its
actual provider response/readback. Use one-shot scope, reject unknown modes,
record consumed/unconsumed state, and preserve positive controls. External fake
outcome controls may change provider behavior; they must not edit product receipts,
attempt state, permissions, queue position, source witness or business success.

## 3. Product scenario fixtures

Separate from external fake faults. Engineering prepares a reviewed scenario
manifest through supported domain setup/mutations, with independently expected
public projections. Required areas:
- Live EN/RU events: sales opening/deadline, capacities and pair/solo policy;
  distinct eventA/eventB and competitors with owned original ingress.
- Live ordinary/global/event-payment-admin roles, including grant/revoke for
  the exact user/event and immediate current-rights behavior.
- Knowledge/private context, source consent, update/delete and marked private
  owner content; ownership and grants stay real domain state.
- Manual/agent registration intent, invitations, cancellation, current draft,
  queue rank/deadline and completed operation replay, with canonical public IDs.
- Deterministic agent plans and public accepted/rejected readback, explicit
  selected-app-locale precondition. Natural-model understanding remains separate.

Prefer existing domain/API fixtures or normal UI setup. A new operator fixture
must invoke real application validation/transactions and declare data ownership;
never fabricate business result rows or bypass permissions. Controlled clock,
original ingress replay, model cancellation/delay and safe provider-input readback
need dedicated bounded implementations if missing; label them unavailable until
built and verified. No generic all-powerful fixture administration framework.

## 4. Lifecycle and SQL fault windows

Document exact owned coordinator and six-component IDs, generation labels,
owned state volume and named database sessions. Before interruption record
business state and image epoch. The supported coordinator alone starts/replaces
managed components. Observe actual old container and writer-session retirement
before claiming the replacement boundary. Keep provider alive for delayed-edit
survival; remote completion never follows from local process death alone.

SQL contention recipes must use one owned lock transaction and independently
observable lock ownership/blocked queries. Release only that transaction; do not
kill arbitrary sessions, edit queue/receipt SQL, widen deadlines or manually free
uncertain delivery lanes. No undocumented fault operation during active QA.

## 5. Import lane prerequisites

Root selects reviewed current CLI/schema epoch and fresh exact source export.
The preserved older full-source materializer requires a reviewed binding successor;
its inventory must not be weakened to accept changed files. Generate actual current
CLI stage and all seven plans/resolutions from the preserved complete synthetic
export, receipt bytes and resources. Independently review decisions/counts/runtime
projections and record actual hashes. Import-specific composition must avoid
ordinary product-fixture startup unrelated to the export.

Single-writer sequence: migrate isolated import database; prove all product/fake
writers stopped; apply/replay all domains and reconcile before removal; archive
verified importer source/binary and temporary SQL receipts while preserving
original source/receipt bytes; build importer-absent app/coordinator with empty
owned cache; verify actual digest/binary bindings and absence of importer mounts,
DSN and dependencies; launch six managed components; run EN/RU imported UI,
owner/ACL/history/proof/resource checks and controlled restart persistence.
No old stand/image/materialization receipt certifies the new epoch.

## Required provider model-consumption boundary

Keep a separate required scenario where a deterministic model step is consumed
but the application has not durably saved its plan when the caller/process stops.
Then reboot the provider and exercise the documented continuation path. Observe
whether the same original operation can recover its genuine result without a
second model decision, duplicate mutation, cross-user data or fabricated restore.
Current in-memory consumed-turn state does not establish replayability or provider
reboot durability. The scenario remains unavailable/unproven until actual controls
and current product behavior are observed; no fixture reinsertion, guessed saved
plan or reset may substitute for recovery. Any missing durable product capability
belongs to a separate root-assigned developer task, not the fake outcome control.

## C fixture preparation ownership proposal

Separate proposed paths: docs/sandbox/fqa-stands/c-scenarios/manifest.json and
prepare.md, plus tools/fqa-fixtures/prepare_registration.go and focused tests only
if real existing domain operations can be invoked from an external operator tool.
Before code assignment inventory supported typed services/API setup operations.
Do not reimplement validation, rank allocation, grants, deadlines or business
transitions in the fixture. Missing domain APIs/capabilities go to root capability
developers with exact original requirements. Stand/data writer is assigned per
scenario; the manifest names users, locales, events, grant holders, capacities,
expected original ingress/rank/deadline and public readback contracts. Tool/schema
permissions and scenario preconditions require review before stand seeding.

## ACK public contract for the next reviewed epoch

GET /lab/callback-receipts?user=101 requires X-Sandbox:1 and
X-Sandbox-Actor:alice matching the canonical selected synthetic user. Actor is
trusted operator metadata, not authentication. Receipt origin is bound from the
actual callback emitted in a successful getUpdates batch and the actual
answerCallbackQuery body after bot-token routing. Query user and additional body
fields cannot choose a receipt's origin. Unknown or unmatched callback IDs do not
produce matched evidence. Original answerCallbackQuery response behavior stays.

Readback includes scope=current_provider_process_response_generated, user_id,
total_observed, truncated and receipts containing sequence, callback_query_id,
bot_id, user_id, message_id, response_status, result and observed_at. At most64
bindings and64 receipts per each of three known synthetic users are retained;
truncation is explicit and output excludes callback data/text/provider tokens.
The observation proves provider response generation, not browser/application
receipt or actual Telegram server guarantees. A new fake process starts empty.