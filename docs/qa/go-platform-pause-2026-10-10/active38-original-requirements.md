# Exact scenario index

Each ACTIVE outcome is pending independent execution under this packet.
Access is presently BLOCKED for all: no current READY/FROZEN stand release.
Short assertions below index, but do not reduce, the detailed original F/R
requirements and active E v2 contract linked in README.md.

| ID | Allocation | State | Required observable assertions |
| --- | --- | --- | --- |
| F01 | C | ACTIVE | EN/RU manual event registration, agent intent change, manual continuation, submit/cancel |
| F02 | C | ACTIVE | Pending-form unrelated question, explicit cancel/change-event, supported media in separate trials; next intent is not trapped |
| F03 | C | ACTIVE | Stored UI locale versus question language both ways; old-card callback/back/navigation after locale change |
| F04 | C | ACTIVE | be/by/uk/ua/pl inputs; genuine primary and fallback catalog gaps with published expected labels |
| F05 | C | ACTIVE | Unique owner-private memory/context; owner read, foreign direct and agent lookup privacy boundaries |
| F06 | C | ACTIVE | Authorized card and held proposal; revoke applicable role OR consent before release; stale buttons/cards, ordinary/domain/global-without-domain and A/B boundaries |
| F07 | C | ACTIVE | Knowledge create/update/history, later update/delete/context revocation; manual/agent and old-history/card freshness |
| F08 | C | ACTIVE | Bounded model/script, delayed calls, provider failure, user cancel/interruption in registration/knowledge; coherent durable effect boundaries |
| F09 | C | ACTIVE | Generic browsing before specific sales check; repeated initiation, manual/agent switch, original ingress replay after competitor |
| F10 | C | ACTIVE | Competing original ingress order versus reversed delivery/replay; pre-opening and sales-open order |
| F11 | C | ACTIVE | Default10min before/at/after expiry, competing unfinished intent, retained draft continuation; separately configured alternative expiry |
| F12 | C | ACTIVE | Published capacity/pair/role/tier edge cases, manual/agent paths and competing last place |
| F13 | C | ACTIVE | Completed/cancelled registration and changed knowledge persist in fresh session; approved outage continuation if supplied |
| R01 | D | ACTIVE | EN/RU Telegram-like messages, keyboards/buttons, callback ACK, edits, upload/download, manual and fixture-agent flow |
| R02 | D | ACTIVE | Full accepted batch, receipt/commit boundary crashes; durable work and acknowledged offset after restart, no acknowledged loss |
| R03 | D | ACTIVE | Exact update replay and multi-event restart; one domain effect per accepted identity, per-chat order |
| R04 | D | ACTIVE | Known-before-send versus possible-send crash; durable resume, factual unknown and approved bounded authorized resends without invented receipts |
| R05 | D | ACTIVE | Managed replacement with active app/helpers and drain timeout; stop/join, no second launch while old processes live |
| R06 | D | ACTIVE | Admission loss during work; old writers stop before replacement writes |
| R07 | D | ACTIVE | Transient DB outage, app exit, supervisor restart/backoff/restoration and work persistence |
| R08 | D | ACTIVE | Repeated valid429 and deferred restart; durable deadline, distinct failure budget, progress; once-only delivery only when no unknown send |
| R09 | D | ACTIVE | Invalid retry delay parks visibly with reason and no immediate loop |
| R10 | D | ACTIVE | Ambiguous429 pacing/fallback; independent chat/topic lanes proceed |
| R11 | D | ACTIVE | Separate deferred/inflight/uncertain heads; no same-lane overtaking, independent lanes progress |
| R12 | D | ACTIVE | Recipient denial affects only selected item; item progress/operation identities survive restart |
| R13 | D | ACTIVE | UI opt-out/revocation while deferred; live authorization prevents later send |
| R14 | D | ACTIVE | Shared credential failure visibly pauses service, not individual recipient denial |
| R15 | C | ACTIVE | Known event initiation order, repeated clicks/exact replay; stable identity/rank/deadline |
| R16 | C | ACTIVE | Default10min unfinished intent moves to tail, draft retained, replay identity stable |
| R17 | D | ACTIVE | Collector failure cannot block business work; bounded redacted observations |
| R18 | D | ACTIVE | Measured CPU-only Linux main app, PG and bounded isolated helpers across scenarios; honest resource/process limits |
| I01 | E | ACTIVE v2 | Immutable input/stage/plan/resolution hashes; explicitly attested owner mapping, no inferred identity |
| I02 | E | ACTIVE v2 | Imported permanent identities, refs, proofs, history and domains independently compared to original projections through exports/UI |
| I03 | E historical | SUPERSEDED, NOT PASS | Original committed-record interruption/resume replaced by user-approved whole-target recovery |
| I04 | E historical | SUPERSEDED, NOT PASS | Original changed-byte resume matrix no longer mandatory; no PASS inferred |
| I05 | E historical | SUPERSEDED, NOT PASS | Original late-conflict/partial recovery matrix no longer mandatory; no PASS inferred |
| I06 | E | ACTIVE v2 | Exact target/roles/session guard, managed writers stopped and sole bulk writer; live-overlap negative not mandatory |
| I07 | E | ACTIVE v2 | Exact immutable proof/full-history bytes; owner access, wrong-owner denial, explicit unavailable, no empty substitute |
| I08 | E | ACTIVE v2 | Temporary receipts absent; permanent identities/refs/proofs/history/domain state preserved against independent expected projections |
| I09 | E | ACTIVE v2 | Importer-free admitted runtime; EN/RU imported-record UI/privacy and one controlled persistence restart |
| E-RESET-REAPPLY | E separate target | ACTIVE v2 | One whole-target restore/recreation to known preimport baseline, identical-input complete reapply and independently observed protected state/byte/mapping equivalence |

Publication consent is the approved author/proposal/version/body/destination
boundary, not a generic grant to private sources. F06 preserves exact "role or
consent"; the applicable withdrawal must govern the tested action. F07 context
revocation includes actual source/context retirement through authorized controls,
not a substituted browser logout. Changed proposal/body/destination needs fresh
consent; retirement and permission restoration cannot revive stale access.
No generic toggle is added, and no original outcome or authority cell is waived.

Partition: C15=F01-F13,R15,R16; D16=R01-R14,R17,R18;
E7=I01,I02,I06,I07,I08,I09,E-RESET-REAPPLY. Historical original40 IDs remain
intact; active38 is not original40 with three silently passed skips.

Every applicable UI outcome covers EN/mouse, EN/emulated-touch, RU/mouse,
RU/emulated-touch in the actual Telegram-like UI. Physical touch is unverified.
Messages, buttons/keyboards, callbacks/ACK, edits, uploads/downloads, permissions,
history freshness, retries, concurrency, persistence and failures remain in
scope. HTTP-only probes cannot replace UI. Applicable authority covers owner,
ordinary user, event-specific role, unrelated-event role, global admin without
domain role and revoked authority; inapplicability needs an explicit reason.
Exact replay, restart after completion and effect-boundary interruption differ.

Written by fqa_lead (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
