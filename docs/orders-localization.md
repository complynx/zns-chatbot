# Order interface localization

Deterministic order cards, buttons, receipt prompts/errors, exports and payment
notifications use the authenticated user's saved GUI locale. English and Russian
share typed catalog IDs. Meal-day counts use the existing CLDR cardinal engine;
prices use locale-aware number formatting. Data such as names, order references
and administrator-supplied payment details remain unchanged.

Changing `/language` refreshes existing order cards in place. A saved deterministic
notice from the old locale is replaced by the current localized menu title; native
agent replies retain the language chosen for the current question. Domain states,
callback commands, versions and authorization rules are unchanged. Notifications
use the recipient's locale, including an administrator who never opened `/orders`.

Local checks cover the actual language callback, English/Russian order labels,
recipient notification locale, preserved conversational language and CLDR counts
0/1/2/5/11/21/22. Existing receipt and order tests retain their domain assertions;
only expected visible labels and numbers change. Browser scripts for orders,
proofs, notifications and meals use the localized Russian status/number strings.

## Functional QA scope

On a frozen Telegram-like sandbox, check both English and Russian separately with
mouse and touch:

- Open orders, create/edit extras, inspect prices and meal-day counts, switch the
  GUI language and verify existing cards and buttons refresh in place.
- Open payment instructions, choose cash, receive an administrator notification,
  inspect its current card, accept/reject and verify the owner's current view.
- Start receipt upload, send a file, choose its order, open the stored proof as the
  authorized reviewer, cancel/retry and use a stale callback.
- Exercise export permission failures and unavailable/deleted/off-page cards.
- Ask an English question with Russian GUI selected, then switch GUI language;
  the conversational answer must retain its own language.

The local implementation gates do not constitute browser acceptance. Independent
Code QA and a fresh frozen-stand Functional QA are still required.
