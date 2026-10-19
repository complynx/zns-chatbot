# Partner contact entry QA — 2026-10-15

## Source checks

Architecture and Go consultations selected the existing account UUID and directed
invitation flow. Independent correctness, security/quality and final diff reviews
reported no actionable findings. The index stayed empty.

Focused race checks passed for strict link/parser behavior, actual-sender
preservation, owner-only reads, malformed metadata fallback, target-argument denial,
revocation during the Telegram metadata request, and explicit invitation/target
acceptance with unrelated-user denial. Durable intake tests cover duplicate updates,
simultaneous IAM/Loom outage, restart, uncertain create/retry, and active-run inputs.
Eleven business-table hashes stay unchanged during link intake. Strict lint passed
with zero issues.

The first full gate failed in the existing archive-validation revocation fixture:
it expected HTTP 403 but received the fail-closed HTTP 401. A background task could
invalidate the IAM cache while the endpoint verified the same revoked identity.
The isolated original test passed 15 repeats; this was not a deterministic
reproduction of the full-suite interleaving. The fixture now stops its queue worker
before testing the endpoint's local revocation response. Production behavior and
the ownership, revocation and archive-state assertions are unchanged. A fresh
review accepted that isolation; 15 repeats passed. The original failed gate/log
remain preserved separately from the new gate.

The replacement full Linux race gate passed all 177 enumerated integration tests
in 12 bounded groups, plus the explicit cmd/internal/migrations packages. All 14
runner states ended with exit code zero and no OOM. Temporary QA containers and
the separate QA PostgreSQL container/anonymous volume were removed.

## Owned native acceptance

The candidate image is
`sha256:a7b54d747a0576e6436b37bce4cbdaf765b54218276e4414f93719f2a396c07c`.
Only the owned `shuttle-live-test` application was replaced. Schema stayed at 25,
manifest advanced to `mvp-26`, and all pre-existing row counts and eleven selected
business-table hashes survived rollout. Real local IAM owner and administrator
tokens each returned their own code only. A supplied target argument was rejected
with HTTP 400. A real bot restart preserved the exact code and verified bot URL.

In the authorized `@complynx_test_bot` chat, request task
`ef1f9464-fe73-40af-afa0-6d86e0013547` / Loom run
`a2a3ce07-a061-4a60-857a-b2a70d0e0b56` called
`modules.zinusya_pairs.partner_code({})` through a real Loom script and returned
the same own-code URL. Clicking that rendered URL in Telegram Web produced the
client-generated `/start` message. Task
`dcdeece7-0fea-4c1e-b9e7-bd8d835b49dc` / Loom run
`71927e3c-dd16-4096-81eb-52d8fb52ac5b` persisted the full payload and untrusted
target hint in the actual checkpointed outgoing input, with `self_reference=true`.
Both tasks completed and their replies appeared in the chat. All eleven business
hashes remained unchanged: registrations, passes, pair invitations/membership/
notifications, payment evidence/reviews, event context and user profiles.

Native checkpoints and the visible Telegram screenshot are retained in ignored
`.cache/shuttle-live-test/`; the screenshot is `native-partner-entry-passed.png`.
No new upstream Loom bug was found. No production acceptance is claimed.

## Acceptance limits

The owned stand uses real local Zitadel and Loom with a manual Codex inference
mailbox. That exercises actual transport, token verification and tool dispatch;
it does not evaluate an unattended model's partner-intent decisions. A single
authorized Telegram account can prove share-link rendering and self-link entry.
It cannot prove a new two-real-account Telegram consent scenario. The explicit
two-party consent path has separate API tests and earlier native pair acceptance.

Browser CA import, real Mini App launch and bidirectional zitadeltg verification
remain deferred to the agreed interactive session. This feature changes neither
the schema nor allocator/financial lifecycle policies.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
