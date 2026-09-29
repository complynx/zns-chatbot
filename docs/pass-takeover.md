# Payment contact takeover

Global booking administrators and payment administrators for the selected event
can open payment-contact controls for an exact Telegram user ID or a shared
Telegram contact. The target must already have a registration in that event.
The command applies to the target and their reciprocal partner.

The current payment contact is separate from the recorded receipt receiver.
Taking over changes only the contact. It does not allocate a pass, alter its
price or state, grant review rights, or change an existing receipt. The target
view shows authorized display names and person links, with localized text when
no receiver has been recorded. Participant booking views also show an exact
current contact who is a global administrator outside the event contact roster.

A new receipt submitted after takeover records the current contact as receiver
when that contact remains a global or event payment administrator. A global
administrator does not acquire receipt review rights through takeover. Event
payment administrators retain the pending review queue. Automatic receipt review
notifications are delivered only to event payment administrators, as in the
Python receipt handler. A removed administrator cannot receive a new receipt.

Received-only requires the selected registration to be paid. It fills missing
receivers only on paid members of the pair without an actual receipt attempt or
an existing assignment backfill. It can fill the partner even when the selected
member already has an actual receiver. Actual receipt provenance remains immutable.
The backfill records the administrator and recording time, bound to the current
assignment. It does not fabricate a receipt, payment time, or acceptance decision.
Export uses actual receipt receiver first, assignment backfill second, and the
existing paid-only current-contact fallback last. A new assignment does not
inherit the old backfill.

Current-event menus remain unchanged. For a past event, explicitly provide its
exact event ID together with the target Telegram ID, or continue from an already
opened target editor. The authorized exact-target read supports finished events;
there is no unrestricted past-event catalog or identity search. The same typed
commands back manual controls and agent actions. Rights and versions are checked
at execution, and buttons are bound to their owner. Retries with the same command
key do not repeat changes or enqueue duplicate notices.

Contact changes enqueue durable participant notifications. Delivery skips a
notice when the contact has since changed again. Delivery records and history
survive restart. Telegram can still duplicate a message if it accepted a send
but the process lost the response before persisting the delivery record.

Focused integration coverage includes English/Russian controls for past events,
read scope, denied review rights, new receipts after global-only takeover, mixed
couple backfills, immutable actual receivers, export cells, concurrent replay,
notification currentness, delivery, and history. These checks do not replace the
independent Code QA and Telegram-like browser Functional QA gates.
