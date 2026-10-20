# Knowledge content runtime QA, 2026-10-20

The isolated general-knowledge write gate passes on schema67 / mvp83, using
image `sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
It uses real Zitadel and Loom with mock Telegram and a controlled counted
provider. The primary remains schema65 / mvp81. This is runtime integration
acceptance, not autonomous model-quality or production-readiness evidence.

## Accepted path

Five synthetic identities are created through Mini App bootstrap. An ordinary
user submits one synthetic reference through the native knowledge HTTP API.
It remains pending and creates no published article. The ordinary proposer and
a global administrator without named moderator membership receive403 when
preparing a review. A distinct named moderator can review it.

One reactive moderator message starts one actual Loom run. Loom reads the frozen
suggestion through the agent tool and receives its exact body, then prepares
approval through `module_call`. The ordinary reply keyboard presents the original
exact terms. Approval IDs and argument hashes are absent from its visible text.
No article is published before consent.

SIGTERM stops only the bot in0.657 seconds with exit0. The test reads PostgreSQL
while the bot is stopped, then starts that same container. Task, run, approval,
argument hash and preparation are preserved; the eight companion services keep
their original fingerprints. This measures shutdown, not total replacement time.

After restart, the same moderator supplies free-text consent. Loom receives it
as steering input for the original pending action. `interaction_resolve` binds
that input to the original approval and performs one review. One article and one
immutable revision are published, with distinct proposer and reviewer identities.
The native status endpoint and a repeated native confirmation return the same
result; there is still only one review operation and one revision. The original
submission receipt still says pending while the current suggestion says approved.
One final reply is accepted by mock Telegram, and the original bot task
completes. The fixture contains two bot tasks and one Loom run.

## Retained failures and cleanup

The first, separate `kb-content-r1` attempt remains failed. Its QA stopped-state
observer incorrectly called the stopped bot's HTTP API. A recovery preflight also
misread the stored preparation wrapper before issuing a start. The original bot
was then started once, but the five-minute proposal deadline elapsed during QA
repair. The original task/run/approval identities and terms were retained; the
actor-owned operation became expired and Loom ended with `wait_expired`. No
review operation or publication occurred, and no deadline was extended. Those
observations, three decoded database archives and the final journal were saved
before the failed project was removed.

Fresh `kb-content-r2` passes the accepted path above. Its initial cleanup failed
when Windows rejected atomic replacement of a local receipt before the logout
callback. The error path stopped all nine services cleanly. A separately reviewed
archive continuation preserved that failure and did not repeat the logout. It
started only PostgreSQL and mock Telegram, with bot/IAM/Loom and other writers
stopped, saved the complete final journal, and fully decoded all three database
archives. It then stopped the observers and removed the owned containers,
networks and volumes. Primary preservation passed.

Ignored evidence is under `.cache/shuttle-work/knowledge-content-runtime-v1/`
and `knowledge-content-runtime-v2/`; the latter includes `readback.json`,
`checkpoint.json` and `cleanup-recovery-proof.json`. Controller and recovery
review loops are retained beside them. The failed checkpoints are not relabelled.

## Limits

The native part uses the HTTP API, not a browser-driven editing session. This
gate covers general knowledge and native-to-agent continuation with native
receipt replay. Separate-device editing, event-scoped write dialogues and
independent model comprehension remain separate gates. Usage values come from
the controlled provider and do not measure real tokens or cost. Full remote CI
remains required before installing mvp83 on the primary.
