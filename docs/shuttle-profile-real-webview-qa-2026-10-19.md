# Actual Telegram profile form QA, 2026-10-19

## Accepted scope

The installed schema59 / `zinusya mvp-68` Mini App passes one complete native
profile-name scenario in the real Telegram WebView: save a temporary synthetic
name, sign out, reopen with a new session, read that saved name, and restore the
original name through the same form. Both sessions were then signed out and
closed. Only the authorized account in @complynx_test_bot was used.

This closes real WebView profile-name writes and persisted reads for this case.
It does not accept full registration/pass parity, language editing, response loss,
agent continuation from these particular writes, or unattended model behavior.
Earlier [profile component/runtime gates](shuttle-profile-entry.md) retain their
historical scope. The separate [launch-target gate](shuttle-miniapp-anchor-runtime-qa-2026-10-19.md)
records the installed image and initial navigation acceptance. No new binary,
configuration, IAM role, schema, API or production deployment was introduced here.

## Observations and independent readback

Before the test, the owner's profile was version22 with no pending name
expectation. The form displayed the saved original name. Saving the synthetic
name showed Profile saved. A new signed-in session read the synthetic value from
the server. Restoring the original name again showed Profile saved. The test
used ordinary form controls; it did not inject browser state or synthesize
Telegram proof. No legal name, passport, bot language, payment or booking was
edited. Both sessions visibly signed out and the test chat remained selected.

PostgreSQL independently confirms versions23 and24 as the two immutable profile
operation results. The final profile equals the original complete row except
for its version increment by two. Two owner-bound `set_name` action-journal rows
reference those exact operation IDs. These are durable context records for a
later user-initiated agent request; this checkpoint does not assert that a Loom
run has consumed them. Both new session IDs are revoked, and all previous session,
operation and action-journal rows remain identical.

The selected profile is the only changed business row. Every other profile,
registration, payment, order, massage and avatar record, all communication rows,
River jobs/queue configuration and database schema/sequence state are preserved.
Only the expected profile/operation/journal/session tables and River housekeeping
hashes differ. Loom stayed at132 runs; no agent task or Telegram message was
created. Loom remained running, so this particular UI test is not an outage test.
Runtime identities and settings of all11 services remained unchanged.

## Evidence and boundaries

Ignored `.cache/shuttle-work/profile-real-native-v1-qa/` contains the before
snapshot, UI observations, exact operation/session readback and proposed/current
custody epoch. The source and served assets were unchanged, so no unrelated Go
suite or image rebuild was repeated. The read-only verifier checks the complete
snapshot and exact additive records; browser-observed facts are recorded
separately from database proof. No exported screenshot path is claimed.

## Subsequent reactive agent follow-up

After the native gate was accepted, one actual Telegram message asked which name
was currently saved and explicitly requested no changes. The first provider
request of that real Loom run contained both exact native operation IDs and
versions23/24, marked as untrusted historical facts, not instructions or consent.
No proactive run was started by the form writes.

A Codex-controlled synthetic provider then requested exactly one script call to
`modules.zinusya_profile.get({})`. Its actual tool result returned the same owner,
current version24 and restored original name. The same run completed with a
Russian read-only reply, visibly delivered in the authorized Telegram chat with
one original receipt. No module write was requested. This verifies transport,
context and scoped tool execution, not unattended model reasoning or a mutating
handoff in either direction.

The separate follow-up snapshot proves one new task/inbox/outbox and one completed
scheduler job, only the selected conversation sequence advancing by one, and no
profile, session, avatar, business or schema change. All earlier communication
rows and other conversations remain identical. Loom increased from132 to133
runs. Runtime identities stayed unchanged and no temporary container was created.
Evidence is `profile-real-native-v1-qa/agent-context-v1`; its reviewed custody
observer is `installed_primary_observer_v11.py`. The earlier native-only gate
retains its132-run baseline and original acceptance boundaries.

The previous actual avatar upload attempt remains unaccepted because browser
control produced no file chooser event. No upload occurred; its revoked session
and unchanged state have separate evidence. That limitation did not prevent the
real profile form write and reactive read. Remaining priorities include mutating
native/agent handoffs from real WebView writes, stale form/restart/lost-reply paths
and the broader key-command registration/pass workflows. Joint CA/zitadeltg login
remains a separate deferred gate. No production readiness or overall roadmap
completion is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
