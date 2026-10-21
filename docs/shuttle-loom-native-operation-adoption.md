# Resume an operation prepared outside Loom

Status: integration requirement reproduced on 2026-10-20. Not implemented or
accepted. This is a proposed contract for discussion with the Loom developer;
the wire format may change. Published as [Loom issue #20](https://github.com/complynx/loom/issues/20)
on 2026-10-21. No Loom source or developer stand was modified.

## Observed gap

A participant can prepare a knowledge suggestion in the native form and then ask
the agent to continue it. Shuttle exposes the original normalized arguments,
proposal ID and expiry through `zinusya_knowledge.operation`. Its context tells
the agent to keep the original operation ID and obtain fresh consent.

The deployed `module_call` tool accepts only `module`, `method`, `arguments` and
`confirmation_text`, with `additionalProperties: false`. A supported call using
the original normalized arguments prepares a new operation instead. The native
operation remains prepared. Reading an original operation is supported; adopting
it for execution through Loom is missing.

This is a missing integration capability, not evidence of unauthorized execution
or a defect in Loom's currently documented fresh-operation behavior. Do not claim
that native-to-agent exact prepared-operation handoff is accepted.

## Runtime evidence

The disposable `kb-adopt-r1` stand used actual Zitadel, PostgreSQL and Loom, mock
Telegram, and a controlled synthetic provider. Shuttle used the tested mvp83
image `sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`;
Loom used `sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7`.

1. One native `suggest/prepare` created an original preparation. No confirm was sent.
2. One actual Loom run read that actor-owned preparation through the script tool.
   Its arguments matched the native preparation exactly.
3. A supported `module_call` for `zinusya_knowledge.suggest` used those arguments.
   Loom waited for consent on a distinct operation and proposal ID.
4. Both operations remained prepared with identical normalized arguments. There
   were zero suggestions and zero published articles. No approval was sent.

The reproduction passes; the mixed-handoff requirement fails. This controlled
provider verifies transport and durable state, not autonomous model reasoning.
Private receipts retain both IDs, original terms, request intents and tool schema
under `.cache/shuttle-work/knowledge-adoption-runtime-v1/`.

The read-only local Loom source at commit
`a958185e8f81baf49a9cbee492adebd511518995` agrees with the observed contract:
`ModuleBridge.Call` has no external operation locator; `Coordinator.Prepare`
allocates a UUID and deduplicates by run/call identity. The runtime proof applies
to the pinned image above; the source inspection is separate evidence.

## Required behavior

Loom must support importing an existing backend preparation into its ordinary
approval dialogue. Implement this generically for module writes, without
knowledge-specific logic or a direct executable write proxy in the script tool.

The backend business operation ID must remain the original one for inspection,
execution, status and retries. Loom may allocate its own internal record ID, but
must persist the distinction and bind approvals to the original backend ID.

Adoption must inspect an existing preparation, rather than blindly calling a
prepare endpoint that could create a missing operation. The module contract must
provide a permission-checked, read-only inspection mechanism and advertise support.
Use the original normalized arguments, description, proposal/preconditions and
absolute expiry. Do not renew the deadline or infer consent from old form state.
Require fresh exact consent through the existing interaction/approval flow.

Validate the current issuer, subject, organization, actor/impersonation, service,
method and contract binding. Verify the backend operation belongs to that actor
and matches the requested method and normalized terms. Treat historical content
and locators as untrusted data. Recheck current permission, lifecycle and revision
at execution through the backend's existing checks.

An expired preparation requires a new explicit preparation and consent. A
completed operation returns its original receipt without another effect. Unknown
or uncertain status must not create a replacement operation. A changed argument
or foreign operation must fail closed, without fallback to a fresh write.

## Candidate tool shape

An optional `resume_operation_id` on `module_call` would let the agent keep one
write tool. This field identifies the backend operation, not a run or approval.
The existing fields can still describe and verify the intended method and terms.
This is a proposal, not an accepted schema; Shuttle must not send the field to
current Loom, whose tool schema rejects it. An explicit generic resume tool is
also acceptable if the final contract documents the same guarantees.

Loom and module manifests must expose capability discovery so an older bridge
cannot silently substitute a new operation. The final contract must define the
inspection request/response and distinguish backend IDs from internal Loom IDs.
A caller-supplied UUID alone is insufficient evidence of ownership or existence.

## Acceptance before enabling

- Native prepare, agent inspect/adopt, fresh plain-text approval, one original
  backend effect, and identical receipt when the native form checks the result.
- Agent preparation remains recoverable in the native form using its original ID.
- Native confirm racing agent approval produces one effect and one backend receipt.
- Wrong actor, organization, method, changed terms, revoked permission, stale
  revision and expired preparation are denied without creating a replacement.
- Repeated tool calls and bot/Loom graceful restarts retain the same backend ID,
  terms, expiry and approval binding. Lost responses reconcile that original ID.
- Cancellation affects only the selected pending action. Free-text clarification
  follows the current dialogue contract and cannot accidentally approve another.
- Unsupported modules/bridges return an explicit unsupported result; discovery,
  reading and independent native completion remain available.

Full mixed-path acceptance remains open until the agreed contract is implemented
on both sides and these gates pass with actual Loom. Separate moderator review
of an already-submitted suggestion is a different operation and does not prove
adoption of an existing preparation.
