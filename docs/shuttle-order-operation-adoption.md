# Original order submission operations

## Inspection and adoption

The delegated-user endpoint POST /internal/agent/operations/inspect supports submit operations for zinusya_orders and zinusya_meals. Given the original operation UUID, it returns recorded arguments, description, proposal, absolute expiry, identity binding and contract revision. A completed original submission also returns its immutable receipt. Inspection creates no order and grants no consent.

Continue a supported submission through module_call with resume_operation_id set to the original backend UUID and empty arguments. Loom adopts the recorded terms and requests fresh approval. The application checks current owner and event/module access before inspection and execution. Meal submission uses its independent entitlement and frozen composition/payment deadline. Cancellation adoption is unsupported; ordinary cancellation tools and forms retain their own contracts.

## Immutable terms and recovery

Native and agent preparations save a private operation_contract alongside the public immutable prepare receipt. It is scoped to the owner, operation and phase because several operation IDs may share one proposal. The API supplies issuer, actor and revision; the transaction supplies verified bot, subject and organization. The original localized description is stored in the same transaction.

Replaying preparation cannot change language, provenance or expiry. Public ordinary operation reads omit the private contract. Old receipts without the authenticated contract retain ordinary recovery paths but cannot be adopted; replay does not retrofit metadata. Inspection does not renew an expired preparation. Obtain fresh terms and consent for an expired pending operation.

A completed receipt remains readable with current access after later draft edits or cancellation. A matching order alone does not prove that this operation completed: its own submit receipt is required. Business-condition comparison may ignore calculation observation time only; prices, selections, services, capacity terms and cutoff must match.

## Native continuation

The approval message includes an inline WebApp link carrying the original event and backend operation UUID. No authentication token is placed in the link. The ordinary reply keyboard remains on the final dialogue message. Opening the form is neither approval nor an answer to the pending question.

An inline launch is required for Telegram initialization data; a reply-keyboard WebApp launch has different initialization behavior. See [WebAppInitData](https://core.telegram.org/bots/webapps#webappinitdata).

After ordinary authentication, the orders or meals fragment reads the original submit receipt first, then the prepare receipt if completion is absent. Opening never prepares or submits. A completed link shows the original order and disables confirmation. A pending link shows the frozen snapshot and requires an explicit click using the original operation UUID, proposal and versions. New draft/menu/event versions or expiry prevent confirmation; a newer saved draft is not consumed accidentally. Local uncertain requests retain priority over linked recovery, and a missing receipt does not authorize a replacement.

## Loom and backend identities

Loom operation L and backend operation N are distinct identities. Approval carries both. Application receipt lookup and form links use backend_operation_id when present; they must not treat L as N. Adoption preserves N's terms, proposal, identity and expiry while L owns the agent approval lifecycle.

If the form completes N while its original agent question remains live, an explicit followup can read N's receipt and resolve that same input, approval and argument hash. L then retains N's original result. Reconciliation must preserve the order, receipts, consumed-draft version, evidence and allocation rows. A routed followup may create a separate input task targeting the same original run; one run does not imply one task row.

Use the [QA procedure](shuttle-qa.md) to check both directions, original receipts, explicit consent and zero duplicate business effects.
