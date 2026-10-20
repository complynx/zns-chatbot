# Complete bounded Linux integration gate, 2026-10-20

Accepted scope: commit `387716bfedee9fdb6164d936e036fb8404cb6d64`.
All786 default Linux integration roots pass in eight sequential race-enabled
batches, with1169 terminal cases, no failed or skipped cases, exit0 and no OOM.
The last batch contains86 roots. Go discovery and exact terminal-name/count
matching replace the old static declaration count. The separate browserqa-tagged
case is not part of this default gate.

This verifies the source after the [scoped follow-up](shuttle-linux-batched-qa-2026-10-20.md).
The earlier785-root run remains failed, as do the preceding timeout runs. Their
records are not overwritten. Full cmd/internal coverage is not supplied by this
integration-only runner; existing affected-package/unit/UI evidence remains in
the linked report. Remote CI and real Telegram WebView are separate gates.

The exact source snapshot, discovery logs, eight JSON streams, runner summary
and lifecycle journal remain ignored under
`.cache/shuttle-work/linux-race-batched-v2/`. Two independent controller reviews
passed before launch. The source-preservation check passes at retirement.

The remaining integration database archive is856 bytes after successful fixture
cleanup. `pg_restore --file=/dev/null` fully decoded it before removal; this is
not a restored runtime or a backup of primary business data. Own temporary
containers, network and volume are removed. The primary-preservation check
passes; no Loom developer or production stand changed.

Knowledge foundation source changes started after this run finished and its
stand was removed. They are outside this786-root acceptance and need their own
behavior, review and runtime gates. The reviewed native UI fixes in the accepted
commit are still not installed on the primary. A Linux candidate binary was
built with SHA256
`8300aeb292cfcb702ed3cd8e65da816fd910e722f13db0f27a335423db4027f1`;
packaging, installation and rollback remain separate work.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
