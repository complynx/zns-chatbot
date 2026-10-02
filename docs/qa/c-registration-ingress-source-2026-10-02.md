# Registration ingress source acceptance

Candidate `12b38e6ce682eb9bafc02ab2ea1b0da1c046f6e7`, base
`257dff1935a971bfb24b79479d4ac56bbf3b476a`, passed independent source-only
Code QA350 with no actionable findings and was locally fast-forwarded.
Scope is four sandbox files: fake provider, durable ingress control and two
PostgreSQL integration test files. No dependency or suppression was added.

The optional keyed synthetic control captures a complete original Telegram
getUpdates batch, preserves bytes/IDs/order, releases it and permits one replay
only after acknowledgment of every original ID. Custody expires durably through
readback, commands, polling and restart. Pending originals survive custody expiry
and precede newer capture. Case, retained-byte and active-control limits remain
bounded; failed persistence restores prior state. Product time and registration
ranking are unchanged.

The developer's Linux worker `3cda3f03` finished21:19:46 UTC, exit0/noOOM:
complete2992 raw-source guards, gofmt, focused real-PostgreSQL race tests,
original pinned sandbox lint and full formatting diff passed.

Actual-root affected integration `2ea891ce` finished21:33:29 UTC, exit0/noOOM.
The same affected commands passed, and actual tracked-root raw hashes matched
the declared consumed baseline before and after. The original tracked inventory
was used; ignored caches were not scanned or copied. The worker and its own PG
used1.5CPU/3584MiB plus0.5CPU/512MiB, with source/tools read-only and separate
owned caches/receipts. This describes the individual check profile, not historical
global slot compliance.

The actual root baseline is not byte-identical to Git-export source:191 files
have newline-only differences and the protected user code-quality document is
a separate declared difference. No bytes were normalized or user edits reverted.
Future fake builds bind separately qualified immutable Git-export bytes and
must verify embedded SQL compatibility with the retained stand ledger.

Code QA inspected tests but did not rerun them. The product test invokes real
Bot.Run and services using previously captured bytes; it does not prove fully
concurrent runtime control. This accepts source and affected automated checks,
not a fake image, live admission, retirement-cause repair, Telegram-like EN/RU
Functional QA, or production. C remains parked with preserved data/evidence.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
