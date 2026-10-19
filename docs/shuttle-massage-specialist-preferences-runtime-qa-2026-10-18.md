# Specialist preference mixed runtime QA — 2026-10-18

Accepted on our owned `shuttle-live-test`, schema49 / `mvp-56`. This gate covers
native HTTP → agent → native HTTP continuation for the current Telegram account,
not the Mini App browser controls or unattended model quality.

## Native authority and operations

The synthetic event is `QA staff access - SYNTHETIC`. Its existing own specialist
binding started revoked at version6. Native and agent preference reads returned404;
the native administrator workspace returned403. Temporary local IAM grants allowed
native prepare/confirm to activate the binding at version7. Both grants were then
removed. Preference acceptance ran without a pass or administrator role.

A synthetic signed current-account Telegram initData proof opened a real native
session through the bot HTTP endpoint and real IAM. This does not prove an actual
Telegram Mini App launch. The existing profile metadata was preserved.

The absent global preference read as enabled/version0. Native operation A disabled
it at version1. Exact replay and operation status returned the original result with
one receipt; missing boolean returned400, stale CAS and changed arguments under the
same operation ID returned409. Native actions started no Loom run.

## Actual Telegram and Loom continuation

One request was sent from the authorized account in `@complynx_test_bot` to enable
new-booking/cancellation notifications, without changing bookings or payments.
Actual task `42d52292-36aa-4656-a5f9-c94cea5d351e` and Loom run
`7ceab860-680b-4243-97ce-8a2de33cea90` completed. The owned provider used manual
inference responses: this proves real transport, context, IAM and tool effects,
not autonomous model decisions.

The actual provider request contained the native receipt as untrusted history.
The preference read and write outputs were correlated to their exact tool call IDs;
the write CAS version came from the successful agent read. Agent operation B enabled
notifications at version2. The original native receipt and its version1 status
remained unchanged. One final outbox item was delivered to the authorized account
(Telegram message4599), and the reply was visible in Telegram Web. A screenshot was
emitted inline; filesystem saves were denied, so no retained screenshot file is claimed.

A GET of that existing Loom run returned503 after three provider responses had
already been recorded. A later observation of the same run succeeded; there was no
new task, response replay or uncertain write replacement. The initial diagnostic
query referenced a nonexistent column and was corrected read-only. The observation
artifact initially said zero responses; its original is retained beside the explicit
correction. No 503 response body was captured, so this is not classified as a Loom bug
or a completed outage gate.

## Native continuation and cleanup

The old native session expired during QA. The helper incorrectly expected403 from
logout, but received the intended401. Its failure, source and review gate were
retained. A fresh independent diff review accepted the status correction and bounded
recovery of the already-saved replacement session, without another bootstrap.

Native replay of A still returned disabled/version1 while the current value stayed
enabled/version2. Bare agent operation-status requests derived each receipt's event
without an explicit event argument. Native operations C and D changed the value to
disabled/version3, then restored its original effective enabled value at version4.
All four immutable receipts remain; versions were not reset. No extra Loom run started.

A separately journaled temporary IAM cycle revoked the fixture binding at version8,
then removed both grants. Native and agent own-preference reads again returned404,
administrator reads403, and historical agent operation status became `not_found`
under current revoked authority. The preference value remains enabled/version4.

Final acceptance checked39 protected table fingerprints covering bookings, orders,
passes, registrations, payments and pairs, plus unchanged settings. There were zero
specialist notification intents, so existing bookings were not backfilled. The same
single task/run completed and one final message was delivered. The native session
was logged out; `/app/me` returned401. All temporary bootstrap/session files were
removed. No containers were created or restarted by this gate.

## Reviews, evidence and limits

Native helper: two review passes, with a separate cleanup IAM cycle added after the
first finding. Loom helper: two passes, fixing correlated read proof and historical
receipt preservation. Finish helper: initial clean review, then one fresh clean diff
review for the observed401 correction and saved-session recovery. The Git index
remains empty; there was no publication or production change.

Ignored evidence is under
`.cache/shuttle-work/massage-specialist-preferences-runtime-v1-qa/`: baseline,
checkpoint, `agent-on.json`, `native-followup.json` (`accepted_and_cleaned`), both IAM
journals, review gates, corrected/original read observation and retained refresh
failure. Private session files have been removed.

Real booked/cancelled specialist delivery, mute/rebind generation fences with the
running worker, pending/inflight/unknown process restart, full live rollback and the
new browser checkbox remain separate gates. Quiet shutdown and component tests do
not substitute these checks. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
