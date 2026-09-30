# Scoped independent Functional QA: combined result and next stand plan

Epoch: `ce427ca56ff9f127eba55321047c64a5e9204683`.
Verdict: the exercised manual/UI subsets have independent evidence. Full C,
D, E and migration acceptance remain OPEN. No whole planned scenario is
converted to PASS by this batch, and no executed product defect was established.

## Independent evidence

- A: `docs/qa/fqa-flows-20261001-execution.md`, evidence in
  `qa.local/fqa-flows-reviewer/`.
- B: `docs/qa/fqa-recovery-import-2026-10-01.md`, evidence in
  `qa.local/fqa-recovery-import-reviewer/`.
- Lead readiness/provenance: `docs/qa/fqa-lead-2026-09-30-readiness.md`.
- Source-blind public handoff: `docs/qa/fqa-lead-2026-10-01-public-handoff.md`.

Both reviewers received original requirements, scope and public access only.
Neither inspected product source, implementation diffs, engineering findings,
prior review conclusions or the other reviewer's evidence. Both used actual
ephemeral headless Edge UI interactions through existing Playwright, including
screenshots and browser downloads. HTTP/control observations supplemented UI.
Lead preparation checks are not independent acceptance evidence.

## Observed bounded coverage

Both independently exercised EN/RU manual workflow selection and confirmation,
existing-card edits, synthetic user isolation, document uploads and exact-byte
browser downloads. A additionally exercised Mini App calculate/save/reopen,
current-state stale version denial, per-user file denial and manual command
escape from a pending profile hint. Each observed one successful deterministic
profile proposal with public accepted/rejected readback and visible profile UI.
This does not establish natural reasoning, question-language behavior, agent
registration/knowledge parity or real identity/provider acceptance.

B additionally armed one legitimate exact `before_apply` edit on its own stand,
observed the held state with the prior visible card unchanged, released once,
and observed terminal completion plus the new visible card. No crash, replay,
restart, uncertain-send or durable-delivery conclusion follows from that case.

Callback admission and card updates were observed. Actual Telegram callback
answer evidence is unavailable in the public stand: no acknowledgement ledger
or receipt readback. This is an observability gap, not an inferred acknowledgement
PASS or a demonstrated product failure.

## Original 40-row scenario accounting

| Original scope | Rows | Current disposition |
| --- | --- | --- |
| A F01–F13 | 13 | Exercised subsets documented; every row retains blocked requirements |
| B R01 | 1 | PARTIAL: supported UI/fixture route only |
| B R02–R18 | 17 | BLOCKED: required runtime/delivery/registration/collector controls absent |
| B I01–I09 | 9 | BLOCKED: import stand and current import rehearsal not handed off |

These are 40 original rows, not 40 PASS results. Lower-level supported checks are
evidence within them. Actual failure versus unavailable capability remains
separate. Full C/D/E and Python parity are not accepted.

## Resource release

A explicitly released flows and closed all browser/process work. B explicitly
released recovery and left no held provider request or browser/process work.
Lead now owns both released stands. Images/configuration/state are preserved;
no reset/rebuild/reseed/restart has been performed by lead. Their actual resources:

- Flows: own `synthetic-qa-zns-fqa-flows-{config,state,pgdata}` volumes,
  prerequisites/runtime/owner Compose namespaces; UI 58403, control 58404.
- Recovery: own `synthetic-qa-zns-fqa-recovery-{config,state,pgdata}` volumes,
  prerequisites/runtime/owner namespaces; UI 58413, control 58414.
- App/fake binding:
  `synthetic-qa-zns-fqa-app@sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`.
- Actual managed `Config.Image`, ImageID and RepoDigests matched the prepared
  binding manifest. All six components per stand plus coordinator were stable.
- App/PG remain internal-network resources. Nominal app/PG host ports are not
  usable access paths. Root-owned PostgreSQL 55432 was not used.
- Import namespace remains unstarted/unprepared.

Flows left Alice's transfer booking v3, festival order unpaid v3/100 BYN,
RU locale, private marker file and pending profile hint. Recovery left its
ordinary workflow draft v8, deterministic EN profile card, document evidence,
and terminal completed provider case. Do not silently reset these evidence
resources or relabel them as a new epoch.

## Genuine scenario preparation versus fake surface work

Primary preparation classification covers each original row once. It is a
work-allocation grouping, not a pass count; capabilities overlap across groups.

| Primary preparation | Rows | What is actually needed |
| --- | --- | --- |
| C authoritative product scenarios | 11: F01,F02,F05–F13 | Real product users/events/roles/consent/knowledge/private context/registration intents and queues; normal supported operations and sanitized public state evidence |
| C locale/provider scenarios | 2: F03,F04 | Existing chosen locale, actual catalog fallback controls and model-question language evidence; real provider final gate remains separate |
| D runtime/system scenarios | 9: R02–R07,R15,R16,R18 | Actual durable intake, process/helper/session ownership, supervisor/replacement, ingress rank/expiry and resource measurements |
| D delivery scenarios | 7: R08–R14 | Genuine queued domain work plus isolated fake 429/denial/lost-response/credential outcomes and wire times |
| D collector scenario | 1: R17 | Actual configured collector/export path and an isolated failure target |
| E imported-state scenarios | 9: I01–I09 | Current reviewed importer/schema, verified complete synthetic source/receipt inputs, writer-stop apply/replay/reconcile/removal and importer-free runtime |
| Telegram surface scenario | 1: R01 | Real UI messages/keys/files plus actual callback-answer observation; supported subset already exercised |

Thus 37 rows primarily need genuine product/system/import preparation, two need
locale/provider preparation and one centers on the Telegram surface. Merely
extending fake UI output does not close any whole C/D/E acceptance row. External
fault controls must change the external response, not fabricate successful
product transactions or edit product receipts/queue state.

## Prioritized next batch

1. Add bounded public observation of real `answerCallbackQuery` endpoint calls
   and callback identity/response status. No fabricated acknowledgement. Give
   it a separate developer branch, focused gates and fresh Code QA; freeze a
   reviewed next image only after released-stand allocation. Preserve redaction.
2. C: materialize a minimum authoritative scenario pack using normal supported
   product contracts: registration event/sales/capacity/pair/role/tier fixtures,
   ordinary/event-payment/global-admin-without-payment-role users, event A/B,
   live ACL/consent changes, knowledge/private contexts and deletion. Expose
   sanitized principal/tool discovery and draft/ingress/rank/deadline evidence.
   Document deterministic delayed/interrupted/budget fixtures and only real
   supported transitions. Missing product capability needs a product developer;
   the stand must not implement substitute business logic.
3. C/D combined: opening burst, first specific-event initiation/repeat/expiry,
   reversed completion and announcement head ordering need one genuine product
   fixture pack and durable readbacks. Generic booking-card versions cannot
   substitute for registration intention/announcement order.
4. D: publish and preflight exact owned lifecycle/session/SQL fault windows and
   observations before execution. Cover accepted batch durability, process
   crashes, lock-session loss, transient DB exit/supervisor backoff, old helper
   completion before replacement, and actual wire timing. Isolated Telegram
   fault scripts must support repeated/ambiguous/invalid 429, recipient denial,
   possible-send response loss, credential-wide failures and independent lanes.
   Current single-use edit hold is a control reuse point, not D proof.
   Explicitly retain the consumed model-fixture before durable plan-save case and
   fixture provider reboot: current turn consumption is not replayable. The next
   stand must expose a genuine documented boundary and outcome; do not omit it,
   fabricate recovered fixture turns, or infer durable recovery from profile success.
5. D: hand off CPU-only process/resource evidence and a real local collector
   failure lane. Bound/redact diagnostics; failure must permit business work.
6. E: obtain exact reviewed current CLI/schema export. Rebind old materializer
   source guards through reviewed changes; preserved complete 17-source synthetic
   data and receipt bytes are inputs, not current acceptance. Regenerate seven
   actual plans/resolutions and validate counts/projections independently.
   Import requires E-specific composition, stopped writers through apply/replay
   and reconciliation, archived original evidence, temporary-receipt removal,
   and actual importer-free new build/digest/UI/restart. Do not start an old CLI
   against a newer schema or overwrite current target state.

Lead coordinates allocation/freeze/release; engineer owns next assigned preparation
writes; product developers own missing genuine capabilities. Reviewer reports
stay independent. Substantive product/observability changes require fresh affected
independent reviewers, not a continuation that already saw earlier findings.
Next integration merges do not change this immutable ce427 acceptance epoch.
