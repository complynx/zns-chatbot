# Browser avatar source retention

This change introduced schema70 and agent manifest `mvp-90`.

## Policy and administration

The existing application-wide DB settings document gains two optional fields:

- `avatar_browser_storage_limit_bytes`: 1 to 10000000000 bytes. Zero or omission uses 10000000000 bytes (decimal 10 GB).
- `avatar_browser_retention_seconds`: 60 to 7776000 seconds. Zero or omission uses 7776000 seconds (90 days).

Use the existing administrative settings read, prepare and exact approval workflow. These are runtime settings, not startup YAML or environment secrets. Retention applies to existing browser originals from their upload time. Editing a crop does not extend retention. Reducing the quota blocks new browser byte admission until storage fits; it does not evict unexpired originals. Telegram and imported history sources remain references and are downloaded on demand.

The per-owner admission limits remain five active drafts and 50 MiB of browser originals. Expired browser drafts no longer occupy active draft slots. Completed JPEGs have their existing independent artifact limit. The global browser byte limit includes retained originals across users, bots and organizations in this application database schema. Expired bytes awaiting cleanup still count until removed.

## Expiry and recovery

Current draft metadata includes `source_expires_at` for browser sources and `source_expired` when unavailable due to age or completed cleanup. Historical operation receipts are immutable snapshots; read current draft metadata for availability. Source access, new saves, previews and finalization reject an expired original. HTTP source requests return 410. Upload another original to continue. Mini-app recovery retains access to already generated documents.

It uses no IAM or Loom requests and can resume after application restart.
Busy owners are skipped and revisited after cursor wrap.
New draft admission can also reclaim the same owner's expired bytes.
Owner locks, a schema-scoped storage admission lock and shared settings locks serialize cleanup and concurrent uploads; lowering settings waits for an admitted transaction.

Cleanup preserves draft IDs, source hashes, original size, crop, versions, timestamps, operation receipts, completed JPEGs and delivery intents. Replaying a completed finalization needs no original. Increasing retention after cleanup cannot restore deleted bytes. This does not erase older database backups, reclaim PostgreSQL disk files immediately, or impose a total database/disk quota.

## Migration and rollback

Migration 70 adds an expiry marker and expands the draft source constraint without deleting data. Run one application version at a time during this change: old binaries do not enforce the new policy or byte quota. Cleanup begins only in the new application maintenance/admission paths.

Rollback to schema 69 requires an exclusive draft table lock and refuses if any original has already been cleared. Restoring an older backup is a separate data recovery operation; a schema downgrade cannot reconstruct purged originals. Stop new workers before attempting rollback and preserve the current database first.
