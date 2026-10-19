# Mini App native fixture acceptance — 2026-10-15

Scope: account bootstrap, safe owner read, exact retry, restart and logout on
the disposable `shuttle-live-test` stand. Real Zitadel validates delegated user
tokens. This fixture signs initData locally with the test bot's token for the
authorized current Telegram user only. It is **not Telegram-issued initData**.
No Telegram message, upload, BotFather change or user grant was needed.

## Source checks

Three independent proof reviews and three DB reviews covered correctness,
security/privacy and code quality. The HTTP/UI round found stored-credential
precedence/account isolation and noncanonical Origin defects. Both were fixed;
a fresh lifecycle review found no actionable defects. No review fix was staged.

After the corrections, strict lint reported zero issues. The full race gate
enumerated 164 integration tests, ran each exactly once across 11 fresh processes,
and verified exact coverage. Other cmd/internal/migrations packages also passed.
Test containers were limited to 1.5 GiB, with a lower Go heap target; all completed
without OOM. Seven Node browser-behavior tests and JS syntax checking passed.
No new external runtime dependency was added for the UI or its tests.

## Native checks

Candidate image:
`sha256:0697262acb3f694890c7a3883723a4ec26d95b546a3cab77b18b62b101547eec`.
Migration 24 was applied explicitly after stopping the idle bot. A 2,158,469-byte
pre-migration custom PostgreSQL dump has the PGDMP signature and a verified archive
listing. User/task/conversation/outbox/payment row counts remained unchanged.

The configured origin was `https://app.zinusya.localhost:18444`. Native API probes
used the loopback HTTP port with that Origin header; no HTTPS proxy or browser
certificate acceptance is claimed by these probes.

Passed:

- Real Zitadel bootstrap for the existing authorized owner, with no profile overwrite.
- Exact proof/nonce replay returns the original bearer, owner projection and expiry.
- Foreign Origin and tampered proof are rejected; another nonce conflicts.
- A bot restart preserves the session and exact bootstrap result.
- Logout invalidates the bearer and retains consumed-proof denial.
- Simultaneous shutdown of our Zitadel and Loom returns retryable 503. Positive
  introspection caching was disabled in this stand, so no outage masking is claimed.
- A bot restart during both outages preserves the active session. A different
  session can still log out while IAM is unavailable.
- Restarting our IAM/Loom restores the original session and expiry, without
  recreating the owner or obtaining another Telegram proof.
- No new bot tasks, conversations, outbound messages, payment evidence or reviews.

In the actual embedded browser, opening the served page outside Telegram displayed
the instruction to open Зинуся from its Telegram bot. The page exposed no account
details or login controls. This checks rendering and the outside-Telegram state,
not signed Telegram login. A screenshot is retained in ignored local evidence.

## Limits and cleanup

Real Telegram launch, WebView iframe/storage behavior, trusted HTTPS and interactive
zitadeltg linking remain deferred until Daniel's CA step. Business forms are not
part of this account slice. Production was not changed or accepted.

The owned QA PostgreSQL container and its anonymous volume were removed after the
gate; completed build/test containers were removed. The nine capped native stand
services remain available. Both IAM and Loom were restored after fault injection.
Loom developer checkouts and stands were untouched. No new upstream bug was found.

Local evidence: `native-miniapp-checkpoint.json`, the pre-migration dump, the exact
164-test batch plan, lint output and outside-Telegram screenshot. Credentials and
raw proofs are absent from the checkpoint; local evidence remains ignored.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
