# Telegram prelink assertion verifier

## Implemented boundary

`internal/identity/telegram_prelink.go` authenticates a compact provider assertion
before any local receipt or IAM mutation. It reuses the existing go-jose and
singleflight dependencies. Provider trust is configured separately from IAM token
trust. The returned proof contains the verified Telegram profile, actual opaque
subject, issuer/JTI, expiry and SHA-256 of the exact compact bytes.

This is one component of the [trusted prelink operation](shuttle-telegram-prelink-plan.md).
The [IAM adapter](shuttle-telegram-link-qa-2026-10-16.md) and
[durable storage](shuttle-telegram-prelink-storage.md) already exist. The [private endpoint and startup wiring](shuttle-telegram-prelink-api.md) are
now implemented. The [isolated hook](shuttle-zitadeltg-prelink-provider.md) and
[private TLS route](shuttle-prelink-route-qa-2026-10-16.md) have scoped combined
acceptance; main provider/backend activation remains open. The verifier alone
neither links an account nor prevents replay; it must precede receipt reservation,
fresh IAM verification and the final local authority check.

## Trust and freshness

Startup requires explicitly configured HTTPS issuer/JWKS URLs, a dedicated
prelink audience and a bounded positive bot ID. URLs cannot contain credentials,
queries or fragments. No discovery or token-supplied key URL is followed. The
HTTP client has at most a three-second timeout, no cookie jar and no redirects;
a caller-supplied transport is trusted infrastructure configuration.

Only compact RS256 is accepted. The protected header needs an explicit key ID.
Proofs are bounded at 16 KiB, and duplicate decoded top-level header/claim names
are rejected. Exact JSON field names are used; case aliases cannot replace a
required claim. Require exact issuer, a singleton audience (string or one-item
array), `urn:zinusya:purpose=telegram-prelink` and nonempty bounded JTI.

Bot/user claims must be canonical positive decimal strings, within Telegram's
52-bit safe ID range. Check `sub=telegram:BOT:OPAQUE_SUBJECT` independently of the
numeric user ID and recompute `tg+BOT+NUMERIC_USER@telegram.invalid`; verified
email must be true. Profile names are bounded initial metadata, never permissions.
Organization, IAM provider and target IAM user cannot be selected by proof claims.

Require integer `iat`, `nbf`, `exp`, positive times, expiry after issuance and a
lifetime at most 60 seconds. `nbf` must be within five seconds before issuance,
matching the provider contract. Issuance/not-before permit at most five seconds
of future skew. Expiry is strict with no post-expiry grace. Verification includes
elapsed key-fetch time, so a proof that expires while fetching keys is denied.
The endpoint must still recheck freshness before effects and storage completion.

## Keys and concurrent requests

JWKS responses are limited to 1 MiB and 16 keys. Require unique bounded key IDs,
public valid RSA keys from 2048 through 8192 bits, signature use and RS256 when
those optional key attributes are supplied. Empty, malformed, private, duplicate,
weak or incompatible key sets are unavailable trust, not successful verification.

Cache keys for at most five minutes. An unknown key can trigger refresh, globally
throttled to one fetch per five seconds. Concurrent refreshes share one fetch.
A failed refresh cannot replace a valid cache; an expired cache cannot authorize
new verification. Cached public keys allow proofs to be checked through a brief
JWKS outage, while each proof still needs its own valid signature and freshness.

A disconnected request stops waiting without canceling the shared fetch. That
fetch has an independent three-second bound and retains the originating context
values. This fixes a review finding where one canceled caller could abort all
waiters and consume the refresh throttle. No IAM access or DB connection is held
by this verifier.

## Verification and remaining gates

Focused Windows behavioral checks passed. Strict identity-package lint reports
zero issues. A first independent source review found the shared-cancellation
issue; the fix and a blocked-JWKS/two-waiter regression passed. A fresh independent
review found no actionable issue.

The complete identity package passed under Linux race detection: 33 root tests,
107 root/subtest pass events, no failures or skips. This includes signature/scope,
expiry during fetch, malformed/duplicate claim JSON, invalid provider key sets,
redirect/outage handling, cached-key outage, unknown-key throttling, concurrent
proofs, rotation and waiter cancellation. It is not an end-to-end login test.

Ignored evidence: `.cache/shuttle-work/prelink-verifier-qa/`, including the full
JSON test stream, stderr/cgroup counters, terminal state and source hashes.
The owned 2 GiB test container exited 0, with OOM false and memory `max`, `oom`,
`oom_kill` counters zero, then was removed. No ports, network or private persistent
volume were created; existing Go build/module caches were reused. Main stand,
Telegram, IAM policies and the sibling zitadeltg checkout were not changed.

The [service-authenticated endpoint](shuttle-telegram-prelink-api.md),
[private TLS route](shuttle-prelink-route-qa-2026-10-16.md), and isolated hook now
have [combined real-IAM/DB convergence](shuttle-prelink-combined-qa-2026-10-16.md)
and [scoped outage/concurrency acceptance](shuttle-prelink-resilience-qa-2026-10-16.md),
including matching receipt before relay. The [main bot image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md)
passed with prelink disabled. Main provider/backend activation, browser CA trust
and real Telegram OIDC/final session-cookie relay remain separate gates.
No production readiness, deployment or complete bidirectional login is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
