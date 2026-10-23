# Protected registration details: Mini App backend

The working tree implements private legal-name and passport-number forms and
their transactional Mini App backend.
Trusted Telegram WebView,
unattended model interpretation and remaining registration/pass workflows remain open.

## API

Routes are registered only when the existing registration-details protector is
configured. They use the same verified Telegram Mini App session and server-side
IAM verification as the [profile form](shuttle-profile-entry.md). No IAM redirect
or caller-supplied owner ID is accepted.

- GET /app/registration-details reads current version and presence flags.
- GET /app/registration-details/operations/{operation} reads an immutable owner
  receipt. A missing receipt returns404 for a current session; an unavailable
  session returns401. A receipt lookup does not submit or retry a write.
- POST /app/registration-details/legal-name saves one legal-name value.
- POST /app/registration-details/passport-number saves one passport-number value.

Each POST accepts operation_id (nonzero UUID), expected_version (required integer)
and value. Unknown fields and invalid values return400 with a fixed error.
Mutations return409 for a stale version or reuse of an operation ID with a
changed canonical request. Retry an uncertain write with the same operation ID,
version, field and value. Successful replay returns the original receipt even
after newer edits. Read current status separately after recovering that receipt.

Success bodies contain only version, legal_name_present and
passport_number_present. Stored private values are never read back or prefilled.
Cache-Control is no-store; POST requires the configured Origin and JSON content
type. Without a protection key the routes are absent: GET returns404 through the
existing page fallback, and POST returns405 from the existing method router.

## Transaction and history

The public API verifies IAM before entering the business transaction. The store
then locks the owner, locks the session and checks its current binding,
revocation, generation and expiry. The shared agent/form mutation replays before
CAS, or encrypts the updated record, records a redacted operation receipt and
fulfills eligible persisted data expectations. The Mini App action journal is
written before the same commit. A journal failure rolls all these changes back.
Logout and local revocation serialize with this transaction.

This global owner operation does not lock events. It shares the encrypted record
and expectation fulfillment with the agent API. A legal name is distinct from
the display/profile name. Saving required fields does not create a registration,
assign a pass or express event/role/payment consent. Historical replay does not
fulfill newer expectations.

Migration37 adds shuttle_miniapp_details_actions. It stores only owner,
operation ID, field and completion time, with a foreign key to the redacted
operation receipt. A repeated form operation creates no second action or fresh
completion time. The next user-triggered agent task receives at most five recent
owner actions predating that task, with historical presence/version from their
receipts. This history contains no private values or session credentials and is
explicitly marked as context, not instructions or consent. A form write does not
start Loom proactively.

Down migration locks the journal before checking retained history. It refuses to
remove a nonempty journal, including an insertion committed while rollback waits
for the lock. Export retained history before attempting rollback.

## HTML forms and recovery

The existing Mini App page now has RU/EN single-field forms with explicit Save
buttons. Both inputs start blank; the page reads only presence/version. Native
required validation blocks empty form submission, and Enter submits the focused
field's form. Saving or recovering one field clears that field without destroying
an unsubmitted draft in the other field. Both drafts and the in-memory submitted
body are cleared on logout attempts, session denial and reconnect. Inputs opt out
of autocomplete, spell checking and automatic capitalization; accessible
presence/help text is associated with each input.

Before a private POST, the page persists only owner, operation_id,
expected_version and field in sessionStorage. The value and complete request body
stay in memory. If this metadata cannot be persisted, no POST is sent. This is
separate from the existing profile form's nonprivate draft behavior. An uncertain
POST keeps an immutable in-memory body; the matching Save button repeats those
bytes with the same key. The other field cannot submit until the pending result
is resolved. Module disablement blocks continuation without discarding metadata.

After reload the page reads current presence and checks the operation receipt.
A successful receipt establishes the historical outcome; a separate read refreshes
current presence/version. Missing receipt404 with the same current version keeps
the original key and requires explicit reentry of its original value. The page
never invents or automatically submits a replacement operation. If a different
value is reentered while the original eventually commits, server request hashing
rejects conflicting reuse; receipt recovery reports the earlier saved request.

A missing receipt observed after a newer monotonic owner version means the old
expected-version CAS cannot apply. The UI reports that conflict and retires its
recovery pointer. It sends no new write; any later save requires a new explicit
user action using the current version. Lookup failure or an inconsistent version
retains the pending pointer. Validation/conflict responses are reconciled through
receipt/status reads before further input is enabled.

A new Telegram launch preserves only safe pending metadata until the server
identifies the owner. The same owner can recover; a different owner never queries
that earlier receipt. Logout retains the pointer for a later same-owner login,
while clearing private drafts immediately even if logout temporarily fails.

## Compatibility

The
poller and bot were drained/stopped; a fresh private database dump was verified
before migration.
The empty journal was the
only new table.
Settings, mounts, limits and security were retained, adding only
the optional local encryption key.
The same poller was
restored and completed a fresh successful cycle.

The first-activation guard checks both effective environment and semantic YAML
key presence, and refuses to replace a key or retained protected data/receipts.
The key is retained in a private service env_file referenced by the recorded
Compose override; later Compose commands need no extra secret CLI argument.
Keep this key with backups; never regenerate it after protected writes. No schema
Down or backup restore was performed. Unknown Compose outcomes are recovered by
observing the same candidate rather than recreating it.

A delegated real Zitadel owner token read both profile and redacted details with
no business mutations.
The probe receipt is bound to the activation ID, exact
candidate fingerprint, key hash, subject, target and timestamp.
This is an owner
API probe, not mixed-flow or trusted WebView acceptance.
No production, commit or push.
