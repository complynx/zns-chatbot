# Specialist notification candidate image QA — 2026-10-18

The complete booked/cancelled specialist notification candidate is built locally
as `sha256:1f9d123de594fd40dc27e18f56576dd0ac91046077074de130e85464dce9561a`
(tag `zinusya-specialist-notice-bot:v1-20261018`). The Linux/amd64 image is
64,033,744 bytes, uses `10001:10001`, and retains the release entrypoint
`/usr/local/bin/shuttle`. This is build acceptance, not application startup,
migration, real Telegram or production acceptance. Main remains schema48 /
`mvp-55`; it was freshly read as service `zinusya`, primary OID 16385.

The candidate manifest revision is `mvp-56`. The only source difference after
[the accepted 42-root race gate](shuttle-massage-specialist-preferences-context-qa-2026-10-18.md)
is the exact revision line, verified against the original manifest bytes. All
five manifest unit roots pass after that change; scoped Linux API lint reports
zero issues. Fresh revision review found no actionable findings. The preceding
176-test mini-app gate remains byte-matched for its UI/test inputs.

The build source gate checks the accepted 42-root/82-event race checkpoint,
its frozen inputs, the UI checkpoint and current hashes, five terminal manifest
roots and final lint. It compares all source-input keys against the accepted
race snapshot and permits only the exact manifest revision replacement. All
866 release inputs match that candidate snapshot. These checks run before
builder creation; their evidence files and hashes are recorded.

The unmodified release Dockerfile uses its existing pinned Go1.27.2 and scratch
stages. A separate pinned BuildKit builder is limited to 2 GiB, no extra swap,
two CPUs and one parallel BuildKit operation. Its diff passed fresh independent
review. The reviewer saw the fail-closed checks before the source-gate file was
created; runtime assertions subsequently verified the actual evidence. This
report does not turn that static review into an independent live evidence audit.

The build finished with exit0, no OOM, unchanged release bytes and unchanged ten
main Compose container fingerprints (identities, images, start times, running
states and restart counts). It does not assert business-row or standalone
provider fingerprints. The exact dedicated builder and its own cache were
removed. The image is retained for owned installation testing. No application,
PostgreSQL, Zitadel, Loom or poller restart occurred in this build gate.

Ignored evidence is in `.cache/shuttle-work/specialist-notice-candidate-source-v1-qa/`
(source gate, original manifest, manifest JSON test log and lint) and
`.cache/shuttle-work/specialist-notice-images-v1-qa/` (build log, metadata, source
freeze and cleaned builder checkpoint). The index remains empty. No commit,
push, production deployment or main image swap was performed.

Next is a disposable schema48-to49 rehearsal and a reviewed finite installation /
recovery controller. Unlike the earlier schema47-to48 transition, migration49
also adds a nullable target column and changes the one-target constraint on the
existing notification resolution table. The verifier must preserve old rows and
permit only those exact schema changes; excluding that whole table would be
insufficient. A fresh current48 archive and verified restore are required before
main migration. Any later rollback must preserve candidate49 history instead of
bypassing its populated-table Down guard. The older47 archive is not a current
recovery source. Real Mini App/Telegram and actual process restart gates follow
installation; upcoming-session notices remain separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
