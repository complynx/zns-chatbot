# Acknowledging a resolved reply keyboard

Real Telegram payment-review QA showed "This choice is no longer active" after a
valid free-text approval. The action completed correctly, but this cleanup text
could suggest that the answer was rejected.

The source fix preserves immediate keyboard removal while the agent continues.
A resolved question or decided approval now says "Response received." or
"Ответ получен." Cleanup still uses its independent durable outbox message and
presentation revision. It does not overwrite inline protocol approval buttons or
rely on another pending message being delivered again after deduplication.

The acknowledgement is selected using the retired keyboard's question/approval
type. A historical decided approval cannot turn a newer unanswered question's
expiry into a success acknowledgement; an answered old question cannot do that to
an expired approval either. Unanswered expiry keeps its existing warning.
No tags, text intent parser, new consent or protocol decision was introduced.
