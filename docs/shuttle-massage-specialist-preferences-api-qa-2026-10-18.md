# Massage specialist preference API QA — 2026-10-18

Shared agent and native preference APIs pass 35 selected race scenarios:
32 integration roots, two observability roots and one manifest root. There are
75 passing test events, zero failures and zero skips. Final scoped Linux lint
reports zero issues. This is source/mock HTTP acceptance; the main stand remains
schema48 / mvp-55. UI, reactive action context, complete feature installation and
real Telegram/restart acceptance remain open.

The API reads and changes the current account's `notify_bookings` preference.
It applies to ALL of that account's specialist events in this bot. A current
private specialist binding in an explicit enabled event is required; a pass or
global administrator role does not substitute for that binding. Absence means
true/version0. Both interfaces require an explicit boolean, including false,
and an explicit nonnegative expected version. Changes use CAS and immutable
operation receipts; an old successful retry returns its original result without
overwriting a newer preference. A second event observes the same account setting.
Upcoming-session `notify_next` is not implemented by these methods.

Agent methods are `zinusya_massage_staff.preferences` and `set_preferences`.
The write method has matching operation/idempotency identifiers and status and
reconcile endpoints. The owned frozen Loom source sends only protocol version
and operation ID to status; the new receipt lookup derives the original event
and rechecks its current authorization. HTTP tests use that exact bare status
shape, not an invented event argument. This is not a real Loom execution gate.

Native routes are GET/POST `/app/massage-staff/{event}/preferences` and GET
`/app/massage-staff/{event}/preference-operations/{operation}`. Session/origin
checks remain in force. Tests cover missing/null CAS and boolean, stale CAS,
changed arguments under the same key, wrong scope, module disablement and
revocation/regrant. A paused IAM request is resumed after session expiry,
binding revocation or module disablement: none may create a preference or receipt.
Historical status requires current access. Missing/inaccessible status is not
proof that an operation never executed. No proactive tasks or Loom runs are
created by preference changes.

Fresh review found a native DTO that treated an omitted/null expected version
as zero. It was corrected to require a pointer value, with omission/null tests
for both transports. A temporary compile error during that conversion and lint
findings were corrected before the final gate. Final fresh correctness and
separate quality/runner reviews found no actionable issues. The index remains
empty. No commit, publication or production action was performed.

The gate froze source inputs and checked ten main Compose container identities,
images, start times, running states and restart counts. It does not establish a
live business-row fingerprint or standalone-provider fingerprint. Temporary
PostgreSQL and Go containers were limited to 512 MiB and 2 GiB respectively;
neither was OOM-killed. Both exact owner-labelled containers and their private
PostgreSQL volume were removed after acceptance. Shared Go caches remain.
Post-test database shutdown is cleanup, not an application restart test.

Ignored evidence: `.cache/shuttle-work/massage-specialist-preferences-api-v1-qa/`
contains expected roots, source freeze, race logs, lint logs and the accepted,
cleaned checkpoint. Next is the native control/recovery journal and bounded
reactive history, then feature-wide review and installation preparation.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
