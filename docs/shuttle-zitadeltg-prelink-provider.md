# Zitadeltg prelink hook

The trusted provider invokes the application's [private prelink API](shuttle-telegram-prelink-api.md) after verifying Telegram and before relaying the IAM login. An unverified browser identity or matching email string is insufficient authority.

Each opted-in bot has a separate prelink endpoint, audience, service bearer, organization and provider identity. Empty configuration disables the hook; partial or unsafe values fail startup. The dedicated proof key is a canonical base64url encoding of 32 bytes, distinct from the bot secret. Prelink and relay audiences are distinct. The exact endpoint contract has no query or fragment.

For opted-in bots, the relay uses the verified reserved telegram.invalid synthetic address matching the prelink intent. Other bots retain their email-domain policy. Enabling one bot must not change another bot's claims. The relay JWT retains its IAM audience and carries no prelink-purpose claim.

Retain the identical compact proof and JTI on transport retry. No successful matching prelink receipt means no token relay. Current bot/organization/provider binding, proof freshness and signature remain authoritative. The complete flow creates or finds the canonical account and finishes authentication without manual linking or registration steps.

See the [trust plan](shuttle-telegram-prelink-plan.md), [verifier](shuttle-telegram-prelink-verifier.md) and [storage contract](shuttle-telegram-prelink-storage.md).
