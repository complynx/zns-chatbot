# Original order submission operations

Status, 2026-10-22: native preparation to real Loom adoption passed on the
isolated `orders-adopt-r2` fixture, including graceful recovery and original
receipt replay. The [current-image reverse browser gate](shuttle-orders-reverse-browser-qa-2026-10-22.md)
also verifies one original form submission, unexpired stale-draft protection and
completed-link recovery. Still-live original-question settlement, other mixed
windows and real Telegram WebView remain under acceptance.
The owned primary now runs source `74604b7e`, image `0d4457b6`, schema72 /
`mvp-92`, through the separately accepted
[candidate installation](shuttle-profile-name-sync-qa-2026-10-22.md#owned-primary-upgrade-with-ingress-fenced).
Its Telegram ingress remains fenced. This is not a production-readiness claim.

## Contract

The delegated-user endpoint `POST /internal/agent/operations/inspect` now accepts
module `zinusya_orders`, method `submit`, and an original operation UUID. It uses
the existing Loom inspection protocol and returns the original arguments,
description, proposal ID, absolute expiry, identity binding and contract revision.
A completed original submission also returns its immutable receipt. Reading the
operation creates no order and grants no consent.

The agent continues a supported submission with `module_call`, setting
`resume_operation_id` to that UUID and `arguments` to `{}`. Loom obtains the
recorded terms and requests fresh approval. The application still checks current
owner and event/module access before inspection and execution. Meal orders and
cancellation are outside this extension and remain explicit unsupported adoption
methods; their existing ordinary tools and forms remain available.

## Storage and recovery

New agent and native preparations record a private `operation_contract` alongside
the public preparation in the existing immutable prepare receipt. The record is
per owner/operation/phase, because several operation IDs can share a proposal.
The API supplies issuer, actor and revision; the transaction supplies the verified
bot, subject and organization. Original localized descriptions are stored in the
same transaction. Replaying preparation cannot change language, provenance or
expiry. Existing public operation reads omit the private contract field.

No schema migration is needed. Old records without this marker retain their
ordinary recovery paths but cannot be adopted. A replay does not retrofit missing
metadata. Inspection does not renew expired preparations; Loom must reject an
expired pending operation and obtain fresh terms and consent. Completed receipts
remain readable with current access even after later cancellation or draft edits.
A matching order from another operation alone does not establish completion of
this operation: its own submit receipt is required.

Canonical results can have a different calculation observation time. Inspection
uses the existing business-condition comparison, which ignores only that time;
prices, selections, services, capacity terms and sales cutoff must still match.

## Acceptance boundary

The first scoped PostgreSQL/HTTP gate passed the new original-operation paths.
An independent review then found a legitimate canonical receipt rejected solely
because its observation time differed. A focused test reproduced the conflict;
the correction uses `sameOrderConditions`. The broader follow-up passed 68 root
tests with no failures or skips, including order, Mini App, meal and knowledge
inspection paths. After the formatter/lint corrections, 18 focused roots passed
with no failures
or skips. The single-connection test was then strengthened to replace the URL
setting and assert `MaxConns=1`; its independent rerun passed. Scoped API, DB,
task and integration lint reported zero issues. Four independent textual review
passes ended without outstanding substantive findings. Local tests did not use
the race detector; full CI and real Loom acceptance remain separate.

These tests use fixture IAM/HTTP peers. They prove original-ID API handoffs,
replay, immutable terms/language, legacy and binding rejection, current access,
expiry, formatter rollback and single-connection operation. They do not prove a
real Loom approval dialogue, autonomous interpretation or an actual browser opening an
agent's existing preparation. Those are required next acceptance steps. The
native browser now retains its prepare operation UUID at explicit confirmation;
the separate committed UI fix has 28 passing behavior cases.

## Native continuation from an agent preparation

The order approval now includes a separate inline WebApp button before the full
quote and conversational answer keyboard. Its URL is built from the existing
configured Mini App launch base, the checked original event and operation UUID.
There is no new production URL setting and no authentication token in the link.
The URL is transport metadata, not part of the frozen price or consent record.
Only service-order submission in the private user chat receives this button.

The button uses an inline WebApp launch. A reply-keyboard launch cannot supply
the required identity data: Telegram documents that its initialization data is
empty in that mode. See [WebAppInitData](https://core.telegram.org/bots/webapps#webappinitdata).
The ordinary dialogue answer keyboard remains on the final approval message.
Both messages use the existing durable outbox, approval context and presentation
keys. Opening the form is not approval and does not answer the pending dialogue.

After authenticated bootstrap, `#orders?event=UUID&operation=UUID` reads the
original submit receipt first, then the prepare receipt if completion is absent.
It never submits or prepares on open. A completed operation displays its original
receipt while separately loading the current draft/orders. A pending preparation
displays its frozen snapshot and requires an explicit confirmation with the
original operation UUID and proposal. Stale versions or expiry disable that
confirmation; a newer saved draft remains unchanged.

Local pending browser saves or order requests take priority over the link. The
form explains the deferral and offers a read-only recheck. Missing or failed
linked receipts remain unknown: writes are blocked until recovery. A link
recheck also clears the previous event's displayed editor before any awaited
request, so a network error cannot retain quantities under another event's ID.

The initial six UI cases failed against the previous implementation and passed
after the import was wired. A fresh review found the cross-event failure path;
its focused case now passes. The final review found no actionable issue.
All 377 Mini App source behavior cases pass; this includes seven new import
cases. The PostgreSQL/outbox gate passes 12 selected roots, including agent and
protocol reply modes, original link persistence and unchanged consent state.
Scoped task/integration lint reports zero issues. These are fixture HTTP/outbox
and JavaScript VM checks. The subsequent real Loom approval/replay gate is
described below. The separate reverse report adds actual browser evidence;
real Telegram WebView remains open.

The preceding backend commit `6441a2e3` passed
[CI53](https://github.com/complynx/zns-chatbot/actions/runs/39703986240): 883
integration roots across four isolated race-enabled workers, checks and final
coverage verification all succeeded in 11 minutes 45 seconds. This run predates
the native import and button changes described in this section.

## Distinct Loom and backend operation IDs

Source inspection before the runtime gate found a consumer bug in the preceding
candidate. Loom retains its own `operation_id` and separately sends the original
bot `backend_operation_id` when adopting a native operation. Zinusya's approval
DTO omitted the latter field and the complete-order prompt looked up the Loom ID
as though it were the bot receipt ID. The initial fixture covered fresh approvals
where those IDs were equal, so it missed this path.

The fixture now uses distinct IDs and reproduces the missing approval in both
agent and protocol reply modes. The correction retains both fields and selects
the nonempty backend ID only for receipt lookup/native continuation. When the
field is absent, the previous operation-ID path remains valid. Loom's operation
and approval IDs are unchanged for dialogue resolution. The initial failed test
is retained. This is a Zinusya consumer correction, not a Loom defect.

The bafe7833 image was built but never installed or used for live runtime QA.
The runtime gate must use a newly built image containing this correction.

The corrected source passes the focused broader gate: 60 PostgreSQL integration
roots, two task-unit roots and one Loom transport root, with no failures or skips. Scoped lint reports
zero issues and independent textual review found no actionable findings. The
initial expanded run failed after its own PostgreSQL fixture exceeded 192 MiB and
entered recovery; it was retained, archived and removed. A fresh 512 MiB fixture
with 32 MiB shared buffers passed the same selection. That infrastructure failure
is not counted as a successful run. The subsequent native-to-Loom runtime gate
is accepted below; the separate reverse report records browser continuation and
its remaining limits.

## Native preparation to actual Loom: accepted isolated gate

Source `d21d657c`, image `daf18c4f`, passed this direction on the owned
`orders-adopt-r2` stand with real IAM and Loom and a controlled model provider.
That source is an ancestor of the current source. This is acceptance of that
recorded candidate, not a new browser or current-image runtime claim.

The native API saved a selection and prepared submission. Loom adopted the
original backend operation with a distinct Loom operation ID, retained its exact
arguments, description, proposal and expiry, and presented the full localized
terms with the inline original-operation WebApp link. A graceful bot stop took
0.766 seconds. The same approval and run resumed; plain-text consent created one
canonical order and cleared its consumed draft. Native replay of the original
operation returned the identical historical receipt. No payment evidence was
created. The Telegram peer and provider replies were synthetic; no browser was
opened in this gate.

The first harness attempt expected an origin without the configured `/app/`
path and failed its URL assertion after preparation. A separately reviewed
continuation checked the actual original link, retained the same approval and
did not resend the original message or model response. The failed observation
remains in `resume-attempt.json`; the completed proof is
`.cache/shuttle-work/orders-adoption-runtime-20261022-r2/forward/checkpoint.json`
with phase `forward_graceful_gate_passed`. Do not replay either driver: its
completed order remains in the stopped fixture.
