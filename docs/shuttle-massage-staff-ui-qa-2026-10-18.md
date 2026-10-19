# Massage specialist Mini App source QA

Scope: specialist read workspace and shared current/retained window discovery.
Source schema48 / manifest `mvp-54`; not installed. Owned runtime remains schema47 /
`mvp-51`. Native administrator form, real-service specialist acceptance and full
Python parity remain open.

## Verification

- Linux/Docker, Go 1.27.2, PostgreSQL 17: 48 selected root tests / 57 pass events,
  race detector, zero failures/skips. Exact expected root set and package terminals
  are checked, rather than relying on process exit alone.
- 143 Mini App JS tests pass with process timezone Pacific/Honolulu; 15 specifically
  exercise the specialist form. Tests cover native-only reads, app/session wiring,
  own contacts, pagination, DST, RU/EN, lost access/session, stale responses,
  retained window versions and malformed responses.
- DB/API/integration lint passes. Final integration lint also passes after the
  instant-comparison fixture correction. Independent correctness, skeptical quality
  and fresh final reviews are recorded in the ignored component checkpoint.
- Source freeze and all ten main Compose service fingerprints remain unchanged
  during the final race gate. Disposable Go/PostgreSQL limits are 2 GiB/512 MiB,
  no additional swap. No OOM occurred.

Current/frozen matching window instants deduplicate at the HTTP boundary. Native
and agent reads return the same projection. The retained-window test covers old
party ID reuse, shortening, removal and later event-date changes. A 25-window
fixture checks page continuation and access revocation between pages. SQL performs
event-local union/dedup; fixed response bounds do not imply constant query work.

## Preserved failures

The first Linux gate could not compile because `_windows.go` and `_windows_test.go`
are implicit Windows-only filenames. They were renamed to `_window_reads.go` and
`_window_reads_test.go`. Windows host checks had passed; Linux checks caught the
platform issue. The second gate had one failed assertion comparing timezone
representations of equal instants. The test now uses `time.Time.Equal`; business
logic was unchanged by that correction. Both failed gates and exact cleanup
receipts are preserved. The third gate is the accepted result.

## Manual browser QA

Actual product HTML/CSS/JS ran against a loopback mock backend, without production
or Telegram mutations. Verified event selection; own-client detail and safe contact
href (not followed); foreign booking without contact/detail controls; distinct
02:30 times with +02/+01 DST offsets; RU labels and decimal price; own-only filter;
clearing private data on revocation and session expiry. A QA-only bootstrap syntax
error was corrected before inspection. The temporary browser tab and HTTP preview
server were closed. This gate does not prove real IAM, Loom dialogue, Telegram
embedding, all browser viewports or unattended model behavior.

## Evidence and next work

Ignored evidence: `.cache/shuttle-work/massage-staff-ui-v1-qa/`, `-v2-qa/`,
`-v3-qa/`, exact diff `zinusya-massage-staff-ui-v5.diff` in the local temporary
folder, and `.cache/shuttle-work/massage-staff-work.json`. These records contain
no claim of installation or production readiness. Disposable containers/private
volumes must be absent at component closure; cleanup is checked separately.

Implement the native administrator grant/revoke form, exact confirmation and
original-result recovery next. Then install the complete workflow only on the
owned stand and exercise real IAM/Loom/Telegram native, agent and mixed paths.
Specialist notifications, instant self-reservation/release and atomic rescheduling
remain later workflows. Release hold remains in force: no stage/commit/push/PR or
production mutation in this stage.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
