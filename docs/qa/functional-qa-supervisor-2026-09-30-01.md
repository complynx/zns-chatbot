# Independent Functional Senior QA — Docker supervisor recovery

Date: 2026-09-30. Reviewer: fresh Codex functional QA agent.

Verdict: **PASS for the assigned manual Docker-supervisor slice**, with the limits below. No blocking defect found in this slice. This is not full migration, real-agent, payment, systemd, or host-reboot acceptance.

## Independence and scope

Used the live Telegram-like stand at `http://127.0.0.1:58118/` through `cua_repl`, real rendered messages and callback buttons, read-only synthetic PostgreSQL observations, and raw lifecycle/log captures. Did not read product source, prior reviews, progress, developer findings, or tracking records. No build, reset, fixture mutation, arbitrary process kill, production access, or direct domain writes. All business changes came from current UI commands/buttons. Root acted as read-only log observer during this run.

Synthetic actors: Alice101 and Visitor303 exercised; Bob202 appeared as Alice's assigned payment contact but was not exercised as an administrator. Database: `synthetic-qa-zns-aud23-prerequisites-postgres-1`, database `zns`.

## Observed flows

1. Alice RU `/start` showed service buttons. Clicking massage edited the existing message to Draft/version1 with confirmation controls. This verified ordinary callbacks and current-card editing before the fault.
2. Public `/passes` showed event selection. Selected `Тестовый фестиваль`, then `Лидер`. The registration card changed from missing role to `Зарегистрироваться соло`. No direct setup writes were needed. `/menu` and `/help` produced no response; these were not established supported commands and are not reported as regressions.
3. Before final registration, SQL showed fault sequence `last_value=1,is_called=false`, zero pass bookings, and zero registration intents.
4. Clicked `Зарегистрироваться соло` exactly once. The installed one-shot SQL-origin completion fault fired. No user resend, service restart, page reload, or manual intervention was required to recover.
5. Alice's same registration card automatically became `Сохранено`, `Статус: Выделен`, price150, payment contact Борис. It exposed payment/profile/invitations/cancel/current-menu buttons. One registration-saved notice and one pass-assigned notice appeared; these are distinct notifications, not duplicate messages.
6. `/language en` changed the current service/profile/registration cards to English. `Registration menu` retained Assigned150. Switching users and reloading the page preserved Alice's successful registration and history. Old RU notification messages remained historical RU messages while the current actionable card was English.
7. Visitor's `/start` exposed only Visitor's conversation. Selecting massage produced RU `Действие не выполнено: forbidden. Показано актуальное состояние.` with Not selected/version0. `/language en` translated the current card to `Action failed: forbidden. The current state is shown.` A further English callback exercised the same refusal path. No Alice booking or history was exposed in Visitor's chat. Managed runtime identities were unchanged after the negative flow.

## Fault and automatic replacement proof

Raw evidence directory: `qa.local/aud23-supervisor-20260930/`.

- `fqa-postgres-fault-1.log`: PostgreSQL raised `synthetic native completion fault` at **06:30:33.815 UTC** while updating native completion. The supplied stand contract identifies this one-shot fault as SQLSTATE08006.
- `fqa-app-before-fault-1.log`: correlation `fb2b112f-a077-4d96-a9fd-57eca4dd5f2e`, attempt1, started06:30:33.776, error06:30:33.816, executable stopped06:30:33.817.
- `fqa-runtime-events-1.jsonl`: the original app died with **exit1** first; all five peers then died and all six original containers were destroyed. Owner coordinator then died with **exit1**, Docker started that owner again, and a new six-component group was created and started. This establishes executable failure before coordinated retirement, not merely an HTTP health result.
- `fqa-app-after-fault-1.log`: new executable started06:30:43.951; the **same correlation**, attempt2, started06:30:44.145 and completed successfully06:30:44.248. UI history identifies this as **update11**. Approximately10.5 seconds separated failed completion from successful same-operation completion.
- Independently queried Docker inspect after the flows: owner `8f5be32a8514` has restart count1 and policy `on-failure`; each managed component has restart count0 and policy `no`.

| Component | Before ID prefix | After ID prefix |
| --- | --- | --- |
| app | 4bb0d64e292e | 815d72db01f9 |
| evaluator | 7ef72dfdebf1 | 5605212480ad |
| media-decoder | 04b05c21bb38 | 1cbe4f4101e4 |
| sticker-decoder | 49ea0cfb0b91 | 8ea1ac380f12 |
| media-broker | 265f4692aec6 | ae1b6eb2f0cc |
| sticker-broker | 38b8a9065a57 | 0aad4a54bd16 |

Pre-fault references: `pre-fault-runtime-1.log`, `pre-fault-ledger-1.json`. Post-fault references: `fqa-post-fault-runtime-1.log`; direct reviewer observations and final database evidence: `functional-observation-01.txt`. The reviewer inspected raw events; a partially written final JSONL line during live capture was not used as evidence.

## Durable input and duplicate-effect proof

Independent read-only SQL after recovery and again after menu/language/permission flows showed:

- Fault sequence2/true: failed attempt plus recovered completion.
- Exactly one native completion ingress: id35, request_key11, event `sandbox-festival`, admitted, native_intent_id1.
- Exactly one Alice pass booking: assigned, leader, solo, version1.
- Exactly one registered intent and one `registration_intent_requests` record for intent1.
- Exactly one pass-booking operation receipt for Alice/event; key hash `7755bd22a7c2b0c9ec38f27d4def3c810dfcf497c5b57562fef46d948dda3e29`.
- Exactly one sent `registered` notification and one sent `assigned` notification; UI agreed.
- Zero payment receipts, as no payment proof was submitted. This does not establish deduplication of an actual payment submission.

The application replay and preserved native request identity establish recovery of the same operation. The test did not create a second registration command to imitate a retry.

## Limits

- Manual-only stand. No real agent/provider, agent intent interpretation, provider failures, or model acceptance claimed.
- Docker owner restart tested. No systemd, Windows service, host reboot, Docker-daemon restart, cross-host failover, or network-partition acceptance claimed.
- Runtime group retirement and replacement proven from lifecycle events. Historical zero database sessions at the exact retirement boundary was not sampled and is not claimed.
- Ordinary EN/RU current-menu, state, and refusal behavior tested. The SQL fault was exercised once in RU; English registration creation was not separately faulted.
- No uploaded payment proof, admin decision, pair/waitlist concurrency, media, callback forgery, broad authorization matrix, or complete product regression suite exercised. Screenshot observations are in the browser tool transcript; no standalone screenshot file is attached.
- Automatic same-input recovery tested once with the supplied deterministic SQL-origin fault. General recovery from all database failures is outside this proof.

Written by functional_qa_supervisor_01 (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
