# Durable deployment identity

One PostgreSQL/River schema belongs to one bot ID, IAM organization and issuer.
A single-instance advisory lock prevents overlapping processes; it does not by
itself prevent a later process from using another bot's credentials.

Migration 00004 adds an additive singleton deployment record. Startup acquires
the instance lock, verifies the IAM client's bot ID matches the task runtime,
then binds or checks the deployment before reconciling deliveries or starting
River workers. First binding refuses existing users from another bot or bound
organization. A mismatch leaves jobs, tasks, deliveries and the binding intact
and fails startup. Empty local identities can be provisioned only after binding.

Changing bot ID, organization or issuer requires an explicit reviewed migration,
not deleting this record as a retry workaround. No API unpins it. Credentials may
rotate while these identity values remain stable. A CA/leaf renewal with the same
issuer hostname does not change deployment identity.

Apply explicit migrations before starting the executable. Binary rollback keeps
the additive table and original configuration; an older executable does not
provide this startup guard. Do not run the down migration against populated
state as routine rollback: it removes the guard. No automatic production
migration, scope rewrite or data deletion is performed.

Behavioral acceptance reproduces a Telegram 429 with a durable retryable delivery,
stops the original process and tries a new bot, organization and issuer. Each
startup is refused, with zero additional sends and the original retry retained.
Restoring the original deployment completes that delivery.

Private APIs independently scope subject lookup by bot/organization and reject
foreign-bot revocation. This defence does not promise concurrent multi-bot workers
in one schema; use separate schemas/databases for separate deployments.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
