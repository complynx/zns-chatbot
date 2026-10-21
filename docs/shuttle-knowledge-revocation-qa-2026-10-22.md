# Knowledge consent invalidation acceptance, 2026-10-22

The isolated `kb-revoke-r2` stand passes one moderator revocation/restoration
workflow with actual Zitadel, PostgreSQL and published Loom `34f46468`.
Application source is `4f03e5ff`, schema70 / manifest `mvp-91`. Telegram is
simulated and provider tool choices are controlled; this is not autonomous model
quality, real-Telegram or production acceptance.

## Observed sequence

An ordinary participant submitted a native knowledge suggestion. Neither that
participant nor a global administrator without named moderator authority could
prepare its review. A distinct named moderator used actual Loom to read the
frozen content and prepare publication. Exact terms arrived with a reply
keyboard, without internal approval/hash tags. No publication preceded consent.

The administrator revoked the named assignment before the moderator sent plain
consent. Loom observed the now-unavailable method, returned `method_unavailable`
and rotated the approval ID. Its operation returned to `prepared`; the original
backend preparation, operation ID, normalized terms, hash and expiry remained
unchanged. No article, revision or review effect existed.

Rights were restored, followed by bot, Loom API and sandbox/run-worker graceful
restarts. The same original operation remained prepared and the rotated approval
survived. Stop commands completed in 0.906, 0.750 and 0.547 seconds respectively,
all with exit code 0 and without OOM. Rights restoration and restart did not execute the action.

A resolution using the old approval returned `interaction_conflict` and produced
no effect. A separate new user message gave fresh exact consent. The original
operation completed once, creating one article, one revision and one review
receipt. Repeated native status reads returned the same immutable result as
Loom. The final explanation was delivered and the run completed.

## Original failed assertion and correction

The first driver stopped after successful publication because it compared whole
status envelopes. Read-only investigation found only `observed_at` changing;
both immutable results matched the completed Loom receipt. This was an overly
strict QA comparison, not evidence of another publication or a Loom defect.

The original controller and failed observation remain retained. A separately
reviewed completion compares all status fields except `observed_at`, verifies
original terms/expiry and exact effect counts, then sends only the pending final
text. It does not replay consent or publication. Business counts and receipts
remain unchanged after that response.

## Custody and limits

The accepted primary stand remained unchanged. All nine temporary services were
stopped; four database archives were fully decoded and service logs retained.
The owned containers, volumes and network were removed. Evidence and independently
reviewed controllers remain ignored under
`.cache/shuttle-work/knowledge-revocation-acceptance-20261022-v2/`.

This accepts the observed pre-dispatch method-loss path and later fresh consent.
It does not prove every revocation race, uncertain dispatched RPC, local expiry,
cancellation, event-admin role change or other business module. Those boundaries
are separate from this result. [Loom #21](https://github.com/complynx/loom/issues/21)
is already closed for the merged producer fix; this report adds consumer runtime
evidence. The [older failed observation](shuttle-knowledge-revocation-runtime-qa-2026-10-20.md)
remains historical evidence and does not identify its hidden generic-error cause.
