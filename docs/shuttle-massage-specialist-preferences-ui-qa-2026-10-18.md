# Massage specialist preference UI QA — 2026-10-18

The native notification control passes 24 specialist UI scenarios. All eight
mini-app test files used by CI pass together: 176 tests, no failures or skips.
This is mock DOM/HTTP behavioral acceptance. No real browser rendering,
Telegram Mini App opening, process restart or main installation is claimed.
The owned main stand remains schema48 / mvp-55.

The existing specialist timetable now has an English/Russian checkbox for
booking and cancellation notifications. Its text explicitly states that the
setting applies to ALL the account's specialist events in this bot. It uses
[the shared preference APIs](shuttle-massage-specialist-preferences-api-qa-2026-10-18.md),
requires an available selected event, and uses the current expected version.
Upcoming-session notifications are a separate milestone.

Before POST, sessionStorage records the owner, operation ID and exact original
arguments (event, expected version and boolean). Failure to persist blocks the
write. An uncertain response blocks new operations. Check and retry remain
available after page reload; retry preserves the original operation and event,
including before an event is selected again. A missing/inaccessible receipt is
not treated as proof of nonexecution and does not discard the journal.

Successful recovery removes the journal, confirms the earlier save and reads
current preference separately when an event is selected. A historical receipt
cannot overwrite a later agent change. A known CAS rejection requires a fresh
read before another choice. Corrupt recovery data or cleanup failure blocks new
writes; a different account cannot adopt the saved request. Late responses
cannot restore an old account's controls or erase its recovery record.

Preference read/recovery failures have separate status handling and do not
clear the authorized timetable. Session expiry still clears the workspace.
Tests cover the normal native path, lost and unsubmitted requests, page reload,
immutable receipt versus newer state, known CAS rejection, storage failures,
account switching, English/Russian labels and earlier timetable behavior.
There are no new frontend dependencies or frameworks. The checkbox uses the
existing page styles with a specific width override.

Fresh correctness and quality/docs review found two defects. Successful retry
before choosing an event left an unresolved message; a storage cleanup failure
inside the retry-conflict path escaped into timetable error handling. Both were
reproduced by focused failing assertions and fixed. Two subsequent fresh
correctness passes, including the final CSS adjustment, found no actionable
issues. The first test run also exposed an old cursor test's assumption that
the last request was always the timetable request; it now selects that request
explicitly, preserving its cursor and DST assertions. Failed review reproduction
output is retained, rather than rewritten as a pass.

Ignored evidence: `.cache/shuttle-work/massage-specialist-preferences-ui-v1-qa/`
contains the baseline, failed reproduction log, full final test log and an
accepted checkpoint with source hashes. Source remained unchanged during the
final run and the index was empty. This gate created no containers and changed
no main service configuration. No commit, publication or production action was
performed.

Next: bounded preference action history on explicit agent requests, feature-wide
mock acceptance, manifest revision and reviewed migration/recovery preparation
before main installation and real Telegram acceptance.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
