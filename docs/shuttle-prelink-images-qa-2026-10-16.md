# Prelink images and schema33 rollback gate

Two exact Linux/amd64 Dockerfiles built from recorded source inputs:

- Zinusya bot: `sha256:2caee3da1ac4fa77d75b3e822438954d5c063b1a3faee717ecdb427897dbe06a`,57,467,333bytes.
- Owned patched zitadeltg: `sha256:867e7b02b84f8f6e5e928a441557ec8070600df6d90f469a7910c1fda3df5901`,16,059,525bytes.

The bot context recorded672 source/build/license inputs and remained unchanged.
The provider context matched all25 files from the reproducible pinned export and
reviewed patch. Both builds used the existing pinned Go1.27.2 toolchain and the
unmodified corresponding Dockerfile. No sibling working directory was built or
modified. Local image tags are convenience references; acceptance identifies the
immutable image IDs above, not mutable tags.

A dedicated pinned BuildKit builder was limited to2GiB with no swap,2CPU and
maximum worker parallelism1. Both builds exited0; builder OOM was false and its
cgroup OOM/kill counters were zero, while the max counter recorded2,809
memory-limit hits. The exact builder and its own cache were removed;
shared builders/caches were not pruned. The two candidate images were retained
for subsequent integration. Build metadata and source hashes are in ignored
`.cache/shuttle-work/prelink-images-qa/`.

Inspection exported each image without running its entrypoint. Both contain the
expected ELF executable, Go license/patent notices and public CA bytes matching
the same pinned base used by the earlier accepted image. Bot dependency notices
match source; the provider license matches its exported source. Runtime users are
10001:10001 and1000:1000 respectively, with SIGTERM configured. No source tree,
private configuration, PEM key, shell or build cache was found in either export.
The avatar executable is byte-identical to the prior accepted one; its previous
behavioral acceptance was reused. These checks do not replace a complete new
transitive-license audit, provider callback acceptance or production release.

## Restored-copy migration and old-image startup

A private custom-format dump of the owned main schema32 database was restored
into a separately named, resource-bounded PostgreSQL container. Its data lived in
an owned tmpfs; no main database or container was changed. A fresh migration
process from the new bot image applied schema33 and exited0. The new receipt table
was empty and the unique bound-identity index existed. Nine selected user/business
table hashes matched before and after migration and both startup checks.

The old accepted bot image
`sha256:8323e6f63d4b2a8967527378ea5dbff9eb7f7fa84076071ef528f64327040c7a`
and the new candidate each started against that same migrated copy, answered
readiness and metrics probes, then exited0 on SIGTERM without OOM. They used the
existing real owned IAM configuration/trust and a synthetic unavailable Loom/
Telegram sink with a dummy bot credential. No accepted task was submitted and
no Telegram message was sent. This proves old-image startup/readiness on schema33;
it does not prove every business operation or recovery of a new prelink receipt
through the old image. Startup checks do not run schema downgrade.

This is the application-rollback gate: retaining schema33 while returning to the
old image is viable for the observed startup path. Dropping schema33 after any
pending/completed receipt is explicitly refused by its Down migration. Do not
force-drop receipts or silently merge duplicate identities to obtain a rollback.
The real stand had zero duplicate nonempty IAM bindings before this gate.

All clone migration/app/peer/PostgreSQL and inspection containers were removed.
The private dump and receipts remain ignored; do not publish their contents.
Terminal states and probes are under `.cache/shuttle-work/prelink-schema-qa/`.
At this image-gate checkpoint, the main bot still used its prior image and schema32. The owned
HTTPS identity route is active, but the application prelink endpoint is disabled.

A later [combined native fixture](shuttle-prelink-combined-qa-2026-10-16.md)
adds scoped provider/API/real-IAM acceptance with synthetic Telegram proof and a
final-relay stub. A [resilience fixture](shuttle-prelink-resilience-qa-2026-10-16.md)
adds synchronized first logins and actual IAM stop/recovery. The subsequent
[main bot image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md) has since passed
with a fresh backup, existing rows preserved and the endpoint still disabled. Main
provider/backend activation remains open. Actual
Telegram Web Login credentials/proof, browser trust, full login and production
readiness remain unaccepted. No commit, push or production deployment occurred.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
