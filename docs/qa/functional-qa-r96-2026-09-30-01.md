# R96 Functional QA 01 — independent sourceblind browser review

Date: 2026-09-30. Reviewer: Codex Functional Senior QA. Status: COMPLETE — scoped Functional QA PASS; evidence and limits below.

Scope: only public access/scenarios/cases packet read; source/images frozen by operator. Browser inputs through own CUA tab at http://127.0.0.1:58132/. Synthetic Bob202, Alice101, Visitor303. Root alone controlled pause/bind/restart/replay/state. Existing preflight content surfaced automatically on first Bob selection; disregarded. Subsequent observations limited to actual #messages DOM; diagnostic action panel is not treated as Telegram history.

## Independent browser observations

All five ordinary scenario types exercised in EN and RU through exact public packet texts. Trusted target used actual contact form with Contact101. Language set with /language and actual en/ru buttons. Each message coordinated with operator pause, binding of actual input, resume.

| Case | Actual update (operator attribution) | Browser result |
|---|---:|---|
| EN queue |43|Invitation committed; other script steps not confirmed. Event-specific saved notification. Alice invited once.|
| EN explicit |46|Same truthful operation receipt; Alice invited once.|
| EN trusted |49|Invitation recorded; partner acceptance still pending. Actual Contact101 visible.|
| EN invented |52|No invitation was confirmed; no event saved notification.|
| EN cross-event |53|No invitation was confirmed; no target success notification.|
| RU queue |63|Приглашение — сохранено; Остальные шаги сценария не подтверждены. Alice invitation once.|
| RU explicit |66|Same localized truthful receipt, Alice invitation once.|
| RU trusted |69|Приглашение сохранено; ответ партнёра ещё ожидается. Actual Contact101 visible.|
| RU invented |72|Приглашение не подтверждено.|
| RU cross-event |73|Приглашение не подтверждено.|

Receipt is an edit of an existing earlier assistant card, not a fresh last message. It has no event label. Root separately attributed accepted edit to actual case. No claim of complete multi-step request or partner acceptance observed. Event-specific saved notifications identify event. Manual /passes, pagination and event buttons displayed r96b-en-explicit Status: Waiting for partner and r96b-ru-explicit Статус: Ожидается партнёр, with profile/invitation/queue controls. No accept/cancel/payment action taken.

Visitor actual chat was empty after EN positive and control cases; Bob and Alice showed only their respective chats. No private queue body/internal receipt ID observed in actual messages. Raw synthetic diagnostics are outside this statement.

Exact original replay/restart of43,46,49,63,66,69: browser reloaded and Bob reselected; complete rendered #messages text matched saved before-replay text exactly. Screenshots and text evidence saved. No duplicate UI message observed.

## Operator-provided facts (not independent DB inspection)

Root reported each original replay payload equal, whole business/winner/admission state and Messages/Edits/Next/Updates unchanged, model counters equal. Each app restart added exactly three startup menu audit calls with unchanged menu content. Positive invitation count one, waiting-for-couple status with target101 and no accepted partner. EN invented52/RU invented72 zero canonical operations and unchanged cancelledv1 invite0. Cross-event53/73 zero operations in source/target and zero target bookings; provider accepted2/rejected0 for controls. Tests use programmable provider; natural model reasoning is not established.

## Evidence

Owned directory: qa.local/r96-functional-20260930/functional-01/. Prefixes en/ru-{queue,explicit,trusted,invented,cross-event}; before/after replay .txt and screenshots; EN/RU manual pass-card screenshots. Initial baseline includes preexisting synthetic content and is not current-case acceptance.

## Outstanding

Resolved below: all four controlled delayed-rights cases, all10 exact replay/restarts and final isolation checks completed.

## EN queue-administration revocation — update87

Operator observed real boundary16:36:21UTC: invitation committed once; earlier Bob registration notification595 lane46 pending after real429, attempt1/message0, retry16:38:10.611399UTC; later receipt card:40 lane47 pending attempt0/message0. Guarded queue-admin-only revocation16:36:43.647024UTC; no clock/queue adjustment. These are operator facts.

Browser before release retained preceding refusal text. After natural release, actual Bob assistant card showed generic unavailable/invalid-action fallback, no committed receipt; registration-saved event notification delivered. Privileged queue/assign/allocation buttons disappeared from existing pass card. Alice invitation once; Visitor chat empty. Root reported privileged receipt cancelled attempt0/message0, distinct generic workflow card sent, invitation remains1. Root separately called signed public queue API and observed403 forbidden16:40:14.8573943UTC; browser had no exposed queue button left to invoke.

Exact original87 replay/restart while role absent: entire Bob message text unchanged after reload/reselect, no privileged receipt reappeared. Root strict business/transport/model comparison passed with only three startup menu calls. Original role restored by guarded operator transaction16:40:31.184189UTC. Evidence en-queue-revoke-*.

## EN explicit authority retained — update93

Operator proved committed invitation, earlier real429 registration notice737 lane53/message0 and later receipt card:48 lane54/attempt0/message0. Queue-admin-only withdrawal16:44:07.958849UTC preceded real retry16:45:43.424702UTC. Before-release browser already showed receipt text from prior/restored card; this is expressly not proof of new delivery. Root attributed exact update93 receipt edit to message22 at16:45:45.246802UTC, attempt1. Browser after release showed event-specific saved notification, one Alice invitation, truthful committed/other-steps-unconfirmed receipt. No partner acceptance claim. Exact93 replay/restart while role absent left Bob #messages byte-identical; root strict business/transport/model comparison passed. Original role restored16:47:48.533937UTC. Evidence en-explicit-retained-*.

## RU queue-administration revocation — update101

Operator proved committed invitation, earlier Bob notice852 lane63 after real429 and later privileged card61 lane64 attempt0/message0. Guarded grant withdrawal16:50:33.718734UTC before real retry16:52:10.871035UTC. After natural release browser showed localized generic fallback (Агент недоступен или предложил недопустимое действие. Продолжайте кнопками.), removed queue/assign/allocation controls, event-specific registration-saved notification, one Alice invitation and empty Visitor chat. No private queue body appeared in actual chats. Operator reported card61 cancelled without attempt, generic card63 edit22 sent, one canonical invitation; signed public queue GET403 at16:51:03.725UTC. Exact101 replay/restart with role absent produced byte-identical Bob chat after reload; root strict business/transport/model comparison passed. Original role restored16:54:23.965897UTC. Evidence ru-queue-revoke-*.

## RU explicit authority retained — update106

Operator proved one committed invitation, earlier Bob notice951 lane70 after real429 and later receipt card69 lane71 attempt0/message0; queue-admin-only withdrawal16:56:25.961734UTC preceded natural retry16:58:02.241908UTC. Exact card69 accepted edit22 at16:58:04.031775UTC with role absent (operator attribution). Browser after release showed localized committed receipt and other-steps caution, event saved notification, one Alice invitation, empty Visitor chat. Exact106 replay/restart with role absent produced byte-identical Bob chat; root full business/transport/model comparison passed. Original role restored16:59:39.197743UTC. Evidence ru-explicit-retained-*.

## Final manual invitation controls and reconciliation

Alice /passes, real pagination, r96b-ru-explicit-retained event and Приглашения button displayed exactly one incoming invitation from202 and Принять · 202 / Отклонить · 202. Actual language button switched the same card to Accept · 202 / Decline · 202. Neither was invoked. Alice locale restored to ru; final Visitor chat remained empty. Screenshots final-alice-invitations-ru.png and final-alice-invitations-en.png. Bob EN/RU pending states and Alice incoming invitation prove pending acceptance rather than completed couple registration.

Root final reconciliation17:00:14UTC reports all14 actual captures match originals; all10 positive cases have exactly one canonical invitation, Bob waiting-for-couple v2 target101 with partner empty; all10 exact-replay verifications passed. All four controls have zero operations; invented registrations unchanged and cross-event target registration absent. Role restored, revocation marker cleared. This reconciliation is explicitly operator evidence, not sourceblind DB access.

## Verdict and limits

PASS for the scoped14 programmed-provider EN/RU functional cases, including all four real delayed-boundary cases and all10 exact original replays/restarts. No actionable functional defect demonstrated. Read-only UI/manual controls, messages, callback rendering, history freshness, cross-user isolation, committed-operation truthfulness and real delayed authorization boundaries exercised.

Browser result comparisons establish unchanged visible chat; root transport/model/state comparisons establish no hidden extra edits/effects/model calls. Removed privileged buttons establish UI current-rights behavior; signed public queue403 is operator evidence because no queue button remained for browser invocation. Receipt edits reuse a historical card without event label, so exact case attribution depends on separate operator transport evidence and event-specific notifications. Programmed provider proves application behavior for supplied proposals, not natural-model reasoning. Physical Telegram client/network and real production identities were not used. Alice acceptance/decline actions intentionally not invoked; pending state was preserved. Initial unrelated synthetic preflight history was ignored. No production/source/image modifications, rebuilds, resets or deletions by reviewer.

Evidence is in qa.local/r96-functional-20260930/functional-01/. Report complete; stand released to root after final manual observations.

