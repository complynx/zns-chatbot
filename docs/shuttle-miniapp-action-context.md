# Mini App profile action context

The working tree records successful Mini App name/language requests alongside
their immutable profile operation receipts.
Migration34 adds
shuttle_miniapp_profile_actions; existing receipt/profile data is not rewritten.


## Transaction and replay

MutateMiniAppProfile first validates the current owner/session under the existing
locks. The profile change, operation receipt and action insert commit together.
A journal failure rolls back the profile and receipt. The journal uses the owner
and operation UUID as its key: replay returns the original profile result without
another history entry or a later completion timestamp. Invalid/stale/session-denied
requests produce no completed action. Name/language requests that succeed without
changing a value remain successful requests and may appear in history.

Only set_name and set_language are accepted by this Mini App write entry point.
The journal stores action kind, operation UUID and database UTC completion time;
it references the existing receipt for its original profile version. Names,
credentials and session tokens are not duplicated. Existing pre-migration form
operations are not backfilled. A successful explicit replay through the upgraded
Mini App can record the historical receipt once at that replay's acceptance time.

## Next user-triggered turn

Before freezing a new Loom request or text-bearing control input, the bot reads
at most five newest completed Mini App profile requests for the authenticated
owner and current task. It excludes actions completed after that task was created.
The context labels these records as untrusted history, not instructions, consent
or a new request; historical versions are not claims about current profile state.
The normal current profile snapshot remains available, and writes still require
fresh versions/expectation checks. Facts can appear in multiple subsequent turns;
this is a bounded snapshot, not an exactly-once notification stream.

The form never starts Loom proactively. Telegram command history retains its
separate bounded context. Other owners' actions and another owner's task cannot
be used to read this history. Existing checkpointed Loom requests remain frozen:
retries do not replace their context with newer web actions.

## Rollout and verification

Apply the additive migration before starting the upgraded binary. Old binaries
can coexist with the expanded schema, but their Mini App saves do not create
journal facts. Keep migration34 when rolling back a binary; do not drop retained
action history. Down migration refuses while the journal contains rows; export
and an explicit retention decision are required before removing them.
