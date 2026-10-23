# Native start and avatar entry

Bare `/start` opens the same native menu as `/menu`. `/start partner_...` keeps
its existing agent route and authenticated sender. Exact command recognition
also preserves ordinary text and media routing.

`/avatar` opens the existing shared avatar Mini App at `#avatars`. The menu and
reply keyboard expose it. Labels and unavailable-form text use the existing
ru/en interface language rules. With no configured Mini App, the command gives
a native unavailable response and does not fall back to an agent.

Both commands use the existing authorized durable task/outbox path. Opening
these entries changes no avatar or business state, starts no Loom run, and does
not resolve or cancel an existing agent question. The next actual user request
includes these completed navigation facts in the bounded owner-scoped history,
marked as untrusted context, not consent or a new request.

No migration, dependency, configuration option, IAM scope or agent tool contract
is added. The existing Mini App backend still validates authorization and
versions for later reads/writes. This change adds entry points, not new image
processing or automatic treatment of incoming images as avatars.
