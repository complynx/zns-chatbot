# Direct Mini App profile entry

The Mini App provides separate first/last name inputs and language selection.
It shares the owner profile with agent onboarding and does not create a Loom run.
The original and optional Latin API fields use the
[shared profile contract](shuttle-profile-expectations.md#original-and-latin-names).

## Interface and shared state

GET /app/profile reads the owner's current profile version and pending name
expectation. POST /app/profile/name and POST /app/profile/language accept an
operation UUID, an explicit expected_version and the corresponding field. Owner,
IAM subject and organization come from the authenticated session; unknown body
fields are rejected. These fixed endpoints do not proxy arbitrary agent tools.
Normal origin/content-type/body-size and fresh IAM authorization checks apply.
No IAM tokens are returned to the page.

Both interfaces use the existing profile mutation and immutable receipt logic.
Setting a name closes the current name expectation, including one requested by
the agent. A stale expected version returns409. An exact repeated operation
returns its original result, even after restart. Reusing its ID with different
arguments conflicts. Saving a bot language preserves the name and expectation.
No conversation or Loom run is created for these form operations. The name
endpoint also accepts the optional Latin pair. Both versions commit atomically;
a Latin-only correction changes the profile version without scheduling IAM
synchronization. The form does not invent a Latin spelling or backfill older
profiles automatically. An original-name edit without a replacement Latin pair
clears stale Latin.

A write rechecks the app capability inside the same transaction as the profile
and operation receipt. It locks the current owner before the session, matching
logout and identity binding. Session expiry, key generation, subject, organization,
state and local owner revocation are checked under these locks. Network IAM calls
stay outside the transaction. Profile GET performs the same final session recheck
as the existing account and session reads before exposing its response.
The [version-74 schema guide](shuttle-schema.md) covers Latin storage and
compatibility with completed version-73 databases.

## Client recovery

The form saves its pending operation and exact arguments in sessionStorage with
the existing per-launch session. An uncertain reply retains that operation for
retry, including after page reload; it does not create a new ID or silently adopt
a newer version. Fields stay fixed while that write is unresolved. Refresh remains
available if the initial profile read failed, without discarding the operation.
An explicit conflict requires refresh/review before another save. Successful
writes refresh current state rather than displaying an old replay receipt as the
current profile. Automatic language uses the browser language fallback; explicit
en/ru preferences update the profile/sign-in labels immediately; existing account
content uses the new language on its next refresh. Server validation remains
authoritative.
