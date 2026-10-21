# Knowledge consent after moderator revocation, 2026-10-20

Status: a bounded safety observation passes; complete workflow acceptance is not
claimed. Published as [Loom issue #21](https://github.com/complynx/loom/issues/21)
on 2026-10-21. The actual Loom result is retained, including its nonterminal operation
state. No autonomous-model, real-Telegram or production-readiness claim is made.

## Later published-runtime acceptance

Published Loom `34f46468` subsequently passes the separately recorded
[bounded revocation/restoration lifecycle](shuttle-knowledge-revocation-qa-2026-10-22.md)
with actual IAM, original-operation preservation and fresh consent. That result
does not rewrite the old observation below or prove its hidden failure cause.

## Owned runtime and sequence

The disposable `kb-revoke-r1` stand used actual PostgreSQL, Zitadel and Loom, mock
Telegram and a controlled synthetic provider. Shuttle used the tested mvp83 image
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`;
Loom used `sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7`.

1. An ordinary participant submitted one native suggestion. No article existed.
2. A distinct named moderator used an actual Loom run to read its frozen body and
   prepare a review. The original exact proposal waited for plain-text consent.
3. An administrator revoked that named moderator assignment before consent.
4. A bot-only graceful restart stopped in 0.625 seconds with exit code 0, retained the
   same bot container and preserved all companion fingerprints. The original
   task, run, approval and backend preparation survived unchanged.
5. The moderator sent plain-text consent. `interaction_resolve` returned exactly
   `{"error":"tool_failed"}`. The original Loom operation remained `approved`;
   this is not a terminal failure receipt or evidence of execution.
6. A separate fresh-ID preparation probe returned 403 with
   `current knowledge authority required`. It created no preparation and did not
   renew or change the original one. This probe was not an execution retry and
   does not identify the cause hidden inside the earlier generic Loom error.
7. The same Loom run then read current capabilities (`moderator=false`) and the
   original operation, which had no completed backend receipt. The controlled
   provider could explain the failure using those explicit reads.

The final snapshot has zero review effects, zero articles and zero revisions;
the original suggestion remains pending. There is one Loom run, and the visible
explanation is delivered once. The original preparation is unchanged throughout.

Private request intents, model inputs, exact operation snapshots and readback are
retained under `.cache/shuttle-work/knowledge-revocation-runtime-v1/`. Source-only
independent reviews cover both the observer and its cleanup. The observer
explicitly does not use the earlier speculative `approve_denied` helper.

The three databases were exported and fully decoded. Final Telegram state/journal
and service logs were retained; all owned containers, volumes and networks were
removed. The primary stand was preserved and cleanup recorded no errors.

## Meaning and limits

The revoked actor did not publish in the exercised sequence, including after the
bot restart. The API independently denied current review authority. The agent
could inspect current permissions and the original operation before responding.
These facts do not turn the generic tool error into an authorization-specific
receipt, and do not prove every possible revocation race or future recovery path.

Read-only local source at Loom commit
`a958185e8f81baf49a9cbee492adebd511518995` is consistent with a pre-dispatch
capability change: Shuttle's dynamic discovery removes `review` when current
editing authority is absent, and the bridge resolves current methods before
preparing/dispatching. Loom's engine maps unclassified tool errors to
`tool_failed`. This source explanation is separate from the pinned-image runtime
observation; no trace proves the precise internal error behind that response.

The preserved `approved` state needs an explicit recovery/consent policy. This
probe does not restore moderator rights, retry the approved action, or test
whether an old consent could later be reused. It must not be reported as a
completed terminal-rejection or consent-invalidation gate.

## Proposed Loom follow-up

This is a contract/diagnostics request for maintainer classification, not an
assertion that the current behavior violates an existing Loom contract. The final
wire shape is open for agreement.

- Return safe structured failure classification and a bound operation locator
  where available. Distinguish changed authority or missing current method from
  transport uncertainty and terminal backend rejection when that distinction is
  known. Do not expose credentials, raw SQL, private URLs or backend stack traces.
- Preserve safe backend rejection code/classification when the module supplies
  them. An opaque terminal `failed` result is different from a transient tool
  exception and from `unknown`; none should cause a new business operation.
- Specify the durable fate of an approved, undispatched intent when current
  authority/method availability disappears. Do not silently execute it after
  rights return. Require fresh exact consent or an explicit documented equivalent
  that preserves the user's current intent and original backend identity.
- Reconcile the original operation after uncertainty. Never extend its expiry,
  invent a replacement ID or infer that generic failure means no effect.
- Test revocation before approval, revocation between approval and dispatch,
  rights restoration, bot/Loom restart, cancellation and expiry. Verify no effect
  while unauthorized and no automatic revival of old consent after restoration.

Until that behavior is specified and exercised, the safety observation and
permissions readback remain accepted separately from the full recovery workflow.
