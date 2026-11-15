# Personal messages and broadcasts

Shuttle personal messages and broadcasts use `zinusya_communications`:
`audience` lists permitted recipients with event, registration-state and dance-role
filters; `prepare` fixes the text, category and explicit recipient list; `status`
returns delivery counts. The author confirms or cancels with native Telegram buttons.
If the author's category exceeds the agent's assessment, the same confirmation offers
keep, use the assessed category, or cancel and explains the trust consequences.

Advertising and information have independent event/general and global subscription
settings. Information opt-out warns about missing event information. Management and
restore buttons use no inference. Warnings cannot be disabled. Rights and subscriptions
are rechecked before delivery; an unknown Telegram outcome is never resent automatically.

Literal mode keeps the body unchanged and adds sender attribution. Personalized mode
uses a frozen recipient memory snapshot in an isolated `native_files` Loom run billed
to the service account. Every delivery creates a dedicated Loom chat before
reading recipient memory. An uncertain chat-creation outcome fails without
replaying that non-idempotent call. It requires delegated memory reads and service-account chat/run
permissions, supported Loom execution profiles, and service credits. It does not fall
back to recipient billing. Drafts expire after 30 minutes; one draft accepts up to
1000 recipients and a text body of 2800 UTF-16 units. Apply migration 82 through the
existing migration procedure before starting the updated service.
Participant contact discovery lists their event administrators and ambassadors.
A global administrator can be addressed after identification through exact
`people.resolve` (username or Telegram link), visible `people.search` results,
or by replying to their delivered message. Hidden global
administrators are not enumerated by the event contact directory.
Global administrators remain contactable by a known account identifier even
when their directory profile is hidden; the permission result can reveal that role.

The native confirmation shows the event, mode and recipient names (the first five
for a broadcast). Preparation and confirmation validate the complete frozen
audience in one query. If access changes before confirmation, prepare a new draft
with a fresh audience instead of silently changing the confirmed recipient list.
Delivery rechecks current rights and subscriptions for each recipient.

Communications use a separate River queue with two workers, so admission waits
do not occupy the workers handling conversations.

Personalization admits at most four active generations per deployment. Queue
admission retries use exponential backoff from two seconds to one minute,
plus up to 25 percent jitter, and run before IAM checks. After
admission, polling starts at two seconds and backs off to one minute. Existing runs are polled with service
authentication; recipient memory and IAM are checked again only when required for
setup or final delivery. A generation has a 30-minute deadline from admission.
Completed text remains available for Telegram retries after that deadline.
If personalized output adds or modifies links or mentions, the approved literal
body is delivered instead. Its Telegram link previews are disabled so memory cannot leak through an
automatic preview request.
Permanent setup rejection, non-shutdown suspension and states that require user
action fail the delivery. Known unfinished runs receive a bounded best-effort
cancel request when delivery fails. Shutdown pauses resume with an idempotent request.

The local private memory snapshot is removed once the Loom run ID is saved, and
on terminal delivery outcomes. Rendered personalized text is retained for a
retryable Telegram send and removed once the outcome is terminal. The upstream
Loom run follows Loom's own retention policy.
