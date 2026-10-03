# Code QA397

Verdict: PASS for the assigned source-review scope. No actionable findings.

Candidate: `907ae621eb957f782747cbb54eaffd961fffe058`
Base: `22ee814eb9d70e900a1e9dc909ac70e898eedeaf`
Worktree: `.worktrees/d-product-boundary-20261003`

## Scope and integrity

Reviewed the exact six-file diff: sandbox `edit_delay.go`, `fake.go`,
`provider_fault.go`, `provider_fault_internal_test.go`,
`provider_fault_integration_test.go`, and `send_document.go`.
HEAD matched the candidate and the developer worktree was clean before and
after review. The merge base of the supplied commits matched the supplied base.
Consulted AGENTS.md and stable migration/coordination contracts, plus relevant
existing sandbox admission, authorization, persistence and Telegram callers.
No prior review reports, author reports, gate evidence or operational history
were read.

## Assessment

- Bounds and lifecycle: provider_fault.go validates count 1..16, lifetime
  1..600 seconds, method/chat/forum selectors, and retained-state invariants.
  New arms permit one active case and eight retained cases. Exact arm replay
  returns the original case without extending its deadline or replenishing its
  count. Expiry is observed without persisting an invalid terminal state.
- Security and admission: the new controls reuse the opt-in, separate-secret
  authorization guard. Telegram token authentication precedes dispatch. Text
  decoding, destination/thread resolution and positive edit ID admission precede
  consumption. Documents are parsed and bounded before consumption; the matcher
  excludes unknown synthetic destinations. Receipts retain selector/status/time
  evidence rather than message text, document bytes or credentials.
- Persistence and transactions: command and consumption paths hold the fake
  mutex. Case maps and consumption slices are copied before save; a failed save
  restores the previous control pointer and returns 503. Rejection precedes
  message/edit/file mutation. Startup validates restored controls. Existing fake
  persistence remains the authority; no new SQL or dependency was added.
- Concurrency: edit arming reserves custody under the fake mutex, then performs
  journal I/O without holding it. The reservation remains until arm completion
  or failure. Provider and legacy fault arms inspect that reservation and delay
  state under the same fake mutex. The examined paths do not hold the delay mutex
  while acquiring the fake mutex, avoiding a new inverted lock order.
- Compatibility and complexity: delivery faults are confined to the opted-in
  synthetic fake and the declared send/edit/document methods. Existing Telegram
  clients decode the emitted 429/401 envelopes and optional raw retry_after.
  The change introduces no dependency, gate edit or suppression.
- Tests reviewed: invalid credentials/payload/destinations, selectors, finite
  counts, replay/release/expiry, envelope variants, edit ID admission, concurrent
  edit/provider custody, legacy exclusion, PostgreSQL restart preservation and
  failed consumption save rollback have focused source coverage.

## Limitations

No build, lint, formatting, Go tests, race detector, Docker, SQL or functional
checks were executed. Test source coverage is not an executed PASS. Git identity,
status and diff inspection were read-only; a git diff --check inspection was
also run and emitted no whitespace findings. Command arm/release save failure
and simultaneous delivery consumption were assessed through the common locked
copy/save path rather than executed fault scenarios. This verdict does not
replace independent Functional QA or required automated gates.

Written by code_qa_397 (Codex/desktop harness)
on behalf of Daniel Drizhuk
