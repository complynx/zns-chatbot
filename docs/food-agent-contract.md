# Food receipt agent contract

This document describes the public typed planning contract for food receipts.
Use it with [model-fixtures.md](model-fixtures.md) and the stand's published
addresses. A programmed provider exercises the normal host actions and permission
checks; it does not demonstrate OCR or language-model reasoning.

## Available operations and field names

Food receipt interpretation and selection use `view: "media"` and
`media_action`. Meal and activity payments are independent destinations.

There is no `food_action`, `food_event`, or `view: "food"` in the Plan contract.
Do not put `begin_payment`, `submit_proof`, `food_meals`, or
`food_prepare_activities` in a Plan's generic `action`. These are not supported
agent action names. Food ordering, payment preparation and payment review remain
available through their food UI controls; they do not have separate typed Plan
operations. `order_action` describes the separate modern-orders feature.

The food event is supplied to the model as `event_id` inside host-owned
`food_target` or `food_pending_hint` input. The model's receipt proposal contains
`order_id` and `food_kind`, not an event field.

| Media proposal field | Food receipt value |
| --- | --- |
| `media_id` | Actual current attachment ID or the ID of the pending receipt being answered |
| `intent` | `receipt` to interpret/select a receipt; `clarify` to ask its purpose; `cancel` only for an explicit close request |
| `order_id` | Actual `food_target.order_id` for a selected food destination; otherwise empty |
| `food_kind` | `meals` or `activities` for a selected food destination; otherwise empty |
| `registration_event` | Empty for food; this field is for pass receipts |
| `amount` | Empty when unknown/unneeded, or a positive decimal string with at most nine integer and two fractional digits |
| `currency` | Empty when unknown/unneeded, otherwise `RUB` or `BYN` |
| `start_ms`, `end_ms`, `frame_count` | All `0` for receipts |

A nonempty `food_kind` requires `intent: "receipt"`, a nonempty `order_id`, and
an empty `registration_event`. An order ID or matching amount alone does not
select between meal and activity payments. Receipt submission does not approve
payment; the authorized review flow is separate.

Fixture and Remote plans may omit unused action objects and unused media strings
or numbers; omitted values default to null actions, empty strings and zero
numbers. `view` must be supplied. Use the fully expanded example below when a
provider requires a strict JSON schema. Unknown fields, conflicting actions and
invalid enum values are rejected. Do not add actor, permission, event, version,
generation or proof fields to `media_action`.

## Host input and actual IDs

For a current image, model input can contain `attachment.id`, `filename`, `mime`
and private attachment data. Pending attachments appear in
`media_context.pending`; each entry has its own `id` and currently displayed
`choices`. Use the actual ID from that input. Never invent an ID or substitute a
Telegram message ID, file ID, callback token, order ID or event ID for `media_id`.

For a later text reply, use the pending receipt's ID. The proposal has no
`reply_to_message_id` field. A Telegram reply is conversational context; it does
not replace the required receipt ID. For an eligible voice answer to a pending
receipt, the destination is still the earlier receipt's ID, not the voice file.

This is an illustrative **input excerpt**, not a complete provider request or a
fixture to paste unchanged. Uppercase IDs must be replaced with observed values:

```json
{
  "text": "This receipt is for activities.",
  "language": "en",
  "media_context": {
    "pending": [
      {
        "id": "ACTUAL_PENDING_MEDIA_ID",
        "choices": [
          {
            "action": "food_activities",
            "order_id": "ACTUAL_FOOD_ORDER_ID",
            "version": 5,
            "label": "Activities · ACTUAL_FOOD_ORDER_ID",
            "food_target": {
              "event_id": "ACTUAL_FOOD_EVENT_ID",
              "order_id": "ACTUAL_FOOD_ORDER_ID",
              "kind": "activities",
              "version": 5,
              "generation": 0
            }
          }
        ]
      }
    ]
  }
}
```

`food_target` is read-only host evidence for a displayed destination. The model
returns its order ID and kind only. The host supplies the authenticated actor,
event, current authorization and bound version/payment generation. The host
rechecks access, eligibility, attachment availability and destination freshness
when acting. A supplied Plan is never a permission grant, and a retry cannot
retarget a receipt to a different destination.

An optional `food_pending_hint` has this shape:

```json
{
  "event_id": "ACTUAL_FOOD_EVENT_ID",
  "order_id": "ACTUAL_FOOD_ORDER_ID",
  "kind": "meals",
  "state": "pending"
}
```

It describes a current payment prompt. It is not an attachment, a displayed
receipt choice, or authorization to consume the next message. Unrelated text or
uploads must remain unrelated. Its absence does not establish that no food order
exists.

## Receipt recognition, preparation and selection

When the user identifies an upload as a receipt without selecting a destination,
a valid interpretation Plan is:

```json
{
  "view": "media",
  "text": "Please choose the payment this receipt belongs to.",
  "media_action": {
    "media_id": "ACTUAL_ATTACHMENT_ID",
    "intent": "receipt"
  }
}
```

The host evaluates the current authorized context and presents applicable
choices. Food selection uses a subsequently displayed `food_target` and the
user's current unambiguous payment-kind instruction. Initial upload recognition
is distinct from a later destination selection.

If payment preparation is needed, the input may instead contain a choice with
`action: "food_prepare_meals"` or `"food_prepare_activities"`, plus `order_id`,
`version` and `label`, but **without** `food_target`. These are labels of actual
host-generated buttons, not values to return as Plan actions. The user activates
the relevant Pay button. Preparation does not submit the attachment. Use the
newly displayed receipt destination when the user subsequently selects it. Do
not manufacture a `food_target` from a preparation choice or pending hint.

For an explicit activity-receipt selection with a currently displayed matching
destination, the fully expanded Plan is:

```json
{
  "view": "media",
  "text": "You selected the activity receipt destination.",
  "action": null,
  "order_action": null,
  "profile_action": null,
  "knowledge_action": null,
  "script_action": null,
  "history_action": null,
  "registration_action": null,
  "lineup_action": null,
  "media_action": {
    "media_id": "ACTUAL_PENDING_MEDIA_ID",
    "intent": "receipt",
    "amount": "",
    "currency": "",
    "order_id": "ACTUAL_FOOD_ORDER_ID",
    "registration_event": "",
    "food_kind": "activities",
    "start_ms": 0,
    "end_ms": 0,
    "frame_count": 0
  }
}
```

For meals, use the actual displayed meal destination and `food_kind: "meals"`.
Do not declare submission or approval merely because the provider returned a
Plan. Establish the outcome through the subsequent host response/current food
state. An unavailable, expired, changed or unauthorized destination cannot be
made valid by changing the model's output coordinates.

Examples of current user inputs:

| Intent | English | Russian |
| --- | --- | --- |
| Select meal receipt | This receipt is for meals. | Это чек за питание. |
| Select activity receipt | This receipt is for activities. | Это чек за активности. |
| Ask which kind | Is this for meals or activities? | Это чек за питание или активности? |
| Unrelated question | What time does check-in start? | Во сколько начинается регистрация? |

An ambiguous payment-kind question requires clarification, including when only
one kind is currently available. An unrelated question is answered without a
media action. A valid text-only response that leaves pending receipt work alone
is `{"view":"workflow","text":"Is the receipt for meals or activities?"}`
(or the corresponding Russian text). The exact user wording is not a command
syntax; fixture providers must supply a Plan consistent with the actual message.

## Fixture installation example

For an already pending receipt whose destination is displayed, POST this object
to the fake server's `/lab/model/fixtures` with `Content-Type: application/json`
and `X-Sandbox: 1`. Replace both IDs with actual observed values. Here 101 is the
published synthetic Alice identity; use the identity configured by the stand.

```json
{
  "input": {
    "user": 101,
    "text": "This receipt is for activities.",
    "language_code": "en"
  },
  "steps": [
    {
      "expect": { "text": "This receipt is for activities." },
      "plan": {
        "view": "media",
        "text": "You selected the activity receipt destination.",
        "media_action": {
          "media_id": "ACTUAL_PENDING_MEDIA_ID",
          "intent": "receipt",
          "order_id": "ACTUAL_FOOD_ORDER_ID",
          "food_kind": "activities"
        }
      }
    }
  ]
}
```

Installation and text delivery are atomic; the returned update ID identifies the
interaction. For Russian, use `language_code: "ru"` and the exact same Russian
text in `input.text` and `expect.text`. Expectations are partial typed input
objects, but supplied arrays must match their complete lengths and order.

The atomic input helper sends text, not files. Perform uploads in the rendered
Telegram-like UI. Do not predict upload IDs or install a fixture after an upload
has already been delivered and assume the provider has not run. For initial
upload interpretation, use a configured synthetic Remote provider that receives
the actual current input; for text fixtures, use an already observed pending ID.

## Remote provider wire format

For `model.provider: remote`, the host POSTs the **input object directly** to the
configured model URL plus `/plan`. Food hints, media context and attachments are
top-level fields of that object. It is not wrapped in `{"input": ...}`. Image
attachment `body`, when present, is base64-encoded bytes; treat it as private data.

Return HTTP 200 with the **Plan object directly**, such as the expanded selection
Plan above. Do not wrap it in `{"plan": ...}`, `{"output": ...}`, an OpenAI
response envelope, or a fixture `steps` array. The fixture transport uses this
same request/response body format at its configured `/lab/model/plan` route; its
lab scope travels in host-supplied headers. The `input`/`steps`/`plan` nesting in
the preceding example belongs only to the fixture-installation endpoint.

The provider receives planning evidence and returns a proposal. It cannot set
the acting owner, grant review rights, approve a payment, create a food receipt
target, or execute a button callback by returning JSON.

Written by food_bot (gpt-6-sol/Codex)
on behalf of Daniel Drizhuk
