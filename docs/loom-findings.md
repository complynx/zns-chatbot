# Loom integration issues

Confirmed findings are maintained as GitHub issues in `complynx/loom`.
The Loom agent can record its bug/feature decision and resolution there.
This file is an index; issue discussions are the authoritative response history.
All six reports below are published; their original runtime observations retain their dated limits.

- L01: [Caller restart causes transient opaque 401 for a valid delegated run](https://github.com/complynx/loom/issues/2).
  Published reproduction, observed 200 → 401 → 200, impact and the Shuttle retry
  mitigation. Local acceptance evidence remains in
  [the stand report](shuttle-live-test-2026-10-13.md).

- L02: [Operation binding seal changes after JSONB argument key reordering](https://github.com/complynx/loom/issues/3).
  Native registration module_call stopped in approved before dispatch. Read-only
  reproduction proved persisted seal mismatch and canonical argument seal match.
  See [registration adapter acceptance boundary](shuttle-agent-registrations.md).

- L03: [File worker silently renders standard-font PDF text as a blank page](https://github.com/complynx/loom/issues/4).
  Native text extraction succeeds, but an unembedded standard Helvetica page
  renders white. The worker has no font faces; pdftoppm logs missing Helvetica
  yet exits zero. Reproduced on our owned isolated pinned worker; no developer
  checkout/stand changes. PDF-to-image visual acceptance remains failed.

- L04 (upstream closed after merged fix; installed acceptance separate): [Delayed execution recovery after SIGKILL](https://github.com/complynx/loom/issues/19).
  Real owned-stand observation found an expired run lease and orphan running
  River job 117 seconds after Loom restart, without a new inference. Source
  analysis suggests a much longer queue-rescue threshold; eventual recovery
  was not measured. The report includes the requested bounded-recovery contract.
  Published on 2026-10-21; [original report](shuttle-loom-abrupt-exit-qa-2026-10-18.md).

- L05: [Native prepared-operation adoption](https://github.com/complynx/loom/issues/20).
  The generic contract is merged in Loom revision `34f46468`. The Zinusya
  inspector and supported knowledge suggestion handoff passed actual-IAM/Loom
  acceptance, including native/agent races, repeated receipts, restarts and
  rejection. Other module adoption and primary installation remain separate.
  [Consumer acceptance](shuttle-loom-adoption-acceptance-2026-10-21.md);
  [historical proposal and failed reproduction](shuttle-loom-native-operation-adoption.md).

- L06 (upstream closed after merged fix; installed acceptance separate): [Consent after moderator revocation](https://github.com/complynx/loom/issues/21).
  Actual owned-stand observation retains `tool_failed` and an unexecuted Loom
  operation in `approved`; no article/review effect occurs. A separate authority
  probe returns 403 and the agent reads disabled rights. The generic error alone
  does not prove its cause. Safe error classification and the fate of old consent
  after rights restoration need a contract and acceptance; maintainer
  bug/feature classification is open. [Original report](shuttle-knowledge-revocation-runtime-qa-2026-10-20.md).

Not filed as Loom bugs: Huma's extra discovery `$schema` field was corrected in
Shuttle; missing administrator role assignment in Loom's IAM project was corrected
in the QA fixture. Both are adapter/configuration responsibilities. The later artifact disclosure
flag defect was also fixed in Zinusya: permitted fetch must declare processing
capability while Loom retains its live run/user disclosure gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
