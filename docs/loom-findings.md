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

- L03 (historical failure; bounded consumer acceptance passed on 2026-10-15):
  [File worker silently renders standard-font PDF text as a blank page](https://github.com/complynx/loom/issues/4).
  The original fontless worker rendered an unembedded Helvetica page white while
  text extraction succeeded and pdftoppm exited zero. That failure is preserved.
  A later owned-stand run on unmodified Loom `1050d74e` and its font-equipped
  worker fetched the same Telegram source by opaque file ID, extracted text and
  rendered a readable PNG. [Consumer acceptance and image provenance](shuttle-loom-upstream-qa-2026-10-15.md).
  This covers that synthetic document and version; it does not establish a new
  end-to-end file-processing pass on Loom `34f46468`, OCR or arbitrary fonts.

- L04 (upstream closed; bounded consumer runtime acceptance passed): [Delayed execution recovery after SIGKILL](https://github.com/complynx/loom/issues/19).
  Published Loom `34f46468` passes the actual-IAM pending-inference crash path:
  only the owned executor is killed; the original task/run/session recover
  automatically, original unknown exposure is retained, and one response is
  delivered. Recovery is observed by 16.429 seconds after restart in this fixture,
  not a general SLA. [Consumer report](shuttle-loom-abrupt-recovery-qa-2026-10-22.md).
  The [original 117-second failure](shuttle-loom-abrupt-exit-qa-2026-10-18.md)
  is preserved. Other crash windows remain separate.

- L05: [Native prepared-operation adoption](https://github.com/complynx/loom/issues/20).
  The generic contract is merged in Loom revision `34f46468`. The Zinusya
  inspector and supported knowledge suggestion handoff passed actual-IAM/Loom
  acceptance, including native/agent races, repeated receipts, restarts and
  rejection. Other module adoption remains separate; the [primary installation](shuttle-candidate-install-qa-2026-10-22.md) is accepted.
  [Consumer acceptance](shuttle-loom-adoption-acceptance-2026-10-21.md);
  [historical proposal and failed reproduction](shuttle-loom-native-operation-adoption.md).

- L06 (upstream closed; bounded consumer runtime acceptance passed): [Consent after moderator revocation](https://github.com/complynx/loom/issues/21).
  Published Loom `34f46468` passes the actual-IAM pre-dispatch method-loss path:
  old consent is invalidated, the approval rotates without changing original
  operation/terms/expiry, rights restoration and graceful restarts do not execute,
  the stale approval is rejected, and fresh consent produces one original effect.
  [Consumer report](shuttle-knowledge-revocation-qa-2026-10-22.md).
  Other revocation windows remain separate. The [original generic-error observation](shuttle-knowledge-revocation-runtime-qa-2026-10-20.md)
  is preserved; the later result does not establish its hidden cause.

- L07 (feature/contract request; source-backed, runtime acceptance pending):
  [Durable trace context across queued execution and recovery](https://github.com/complynx/loom/issues/27).
  Current Loom has HTTP propagation and worker spans. The remaining request is
  bounded persisted causal context, documented parent/link policy, later-input
  treatment and recovery links across River delivery. No new runtime failure is
  asserted from source inspection. [Contract and acceptance requirements](shuttle-loom-durable-tracing.md).

Not filed as Loom bugs: Huma's extra discovery `$schema` field was corrected in
Shuttle; missing administrator role assignment in Loom's IAM project was corrected
in the QA fixture. Both are adapter/configuration responsibilities. The later artifact disclosure
flag defect was also fixed in Zinusya: permitted fetch must declare processing
capability while Loom retains its live run/user disclosure gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
