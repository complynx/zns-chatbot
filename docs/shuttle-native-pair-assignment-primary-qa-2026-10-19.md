# Native pair assignment: local-primary installation QA, 2026-10-19

Status: accepted on the owned `shuttle-live-test` stand. This follows the
[source and isolated four-path gate](shuttle-native-pair-assignments.md).
It does not establish complete registration/pass/payment parity or production
readiness. Production and the Loom developer's stand were not changed.

## Installed result

- Image: `sha256:6e1ca865cb8095c8456822c58763b91d32d5c388f0792b99322ce3ec88caefd8`.
- Binary SHA256: `ea06c41cf89b92540f36c3115a0b63ac1a5bd48d8a37bb13b39269a5c58bd350`.
- Schema: 63; manifest: `zinusya/mvp-72`.
- Eleven primary services running; 134 Loom runs preserved.
- Sixteen public Mini App assets match current source bytes and use `no-store`.
  The pair-assignment events endpoint rejects a missing session with HTTP 401.

The primary HTTP gate reads assets and checks the authentication boundary. It
performs no logged-in business/form mutation and is not real Telegram WebView
acceptance. The isolated gate supplies the four actual form/agent combinations
using a controlled provider, real IAM/Loom, mock Telegram and headless Edge.
It is separate from unattended model comprehension.

## Expansion, preservation and rollback

The same Telegram poller was stopped before draining and stopping the bot.
Custom-format Shuttle, Loom and Zitadel archives were listed and fully decoded
before migration. Their sizes were 2,906,755 / 2,752,490 / 481,222 bytes.
Archive decoding was verified; a database restore was not exercised.

One owned 256 MiB migration container applied migrations 62 and 63. It exited
zero without OOM. All pre-existing domain rows and sequences were preserved;
the two new tables remained empty. Existing public functions and user-defined
triggers were preserved. The exact additions were the namespace guard function
and its three triggers (five original objects became nine).

The expanded schema and original business snapshots survived these normal-start
binary legs, with ingress fenced throughout:

1. New image, `mvp-72`, schema63.
2. Previous image `sha256:17763b12faaa79402960c3c37f8b76fa1662d20a7b19e905fde083a183bb5770`,
   `mvp-69`, schema63.
3. New image again, `mvp-72`, schema63.

The previous binary was started normally. No Down migration or previous
`-migrate` command was used. Old automatic-migration startup and old-binary
handling of newly created pair aliases/actions are not acceptance claims:
the added tables were empty during this compatibility gate.

The three idle bot stops took 0.703 / 0.782 / 0.812 seconds, each exit zero
without OOM. These were graceful stops, not SIGKILL recovery. Pending-approval
restart behavior is covered separately by the isolated four-path gate.

Readiness, settings, domain state, functions/triggers, companion identities and
Loom run count were checked through every leg. The same poller was restarted
and its actual polling cycle observed. The stopped migration container was
removed. No additional QA database container or volume was created here.

## Review and source boundaries

The installation controller passed two independent review rounds. A verified
finding covered finalization interrupted after migrator removal: finalization
now takes the existing exclusive lock, settles recorded lifecycle effects and
recognizes the journalled container's absence without repeating removal.
Unknown launch/removal outcomes remain observable and never authorize a blind
retry. Final fresh correctness and quality reviews found no actionable issues.
The Git index remained empty.

Finalization was invoked again with the migrator already absent and its removal
journal present. It completed without another lifecycle command (11 effects
before and after). No forced interruption was injected.

The source gate has 14 affected PostgreSQL behavior scenarios, unit/lint success
and Linux cross-build, following the earlier bounded identity checks and mvp71
full-suite baseline. This installation did not rerun the full current Go suite
or race detector. See the source report for exact baseline counts, retained
lint/controller failures and runtime limits.

## Evidence and next scope

Ignored local evidence is under
`.cache/shuttle-work/native-pair-assignment-install-v1/`: `checkpoint.json`,
`review-loop-final.json`, `effects.json`, private archives, `migration-cleanup.json`,
`installed-epoch.json`, `finalizer-absence-readback.json` and
`http-assets-and-custody.json`.
The current read-only custody helper is `installed_primary_observer_v19.py`.
Historical installed-state records and previous observer files remain intact.

Next: complete participant payment evidence, partial payment/top-up and
ambassador review through native, agent and mixed workflows. Preserve the open
[registration/pass matrix](shuttle-registration-pass-parity-plan.md) and
[infrastructure priorities](shuttle-infrastructure-priorities.md). In particular,
separate pair joining and assignment component gates do not establish the whole
participant registration-through-assignment journey.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
