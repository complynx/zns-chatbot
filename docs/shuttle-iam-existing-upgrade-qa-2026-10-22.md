# Existing IAM database upgrade qualification, 2026-10-22

An isolated copy of an existing synthetic Zitadel 4.16.3 database passed upgrade
to the server-published 4.20.0 image, token/profile/grant checks, a 4.20 restart,
and restoration of the pre-upgrade backup followed by authentication on 4.16.3.
The primary and base identity installations were not upgraded.

## Scope and procedure

The source was the stopped, owned `orders-adopt-r2` fixture. Only its PostgreSQL
container was started, for a read-only custom-format IAM dump, then stopped.
Its bot, Loom and IAM were never started. The separate
`zinusya-iam-upgrade-v1` project restored that dump into a private PostgreSQL
volume. No production database, account or credential was involved.

The copy retained the original issuer, master key, signing state, OAuth client
IDs/secrets, admin PAT and two existing synthetic user subjects. TLS verification
used the copied fixture CA. The PostgreSQL image was unchanged. Exact IAM images:

- 4.16.3: `sha256:f3738fd984131d3f02e386d37fa480d1a30e42b7d4202e2b322f12f7cf556b64`.
- 4.20.0: `sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`.

Both binaries used `start-from-setup` against an already initialized database,
following the [Zitadel update procedure](https://zitadel.com/docs/self-hosting/manage/updating_scaling).
Only one IAM binary ran at a time. Restore and database-drop operations checked
clone ownership and stopped IAM containers first. A fresh independent textual
review found no actionable findings before execution.

## Accepted behavior

1. The initial restore retained all 1,072 source event rows byte for byte. The old
   binary then served both existing users through the existing configured clients.
2. The ordinary user retained no project grants; the fixture administrator
   retained `zinusya.admin`. Full management user records and the selected grant
   identity, project, role, state and organization fields matched before/after
   upgrade. No user or grant was recreated for this test.
3. The existing service client obtained a `client_credentials` token. The
   existing exchange client obtained user JWTs with the configured user-ID token
   exchange; real IAM `userinfo` returned each original subject. Both freshly
   exchanged tokens and the tokens obtained before upgrade worked on 4.20.
4. All 1,094 event rows in the pre-upgrade snapshot remained byte-identical after
   migration and authentication. The current count became 1,140; original rows
   were compared as a multiset, so legitimate new events did not hide changes.
5. A stop/start of the same 4.20 container preserved fresh authentication and the
   same user/grant records. All stopped test containers exited 0 without OOM.
6. With both IAM containers stopped, the private clone database was replaced by
   the saved pre-upgrade backup. Its event rows matched that snapshot exactly
   before starting 4.16.3. The old binary then issued fresh user tokens and
   returned the same user/grant records.

The controller exited 0 with `passed_resources_removed`. Clone containers,
network and volume were removed after retaining logs and inspect records. All
original source container IDs/images remained stopped without OOM; source
configuration/credential-file hashes were unchanged. Backups, review/input
hashes and the accepted checkpoint remain in ignored
`.cache/shuttle-work/iam-existing-upgrade-20261022-v1/`. No real Telegram login,
model request or production change occurred.

## Limits and next gate

This is a successful upgrade and backup-restore rehearsal for this existing
synthetic database. It is not a downgrade of migrated data, proof that tokens
issued after upgrade survive rollback, token-expiry/refresh acceptance, or an
upgrade of the primary/base installations. It does not run Zinusya or ZitadelTG;
their separate [4.20 compatibility and bot profile checks](shuttle-iam420-compat-qa-2026-10-22.md)
retain their own scope. The Zinusya prelink callback on 4.20, real Telegram
consent and production qualification remain separate gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
