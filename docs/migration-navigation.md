# Go migration documentation

The goal is full Python-to-Go/PostgreSQL parity, architectural stages C-E,
disposable forward import, both independent QA gates and final real integrations.
Implementation, source review, integrated checks and Functional acceptance are
separate claims. A scenario matrix is coverage planning, not a PASS verdict.

## Current work

The ignored [progress](../management.local/PROGRESS.html),
[board](../management.local/KANBAN.html) and
[current handoff](../management.local/resume-2026-10-02.md) describe observed work.
The [estimate](../management.local/readiness-estimate.md) records uncertainty;
[bottlenecks](../management.local/bottlenecks-ai-2026-10-02.md) records the next
constraints. These local files may be absent in a clean checkout. Historical
checkpoints do not establish current stand access, permissions or acceptance.

## Requirements and workflow

- [Full migration and completion gate](go-migration.md).
- [Architectural scope](architecture-refactor-plan.md) and
  [ownership boundaries](architecture-ownership.md).
- [Git development](git-development.md), [coordination](coordination-process.md)
  and [quality gates](code-quality.md).
- [Deferred obligations](../DEFERRED.md): deferral is not acceptance.
- [Parity inventory](parity-current.md): source comparison at its stated commit,
  not a complete behavioral audit or current acceptance report.
- [Functional requirements](fqa-scenarios.md) and
  [evidence retention](qa-evidence-registry.md).
- [One-off import and recovery](import-oneoff.md),
  [identity import](identity-import.md) and
  [production runtime contract](production-runtime-contract.md).

## Retained decisions and evidence

At D finalization, obtain a fresh independent architectural audit of composition,
lifecycle/replacement, FIFO/throttling, partial batch failures, module boundaries
and remaining E obligations. Both stage QA gates remain mandatory.

After the first Go production release, conduct a separate end-to-end UX audit of
registration, queues, manual/agent transitions, forms/payments, batch outcomes,
retries/cancellation, card freshness, EN/RU and mouse/touch. The UX audit is not a
condition of the first release; current rights, ordering and reliability are.

Real API/ASR and Telegram checks follow synthetic checks, use the authorized test
account and CLX Test Bot, and account for provider cost. Keep secrets private.
Retain full old personal history, proposal-scoped manual knowledge publication,
and Telegram-only identity under their respective contracts. Registration
retention is configurable, default ten minutes; repeat inputs do not renew
priority. The architectural plan owns that requirement.

Obsolete September restart/handoff snapshots were removed from the working tree;
their exact content remains in Git history. They are not current source/resource
instructions. Immutable reports in `docs/qa/` retain original scope, failures and
limitations. Superseding a recipe or deleting a redundant owner artifact does not
change a review verdict. Original unique failures and unaccepted runtime evidence
remain protected until their retention dependency is resolved.

Local commits and serialized merges are authorized. Push, publication and
production cutover require separate authorization.
