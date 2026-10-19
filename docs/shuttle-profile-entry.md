# Direct Mini App profile entry

The working tree adds a name form and language selection to the existing Mini App.
This is part of structured registration/profile parity. It does not establish
complete Telegram registration, agent reply-keyboard questions or mixed workflow
acceptance for complete registration. The combined profile/name image is now running
on the owned stand. Static Mini App assets match the frozen source byte for byte;
trusted WebView sign-in and form writes remain unaccepted. See the
[owned native QA report](shuttle-profile-name-native-qa-2026-10-16.md).

The later [actual Telegram profile-name gate](shuttle-profile-real-webview-qa-2026-10-19.md)
now accepts native name save, new-session persisted read and restoration of the
original name on the installed schema59 / mvp68 image. Two exact profile/action
receipts and two revoked sessions are independently verified. Broader form,
response-loss and mixed-agent acceptance remain separate. The earlier statements
above and component evidence below describe their original checkpoints.

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
No conversation or Loom run is created for these form operations.

A write rechecks the app capability inside the same transaction as the profile
and operation receipt. It locks the current owner before the session, matching
logout and identity binding. Session expiry, key generation, subject, organization,
state and local owner revocation are checked under these locks. Network IAM calls
stay outside the transaction. Profile GET performs the same final session recheck
as the existing account and session reads before exposing its response.
There is no new table, migration or dependency.

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

## Verification boundary

Sixteen Node tests with a simulated DOM/fetch cover the existing entry/account
paths and new profile retry, conflict, pending reload/read failure and automatic
language cases. These are UI behavior fixtures, not a native Telegram WebView test.
Independent source reviews found a pending-write recovery dead end, a missing
post-read session check and stale automatic-language selection. All were verified
and fixed; a fresh reviewer reported no actionable findings. Strict lint passed.

The first Linux/race attempt failed because the disposable PostgreSQL data tmpfs
filled its256MiB limit. The server and test runner exited1 without OOM. Those logs
and terminal states are preserved under ignored .cache/shuttle-work/profile-entry-qa/;
only their verified owned containers were removed. A separate bounded retry uses
a1GiB data tmpfs and a1536MiB PostgreSQL memory cap. The fresh retry passed
70 root tests /131 root-and-subtest pass events: all32 db roots, all11 API roots
and27 selected real-PG integration roots, with no failures or skips. This includes
name expectation completion through the form API, exact replay after restart,
stale-version denial, language preservation, logout denial, forged-owner rejection
and expiry during a blocked profile read. It is not a full integration-suite run.
Source hashes stayed unchanged; the ten main containers retained their fingerprints.
The successful Go and PostgreSQL containers exited0 without OOM and were removed
by verified identity. The final receipt is
.cache/shuttle-work/profile-entry-retry-qa/checkpoint.json.

Real trusted Mini App/WebView entry, native form writes and runtime acceptance of
next-agent Mini App manual-action facts from trusted WebView use remain open.
The [transactional action journal](shuttle-miniapp-action-context.md) is now running
on schema34. A locally signed proof, real IAM/form APIs, native Telegram ingress
and manually driven real Loom passed the [scoped backend/runtime QA](shuttle-miniapp-action-qa-2026-10-16.md);
that does not establish a trusted Telegram WebView launch or unattended model use. Direct Telegram name entry and continuation
between native commands and real Loom profile tools passed the scoped native QA.
Its provider was manually driven; unattended model behavior remains unaccepted.
The form's shared state and component tests must not be presented as those gates.
Quoted-name completion in that historical image is retired by the new
[conversational name-entry contract](shuttle-telegram-name-entry.md); explicit
commands and form writes remain native.
User CA installation and actual Telegram OIDC credentials remain deferred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
