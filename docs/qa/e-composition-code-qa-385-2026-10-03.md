# E composition Code QA385

Date: 2026-10-03. Verdict: source-only FAIL. No operational or Functional
acceptance is claimed.

The fresh independent Codex reviewer examined complete original requirements,
source, reference implementations, diffs, dependencies, native provenance and
tests. Packet SHA256:
`69fd49146c5bd5e9fa1c3e89d84e6f43d4728d84bc55e8e0e236dc4202aa185d`.
All 59 packet files and 42 frozen inputs matched before and after review.
The reviewer performed no tests, container, SQL or UI operations. Its exact model
slug was unavailable in exposed metadata; the harness was Codex.

Two P1 findings were confirmed against the source and original contracts:

- Runtime profile admission requires running NetworkIDs while the owner can
  legitimately expose CREATED profiles with empty NetworkIDs before START.
  This check runs before readiness evaluation and can terminate observation
  before the original readiness deadline.
- Fake admission exempts an older healthy START timestamp from the required
  fresh observer-origin window. A new observer can therefore accept a stale
  START instead of the single fresh recovery START.

The complete 70-test Linux source gate passed before review. This remains local
test evidence only; it does not override these source findings. The original
four recovery functions remain byte-identical. A separate successor must retain
the original tests and contracts, cover the native CREATED-to-running transition
and stale-start rejection, then pass complete gates and fresh affected review.

Detailed immutable source packets, reports and custody receipts remain in
ignored `qa.local/code-qa-385/` and the assigned E successor directory. Current
assignments and estimates remain only in `management.local`.

Written by Codex (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
