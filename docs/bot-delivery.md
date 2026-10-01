# Unknown Bot delivery recovery

An unknown attempt retains its ordered chat barrier. It does not prove success
or failure. Ordinary users, event payment administrators and agent tools cannot
resolve it. A trusted local operator with current global administrator membership
uses `deliveryctl`; database access belongs to that operator and is supplied
through `DATABASE_URL`, never argv. The exact configured bot ID is required.

Read exact transport metadata from bounded JSON stdin:

```text
deliveryctl inspect --actor OPERATOR --bot-id BOT_ID
{"operation":"card:123","effect":"view"}
```

Stop and join the existing owned runtime before resolution. The command acquires
the same exclusive bot session lock and refuses a competing sender. A free lock
does not prove an external request cannot still apply. Confirm that the old
sender and sink outcome are quiescent before submitting a disposition.

```text
deliveryctl resolve --actor OPERATOR --bot-id BOT_ID
{"operation":"card:123","effect":"view","key":"operator-recovery-1","attempt":1,"disposition":"confirmed_sent","evidence_kind":"provider_receipt","evidence_sha256":"64_lowercase_hex_digits","payload_sha256":"admitted_hash_from_inspection","message_id":42,"sender_joined":true,"sink_quiescent":true}
```

`confirmed_sent` requires a genuine provider receipt correlated to the exact bot,
intent, attempt, method, destination, target and admitted payload hash. Edit
receipts must match the original target. It records transport truth without
resending or rerunning a business effect. Current authorization still controls
receipt continuation and private card materialization.

`confirmed_unsent` requires `pre_dispatch_failure` or `complete_sink_proof`
evidence that the exact attempt never applied and cannot apply later. Omit
message_id. It schedules the same intent after a five-second recovery fallback;
the worker rechecks current source/authority and either sends safely or cancels
stale unsent work. Original sequence/attempt/audit remain; there is no reset.

The evidence fields are a trusted operator attestation and immutable linkage,
not product verification of an arbitrary document. Never submit message bodies,
credentials or secret URLs. Missing responses, sampled UI/history, and empty
journals are insufficient unsent evidence. A screenshot alone cannot attribute a
message to one attempt. Real Telegram may provide no way to establish a lost
response's outcome. In that case leave it unresolved and keep the barrier.
There is no unresolved abandonment or automatic retry command.

Identical replay keys return the recorded result. Changed input conflicts.
Legacy attempts without captured original continuation/hash are inspectable but
cannot be resolved. Migration092 adds only attempt/audit tables; it fabricates no
old evidence. Rollback retains these tables and the existing unknown barriers;
old readers cannot resolve new audits. Do not delete audits as a downgrade.

Capture hashes normalized logical requests; documents include body hash and
filename rather than random multipart boundaries. Original private continuation
remains in trusted database storage and is excluded from operator output.

Implementation is subject to pinned checks and separate fresh Code/Functional QA.
Resolver proof does not accept all F08 business-plan/effect or C–E requirements.

Written by provider_reboot_delivery_developer (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
