# zns-chatbot and Zinusya naming

The project is zns-chatbot. The bot is Zinusya (Зинуся). Shuttle is the codename
of the Go rewrite, not a separate product or bot identity.

The Go module is github.com/complynx/zns-chatbot. Loom service_id is zinusya;
its read tool modules are zinusya_account and zinusya_administration. The required
administrator role is zinusya.admin in the API project and, for agent discovery,
also the Loom project. The owned test actor uses zinusya.service. Legacy
shuttle.admin is not an administrator alias and grants no new API access.

Loom accepts no caller-specific system instructions in create-run. The method
descriptions included in its script-tool prompt carry the bot's name, event
assistant description and language preference. The original user message remains
unchanged. This describes the assistant without replacing Loom's system prompt.

Telemetry defaults use zinusya, with zinusya.operations and zinusya.http metrics.
Update dashboard selectors at rollout. Configured service_name still overrides
the default. Existing SHUTTLE_* startup variables, cmd/shuttle, deploy/shuttle,
config/shuttle.* and document names remain rewrite artifacts and compatibility
paths. Historical QA reports retain their recorded identities and build hashes.

## Rollout and rollback

Create the new roles and explicitly assign intended administrators in both IAM
projects before updating Loom role_permissions and the bot. The test provisioner
expands its own machine grant; it does not grant human administrators automatically.
Old roles may remain in IAM for rollback but are not accepted by the new bot.

Drain accepted runs before changing the Loom caller ID. Existing auth sessions
are bound to the old caller; an active-session migration is not implemented.
No production Go deployment exists yet. Do not switch a populated production
caller while tasks are in flight. Rollback restores the former manifest, role
mapping and executable together.

SQL table names, River job kind, advisory-lock names and operation/delivery keys
retain their existing shuttle identifiers. They are storage contracts: changing
them could strand queued jobs, break mutual exclusion or repeat effects. No SQL
rename or destructive migration is needed. Completed task/history data remains.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
