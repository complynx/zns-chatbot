# Browser avatar source retention

This change introduced schema70 and agent manifest `mvp-90`. It is included in the accepted primary test installation at schema70 / `mvp-91`; see the [installation report](shuttle-candidate-install-qa-2026-10-22.md).

## Policy and administration

The existing application-wide DB settings document gains two optional fields:

- `avatar_browser_storage_limit_bytes`: 1 to 10000000000 bytes. Zero or omission uses 10000000000 bytes (decimal 10 GB).
- `avatar_browser_retention_seconds`: 60 to 7776000 seconds. Zero or omission uses 7776000 seconds (90 days).

Use the existing administrative settings read, prepare and exact approval workflow. These are runtime settings, not startup YAML or environment secrets. Retention applies to existing browser originals from their upload time. Editing a crop does not extend retention. Reducing the quota blocks new browser byte admission until storage fits; it does not evict unexpired originals. Telegram and imported history sources remain references and are downloaded on demand.

The per-owner admission limits remain five active drafts and 50 MiB of browser originals. Expired browser drafts no longer occupy active draft slots. Completed JPEGs have their existing independent artifact limit. The global browser byte limit includes retained originals across users, bots and organizations in this application database schema. Expired bytes awaiting cleanup still count until removed.

## Expiry and recovery

Current draft metadata includes `source_expires_at` for browser sources and `source_expired` when unavailable due to age or completed cleanup. Historical operation receipts are immutable snapshots; read current draft metadata for availability. Source access, new saves, previews and finalization reject an expired original. HTTP source requests return 410. Upload another original to continue. Mini-app recovery retains access to already generated documents.

The maintenance owner clears expired original bytes in pages of four owners with a one-second attempt budget. It uses no IAM or Loom requests and can resume after application restart. Busy owners are skipped and revisited after cursor wrap. New draft admission can also reclaim the same owner's expired bytes. Owner locks, a schema-scoped storage admission lock and shared settings locks serialize cleanup and concurrent uploads; lowering settings waits for an admitted transaction.

Cleanup preserves draft IDs, source hashes, original size, crop, versions, timestamps, operation receipts, completed JPEGs and delivery intents. Replaying a completed finalization needs no original. Increasing retention after cleanup cannot restore deleted bytes. This does not erase older database backups, reclaim PostgreSQL disk files immediately, or impose a total database/disk quota.

## Migration and rollback

Migration 70 adds an expiry marker and expands the draft source constraint without deleting data. Run one application version at a time during this change: old binaries do not enforce the new policy or byte quota. Cleanup begins only in the new application maintenance/admission paths.

Rollback to schema 69 requires an exclusive draft table lock and refuses if any original has already been cleared. Restoring an older backup is a separate data recovery operation; a schema downgrade cannot reconstruct purged originals. Stop new workers before attempting rollback and preserve the current database first.

## Verification scope

Focused behavioral tests cover atomic admission between different owners at the byte boundary, idempotent upload replay, quota reduction without eviction, expiry rejection after rendering, preservation of completed JPEGs and receipts, automatic idle-owner cleanup, HTTP 410 and replacement uploads. Mini-app browser tests cover keeping completed documents downloadable when the original expires. This does not establish production readiness or real Telegram WebView acceptance.

Local acceptance on 2026-10-21: 99 avatar/history integration roots passed on a disposable PostgreSQL instance, with no skips; 28 avatar browser tests passed, and DB/API/task package tests, SQLc compile/generation and scoped lint passed. Two fresh independent textual reviews reported no actionable findings. This local PostgreSQL run did not enable the race detector. The first full run retained a failure in an old migration test that recreated schema 68 rather than current schema 70; its fixture now restores both migrations, and the second full run passed. Original failure logs and the final decoded database archive were retained before removing the owned QA container and its anonymous data volume. Primary installation remains unchanged.
