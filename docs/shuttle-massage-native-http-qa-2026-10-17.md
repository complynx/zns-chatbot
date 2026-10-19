# Massage live availability and Mini App HTTP checkpoint

Date: 2026-10-17. Source: schema 46 / manifest `mvp-47`.
Owned runtime remains schema 45 / `mvp-44`; this work was not deployed there.
This extends the [atomic store and agent API checkpoint](shuttle-massage-agent-api-qa-2026-10-17.md).

## Implemented behavior

`zinusya_massage.availability` and the native HTTP availability endpoint observe
current bookings, the owner's party count and shared table capacity. The query
selects an explicit event, schedule versions, party window, specialist, service
and an absolute range of at most 24 hours, with at most 100 candidates per page.
The observation time is the database time; ordinary starts respect the 15-minute
lead and public lengths of 1, 2, 3 or 5 blocks. Times include event-zone offsets.
Unavailable starts remain in the page with `occupied` or `party_limit`, without
other participants' identities. Follow `next_after` even when all starts are busy.

This is a snapshot, never a hold. Preparing also holds no capacity. Exact approved
apply rechecks access, versions, occupancy and limits. The earlier
`candidate_starts` endpoint remains schedule-only; it is not live availability.
One lightweight occupancy query reads only IDs/times/table requirements over the
candidate range instead of loading historical booking snapshots.

Native routes are:

- `GET /app/massage/events`
- `GET /app/massage/{event}/schedule`
- `GET /app/massage/{event}/bookings` and `/bookings/{booking}`
- `GET /app/massage/{event}/operations/{operation}`
- `POST /app/massage/{event}/availability`
- `POST /app/massage/{event}/book/prepare` and `/book`
- `POST /app/massage/{event}/cancel/prepare` and `/cancel`

Native requests use the current Mini App bearer, key generation, IAM preflight,
owner binding and session checks within the store transaction. Responses recheck
the current session before delivery. Writes validate origin, bounded strict JSON,
matching event/operation IDs and explicit proposal confirmation. Reads are
`no-store`. Historical own bookings and receipts remain accessible after pass or
module loss; new schedule/availability/booking access still needs an assigned
pass and enabled module. Existing own bookings can still be cancelled. Retained
history keeps its event discoverable. This does not grant specialist/admin rights.

Read transactions acquire owner/session locks without first locking the event,
so they do not reverse the established write lock order. One-connection tests
cover native read composition. History pages retain the 60 KiB encoded allowance
and cursor behavior from the earlier API checkpoint.

## Verification and failures

Linux Go 1.27.2/PostgreSQL 17 race verification covers 89 selected test roots and
965 frozen inputs. The broad final run passed 88 roots and failed one assertion
that expected HTTP 200 from logout; the established handler returns 204. After
changing only that test expectation, a separate focused race run passed the
remaining root, including all post-logout read and availability denials. Combined
passing test events total 170; neither run skipped a scenario. The broad failed
run is retained as failed, not relabelled as a green all-89 run.

Earlier retained failures exposed an outdated manifest test method list and a
native HTTP fixture that reused the stopped storage-test server. Those test
fixtures/assertions were corrected before the broad final run. Independent
correctness and quality reviews found one nonempty-page assertion gap; it was
fixed. Fresh follow-up reviews of the test, server fixture and logout corrections
found no actionable findings. Configured scoped lint reported zero issues, and
SQLc compile/generation reproduced all three generated Go files byte-for-byte.

Coverage includes live occupied/free/adjacent starts, shared tables, party limits,
no preparation hold, pagination, current access/version denial, bounded maximum
pages, native booking/replay/cancellation/history/status without creating agent
tasks, both native/agent HTTP handoff directions, exact approval rejection,
foreign owner isolation, and expired/revoked/logged-out session denial. HTTP tests
use real isolated PostgreSQL and mock IAM; they are not real Telegram or Loom QA.

Evidence is in ignored `massage-native-http-v1-qa/` through `v4-qa/` directories
under `.cache/shuttle-work/`. The final `component-acceptance.json` links the
88-root result and focused recheck, with the test-only source delta. Every
attempt's temporary containers and private volume were removed. No runner was
OOM-killed. Go was limited to 2 GiB and PostgreSQL to 512 MiB with swap disabled.
The ten existing Compose services retained their IDs, images, start times and
restart counts. The existing counted-provider fixture remains in use and unchanged.

## Remaining work

At this checkpoint native owner forms were next. The later [owner form source
checkpoint](shuttle-massage-native-ui-qa-2026-10-17.md) records their component tests.
Native launch and the pending-proposal/action projection for reactive agent
continuation remain open. Current tests transfer known proposal IDs; they do not
prove that an agent discovers a native user's pending choice. Notifications,
specialist timetable/exceptions, rescheduling and full legacy parity remain open.
No actual embedded massage form, real IAM/Loom workflow, or unattended model path
has passed acceptance. Browser access was later explicitly reauthorized and the existing embedded app
opened and signed in; no new massage form was installed or accepted there. Production and publication are unchanged.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
