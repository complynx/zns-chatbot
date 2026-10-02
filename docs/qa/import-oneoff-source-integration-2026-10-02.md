# One-off importer source acceptance

## Scope

Importer candidate `e4710e85b5700b1a27b18629e7ed5b859f5cf8c5`, base
`7d55bd809808049cf1ba7bdd7d0a19786711fb3a`, passed independent Code QA347
and was fast-forwarded into the integration branch.
Caller candidate `5e5e20f51cd061aa8bca5894160381726ea0931e`, base `e4710e8`,
passed independent source-only Code QA349 and was fast-forwarded afterward.

The recovery contract is whole-target restore/recreation and complete reapply.
Users, events, orders and messages apply atomically per domain. Partial-record
resume and receipt upgrades are removed. Permanent identity mappings, history
bytes/order, validation, domain dependencies and explicit reconciliation remain.
There is no automatic database wipe or production operation.
The caller now performs seven ordered apply calls and seven explicit reconciles.

## Automated Proof

The importer developer's Linux full gate passed: full race suite 300.933s,
CLI race 1.205s, pinned lint/format, module and source guards. Five focused
real-PostgreSQL atomicity/history cases passed in 18.621s.

Actual-root affected integration container
`d52f4dd27848109d4006258164085117b13acbd8b1bb360103dd154669fe713a`
finished at 20:42:06 UTC, exit0/noOOM. Five focused PostgreSQL race cases
passed in 19.801s; all five CLI tests passed in 1.137s. Compile, pinned lint,
format, module verification and before/after raw-source guards passed.
The worker and owned PG aggregate remained within 2CPU/4GiB. PG was stopped
after zero-client verification; retained data and caches were not deleted.

The original author-to-root raw-byte comparison failed on 32 CRLF/LF
differences, including SQL queries and migrations. No normalization was made.
Actual root inputs were tested against a separately qualified original-path
baseline SHA256 `2858318081511596066c624a9b663b9275d35cac7c0b551e5f4817cf4be43bc7`.
This does not claim identical author/root binary or migration checksums.
An earlier broad metadata scan included an ignored cache and was explicitly
interrupted before compilation or database writes; it is not a passed gate.

The caller developer's bounded Linux gate `4656bf5b` passed 25 tests in25.614s,
syntax, committed-diff whitespace, full clean-before/after and raw-source
checks. Total102.312s stayed within120s. Two earlier deadline failures remain
failures, including one late host stop at133.914s. The reviewer independently
accepted source correctness; its WSL test run lacked the explicit resource
profile and is not counted as a qualified integration gate.

The actual-root caller gate `696c3ec4` finished at21:11:28 UTC, exit0/noOOM,
total10.404s. All25 tests passed in8.681s; syntax, committed-diff whitespace,
exact integrated HEAD and four owned/dependency paths' clean/hash guards passed.
Unrelated root documentation was dirty and is not included in that clean claim.
Source/Git were read-only, receipts disjoint, .2CPU/256MiB/netnone/rootRO.

## Limits

This accepts the scoped source changes, not Functional QA or whole-target
restore/reapply. The caller's affected integration does not run real import CLI.
The historical E205 import used the earlier importer and does not prove the
new binary. A fresh isolated reset/reapply proof must bind its own raw source,
SQL checksums and binary. Runtime admission, EN/RU public UI, persistence and
failure acceptance remain open. No push or production authorization is implied.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
