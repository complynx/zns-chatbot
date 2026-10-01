# Bot delivery

HTTP Bot API delivery is at least once. A connection failure or lost response may
have delivered a message. The bot retains that uncertainty and schedules only the
transport intent again. It never repeats the business operation or payment.

After the first uncertain outcome, at most three additional wire admissions are
allowed. Admissions after that outcome consume the durable budget, including
confirmed rate-limit responses. Authority denial, pacing and preparation consume
no resend. A replacement runtime preserves the counter.

Resends wait at least the configured fallback multiplied by 1, 2 and 4 (normally
5, 10 and 20 seconds). A later provider deadline still applies. Exhaustion fails
the delivery with `telegram_uncertain_retry_exhausted` and releases followers.
The last uncertain attempt, reason and observation time remain factual metadata;
they do not prove the message was absent. Duplicates are allowed.

TODO: assess MTProto after migration and production launch. The current transport
remains HTTP Bot API.

Written by provider_reboot_delivery_developer (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
