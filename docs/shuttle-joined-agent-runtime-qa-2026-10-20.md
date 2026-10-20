# Joined agent registration, pair and payment QA — 2026-10-20

One complete two-participant agent journey now passes on an isolated schema64 /
`zinusya/mvp-74` stand. The path starts with fresh identities and ends with two
fully paid passes. All business actions use actual Loom tools and separate users'
messages. This closes the agent-only joined happy path in the
[workflow plan](shuttle-registration-pass-parity-plan.md). Both [whole mixed happy paths](shuttle-joined-mixed-runtime-qa-2026-10-20.md) subsequently pass on candidate mvp75. This is controlled-provider orchestration evidence, not unattended
model comprehension or real Telegram/WebView acceptance.

## Fresh setup and actual workflow

The stand starts with four users in its own real local Zitadel, one event and two
event-authority records: administrator and ambassador. SQL fixture writes only
event rules, deposit policy and authorities. Readback verifies no participant
profiles, legal fields, registrations, invitations, pairs, passes or payment
records, and zero Loom runs. The event uses `Europe/Minsk`, a 100.00 BYN pass,
30.00 BYN minimum deposit and 25 percent minimum share.

1. Each participant explicitly supplies a display name, separate legal name,
   synthetic passport and registration role. Real tools discover the event by
   its human name, save the fields, complete readiness and join the waiting list.
   The follower requests their own partner code and receives it in the reply.
2. The leader supplies that shared code and requests an invitation, acknowledging
   the solo-allocation hold. The follower separately accepts the pending
   invitation as follower. One waitlisted pair results, with no passes.
3. The event administrator asks to assign both passes. Authorized discovery
   returns the pair with both display names and bound Telegram/account IDs.
   The delivered approval includes the canonical prices and required deposits,
   plus minimum amount/share and event-local final top-up deadlines. There are
   zero passes and zero assignment receipts before consent.
4. The bot stops and starts while this approval is pending. The original task,
   Loom run, approval ID/hash and canonical preparation survive. Natural-text
   consent resumes that same run. One full canonical assignment receipt creates
   exactly two passes for the original registrations.
5. Each participant uploads a synthetic PDF with a caption explicitly requesting
   a 30.00 BYN initial deposit. The actual model input contains the accepted file
   ID and exact caption. The agent reads current expectations/balance and submits
   pending evidence. Confirmed money remains unchanged before review.
6. The ambassador requests review of the actual evidence reference, fetches its
   protected artifact and prepares the exact amount. Original terms are delivered
   before a separate natural-text consent. Real `interaction_resolve` completes
   the canonical review, credits the amount and returns its full receipt. This
   repeats for each owner's 70.00 BYN top-up. Final agent reads show both passes
   fully paid, 100.00 BYN confirmed and 0.00 BYN remaining.

All four original submit receipts remain exactly equal to their stored pending
results after later confirmation. Current evidence/review state is read separately.
Completed review-operation reads return the same canonical envelopes. This tests
completed-operation recovery, not an uncertain execution/replay failure.

## Result and restart evidence

There are two assigned registrations, one assigned pair, two passes, four files,
four evidence records and four confirmed ambassador reviews. Total confirmed money
is 20000 synthetic BYN minor units. All 15 Loom runs and all bot tasks complete;
no outbox delivery remains pending. No native payment action was used.

A live Docker event subscription is observed before the restart using a harmless
exec marker in this stand's PostgreSQL container. Matching events for the exact
bot container record signal 15 and exit 0, 0.53029413 seconds apart. The stop command
wall time is 0.735 seconds; `State.FinishedAt` is retained before start. The bot is
not OOM-killed. This establishes subsecond signal-to-exit for this one probe,
not a general latency guarantee. The earlier native run's 4.125-second command
wall time and 10.54 ms application drain remain separate measurements with their
original limitations; this does not retroactively explain them.

## Review, archives and boundaries

Independent normal and adversarial reviews cover the QA controller, assignment
and payment steps. Valid QA findings strengthened the delivered deposit-policy
checks, conditional signal claims and exact original-caption checks. The payment
invocation started before the new inline caption assertion; a separate read-only
audit checks all four original saved model requests before final acceptance.
No business action is replayed for that audit.

Cleanup review also tightened exact project membership: the expected nine service
names, full container IDs, images and ownership labels must match before the first
stop and again before destruction. Eight writers stop before three PostgreSQL
archives are created and fully decoded with `pg_restore --file=/dev/null`.
PostgreSQL then stops and the exact temporary containers, volumes and network are
removed. This validates archive decoding, not a fresh restore. Eight services exit zero; the mock Telegram service exits137 without OOM. Its
missing explicit SIGTERM handler is addressed in the fixture-only follow-up below. This does not
change the completed business receipts. The owned primary remains unchanged: eleven
services, schema64/mvp74 and 134 original Loom runs.

The product source hash stays
`c4171086d50e64ade8061e44e6f828c37251533d800c21d290a962479e48fdbc`.
No Go implementation, schema or installed image changes in this stage. No fresh
full Go suite, race or remote CI run is claimed. Protected artifact fetch proves
identity/metadata access; the controlled provider does not interpret receipt
content, classify documents or establish PDF rendering. Real Telegram/WebView,
whole mixed journeys, broader fault/revocation/deadline cases and production
readiness remain separate gates. No new Loom defect is established.

Ignored evidence is retained under
`.cache/shuttle-work/joined-agent-runtime-v1-qa/`: `runtime-acceptance.json`,
`caption-input-audit.json`, `checkpoint.json`, `review-final.json`,
`restart-live-events.private.jsonl`, `cleanup-proof.json`, private logs and three
private database archives. `docs-review-final.json` records documentation review.

## Fixture shutdown follow-up

The original cleanup exit137 remains in the retained evidence. A focused Linux
process test reproduces the missing handler as exit -15 instead of clean exit0;
the mock running as container PID1 had required the later forced stop. The narrow
fix installs an explicit SIGTERM handler that unwinds the existing cleanup of
both listeners. It changes test infrastructure only.

The new behavior test verifies both HTTP/TLS listeners, an acknowledged message,
shutdown with an incomplete HTTP connection, clean exit0, restart from the same
SQLite file, exact message preservation and monotonic message IDs. All26 nearest
mock-stand tests pass in Linux after the fix. An initial broad run failed because
the test runner's32 MiB tmpfs filled during the existing large-Unicode journal
case; that failure is retained. With bounded384 MiB memory and192 MiB tmpfs the
same unweakened26 cases pass. No OOM occurs.

A separate direct-PID1 container probe starts both listeners and exits0 after
`docker stop --time5`; the command wall time is0.625 seconds. This is not a live
signal-to-exit measurement. The exact probe container is removed. Fresh normal
and adversarial source reviews are clean. Fix evidence is retained separately in
`.cache/shuttle-work/mock-telegram-shutdown-v1/`, including the before/after logs
and `pid1-probe.json`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
