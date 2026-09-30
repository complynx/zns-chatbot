# R96 independent Functional QA operator evidence

The independent reviewer owns functional-qa-r96-2026-09-30-01.md. This report records root-controlled setup and backend evidence, not independent browser findings or whole migration acceptance. Product sources/images remain frozen; 1991 build-source hashes rechecked unchanged during review. Root is sole operator. No existing stand was deleted or reset.

## Ordinary cases

Fresh r96b fixtures use zero-capacity events with Alice waitlist1 and Bob cancelled1. Existing initial fixtures/preflight failures remain. The reviewer submitted every actual message/contact through the Telegram-like UI; root paused only app, extracted the exact persisted fake pending Update, bound its fixture, armed first real inbox capture and restarted the same app.

| Locale | Queue | Explicit | Trusted contact | Untrusted target | Other event |
| --- | ---: | ---: | ---: | ---: | ---: |
| EN |43|46|49|52|53|
| RU |63|66|69|72|73|

Positive cases commit one invitation with target101 and no accepted partner. Negative cases commit none; cross-event target bookings remain absent. Exact originals43/46/49/63/66/69 were replayed from first durable captures after app-only restart. Whole business/admission/winner state and complete message transport remained equal; programmable-provider counters remained equal.

App startup always adds exactly three successful command/menu audit calls. The restart verifier constructs the entire expected fake state with only those exact calls, unchanged menu payload and the prescribed64-entry history bound; whole-state equality then must hold. Full pre/post fake-state equality without this addition is false and is not claimed. Original initial verifier failure is retained. This proves programmable-provider behavior, not natural model reasoning or real Telegram.

## Timed authority boundaries

Preparation uses real language UI changes and read-only quiescence/lane-order checks. No queue, clock, fairness or pacing value is edited. The existing one-shot fake429 creates a real preceding Bob registration notice; only after an exact committed operation and later pending attempt0/message0 receipt are observed does a guarded transaction remove Bob's original synthetic global queue-admin grant. The transaction rechecks the same-chat FIFO boundary with at least30s remaining and aborts on ambiguity. Grant restoration requires its exact recorded case.

| Case / actual update | Observed predecessor → receipt | Grant withdrawal UTC | Natural retry UTC | Result before restoration |
| --- | --- | --- | --- | --- |
| EN queue-revoke /87 | notice595 lane46 → card:40 lane47 |16:36:43.647024|16:38:10.611399|Privileged receipt cancelled attempt0/message0; one generic current fallback; invitation once. Public queue API403. Exact replay unchanged.|
| EN explicit-retained /93 | notice737 lane53 → card:48 lane54 |16:44:07.958849|16:45:43.424702|Original retained receipt sent once as edit22 at16:45:45.246802; later redundant intent cancelled attempt0. Invitation once. Exact replay unchanged.|

RU queue-revoke actual101: notice852 lane63 preceded card:61 lane64. Guarded withdrawal16:50:33.718734UTC before retry16:52:10.871035UTC; actual16:52:44 observation showed card61 cancelled attempt0/message0, generic card63 sent, invitation once. Authenticated queue API403 at16:51:03.725UTC. Exact101 replay passed unchanged business/transport/model comparison with only the exact startup menu sequence; reviewer observed identical Bob chat. Original grant restored16:54:23.965897UTC. Its settled-facts.json filename predates the actual deadline; observation-165229.json contains the actual16:52:44 settled state. Timestamps and data govern interpretation, not filenames.

Role restored after reviewer observations and strict replay verification:87 at16:40:31.184189UTC;93 at16:47:48.533937UTC. RU explicit-retained106: notice951 lane70 preceded card69 lane71; guarded withdrawal16:56:25.961734UTC, actual retry16:58:02.241908UTC. Card69 sent once as edit22 at16:58:04.031775UTC; later redundant card70 cancelled attempt0. Exact106 replay passed the same strict state/model comparison. Original role restored16:59:39.197743UTC.

The explicit case had identical text in an earlier existing workflow card before its new receipt was sent. Attribution uses exact update93/card:48 attempt timestamps, not text appearance alone. The card carries no event label; reviewer assesses actual UI separately. UI removal of queue buttons is not a direct callback-denial experiment; operator independently obtained authenticated Core403 for87 with a short-lived synthetic signer and did not save its bearer token.

Evidence: qa.local/r96-functional-20260930/functional-operator/ per-case directories. A first file named after-natural-release.json for87 was captured before deadline; its timestamp is authoritative. Actual settled observation is after-natural-release-2.json. Initial operator23 timing miss and all failed quiescence attempts remain preserved; none were counted as passes.

## Final operator reconciliation

At17:00:14UTC all14 actual reviewer captures matched the exact submitted originals. Ten invitation cases each had one canonical operation, Bob waiting-for-couplev2 target101 and empty partner; each had a verified exact replay. Four negative controls had zero operations; invented-target bookings stayed cancelledv1/target0 and cross-event target bookings stayed absent. Bob original synthetic grant was present, role_control.revoked_for null. The independent reviewer subsequently completed EN/RU incoming-invitation control observation without acceptance/decline mutations and issued scoped PASS. Stand released; root rechecked all1991 frozen source hashes before adding unrelated R100 integration tests. Per-case facts: functional-operator/final-case-facts.jsonl.
