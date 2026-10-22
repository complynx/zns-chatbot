# Original order submission operations

Status: source implementation under acceptance, 2026-10-22. The owned primary
still runs application `4f03e5ff`, schema70 / `mvp-91`; it does not include these
order-inspection changes. Real Loom adoption and browser import of an agent
preparation remain open. This is not a production-readiness claim.

## Contract

The delegated-user endpoint `POST /internal/agent/operations/inspect` now accepts
module `zinusya_orders`, method `submit`, and an original operation UUID. It uses
the existing Loom inspection protocol and returns the original arguments,
description, proposal ID, absolute expiry, identity binding and contract revision.
A completed original submission also returns its immutable receipt. Reading the
operation creates no order and grants no consent.

The agent continues a supported submission with `module_call`, setting
`resume_operation_id` to that UUID and `arguments` to `{}`. Loom obtains the
recorded terms and requests fresh approval. The application still checks current
owner and event/module access before inspection and execution. Meal orders and
cancellation are outside this extension and remain explicit unsupported adoption
methods; their existing ordinary tools and forms remain available.

## Storage and recovery

New agent and native preparations record a private `operation_contract` alongside
the public preparation in the existing immutable prepare receipt. The record is
per owner/operation/phase, because several operation IDs can share a proposal.
The API supplies issuer, actor and revision; the transaction supplies the verified
bot, subject and organization. Original localized descriptions are stored in the
same transaction. Replaying preparation cannot change language, provenance or
expiry. Existing public operation reads omit the private contract field.

No schema migration is needed. Old records without this marker retain their
ordinary recovery paths but cannot be adopted. A replay does not retrofit missing
metadata. Inspection does not renew expired preparations; Loom must reject an
expired pending operation and obtain fresh terms and consent. Completed receipts
remain readable with current access even after later cancellation or draft edits.
A matching order from another operation alone does not establish completion of
this operation: its own submit receipt is required.

Canonical results can have a different calculation observation time. Inspection
uses the existing business-condition comparison, which ignores only that time;
prices, selections, services, capacity terms and sales cutoff must still match.

## Acceptance boundary

The first scoped PostgreSQL/HTTP gate passed the new original-operation paths.
An independent review then found a legitimate canonical receipt rejected solely
because its observation time differed. A focused test reproduced the conflict;
the correction uses `sameOrderConditions`. The broader follow-up passed 68 root
tests with no failures or skips, including order, Mini App, meal and knowledge
inspection paths. After the formatter/lint corrections, 18 focused roots passed
with no failures
or skips. The single-connection test was then strengthened to replace the URL
setting and assert `MaxConns=1`; its independent rerun passed. Scoped API, DB,
task and integration lint reported zero issues. Four independent textual review
passes ended without outstanding substantive findings. Local tests did not use
the race detector; full CI and real Loom acceptance remain separate.

These tests use fixture IAM/HTTP peers. They prove original-ID API handoffs,
replay, immutable terms/language, legacy and binding rejection, current access,
expiry, formatter rollback and single-connection operation. They do not prove a
real Loom approval dialogue, autonomous interpretation or a browser opening an
agent's existing preparation. Those are required next acceptance steps. The
native browser now retains its prepare operation UUID at explicit confirmation;
the separate committed UI fix has 28 passing behavior cases.
