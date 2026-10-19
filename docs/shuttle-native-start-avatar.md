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

## Verification and installation boundary

Focused PostgreSQL scenarios exercise ru/en entry with Loom unavailable,
accepted-update replay after service restart, and navigation during an existing
agent question followed by same-run continuation. The continuation checks that
both commands appear in the agent context. Existing partner-entry, navigation,
orders, meals and massage scenarios remain in the neighboring gate.

The config env-override test now supplies its own local-launch fields. It no
longer assumes test-only options appear in the minimal production example.
No production configuration field was added.

Local evidence is under ignored `.cache/shuttle-work/native-entry-v1`. It retains
failed assertions rather than relabeling them as passes: temporary task state
and background agent polling were unsuitable native-independence assertions;
the missing next-agent history entries were a real defect and were corrected.
An earlier unit probe exposed the config fixture problem; the first lint probe
used the wrong Go on PATH. Final gates and source hashes are recorded separately.

The [owned native-entry runtime gate](shuttle-native-entry-runtime-qa-2026-10-19.md)
installs the commands and accepts two real native deliveries without an agent.
That checkpoint retained an initial avatar landing failure. The subsequent
[corrected landing gate](shuttle-miniapp-anchor-runtime-qa-2026-10-19.md) now
installs the shared `app.js` correction after rollback checks. All227 UI tests
pass; actual Telegram Avatar and all four neighboring launch targets land in
their requested visible sections and sign out cleanly. Full avatar form writes
and generated-file delivery remain separate acceptance work.
The earlier [read-only checkpoint](shuttle-native-miniapp-readonly-qa-2026-10-19.md)
retains its historical statement that direct native entries were absent then.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
