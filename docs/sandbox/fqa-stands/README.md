# Isolated Functional QA stands

FQA lead owns allocation, preparation handoff and QA scheduling. Stand engineer
is the sole preparation writer. These templates are not a running or accepted
stand. Never substitute an old application image for the reviewed Git epoch.

| Stand | Scope | PostgreSQL | App | Telegram UI | Provider control |
| --- | --- | --- | --- | --- | --- |
| flows | EN/RU manual and deterministic agent flows | 58401 | 58402 | 58403 | 58404 |
| recovery | failure, concurrency and managed replacement | 58411 | 58412 | 58413 | 58414 |
| import | importer apply/replay/reconciliation/removal | 58421 | 58422 | 58423 | 58424 |

Each directory owns its database, volumes, networks, installation and synthetic
credentials. Users may share numeric fixture IDs across isolated databases;
their writes must never cross a stand boundary. The root-owned database on
55432 and previous stopped stands are outside this allocation.

## Preparation boundary

1. Root supplies the reviewed merged application commit, including agent fixture
   support. Record the exact commit and clean source state.
2. Build application/fake, evaluator and required current helper/coordinator
   binaries once. Use actual locally inspected image IDs/digests. Save compiler,
   binary and image provenance; do not invent receipts or pull old substitutes.
3. FQA lead approves the image set for the epoch. Create each external front
   network and config/state volume using its own namespace. Config contains only
   that directory's files. Managed runtime configuration retains six components
   and the coordinator's actual retirement/session barrier.
4. Start prerequisites with pull policy never, then the deployment coordinator.
   No direct compose start may bypass managed runtime admission/retirement.
5. Prove each UI, real send/edit/callback/upload route, agent fixture path and
   independent data boundary. Record exact controls that work. E data import and
   removal controls require separate verified preparation, not ordinary fixtures.
6. Freeze commit, images, configuration and seeded data for each QA batch. Give
   reviewers public access/control instructions and original requirements only.
   No rebuild or another scenario's resource mutations while QA owns a stand.

## Current gaps

No application images, containers, database imports or runtime acceptance have
been produced from these templates. Recovery requires actual lifecycle and
SQL-lock evidence beyond local provider tests. Agent fixture consumption before
successful persistence and provider restart durability are separate open gaps;
deterministic fixtures do not prove natural language understanding. Complete
importer apply/replay/reconciliation/removal is not established by this template.

Synthetic resources may be changed or deleted only by their current owner after
checking that no other test depends on them. Production remains separate.
