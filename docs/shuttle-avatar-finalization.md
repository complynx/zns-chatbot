# Avatar draft finalization

## Persistence contract

`AvatarDraftFinalize` contains operation ID, draft ID and expected version.
It cannot supply a second transform or a Loom run. `AvatarDraftFinalizePreflight`
reconciles successful operations before source access or rendering. A new request
must reference the current owned version and a compatible pinned frame.

Rendering belongs outside transactions. `CommitAvatarDraft` and its native
`CommitMiniAppAvatarDraft` wrapper serialize admission with owner edits, reconcile
the operation again, then validate current version, original hash, pinned frame,
transform, processor version, JPEG signature/hash/dimensions and existing bounds.
The existing `MatchesTransform` validator distinguishes v1 identity from v2
edited output. Native commit uses the existing bearer/generation check under
the owner lock. Expiry before commit rejects admission; expiry after commit
does not erase the durable intent.

One transaction writes immutable JPEG bytes, a distinct `AvatarDraftReceipt` and
one `shuttle_avatar_deliveries` row in pending state. No task or task outbox row
is created. The recipient Telegram ID is captured for later delivery eligibility
checks. Finalization does not freeze the draft: subsequent edits change the draft
version while the artifact remains a snapshot. Replay returns the original
receipt even after later edits or owner quota exhaustion, without rendering again.

The20-artifact owner limit counts both legacy and draft-origin artifacts.
Concurrent agent/native commits of the same operation return one receipt and
create one artifact/intent. Reusing an operation with different arguments or
another artifact origin conflicts. Reads remain scoped to the current owner.

## Schema and compatibility

Migration56 makes the three legacy provenance fields nullable, then checks an
explicit exclusive choice. Legacy rows require task, outbox delivery and source,
with no draft fields and native=false. Draft rows require draft ID and positive
version, with all legacy fields absent. Explicit null checks prevent SQL UNKNOWN
from admitting a partially populated provenance branch. A composite draft-owner
FK prevents referencing another owner's draft.

The separate delivery table has a unique artifact, owner-consistent artifact FK,
captured recipient, bounded state/receipt fields, positive version, attempt ID
and retry timestamp. It stores no second JPEG copy. Application commit creates
artifact and intent together; the migration does not enforce existence of a
reverse intent reference on every draft artifact inserted by arbitrary SQL.

Legacy request hashing and receipt JSON are unchanged. Legacy receipt/operation
and content readers explicitly exclude draft origin rather than decoding a new
receipt as a legacy object with zero task/source IDs. New operation/content reads
return the distinct draft receipt. The original legacy status path still joins
the task outbox and cannot represent the new delivery table.

Down takes exclusive NOWAIT locks before checking draft artifacts or delivery
history. It refuses either kind of new history. Export/retention precedes any
separate deliberate destructive removal; export does not bypass this guard.
Empty Down/Up keeps legacy artifacts. Tests of historical migrations20/55 now
remove extension56 first and restore it after recreating the older tables.
