# Native-entry local installation and real Telegram QA, 2026-10-19

## Installed and accepted scope

At this checkpoint the owned shuttle-live-test ran schema59 / `zinusya mvp-68` on image
`sha256:0a92236c23e79ec6b470e7c20cd8dd5ccdfe17673091d50dcc3fe7fadf8d1ff6`.
Bare `/start` and `/avatar` are now installed. Two real Telegram commands pass
without an agent run. Avatar-form bootstrap/logout passes, but its initial
scroll position failed. This is bounded local acceptance, not production readiness
or complete avatar workflow acceptance. Runtime/tool identity and manifest
contracts did not change; image digest distinguishes this binary update.

The later [corrected landing installation and QA](shuttle-miniapp-anchor-runtime-qa-2026-10-19.md)
now supersedes the failed initial focus with a new installed image and real
Telegram checks for all five launch targets. The evidence below remains the
historical native-entry checkpoint.

## Binary switch and rollback

The candidate layers the already accepted Linux binary over the exact previous
runtime image. Image configuration and all previous filesystem layers match;
the binary extracted from a stopped inspection container matches the accepted
SHA-256. That inspection container was removed.

A first Dockerfile `FROM` using the raw image ID was interpreted as a registry
repository/tag and failed before candidate creation. The corrected build uses
an existing owned local tag verified against the same image ID before and after
build. This failure is retained and is not described as a successful build.

Independent installation review found and corrected a wrapper/mechanics review
hash mismatch, post-start ingress failure fencing, and recovery after interruption
between saving failure evidence and stop intent. Focused fake-Docker probes cover
one exact poller stop, persisted failure, transient inspection failure and resumed
fencing without re-verification acceptance or automatic restart. Those are
controller probes; the actual installation below followed its successful path.
Historical phase mechanics and earlier epoch files were not edited.

Polling was fenced, then the original bot stopped normally. A fresh custom-format
dump and combined stdout/stderr logs were retained. Candidate, previous image,
and candidate again each passed readyz, manifest, schema/metadata, unchanged
business/schema/sequence snapshots, original River rows, configuration and peer
runtime checks. No migration, Down, database replacement or restore ran.
Three bot stops took0.640s,0.609s and0.672s, all exit0 without OOM. These are Docker
stop elapsed times, not total installation downtime.

The original polling container restarted only after the final candidate passed.
Acceptance required a fresh successful polling cycle and stable runtime identity.
Eleven original services remain; no temporary inspection/database service remains.
Previous/current images, stopped logs and the fresh dump are retained. No
production service or Loom developer stand was changed.

## Real Telegram acceptance

Only the already authorized account in the test-bot chat sent `/start` and
`/avatar`, once each. Both tasks completed through the structured route with
no run ID or auth-session ID. Two Telegram receipts confirm delivery. All older
communication rows remain identical; the selected conversation only advanced
its intake/routing sequence by two. Business/user/schema state and every other
conversation were preserved. Loom remained at132 runs.

The visible start response included `/avatar`. The `/avatar` response button launched the actual
Mini App through the existing public-to-local test redirect and signed in without
a user-facing Zitadel redirect. Avatar controls and the empty-draft state loaded.
No upload, edit, generation, payment, booking or registration write was submitted.

The WebView stayed at the profile header rather than the avatar section. Telegram
appends initialization parameters inside the requested fragment, and sections
are hidden until bootstrap. The initial landing requirement therefore FAILED.
This is retained in `miniapp-readback.json`, not accepted by the runtime custody
gate. [The source correction](shuttle-native-start-avatar.md) restores a whitelisted
visible target after all modules finish startup. All227 UI tests pass, including
plain/Telegram-extended fragments and denied/hidden/unrelated targets. It is not
installed by this checkpoint; actual corrected landing still requires real QA.

A keyboard Sign out attempt timed out and visible state remained signed in.
The AX button click then showed signed out; PostgreSQL independently confirms
the new session is revoked. Earlier session rows remain identical. The modal
was closed and the original test chat remained selected. The Mini App changed
only its session row and River housekeeping; business/user rows and Loom runs
were unchanged.

One AX filter accidentally included the authorization URL in tool output because
its fragment contained the word avatar. The raw value is not copied into reports
or proof JSON. Subsequent AX state is redacted before storage/filtering. Logout
and database revocation are verified; this report does not claim the transcript
was secret-free. A native screenshot was displayed, but no exported image path
is claimed. Avatar editing, generated-file delivery and joint provider/CA login
remain separate acceptance gates.

## Local evidence

Ignored `native-entry-install-v1` retains source/image hashes, review gates,
failure probes, dump/logs and candidate/old/candidate checkpoints.
`native-entry-real-v1-qa` retains before/readback hashes, delivered command
receipts, revoked-session proof and the explicitly failed initial-focus result.
Its separately reviewed epoch observer preserves current runtime/domain custody
while historical baselines remain unchanged. The goal remains active.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
