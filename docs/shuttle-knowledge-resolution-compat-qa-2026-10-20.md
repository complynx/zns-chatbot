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

## Installed mvp81 follow-up

A second, fresh nine-service project, `zinusya-mock-kb-resolution-old81-r1`,
separately passes83/81/83 on populated schema67. The mvp81 image is exactly the
installed primary image:
`sha256:98f5149909fc3fa1b852c42c93fc5c538fbb3f40889b0ada4474c8a801cb90a0`.
The primary itself is not changed by this fixture gate.

The old binary reads current capabilities and creates an ordinary native
suggestion with exact preparation and consent. This version predates knowledge
notices: no notice exists for that suggestion at its confirmation, and the gate
does not claim old81 delivery or later catch-up. The participant reads the new
suggestion through the agent API. After returning to83, all six post-write
business-table hashes match; original notice and resolution audit rows remain
unchanged. Both original resolution status results and the new suggestion are
read back. No Loom run starts.

The bot shutdowns take0.750s and0.671s, both exit0, with unchanged companion
fingerprints and no observed OOM. Total replacement duration is not measured.
The same logout, producer-stop, final paginated journal and three fully decoded
private-dump sequence precedes removal of all nine containers, project network
and volumes. Primary custody is verified unchanged. A separate fresh controller
review passes. Ignored proof is in
`.cache/shuttle-work/knowledge-resolution-compat81-v1/compat.json` and its native
checkpoint, intents, journal and archives.

Main installation, content-moderation Loom write handoffs, joint
browser-CA/ZitadelTG acceptance and final remote CI remain separate. A primary
installation must first prevent synthetic fixture recipients from becoming
eligible for real-Telegram knowledge delivery. No production readiness or
deployment is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
