# Loom integration issues

Confirmed findings are maintained as GitHub issues in `complynx/loom`.
The Loom agent can record its bug/feature decision and resolution there.
This file is an index; issue discussions are the authoritative response history.
Unpublished findings below are marked explicitly until a write channel is available.

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

- L04 (local draft, not filed): [Delayed execution recovery after SIGKILL](shuttle-loom-abrupt-exit-qa-2026-10-18.md).
  Real owned-stand observation found an expired run lease and orphan running
  River job 117 seconds after Loom restart, without a new inference. Source
  analysis suggests a much longer queue-rescue threshold; eventual recovery
  was not measured. The report includes the requested bounded-recovery contract.
  GitHub publication awaits an authenticated write channel.

- L05 (local contract proposal, not filed): [Native prepared-operation adoption](shuttle-loom-native-operation-adoption.md).
  Actual Loom reads a native preparation but a supported `module_call` creates a
  distinct operation. The required generic adoption capability is missing; this
  is not labelled a defect in fresh-operation behavior. Exact mixed-path
  acceptance remains open. GitHub publication awaits a write channel.

- L06 (local diagnostics/lifecycle request, not filed): [Consent after moderator revocation](shuttle-knowledge-revocation-runtime-qa-2026-10-20.md).
  Actual owned-stand observation retains `tool_failed` and an unexecuted Loom
  operation in `approved`; no article/review effect occurs. A separate authority
  probe returns 403 and the agent reads disabled rights. The generic error alone
  does not prove its cause. Safe error classification and the fate of old consent
  after rights restoration need a contract and acceptance; maintainer
  bug/feature classification is open. GitHub publication awaits a write channel.

Not filed as Loom bugs: Huma's extra discovery `$schema` field was corrected in
Shuttle; missing administrator role assignment in Loom's IAM project was corrected
in the QA fixture. Both are adapter/configuration responsibilities. The later artifact disclosure
flag defect was also fixed in Zinusya: permitted fetch must declare processing
capability while Loom retains its live run/user disclosure gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
