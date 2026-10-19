# Massage saved-choice form source checkpoint

Date: 2026-10-17. Source schema46 / manifest `mvp-49`.
Owned runtime remains schema45 / `mvp-44`; the candidate is not installed.
This extends [reactive context](shuttle-massage-reactive-context-qa-2026-10-17.md)
and completes the source wiring for pending discovery in both interfaces.

The owner form lists unfinished booking/cancellation choices for the selected
event, with independent first/next paging. Booking starts use event-local time
and an explicit offset. Labels are English/Russian; record text uses textContent.
The list remains readable when new booking access is lost, so an owner can
review a historical cancellation. Discovery never submits or confirms an action.

Selecting a choice persists a new operation ID, original proposal selector and
exact arguments before preparation. The server must return the same proposal ID.
The form then shows revalidated names, price, treatment/occupancy times, timezone
and expiry, and requires a separate explicit confirmation click. Reload and retry
retain the same operation and selector. A rejected/expired adoption or a response
with a replacement proposal cannot enable confirmation or silently start a new
preparation. The user can discard the local preview and reload current choices.
Discarding a local preview does not cancel a booking or delete a server proposal.

Unknown apply results retain the existing exact-request recovery gate. If another
interface consumes the same proposal during preview, apply recovers the canonical
result. Busy state, language changes, pagination, logout and owner epochs cover
the new controls. No new styling system, dependency or API was introduced.

## Verification

The initial24-case run passed21 cases; three saved-booking
cases failed because refreshing slots removed their buttons from the shared busy
registry, leaving them disabled. That production UI bug was fixed by retaining
the saved-choice controls. The first failure summary is retained locally; its full
raw output exists in task history, not a separate saved log.

The final full Mini App suite passes128 tests, including25 massage cases, with
zero failures/skips. Source hashes remained unchanged during the suite. Coverage
includes adoption under a new operation without a replacement, persistence before
HTTP, separate confirm, reload/retry identity, saved cancellation without new
booking access, expired rejection, replacement-ID refusal, independent pagination,
Russian labels, busy controls, logout during read and another-interface consumption.
The existing embedded-asset route test passes. Two independent source reviews
report no actionable findings. The index remains empty.

This gate uses a lightweight DOM and a mock API following the accepted adoption
contract. It does not establish actual rendering, real Loom/model execution or
Telegram mixed acceptance. Existing Go lint acceptance is inherited unchanged;
this component changes HTML/JavaScript only. Evidence is in ignored
`.cache/shuttle-work/massage-choices-v1-qa/`. No QA database/container was needed.

## Combined runtime acceptance

The accepted source is being built as a local candidate with the unmodified
release Dockerfile in a dedicated2GiB/no-swap/2CPU builder. Build completion,
restored-copy migration46, owned-stand activation and real native/agent/both mixed
Telegram workflows remain open. The builder is temporary and must be removed
after its terminal result. No production deployment or publication occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
