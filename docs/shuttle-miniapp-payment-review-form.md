# Payment reviewer Mini App form

Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

The ru/en reviewer form, payer projection, direct event-context read and
[native entry/reactive context](shuttle-payment-review-entry.md) pass component
checks and are now installed on the owned schema39 test image.
Full real-IAM/Loom/Telegram payment-review workflow acceptance remains open.

## Workflow

After the existing Telegram session starts, the page lists events for which the
current user can review payments. Choose an event and a pending proof. The detail
shows its payer display name, reported amount, remaining balance, balance version,
proof timestamp and deadline. Related times use the event's IANA zone. Names and
reasons are rendered as text. The proof loader makes an authenticated content
request; a separate blob download link allows inspection. No IAM token enters the
browser or download URL. Changing detail, reloading or leaving the account revokes
the blob URL. There is no inline PDF renderer.

Enter a credited amount in the frozen currency's decimal units, or choose rejection
and enter a reason. A rejection submits zero. Decimal parsing uses integer arithmetic,
accepts comma or point and rejects excess fractional digits or unsafe minor units.
The server retains all financial and deadline validation, including reasons for
late exceptions. Preparation saves an exact decision and credits nothing.

The preview displays the saved decision, credited amount, reason and expiry.
A separate explicit button confirms that exact immutable workspace revision and
canonical proposal. Editing inputs disables confirmation until the edit is prepared
or current state reloaded. Cancellation cancels the saved intention and credits
nothing. An agent-prepared workspace is displayed and can be edited, cancelled or
confirmed through the same controls. Completed evidence shows the recorded decision;
its confirm controls are disabled. No tool or agent is invoked proactively.

## Request recovery and lifecycle

Before any prepare/cancel/confirm POST, the form saves one bounded pending record
in tab-scoped sessionStorage under the verified owner's UUID. It contains operation
UUID, kind, event/evidence IDs and the exact body, including any reason. It contains
no IAM or Mini App bearer token. The reason is retained while the request outcome
is unknown; this is deliberate support for exact replay. Unsent inputs remain in
memory. Logout clears rendered data and blob URLs, while a pending record remains
available only to that same owner's subsequent authorized session in the tab.

Reload first checks operation-only status. A404 means unknown and keeps the record;
it does not mean that payment failed. The user may check again or retry the exact
body with its original UUID. No new edit is dispatched while that record is pending.
A successful receipt clears it. A400/409 rejection clears the rejected request and
requires loading current state before another edit. Other failures retain recovery.
Storage failure prevents a new write; a malformed record blocks new writes on the
page instead of silently replacing an uncertain financial request.

Recovery also restores the event and its proof list.
The directly authorized
`GET /app/payment-reviews/events/{event}` supplies the current name/timezone when
the event is absent from the first list page.
It checks reviewer authority and
session in one transaction and exposes only ContextEvent fields.
A completed proof
outside the pending list remains selected as history, while other pending proofs
remain reachable.

## Next acceptance
