# Frozen native pair-assignment terms — 2026-10-20

Source and packaged candidate `zinusya/mvp-77` now show new pair-assignment
preparations without the canonical description's internal IDs/raw seconds.
This source/package gate preserved the schema64/mvp74 primary with eleven
services and134 original Loom runs. The subsequent [mvp77 primary installation](shuttle-pair-terms-primary-qa-2026-10-20.md)
now accepts schema64/mvp77 separately; packaging alone did not establish it.

## Original terms, including old proposals

New preparations persist optional `display_terms` alongside their unchanged
canonical description and arguments. It freezes `event_name`,
`payment_window_seconds` and `payment_closes_at`; the original event timezone is
already stored in the preparation. The native view displays the event, localized
duration after assignment and event-local closing cap, followed by each recipient,
price, tier, required deposit, configured minimum amount/share and top-up deadline.
It still requires explicit consent and shows proposal expiry. Payment is not
recorded by assignment.

No current event settings are substituted into an earlier proposal. Preparations
without this snapshot retain their complete original description. That legacy
fallback can still contain IDs/raw seconds; it is deliberate until original
structured terms can be established without inventing history. Incomplete supplied
structured terms reject the view rather than silently using current settings.
The existing agent canonical description and confirmation arguments are unchanged.

The field expands the JSON document in schema64; there is no schema migration,
backfill or historical receipt rewrite. New-reader/old-document behavior is tested
through actual native HTTP reads and confirmation after restart. Old-binary reads
of newly written snapshots subsequently pass the separate [owned binary runtime gate](shuttle-pair-terms-binary-runtime-qa-2026-10-20.md).
Primary installation/rollback remains open; this component report does not claim it passed.

## Behavioral proof and review

Two language-specific UI cases fail before the view change, and the new HTTP
persistence case fails before the snapshot exists. After the change:

- All 22 pair-assignment UI cases and all 310 UI cases pass. They cover original
  name/window/cap after current-event changes, English/Russian duration text,
  malformed snapshots, explicit consent, receipt recovery and legacy display.
- 31 affected PostgreSQL roots pass, including native/agent handoffs and snapshot
  recovery after event change/restart. A separate two-root compatibility run passes
  both new and legacy document cases. The legacy case confirms two passes and one
  canonical receipt, with the old document still lacking `display_terms`.
- The full Go command across `cmd`, `internal`, `migrations` and `integration`
  passes 1,061 roots with zero failed tests and six recorded skips on Windows.
  That run started before the additional legacy-document case was added; the
  separate compatibility run proves the final added case. Product source did not
  change between those runs.
- All five executable Linux process roots pass separately: watchdog exit/disarm
  and four external-avatar process cases. The sixth Windows skip is the
  subprocess-only watchdog helper, not an independently invoked acceptance test.
  This is normal Linux testing, not race coverage.
- Affected API/task units, lint and Linux cross-build pass. Isolated Edge component
  checks with the existing test backend pass both languages, one confirmation each,
  unchanged original preparation and zero page errors. Manual Russian screenshot
  inspection confirms the human event/window/cap, financial terms and local dates.

The first legacy-case assertion incorrectly expected one native history marker;
`prepare` and `confirm` correctly create two. The corrected test preserves the
original failure. The first Linux runner exhausts its 256-MiB temporary build
filesystem before tests, without OOM. A separate retry places build work on a
disk-backed owned volume and passes under the same 2-GiB memory limit. Both
containers and their volumes are removed.

Fresh ordinary and code-quality source reviews finish clean. A packager review
finds that review evidence must be consumed, not synthesized by packaging; the
packager now checks completed review identities, an empty finding list and exact
source hashes. Its final independent review is clean. No fixes were staged.

## Candidate and cleanup evidence

The package copies the verified Linux binary over the exact mvp74 base, retaining
its runtime configuration, license files and SIGTERM signal. A nonrunning owned
probe verifies the image's binary and is then removed.

- Image: `sha256:9cbe199071fbcdad221aa9a4c6a9cedd16158c416af0e94d86ed8b224e553614`.
- Binary SHA256: `df2ab626c23820688a398ef37dad498471c01cc418c0f7200c02e0f4bac75878`.
- Product source SHA256: `cc71c43c9657d0323d4a21e5980e432e005f6e84affd2c400e1bd29223bc658b`.

Local ignored evidence is in `.cache/shuttle-work/pair-terms-presentation-v1`:
PostgreSQL JSONL/results, UI/unit/lint/build logs, both Linux attempts, screenshots,
source reviews and image proof. The temporary PostgreSQL stops with code0 and no
OOM, and its volume is removed after fully decoding a default-database archive.
This is not archive restoration acceptance. Per-test databases use test cleanup.
No temporary container from this stage remains; the primary is preserved.

Both binary directions pass the linked isolated runtime gate. Primary installation/
rollback now passes in the separate report. Next verify remaining deadline/failure paths and whole orders/meals/massage journeys. The
[new receipt API](shuttle-payments.md#original-receipt-reads) and earlier
[mixed runtime gate](shuttle-joined-mixed-runtime-qa-2026-10-20.md) remain separate
accepted stages. This check does not accept unattended model comprehension, real
Telegram WebView, a full Linux/race run, remote CI or production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
