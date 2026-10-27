# Native event registration: missing legal details

This slice adds no migration.
Trusted WebView, unattended model decisions
and remaining pair/admin/pass/payment workflows stay open.

## User path

The existing /register event and role buttons remain the entry. If that verified
role click cannot register because mandatory legal fields are missing, its response
names the missing fields and shows the data-request deadline in the event timezone.
A `web_app` button opens the existing protected Mini App when its origin and the
optional protection key are configured. It uses the existing authenticated forms;
no identity, private field value or bearer token is placed in the URL. Telegram's
[inline Web App button contract](https://core.telegram.org/bots/api#inlinekeyboardbutton)
applies to these private user/bot chats.

The same response offers a return to event role selection. After saving, the user
makes a new explicit role click; field completion does not register, reserve a pass
or imply payment. A new click on an original unexpired, still-valid role button is
also an explicit choice. A fresh menu presentation is not mandatory. Event version,
owner binding, eligibility and expiry are still checked by the registration store.
The form edits the global owner details; it does not carry or execute a hidden role
choice. An unavailable form produces a clear fallback, not a broken launch button.

## Recovery and shared state

The native task UUID identifies its details-request operation. The registration
transaction detects missing fields while holding the event and owner locks, creates
or reuses the existing durable event expectation, records its immutable begin
receipt and commits that outcome together. Subsequent attempts of the same native
task replay that outcome before any new registration effect. This prevents a task
interrupted before its response checkpoint from becoming a registration after the
owner fills the fields elsewhere.

Canonical successful registration receipts keep their existing argument hash and
replay semantics. The task-specific details operation is a transport guard and is
not serialized into the presented entry or added to that canonical hash. The
expectation retains the original deadline (two hours capped by sales close) and
appears in later reactive agent context. Both form and agent field writes resolve
the same expectation. No proactive Loom run is started.

The details screen reads current expectation status separately from its historical
begin receipt. A completed request can therefore show saved fields after restart;
it still offers an explicit role choice. Expired/superseded choices return to
/register. Final registration always checks current business rules independently
of details completion. The protection key must be retained with encrypted data.
