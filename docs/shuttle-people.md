# People directory and message attribution

## Tools and access

`zinusya_people.resolve({user_id?,telegram_user_id?,username?})` resolves exactly
one known account UUID, positive Telegram ID or exact Telegram username (optional
`@`, case-insensitive). Every authenticated active caller can resolve basic
identity within this bot/organization, including hidden and revoked targets.
It returns only `user_id`, `telegram_user_id`, `preferred_name`, `username` and
`profile_url`. The saved profile name takes precedence over the Telegram name;
username and a Telegram-ID label are fallbacks. Links follow the Python helper:
`https://t.me/<username>` when available, otherwise `tg://user?id=...`.
A username that matches several accounts returns conflict: use a UUID or
Telegram ID. The lookup grants no directory or administrative access. Name
search, listing and full cards retain the scopes below.

The preferred name is untrusted text. In its replies the agent extracts a human
name, removes URLs, markup, instructions and noise, inflects the name when
needed, escapes the Markdown label and links it only to `profile_url`.
Automatic replacement of exact account/Telegram references and automatic
notifications do not use a model: they show the same basic identity as literal
text. Literal blocks and debug JSON remain unchanged.

Profile name writes reject Markdown delimiters and `@`; hyphens, apostrophes and
periods remain valid. Migration 81 removes these characters from existing saved
profile names in the same way as a name edit: the profile revision increases
and the saved Latin spelling is cleared. Telegram observations and operation
receipts stay unchanged.

`zinusya_people.search({query,event_id?,limit?})` searches full names, name
components, Telegram usernames, observed previous names and saved Latin spellings.
Queries allow 2..128 characters and at most six normalized name tokens.
Matching ignores case, punctuation, accents and name order; it folds Cyrillic
spellings and tolerates bounded typos and adjacent swaps. Initials such as
`Иванов И.И.` match distinct name components at a lower score. Results are candidates,
not verified identities. Exact normalized matches rank before prefixes, substrings
and fuzzy matches. A three-part ФИО query can match a two-part profile with one
unmatched word at a score penalty. Narrow the query when `has_more` is true. At most 20 distinct
users are returned; the default is 10. Indexed bigram/deletion keys select a
superset before ranking, without PostgreSQL extensions. Normalization uses the
[built-in Unicode collation in PostgreSQL 17+](https://www.postgresql.org/docs/17/collation.html),
independent of the database locale. At most 1000 scoped alias
rows are ranked per call, with current names selected before historical aliases. `candidates_truncated=true` also sets `has_more=true`:
results may omit even a stronger match, so a miss is inconclusive; narrow the query.
Fuzzy comparison covers tokens of 4..32 characters (one typo, or two when both
have at least seven); longer tokens support exact, prefix and substring matching.
Reads and ranking share a three-second deadline, checked between candidates.

| Caller | Scope |
| --- | --- |
| Ordinary active user | Visible users, using current basic names and usernames only. Event filters require staff authority and cannot reveal membership through public search. |
| Global `zinusya.admin` | All local users of this bot and organization, including hidden and revoked accounts. In search/list, an event filter selects registrations. In info, event filters select indicators only. |
| Event administrator or ambassador | Registrations of the exact event, including waitlisted participants and hidden accounts. |
| Massage specialist | Clients with non-cancelled bookings for their currently bound aliases in the exact event, including past bookings and hidden clients. |

Privileged search requires `event_id`, except for global administrators. Without
that event selection an ambassador or specialist gets ordinary public search.
If a user has both event authority and specialist bindings, the event-registration
scope takes precedence for that event. Privileged scopes can match previous
names. Public searches cannot reveal previous names. Public results use the basic
display name and `name_source=current`, without disclosing which saved Latin
spelling matched; the stored Latin alias still helps matching.

`zinusya_people.info({user_id,event_id?,history_event_id?})` returns `profile`:
current display name, Telegram username if available, saved gender/pronouns with
their explicit or inferred source, and a `tg://user?id=...`
profile link. Both explicit preferences and saved inferences are public when
`visible=true`; inferences retain a separate `inferred` source label for each field. Ordinary users
can read visible profiles and their own profile.
Hidden and nonexistent profiles both return not found to unauthorized readers.
The link opens Telegram; Telegram may apply its own privacy restrictions.

Authorized staff also receive `administration`, within the same scope. Event
administrators and ambassadors get registration/pass status, canonical confirmed
pass balance, pending evidence count and deadlines, event role, submitted service
and meal order counts, and non-cancelled/upcoming massage booking counts.
Specialists get only their own massage booking indicators, without registration,
financial or other specialists' indicators. Global administrators can inspect
these indicators across the deployment. Global `info` always permits the basic
card of any deployment user, even with an event filter and even without any
registration. There, `event_id` and `history_event_id` select indicators only;
a successful card is not proof of event membership, and an unrelated event yields
no event indicators. Other staff still need registration or own-client access.
Cards use a four-second server deadline, below the five-second tool timeout.
No card exposes passport data, arbitrary
files, chat contents or raw payment proofs.

By default, past events contribute a count, latest end date and up to five brief
name/date/ID summaries; their registration and financial details stay omitted. Active and
future event details are limited to 20; `events_truncated` asks the caller to select
one event. `history_event_id` explicitly requests details for that one event and
rechecks access there. It must match `event_id` when both are supplied.

`zinusya_people.list({event_id?,sort?,after?,limit?})` is staff-only and uses the
same privileged scopes. Sorts are `name` (default), `name_desc` and
`recent_activity` (latest accepted task first). Pages contain at most 20 basic
cards. Copy `next` unchanged as `after` with the same event and sort. The cursor
is a position, never a permission. Name/activity edits can move rows between
pages, so restart a traversal when a current complete list is required.

The server checks verified IAM identity, current local account, deployment scope,
event authority and private specialist bindings. Event reads share the event lock
with authority and binding changes. Every page and card authorizes again.

## Visibility and retained names

`zinusya_profile.get` includes `visible`, default `true` for existing and new
accounts. `zinusya_profile.set_visibility({expected_version,visible})` changes
only the caller's flag through the existing versioned, idempotent profile-write
protocol. `false` hides public search and card access. Staff access described
above remains available. Set `true` to restore public discovery. A stale version
does not change the flag; an operation receipt describes the original operation,
not the current visibility. This setting is independent of roles and bookings.

Migration 80 adds the flag and an observed-name table. It backfills current
profiles, profile-operation receipts and sender metadata in retained task requests.
The stored key function binds its dependencies at definition time so logical
restore COPY can recompute keys with an empty search path. Triggers retain subsequent Telegram/name/Latin changes from all writers. Unknown
old names cannot be reconstructed. Legal registration names are not directory
aliases. Startup installs table privileges through the existing runtime-grant
workflow. Rollback refuses to discard retained names or a hidden account; use a
reviewed forward migration instead. Older writers preserve the separate flag.

`zinusya_profile.set_gender({expected_version,gender?,pronouns?,source})` stores
gender and address preferences as bounded free text. `source=explicit` records a
user statement; `source=inferred` records an assumption from the name or available
data when language agreement requires it. Explicit values take priority, and the
backend rejects inferred writes over an explicit preference in the same field.
Supply at least one field; omit any field the statement did not confirm or infer.
Gender and pronouns have independent `gender_source` and `pronouns_source` labels.
Confirming only pronouns preserves any inferred gender and its label. When a new explicit
statement omits pronouns, old inferred pronouns are cleared rather than relabeled
as user-stated. A changed inferred gender also clears omitted guessed pronouns.
Already explicit pronouns are retained independently of gender. An empty string clears that field and its source. The setting is owner-only and shares profile
versions and immutable operation receipts. Ordinary name/language changes retain
it. Basic cards respect directory visibility and expose both explicit preferences
and labeled inferences when visible. Inferences retain `gender_source=inferred` or `pronouns_source=inferred` for their field.
The main prompt checks pronouns and
gender agreement before replying and consults the saved profile, including when
the user asked the bot to remember their preference. Forwarded authors remain
separate from the current user.

## Forwarded Telegram messages

Telegram `forward_origin` is stored in the normalized intent and passed to Loom
in both initial and steering input. The forwarding user remains the authenticated
actor and owner of the incoming files. The quoted content has a separate original
author: user, hidden-user display name, chat or channel, with Telegram's available
date, signature and original channel-message ID. A hidden author name is not a
resolvable identity. Telegram may omit usernames or other author information.

Forwarded text is encoded as quoted data. Attachment metadata and inspection
guidance are supplied separately, attribute captions and speech to the original
author, and never call them the forwarding user's request. Forwarded sticker/emoji
descriptions also stay separate from transport instructions and are attributed to
the original author. Matching sender IDs identify a self-forward as the current
user's past statement, while forwarding alone never renews a request or consent.
This includes explicit guidance
to avoid attributing first-person claims, names, requests or consent to the
forwarding user. Forwarded commands do not run as personal slash commands or
partner-start links, and a captionless forwarded photo does not launch the native
avatar path. Direct messages keep their existing representation.

Queued intents without the new field can recover origin from their stored raw
update. Legacy structured/native routes are completed without starting new
actions; existing receipts and deliveries remain intact. The user is asked to
state their request separately. Recent conversation excerpts also retain origin. Already dispatched or
checkpointed model requests are not rewritten. Forwarding metadata does not
reconstruct authorship for copied text or screenshots without `forward_origin`.

Sources: `internal/db/people*.go`, `internal/api/people.go`,
`internal/db/profile.go`, `internal/task/message_origin.go`,
`internal/task/intent.go`, `migrations/00080_user_names.sql`.
