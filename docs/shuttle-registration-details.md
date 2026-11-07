# Registration details and event prerequisites

Owners may change their legal name and passport
data at any time, including after pass assignment or payment. No approval or freeze
is required. The ordinary profile display name remains a separate field.

## Contract

The specification separates Telegram metadata, the independently chosen profile
name and legal registration data. Existing /name, profile mutation receipts and
profile Mini App behavior keep their current meaning. Python passes.py requests
legal name and passport number when event.require_passport is true and skips
both when false. This is behavior evidence, not a storage/logging pattern to copy.

Store one owner-scoped legal-details record separately from UserProfile. Event
registration rules have explicit optional legal-name/passport requirements in DB;
absent requirements preserve current Go events. Requiring a passport also requires
a legal name. Readiness reports only missing field identifiers and versions.
Existing successful registration receipts replay before current requirements or
deadlines. Existing registrations and passes are not cancelled by changing rules.
New joins and new pass assignments check current requirements after event/owner
locks. Keep event -> owner lock order; never continue a registration transaction
while holding a profile/details owner transaction open.

A typed details request now retains owner, event, shown event version, full required
field flags and original expiry.
It holds no passport value, registration or
capacity reservation.
The protected details write closes qualifying expectations
in the same transaction.
Changed rules or
expired requests require fresh choices; successful details stay saved.
Form writes
record redacted context without starting a proactive Loom run.

## Dedicated storage protection

The initial internal/registrationdetails component uses AES-GCM for a complete
serialized record, with authenticated owner identity and fresh random nonces.
HKDF separates encryption and receipt keys from one persistent32-byte deployment
secret. Receipt fingerprints use HMAC over a canonical normalized mutation and
owner; receipts contain only version and presence flags. Back up this secret with
the database. Replacing it without re-encryption breaks old data decryption and
request replay; key rotation is not implemented by this slice.

This protection applies to dedicated details storage, not every chat copy. A
passport sent in Telegram or to the agent already enters their ordinary message
history. A Mini App can avoid those chat copies. Do not automatically copy saved
passport values into prompts, public profile, receipts, action journals, logs,
traces or success messages. General context carries presence/readiness only.
Never reinterpret an existing profile name as a legal name.

Rollback locks the receipt and details tables before checking the export guard,
so a concurrent first write cannot commit unseen and then be dropped.

## Owner API and startup configuration

The optional zinusya_registration_details module exposes get, set_legal_name, set_passport_number and operation under /internal/agent/registration_details/. Without the configured protection key, the module and routes remain absent.

Configure a persistent, randomly generated32-byte secret as unpadded canonical
base64url in `registration_details_key`, preferably through the secret environment
variable `SHUTTLE_REGISTRATION_DETAILS_KEY`. Environment overrides YAML. The key
has no generated default. Invalid encodings/lengths fail startup without echoing
the secret. Keep it with backups; removal disables the API, while changing the key
without a data migration breaks decryption and exact write replay.

The bearer token determines the owner. Callers cannot select another owner's
record. Mutations carry an operation UUID, matching idempotency key, expected
record version and one explicitly supplied value. Reads and successful receipts
return only `version`, `legal_name_present` and `passport_number_present`. Operation
lookup and duplicate writes retain the immutable historical redacted result after
later edits or a service restart. Revoked owners cannot read, mutate or recover
receipts. Missing status is unknown and does not authorize a new operation after
an uncertain write.

Framework validation errors are sanitized on these routes before serialization:
neither invalid field values nor malformed request bodies are returned. This does
not redact the original Telegram/Loom input history. It also does not implement
owner-value read-back, a registration continuation or a form. Event-policy gates
are implemented separately below.

## Event requirements and shared mutation gates

Under the existing registration rule document, `details_requirements` is optional:

```json
{"details_requirements":{"passport_number":true}}
```

This is a policy fragment; the other required registration settings remain
required. Omitted flags default false. Requiring a passport normalizes legal-name
requirement to true. A legal-name-only policy is supported. Null flag values,
wrong types and unknown fields are rejected. An absent policy preserves the old
serialized rule document and its behavior. Huma's HTTP schema and the agent
manifest both permit single-flag policy objects.

New solo joins and registrations created by accepting a pair invitation check
missing legal fields.
An existing registration is preserved when rules change;
a repeat join can return it without requiring new details.
New pass assignments
check current requirements even for those earlier registrations.
Both solo and
pair preparation check readiness without reserving capacity; confirmation checks
again.
A pair remains entirely unassigned if either owner lacks required fields.

These checks run in the event transaction after owner SHARE locks. Details writes
hold that owner's UPDATE lock, so they cannot change readiness while an accepted
effect commits. Successful immutable operation receipts replay before new event
requirements; changing rules does not cancel old registrations or passes.
Missing-field errors contain field identifiers/version internally and a static
public message, never private values. A public profile name does not satisfy the
separate legal-name requirement. Context/list outputs advertise the new optional
policy as well as event administration schemas.

Older binaries reject unknown rule properties. Before rollback after storing new
policies, provide a compatible binary or an explicit preserving data transition;
do not silently strip requirements to restore access. Migration35's nonempty-data
rollback guard still applies to the protected legal records.

## Durable details request foundation

Migration36 adds redacted request history and small immutable begin-operation
receipts. `BeginDetailsRequest` takes an explicit event ID and expected version.
It reuses a matching live expectation, with a fixed original two-hour expiry
capped by sales close. A new begin operation can refer to that same request;
replaying an older operation returns its original snapshot even after completion,
expiry or event changes. `DetailsRequest` reads current owner metadata, while
`DetailsRequestOperation` recovers the historical receipt. These semantics differ
intentionally: receipt replay does not refresh a request or its deadline.

Requests capture normalized required flags and the event timezone. They contain
only identifiers, versions, flags and timestamps. Expired or superseded requests
are not fulfilled by later legal-data writes. A fulfilled request remains history;
it does not authorize registration or establish current event eligibility. The
current missing-field snapshot can differ from historical completion after data
removal. A later continuation must recheck current event version, enabled module,
sales close, owner access and saved details before showing fresh choices.

Begin locks the event, takes a compatible owner SHARE lock, then serializes begins
using an owner-specific transaction advisory lock. Details writes take owner UPDATE
and atomically save the protected record, immutable field receipt and completion
of matching unexpired requests across events. They acquire no event locks. This
keeps request fulfillment distinct from event-dependent continuation and prevents
partial data/expectation commits.

The tests cover original deadline reuse, restart and historical receipt recovery,
expiry and event-version replacement, concurrent begins, owner/revocation
isolation, redaction, atomic rollback on fulfillment failure, separate event
deadlines and cross-event completion, protected migration rollback, the controlled
administrative race, and neighboring legal-details/pass behavior.

The next transport layer must keep Mini App authentication/session validation and
private writes in one transaction. An event-dependent Mini App operation must use a compatible owner SHARE lock
with session validation after locking its event. The existing owner UPDATE
helper is unsuitable here: event-first alone does not remove the administrative
actor SHARE/event lock cycle. Private field writes remain event-free and take
owner UPDATE. Do not store private draft
values in sessionStorage or bot checkpoints; unlike ordinary profile edits, only
request/operation IDs and safe version metadata may persist in the browser.

## Owner request API and agent context

The owner request API adds five methods to the optional
`zinusya_registration_details` module. The configured persistent protection key
is still required to expose this module and its routes.

- `readiness`: explicit event ID; returns event/version/timezone, sales close,
  details version and missing fields. `ready` means only required fields exist;
  it is not consent, capacity, or pass eligibility.
- `begin_request`: explicit event ID/version, nonzero operation UUID and identical
  idempotency key. Creates/reuses the durable expectation without extending its
  original deadline or performing registration. Its status endpoint is
  `/internal/agent/registration_details/request_operation`.
- `request`: owner-only current snapshot by request ID.
- `request_operation`: immutable begin receipt by operation ID, including the
  original envelope on the status endpoint. `not_found` means unknown outcome;
  retain the same operation and arguments when retrying.
- `requests`: UUID-ordered pagination over nonsuperseded owner requests, including
  expired/completed history; default/max20 per page. Follow `next` until absent.

Requests and readiness expose metadata only. Existing field-write receipts and
request-begin receipts have distinct endpoints. Known begin conflicts, invalid
arguments or closed/unavailable events are terminal rejections; storage failures
remain retryable HTTP503. All routes use the token-derived owner scope and reject
revoked access. Framework errors on these paths remain sanitized.

Reactive agent prompts include at most five recent nonsuperseded expectations
created before the triggering task. Their states and missing fields are current
snapshots; the creation cutoff does not freeze later fulfillment. Context carries
no legal values and instructs the agent to classify the actual message independently,
refresh current state, and never infer registration consent from data completion.
No proactive Loom run is started when data completes. Existing checkpoint rules
freeze an uncertain outbound prompt for exact retry.

## Protected Mini App backend

The [transactional Mini App backend](shuttle-miniapp-registration-details.md) uses the same encrypted owner record, redacted operation recovery and manual-action context.

## Acceptance still required

The owner API and context component is described above.
Verify
old event documents, missing fields, both completed routes and both mixed
continuations; restart/lost-receipt recovery; concurrent edits, wrong-owner or
unverified delivery, changed rules and closing while locks are held.
Prove
historical successful receipt replay after requirements change.
Inspect dedicated
DB records, receipts, journal and automatic prompts for raw passport copies.
Trusted WebView entry remains a separate user CA/browser gate.
