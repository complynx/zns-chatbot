# Functional QA flows plan

Status: PLAN ONLY. No runtime observations, failures, or PASS verdicts yet.

Reviewer ownership: this report and `qa.local/fqa-flows-reviewer/` evidence only. Product edits, Git mutations, production access, and external messages are excluded. This plan uses only the supplied requirements.

## Handoff prerequisites

- Lead identifies the immutable reviewed commit and image digest, acceptance scope, and stand freeze owner.
- Stand `synthetic-qa-zns-fqa-flows` is ready and isolated. Supplied port allocation is PostgreSQL 58401, app 58402, fixture provider 58403, control 58404 on 127.0.0.1. These are allocations, not verified endpoints.
- Lead supplies the Telegram-like UI URL, synthetic account credentials, installation/config identity, fixture-control documentation, evidence access, and cleanup/reset rules. No production credentials or copied private production material.
- UI supports visible messages, reply keyboards/buttons, callbacks and their acknowledgements, message edits, uploads/downloads, and WebApp flows where the accepted scope uses them. It exposes stable message/card identifiers and a way to switch synthetic users without sharing sessions.
- Engineering supplies a seeded scenario manifest describing expected policy/configuration, synthetic event capacities and constraints, and expected results. No source details or prior findings.
- Fixture controls can change live roles, consent, knowledge access, knowledge content/deletion, locale, event sales-open state, capacity, draft expiry, and time. Changes must take effect through the application's normal authorization and persistence behavior; controls must not fabricate a successful result.
- Deterministic provider fixtures can offer a tool call, delay a call, fail or interrupt a call, return an invalid/unauthorized proposed binding, exhaust the configured work budget, and record sanitized invocation metadata. Controls document exactly which intent responses are fixtures.
- An ingress control can deliver/replay a message with a stable original ingress identifier and timestamp, hold/release competing deliveries, and inspect sanitized registration rank/deadline/draft snapshots. It must distinguish original ingress from delivery/replay time.
- Durable-state evidence is available through authorized UI/admin/control readbacks. If process interruption requires a restart, lead defines a controlled outage exercise outside the no-restart QA freeze, then hands back the same immutable build for continuation checks.
- Provider budget values and interruption behavior are documented. Fixture time advance permits expiry tests without waiting ten minutes. The actual default expiry is verifiable from black-box configuration evidence.
- No rebuild/restart during frozen QA. Missing capabilities block the affected scenario; reviewer requests an upgrade and releases the affected stand before engineering changes.

## Evidence and verdict rules

For each run record scenario ID, account, locale, ingress/time, fixture mode, visible actions/messages, callback acknowledgements, expected result, actual result, and evidence path. Keep synthetic user material labeled, and use a unique private marker per user/context to detect leakage.

Classify separately: observed (executed facts), failed (executed requirement mismatch), blocked (missing stand/control), and untested (not executed). A fixture pass covers orchestration/policy behavior only. Natural understanding and actual language switching require a separate real-provider final acceptance.

## Bounded scenarios

| ID | Scenario and action | Required observation |
| --- | --- | --- |
| F01 | For EN and RU, begin a specific-event registration through visible manual navigation; switch to an agent question that changes registration intent; continue manually through buttons and submit or cancel. | Draft and current selection are coherent across modes; final constraints hold; visible UI/history matches durable state. Callback acknowledgement and edited cards are observable. |
| F02 | During a pending registration form, send an unrelated question, an explicit cancel/change-event request, and supported media in separate cases. | Each message is interpreted as current intent. A pending form does not consume unrelated text/media as its field. Returning to registration preserves the valid draft. Supported upload/download/WebApp paths behave visibly. |
| F03 | Set stored UI locale to EN, ask an agent question in RU, then reverse. Change UI locale while an old card is visible and navigate/back or invoke its callback. | Stored UI locale persists across new UI session. New/refreshed cards and buttons use the stored UI locale. Agent response language follows the current question independently. No mixed stale labels after a required refresh. Fixture-only language claims remain limited. |
| F04 | Select be, by, uk, ua, and pl; use controlled catalog gaps at primary and fallback locales. | be/by and uk/ua fall through ru then en; pl falls through en. Visible labels resolve without raw keys or incorrect fallback ordering. Missing-all-locales behavior is recorded against the documented contract. |
| F05 | User A creates/reads a uniquely marked private memory or authorized private knowledge context; user B asks directly and through an agent-proposed lookup. | B sees no private marker/content or unauthorized context metadata. Model-proposed user identity/binding cannot change the authenticated principal. A's authorized access remains functional. |
| F06 | Render an authorized knowledge card and queue a delayed knowledge tool proposal. Revoke the role or consent before releasing the call; invoke old buttons and old cards. Cover ordinary user, event-specific payment admin, global admin lacking that payment role, and event-A versus event-B. | Current ACL determines discovery visibility and call authorization. Delayed/old callbacks cannot read or mutate revoked material. Inspect sanitized provider input/discovery names when public controls expose them. Denial is visible and acknowledged; private content is absent from resulting UI/history. |
| F07 | Create/update knowledge, open history/cards, then update or delete it and revoke its context. Ask through manual and agent paths; navigate old history/cards. | Fresh reads reflect current authorized content. Deleted/revoked content does not leak through stale cards, agent retrieval, or another user. Historical-message handling is assessed against the supplied retention/visibility contract. |
| F08 | Exercise bounded model/script work, delayed calls, provider failure, and user cancellation/interruption during registration/knowledge operations. | Work terminates within configured limits. No unauthorized or duplicate mutation; visible recoverable outcome; subsequent intent works. Durable completed state and unfinished draft stay coherent after interruption/replay. |
| F09 | Generic navigation precedes specific-event sales-open check. Initiate twice, switch manual/agent modes, and replay the original ingress after a competitor arrives. | Rank begins at the first specific-event sales-open check. Generic navigation earns no rank. Repeated initiation preserves rank/deadline. Replay retains original ingress priority rather than replay delivery time. |
| F10 | Begin competing registrations with controlled original ingress order and reversed delivery/replay order; include pre-opening then sales-open checks. | Initiation begins at the first specific-event request reaching the sales-open check. Before opening no pass is acquired; retained queued intents preserve original initiation order, including repeats around opening. Generic navigation never creates rank. Competing users have isolated drafts. |
| F11 | Verify configured default expiry is ten minutes. Advance just before and through expiry with a competing unfinished registration; continue the expired draft. Repeat with an explicitly configured alternative expiry. | Before expiry rank/deadline persist. Expired unfinished intent moves to the tail while preserving draft. Repeated initiation does not silently refresh the original deadline. Configured alternative behaves as documented. |
| F12 | Execute seeded capacity/pair/role/tier edge cases through manual and agent paths, including competing last-place attempts. | Existing limits hold and outcomes persist without overbooking, invalid pairing, wrong role/tier, duplicate completion, or cross-user state. Expected exact rules must be supplied before execution. |
| F13 | Complete/cancel one registration and change knowledge, then start a new UI session. Execute an approved outage continuation exercise if supplied. | Committed state and authorized history survive session changes; cancellation is durable; unfinished state resumes as documented. Process-outage durability stays blocked until a controlled freeze-compatible exercise exists. |

## Execution boundaries

Run F01–F08 first for Stage C functional evidence, then priority/constraint scenarios F09–F13 where their stage is included in the handed-off scope. Do not declare the full migration accepted from this bounded slice. The lead must map remaining Stage D/E and Python parity requirements to independent acceptance ownership.

Execute locale and callback checks through the Telegram-like UI; HTTP/control readbacks supplement UI evidence. Keep one reviewer writer on this stand. Batch nonconflicting cases only within the assigned synthetic accounts and reset contract.

## Current result

- Observed: supplied acceptance requirements and assigned stand allocation only.
- Failed: none established.
- Blocked: execution awaits frozen build/stand handoff and documented capabilities above.
- Untested: F01–F13, including all real-provider behavior.
