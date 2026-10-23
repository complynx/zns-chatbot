# Readable payment approval summaries

A newly prepared payment review now asks a question in the reviewer's selected
bot language. In automatic mode, Telegram ru/be/uk maps to Russian; other
languages map to English. The bot default is used when Telegram has no language.
The summary shows the event name and payer, decimal amounts with the frozen pass
currency and scale, the balance before and after the proposed decision, deposit
requirements, and proof timing in the event's IANA zone. A rejection credits zero.
It does not describe the payment as confirmed before the required decision.

Amounts use integer arithmetic. Event/payer/reason metadata is quoted and placed
in a literal block; embedded Markdown cannot create links, emphasis or mentions.
The complete exact argument block remains available in the approval message.
This changes presentation only: permissions, argument hashes, workspace revisions,
expiry and payment effects keep their existing rules.

The description is stored with the preparation. Repeating its original operation
keeps the original description, arguments, expiry and proposal ID, including
after a profile or language change. Earlier stored preparations retain their old
text. This is a schema39 source change with no data migration.
