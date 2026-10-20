# Knowledge resolution binary compatibility, 2026-10-20

The exact mvp83 candidate and preceding mvp82 binary pass an83/82/83 cycle on
populated schema67 in the owned `zinusya-mock-kb-resolution-compat-r1` project.
Candidate image: `sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
Preceding image: `sha256:984136110934f0ab747b58a08a797da47fc64bedae42c013df3e7bf98e405fad`.
The product source is `ae663a38`; subsequent commits update reports, CI coverage
and one renderer test deadline, without changing either runtime image.

## Scope and result

The nine-service fixture uses real PostgreSQL, Zitadel and Loom plus mock
Telegram. Five synthetic users bootstrap through the native API. Exact
administrator actions populate retained ambiguous, dismissed and delivered
knowledge notices, including two immutable resolution receipts. No real
Telegram account or Loom developer stand is used.

After a graceful bot stop, the exact mvp82 image starts against schema67 without
a down migration. It reads deployment-wide knowledge notification metadata,
including the new `dismissed` state. An ordinary participant prepares and
explicitly confirms a new native knowledge suggestion; the moderator receives
one notice. The participant reads that exact suggestion through the agent API.
No Loom run is started.

Returning to the exact mvp83 image preserves all six knowledge business-table
hashes captured after the mvp82 write. The three original notice rows and all
resolution audit rows match their pre-swap values. Both administrative status
reads return the original resolution results. The newly created suggestion is
still readable. Both swaps preserve every companion container fingerprint;
the schema remains67 throughout.

Measured shutdown command durations are0.657s and0.672s, both with exit0.
These are shutdown measurements; total replacement/restart duration is not
measured. No OOM is observed. This establishes compatibility for the exercised
knowledge read/write and resolution-history paths, not every business module.

## Cleanup and remaining gates

All five native sessions are logged out. Producers stop before the final
paginated Telegram-journal export. Private dumps of `shuttle`, `loom` and
`zitadel` are fully decoded with `pg_restore` before removal; this proves dump
readability, not a restore. All nine temporary containers, network and volumes
are removed. The primary stand and its domain state remain unchanged.

Ignored proof: `.cache/shuttle-work/knowledge-resolution-compat-v1/compat.json`,
the native fixture checkpoint, original private request intents, journal and
three dumps. The controller passed a fresh textual review after fixing empty
HTTP204 handling; no runtime failure occurred in this gate.

The installed primary binary is mvp81/schema65. Its compatibility with schema67
has not been tested by this83/82/83 gate. Main installation, content-moderation
Loom write handoffs, joint browser-CA/ZitadelTG acceptance and final remote CI
remain separate. No production readiness or deployment is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
