# Native payment review HTTP adapter

Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

Eight installed session-authenticated routes pass component checks over the
[shared native transaction store](shuttle-miniapp-payment-review-store.md).
The [local launch rollout](shuttle-miniapp-local-launch-qa-2026-10-16.md)
installed these routes on the owned runtime at schema39/mvp-39. The [reviewer form](shuttle-miniapp-payment-review-form.md), payer display and a
ninth direct event-context GET route were installed at the schema39 milestone,
together with native entry and redacted reactive context. The four real-service
happy paths above subsequently passed. A separate [cross-person browser gate](shuttle-cross-person-browser-qa-2026-10-18.md)
now passes on schema49/mvp-56 with mock Telegram and real IAM/Loom/backend,
including native lost-reply recovery; real WebView critical paths remain separate.

## Routes and decisions

All paths below start with /app/payment-reviews/. GET requests require the current
ZinusyaSession authorization; POST also requires the configured Origin and JSON.
The static events/ prefix keeps event routes distinct from operations/ in ServeMux.

| Method | Suffix | Result |
| --- | --- | --- |
| GET | events?after=UUID | Current reviewer-only event choices, at most20 plus next cursor |
| GET | events/{event}/evidence?after=UUID | At most20 authorized evidence/history records and possible next cursor |
| GET | events/{event}/evidence/{evidence} | Evidence, current balance and this actor's latest workspace or null |
| GET | events/{event}/evidence/{evidence}/content | Bounded authorized proof attachment |
| GET | operations/{operation} | Original authorized receipt, from either transport |
| POST | workspaces | Save exact pending review; no money credited |
| POST | cancellations | Cancel a pending workspace; no pass/payment cancellation |
| POST | confirmations | Explicitly confirm the exact review and canonical proposal |

Event discovery includes event ambassadors/administrators and a verified global
zinusya.admin, including historical events with the registrations module enabled.
An ordinary pass owner does not gain reviewer access, even for their own evidence.
Evidence pagination may return a cursor when the page has exactly20 entries and
the following page is empty. Metadata reads can span separate authorized store
reads; write CAS/proposal checks remain authoritative for financial decisions.

### Payer display: component-verified and installed on the owned stand

The detail response now also has `payer: {user_id, full_name}` in the current owned candidate.
The owner is derived from the evidence, never a browser-supplied target user.
The display name uses the chosen bot profile name, then Telegram first/last name,
limited to256 characters; it is empty when neither exists. Clients can show the
UUID in that case. This is a display label, not a verified legal identity. No
passport, private registration fields, Telegram username or IAM identity is added.
Current reviewer authority and session are checked in the lookup transaction and
again before the complete detail is returned. List and write contracts are unchanged.

The focused Linux/race gate passed seven HTTP root tests /12 pass events with
real PostgreSQL and mock peers. It includes chosen/fallback/empty names, minimal
projection, ordinary-owner denial and authority revocation, plus the existing
handoff, replay, logout and proof-access scenarios. The frozen839 inputs and ten
main stand services were unchanged. Lint passed; sqlc compile and byte-stable
regeneration passed. Independent source review found no actionable findings.
An initial QA preparation failed before any resource creation because its result
directory was missing; that harness failure is retained separately. Temporary
containers and the volume were removed after the passing run. This is component
proof from before candidate installation. The current owned schema39 candidate
contains the payer field. Its real-service happy paths now pass with a manual
provider; embedded critical paths and independent model behavior remain open.

A save body has operation_id (UUID), expected_revision and review. Review contains
event_id, evidence_id, expected_version, decision, amount_minor and reason. Numeric
zero must be explicit for the revision/balance version and rejection amount; missing
version/money fields are not silently treated as approval of zero. Amounts are
integer minor units, with frozen currency/scale supplied by the balance/proposal.

A cancellation body has operation_id, workspace_id and revision. Confirmation has
operation_id, canonical_proposal_id, confirmed:true and arguments containing the
workspace_id, revision and full review. Missing consent, changed money/proposal,
stale/cancelled intent and invalid decision are rejected. Unknown JSON fields and
trailing values are rejected; bodies are bounded by the existing64KiB Mini App limit.

Keep the same operation and exact body after an uncertain response. Successful
writes return bare native domain results; operation status returns its receipt.
A prepare/cancel receipt is not a financial success. An unknown operation returns
404, which means unknown, not proof that a financial effect did not occur. A lost
successful confirmation replays the original result and never credits money twice.

## Authorization and proof IO

The browser cannot supply actor, organization, source or administrator flags.
The server obtains the owner's token, verifies its principal and deployment actor,
then rereads the session before constructing scope. Existing account/profile
handlers keep their prior error-only principal contract through a small wrapper.
IAM verification retains the configured positive-cache policy; this is not an
uncached introspection guarantee. Local event authority and session are checked
in store transactions, including known receipt recovery.

Proof download checks the reviewer role/session, resolves the evidence-file
association, performs bounded Telegram IO without DB locks, then invokes IAM
verification again, rereads the association and checks the current event role and
session before returning bytes. The association must still identify the same file.
A logout or local role revocation during source IO blocks disclosure. Existing
20MiB file read limits apply. Responses use attachment, private/no-store, nosniff
and sandbox CSP. Missing/oversized source files fail without returning proof bytes.
General file endpoints remain owner-only; payment review grants no general access.

Native save/cancel/confirm use existing atomic session guards and completed native
call receipts. They do not start an unsolicited agent run. Their redacted action
projection into the next explicit agent turn is component-verified and installed;
full real-service handoff acceptance remains open.
No IAM bearer token or raw Telegram initData is returned by these payment routes.

## Verified scope and retained failures

The final complete Linux Go race gate passed90 top-level tests /121 passing test
events, with real disposable PostgreSQL and mock IAM/Telegram/Loom. This includes
all selected Mini App tests and existing payment, authority and notification tests.
It is not the complete repository test suite. All837 frozen inputs stayed unchanged;
there were no failures, skips or OOM in the final gate. Scoped API/db/integration
lint reports zero issues; sqlc compile and byte-stable regeneration pass.

Six new HTTP roots cover native/agent store handoff and lost replies, exact input
and access checks, cancellation/edit/rejection, verified global role, proof bounds/
owner isolation, and role revocation/logout during proof IO. They include five
proof subcases. The native handoff test stops the worker, closes mock Loom, and
asserts that native actions leave the fixture's existing task count unchanged.
This is HTTP/store acceptance with mocks, not a real agent or Telegram WebView run.

Three failed gates are retained locally. The first exposed a real ServeMux route
ambiguity, fixed by events/{event}/... paths. The second exposed a fixture calling
h.stop, which closes the HTTP server; it now stops only the worker. The third
passed89 roots but failed an incorrect zero-total-task assertion: fixture payment
submission already created a reactive task. The final assertion compares the
pre-native baseline instead. These failures are not relabelled as passing runs.

Five fresh source-only reviews across the complete diff versions returned no
actionable findings. Dynamic tests supplied the route/fixture failures above;
static review is not runtime proof. Each gate's exact PostgreSQL/Go pair and
volume was removed after its terminal result. Main ten container IDs, images,
starts and restart counters stayed unchanged. Nothing was committed or published.

Local evidence: .cache/shuttle-work/payment-native-api-v4-qa/checkpoint.json,
expected-roots.json, retry-source-freeze.json, lint.txt and sqlc-proof.json.
Earlier payment-native-api[-v2/-v3]-qa directories preserve failed logs and cleanup.

## Next integration

The [ru/en reviewer form](shuttle-miniapp-payment-review-form.md) and payer
presentation, native entry and reactive context are component-verified and
installed on the owned stand. Full-image build, isolated schema39 no-op migration
and image-only rollout passed. Native-only, agent-only and both handoffs now pass
in the real Telegram iframe with real IAM/Loom and a manual provider. Embedded
critical paths, UI follow-ups and unattended model behavior remain open.

Daniel identified the existing public localhost redirect for real local Telegram
launches; [session setup](shuttle-miniapp.md) now separates launch URL and request
origin. The owned schema39 rollout passed real Telegram-issued initData and form
save/reload in a top-level tab. After browser permission approval, embedded
opening, authenticated reads, draft writes and logout/re-entry also passed.
That initial launch check did not exercise payment review. The later four-path
QA record above covers its real embedded happy paths.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
