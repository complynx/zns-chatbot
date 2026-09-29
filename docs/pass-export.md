# Pass workbook export

`GET /v1/passes/export`, the `/passes_table` command, and the pass-menu XLSX button
use the same domain export. Global booking administrators receive all current
events. Event payment administrators, including hidden administrators, receive
only their current events. Other users and users whose booking access was revoked
are denied. Current means `finishes_at` is later than the transaction timestamp.
This matches the authorization scope in `plugins/passes.py:2314-2440`.

The read-only repeatable-read transaction checks authorization, bounds, bookings,
profiles and payment attachments in one snapshot. The workbook has the source's
25 columns and one `Passes` sheet, a frozen header, an autofilter and deterministic
event/creation/Telegram-ID order. Dates are UTC strings with millisecond precision.
Stored participant prices are exported without applying today's tiers; a linked
partner's stored price contributes to the displayed pair price. Assignment tiers
are one-based. The immutable payment receiver takes precedence; legacy paid rows
without one fall back to the current payment administrator.

The export deliberately contains authorized legal names and payment metadata,
but never passport values or receipt bytes. Text is stored as XLSX string cells,
including text beginning with formula markers. XML-invalid text fails closed.
Limits are 10,000 rows, 16 MiB of projected input JSON, Excel's per-cell character
limit, and 20 MiB of final XLSX. Limit violations return an error, never a partial
workbook. HTTP responses disable caching. No workbook contents enter logs or chat
history; delivery records contain only message ID and filename.

The bot records successful file delivery to suppress completed-update replay.
As with the order exporter, a crash after Telegram accepts the file and before
the delivery record commits can duplicate a send. Transient transport errors are
propagated for retry. Export results and buttons use EN/RU catalogs.

Username, first name, last name and print name come from stored Telegram sender
metadata. The bot accepts only its validated private sender (`callback.from` for
callbacks), with matching existing owner and Telegram ID. It never provisions an
identity, changes permissions or infers legal names. First/last names retain raw
text; `print_name` and the public UI name join them with one space, matching
`telegram.py:user_print_name` for a valid Telegram user. Username and last name can
be cleared by a later valid update. A monotonic update-ID marker prevents retries
and older deliveries from restoring stale names. Missing/empty first names and
invalid or oversized fields are ignored as a whole, preserving synthetic seed
names. Application bounds are 256 Unicode code points per name and 64 ASCII
letters/digits/underscores for usernames. Legal profiles are never modified.
Historical users remain blank in these columns until a valid sender update or a
separate identity import supplies the source fields; no name splitting is inferred.

Current parity limitations: the proof identifier is the Go stored proof ID, not a legacy Telegram
file ID. The authenticated `APIClient.ExportPasses` is callable by host code; a new
model tool was not added in this bounded slice. The frozen functional stand has
not been rebuilt for this work. Built is not accepted.
