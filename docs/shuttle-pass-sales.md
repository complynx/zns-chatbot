# Event pass sales

Set an event `slug` to give participants a stable Telegram entry such as
`/passes grodno-26`. Slugs are unique within the bot and organization.
The canonical UUID spelling is reserved for the event's internal ID. The
event administration API and the approved event change operation accept the
same event document. These operations replace the full document.
Telegram resolves a public slug when it accepts the command. Renaming or
reassigning that slug later keeps queued commands tied to the original event.

Participants can open `/passes` and select an event, or send a partial command
such as `/passes grodno-26 --role leader`. Options include `--solo`, `--pair`,
`--invite CODE`, `--legal-name "Full Name"`, `--passport-country` and
`--passport-number`. A personal invitation supplies a Telegram message draft
containing `/start /passes grodno-26 --invite CODE`. The recipient sends the
draft to claim the code. The invitation link contains only the event and code.

Set `registration.sales_opens_at` and `closes_at` to absolute instants.
Participants can inspect a future sale. Registration opens at the first
instant. Registration, assignment and payment windows end at the second.
Unassigned participants stay on the waiting list at closing.
Enable `auto_assign` to allocate passes
from the waiting list. Existing events without this flag use manual assignment.
Both modes share role capacities and per-owner pass and payment records.

For automatic sales, ordinary registrations enter the queue on completion.
During a promo tier, selecting the ticket grants up to one hour to complete a
solo registration, capped at sales close. An eligible earlier selection retains
priority while the participant fills in their details. The next tier stops new
promo selections. A timely completed registration can still use its original
selection. When tiers have separate role quotas, the click records the promo
tier available to each eligible role at that instant. Completing registration
uses the tier for the chosen role. A confirmed choice to register as a pair releases the solo promo
hold. Each awarded promo place belongs to one completed registration, including
when the organizer switches to manual assignment.
The participant's queue position and the organizer's roster use the ticket
selection time for promo winners and the registration completion time for everyone else.
Promo winners can receive their pass outside the configured role percentages.
Hard role capacities still apply. While a winner is waiting for a pass, keep
the registration rules and their currency and scale in place. The minimum deposit cannot exceed the winner’s frozen price. An organizer
can still change capacity or issue the pass manually at its awarded price.
Each pricing tier has an amount and a start time. A tier may open when the
previous quota is exhausted. Set `blocked_by_date` to enforce its start time
even if the previous tier fills early.

Configure `max_leader_percent` and `max_follower_percent` independently for
automatic allocation. Omitted limits use 52 percent. Hard role capacities still
apply to manual assignments. Pair invitations and replacements require two
waiting participants. A code can be claimed once, and the claim can precede
completion of the claimant's registration. The earlier of the two individual
queue times determines when a pair is considered. Each participant receives
their own pass and payment terms. Dissolution preserves the individual records.

With a configured sales opening or automatic assignment, the form requires the
participant's full legal name. In `details_requirements`, enable
`passport_number` or `passport_photo` as needed. Use `passport_countries` to
distinguish the listed issuing countries from other passports. Passport
category and dance role are separate fields.

The organiser's Mini App provides an event-scoped pass list and XLSX export
of registrations and payments. A separate download in the same panel supplies
the identity list for the selected event. The ZIP contains `participants.xlsx`
for participants with an assigned pass. The columns follow the event's
registration requirements. Each row includes fields requested at enrollment and
any additional required fields the participant later saves in that event's
protected form. If requirements expand, a waitlisted participant or pass holder can
open `/passes <slug>` and use the form button to submit the additional fields,
including after sales close while registrations are enabled. The export omits
fields the event no longer requires. For older registrations, a completed details
request from the same event version can establish which fields were requested.
Fields without that evidence remain blank. If passport photos are required, the archive
includes the original images and the workbook identifies the file for each
registration. The ordinary roster and payment export omit these private fields.
The download requires a current organiser session and an enabled registrations
module. Before sending the attachment, the server rechecks the session, event
permissions and policy, and verifies the current organiser role with IAM.
Organizers can also ask Zinusya to send the protected ZIP to their own private Telegram chat.
The tool confirms the event and uses the caller's linked Telegram account. Only the delivery status reaches Loom.
If Telegram delivery is uncertain, check the private chat. Archives over the Telegram document limit remain
available through the Mini App download.

A pass return releases the event place and records the agreed refund amount.
A sold promo place still counts toward the promo limit after a return,
including when an organizer issued the discount manually. New passes record
the tier's promo status at issuance. The upgrade classifies older tiers from
their recorded awards or verified assignment history.
Record the refund after the external payment is complete. Both actions retain
the history of confirmed payments and the other participant's pass. The owner
sees settlement amounts while the event's registrations module is enabled.
An authorized organizer can still reconcile historical returns after disabling it.

### Reviewing Telegram and Mini App copy

The shared Russian and English strings are in `internal/passcopy/messages.json`.
Collect all text changes from a QA or review cycle into one batch. Give Muse
Spark the batch and a small selection of anonymized replies from ZiNuSya's
conversations, then give Opus 5.5 the revised batch and the same examples.
Prefer one meaningful icon alongside the text on action buttons. Keep quoted
button labels in sync with the rendered controls in both languages.
