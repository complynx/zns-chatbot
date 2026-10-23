# Native start and avatar entry

Bare `/start` provides native guidance and menu access. If a profile still needs
first/last names, that guidance is delivered before the agent name dialogue; a
filled linked profile keeps native navigation even while Loom is unavailable.
The guidance includes a Mini App entry when available. An unknown or failed
greeting is held for reconciliation before a new agent run starts.
`/menu` remains native navigation. `/start partner_...` keeps
its existing agent route and authenticated sender. Exact command recognition
also preserves ordinary text and media routing.

`/avatar` opens the existing shared avatar Mini App at `#avatars`. The menu and
reply keyboard expose it. Labels and unavailable-form text use the existing
ru/en interface language rules. With no configured Mini App, the command gives
a native unavailable response and does not fall back to an agent.

Native navigation uses the authorized durable task/outbox path. Opening it
changes no avatar or business state, starts no Loom run and does not resolve
or cancel an existing agent question. The missing-name `/start` dialogue can
start an agent run after its native guidance has been delivered. The next actual user request
includes these completed navigation facts in the bounded owner-scoped history,
marked as untrusted context, not consent or a new request.

The existing Mini App backend validates current ownership, authorization and
versions for later reads/writes. Captionless photo and image-document handling
uses the separate [native avatar policy](shuttle-avatars.md#native-image-entry).
Opening a menu or editor is not permission to send a document.

## Command list and processing feedback

Start guidance refreshes the private chat's command list with English defaults
and a Russian-language override, then selects Telegram's command-menu button.
The list covers start/menu, profile/name, registrations/register, orders, massage,
avatar, pairs, payments and knowledge. Descriptions are localized navigation
hints; they do not bypass the corresponding owner or business checks.

Telegram's typing indicator is best-effort feedback during active processing or
an admitted queued send, including native avatar delivery. It is refreshed while
work remains eligible and rechecked before each cosmetic API call. Waiting for a
person, a completed delivery, a closed task or an unknown send needing manual
reconciliation is not processing. Typing does not prove model execution,
business completion or Telegram delivery, and its failure does not retry a
business operation.
