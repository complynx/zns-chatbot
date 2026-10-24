# Durable Telegram prelink storage

## Implemented boundary

Migration 33 and `internal/db/telegram_prelink.go` implement the local durable
part of the [trusted prelink design](shuttle-telegram-prelink-plan.md).
Database methods must only receive
freshly verified, server-scoped provider assertions.

`ClaimTelegramPrelink` commits the canonical local user and pending receipt before
IAM work. `CompleteTelegramPrelink` follows fresh IAM account/link verification,
locks USER then RECEIPT, checks current identity/revocation/proof expiry, and binds
the IAM subject plus completed receipt in one transaction. No method holds a DB
connection while the caller performs IAM requests. A completed receipt does not
replace fresh proof verification, local authority checks or IAM link read-back.

The canonical-user upsert and row lock are shared with Mini App under generic
Telegram names. Their SQL behavior is unchanged: one UUID per bot/numeric user,
and an existing profile is preserved. Provider intake creates no conversation,
queue job or agent request. Existing Telegram intake still owns its usual inbox
and conversation behavior.

## Receipt and replay

`shuttle_telegram_prelinks` keeps a UUID, bot/user, provider issuer/JTI, exact
compact-proof SHA-256, configured organization/IdP, exact external subject,
expiry and pending/completed result. It stores neither a raw assertion nor a
service bearer. `(provider_issuer, assertion_jti)` is unique. Same proof and scope
recover the same receipt; changed hash, user, provider, organization, external
subject or expiry cannot repurpose it. A fresh JTI may reconcile the same account.

Proof timestamps are absolute instants with whole-second precision, matching
JWT NumericDate. The DB uses its own clock and limits remaining freshness to 65s
(the verifier's 60s lifetime plus 5s skew). It checks expiry again after
waiting for locks and before completion. Expiry never silently deletes intent or
resurrects an old assertion. Revoked users are denied for both pending and
completed receipts. A conflict leaves the other user's binding and pending intent
unchanged through transaction rollback.

## Schema and compatibility

The partial unique index covers (bot_id, zitadel_org, zitadel_subject) for nonempty subjects. It protects SQL writers as well as the completion path. Existing duplicate groups make schema transition fail; no account is selected, merged, unlinked or deleted automatically.

Identity receipt history must survive rollback, including pending intent. Do not remove a populated receipt table to bypass accepted identity operations. The current [schema73 baseline](shuttle-schema.md) defines fresh-schema construction and refusal of legacy-history Down.
