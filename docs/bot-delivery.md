# Bot delivery

HTTP Bot API delivery is at least once. A connection failure or lost response may
have delivered a message. The bot retains that uncertainty and schedules only the
transport intent again. It never repeats the business operation or payment.

After the first uncertain outcome, at most three additional wire admissions are
allowed. Admissions after that outcome consume the durable budget, including
confirmed rate-limit responses. Authority denial, pacing and preparation consume
no resend. A replacement runtime preserves the counter.

Resends wait at least the configured uncertainty retry base multiplied by 1, 2 and 4 (normally
5, 10 and 20 seconds). A later provider deadline still applies. Exhaustion fails
the delivery with `telegram_uncertain_retry_exhausted` and releases followers.
The last uncertain attempt, reason and observation time remain factual metadata;
they do not prove the message was absent. Duplicates are allowed.

The trusted runtime stores the normalized original text, keyboard or uploaded
document bytes in private interaction storage before wire admission. Host admission
receives only a bounded reference and hash. Ready, the exact attempt, the private
snapshot and resend budget commit together. An uncertainty chain reuses those
contents and its original continuation; each admission still checks the current
recipient, source, version and permissions. New intents render current UI.

Private wire storage belongs to its history generation and is removed with saved
results when history is deleted. A stale generation or revoked source cancels the
delivery. A missing or corrupt previously captured original fails visibly with
`original_wire_unavailable`. Neither case sends again or revives a continuation.
Historical unknown intents without a capture may prepare their next authorized
wire once; this does not establish what the historical provider delivered. Later
resends reuse that capture. No model or business operation is repeated.

Telegram rate limits without retry_after retain their separately configured
cooldown fallback (normally 30 seconds), including inside an uncertainty chain.

A late confirmed response can complete the same uncertain attempt before another
admission. After a new admission, the previous response is stale. A positive
receipt for a failed or cancelled attempt records only its message ID when it
matches the last uncertain attempt. Terminal status, queue position and business
continuation remain unchanged.

TODO: assess MTProto after migration and production launch. The current transport
remains HTTP Bot API.

Written by provider_reboot_delivery_developer (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
