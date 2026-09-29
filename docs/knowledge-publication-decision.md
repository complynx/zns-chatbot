# Publication of proposals derived from private context

Status: product decision approved by Daniel on 2026-09-28. Implementation and independent acceptance are pending.

The current privacy contract prevents another person from reading model output
derived from private correspondence or private memory without an explicit
publication boundary. A review grant alone must not expose that context.

The current UI can nevertheless label such a proposal as waiting for review,
while no other reviewer can see it. An established conversation can supply
private history even when the proposed fact itself is intended for shared use.
This leaves a legitimate suggestion without a usable review path.

## Approved flow

1. Create a complete, self-contained proposal text and keep it private to its author. Show its exact text and
   destination, with an explicit explanation that it has not reached reviewers.
2. Offer the author a manual **Send this text for review** action. Bind consent
   to the immutable proposal ID, version, text, destination and author. A model
   tool call cannot grant this consent.
3. After consent, authorized reviewers of that destination can read that exact
   proposal and approve or reject it. They cannot read the author's original
   correspondence, memory, attachments or unrelated generated output.
4. A separate review approval publishes the exact proposal. Preserve original
   provenance and live source checks throughout; consent is not a source grant
   and cannot revive a retired proposal. Changed text or destination requires
   new consent. Retried/stale/foreign callbacks cannot create extra effects.
5. Expose the same state through typed APIs and tool help, with EN/RU UI and
   independent checks for owner isolation, review grants, stale versions,
   restart/replay, source retirement and permission restoration.

The decision concerns intentional disclosure of the selected proposal body to
reviewers. It does not authorize disclosure of the underlying private sources.
