# Payment successor — full native PostgreSQL gate

Frozen b747d50dcbcc1efbb36f2e1c084afab3b6a39115, prepared against root0a241c0b.
Root independently verified all ten raw source hashes against manifest SHA256
D90EBCA7D6C29330248AFF567A761296AE4718271E029CFA21D87F6A24B8846D.
Developer offline gates and actual Linux unit race passed; real PG was root-only.

Root session53737 completed exit1. PostgreSQL17.11, exclusive synthetic58451,
unchanged full15 selector, count1, p1, parallel1, original4min test budget.
Package174.577s; all15 parents completed:12PASS/3FAIL. Event totals69PASS,
21FAIL, zeroSKIP:17 failing leaves,3 failing parents,1 failed package.
No timeout or unfinished case. FAIL is not acceptance; no fresh review allocated.

Failure groups:

- OriginalSourceThroughRefreshAndFallback: both fallback leaves fail with
  stale order read at order_read_delivery_test.go:272, invoked at181.
- RestorationKeepsProjection: both opening_receipt_rights_loss leaves fail
  changed.Load assertion at payment_instructions_test.go:850; delivery returned
  nil without the expected receipt hook being observed.
- RetiresRevokedSource: eight pending-opening leaves have nil reference at954,
  four fresh-manual leaves fail stale order read at1024, and superseded-opening
  EN repeats the previous reference at1111.

These observations do not distinguish fixture defects from product defects.
Developer owns read-only coherent source/receipt diagnosis before successor edits.
Preserve original assertions, prior cases and the frozen failed candidate.

Raw worktree-local packet: qa.local/root-native-final/tests.jsonl.
SHA256509CBA18ED33724FA6813E38B59304EEE442631756E8C772352C0542CEECE8D7.
Authenticated parent DB/role/version snapshots before and after are identical,
both SHA256C072BA987E88F88DB0D1D487DB6BB639C51A8DB3886BF7BE888164BEB4450BB7.
Four parent databases, three unprivileged login roles and absent zns_app retained.
58451 released. No secret export or production operation.

Next candidate must run full real PostgreSQL gates under developer ownership
on a unique synthetic fixture, then fresh complete Code QA and source-blind
EN/RU Functional QA. Root-only private credentials are not a reason to skip
the author's independent synthetic PostgreSQL run.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
