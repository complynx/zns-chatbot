# Payment proof checkpoint

Port document receipt submission and payment review through Telegram.

- Explicit order selection before upload; persist selection across restart.
- Download real document bytes through the Telegram Bot API; bounded size.
- PostgreSQL proof storage, owner-bound IDs, authenticated owner/admin retrieval.
- Bind submit to selected version and retry key. Stale upload cannot change order.
- Country/admin choice, cancellation, admin document review, accept/reject.
- Fake Telegram supports file input, documents, download and multipart sending.
- Mouse/touch upload/review, foreign proof rejection, retry and persistence tests.

Proactive notifications, reminders and exports follow this slice. Existing
payment inbox remains available through `/orders`. No production parity claim.

## Evidence

Implemented the document path through the Telegram-like browser, Bot API,
authenticated Core API and PostgreSQL. Receipt review sends immutable stored
bytes; it does not depend on the original Telegram message. File owner checks,
version/attempt checks, persisted upload selection and retry binding have
PostgreSQL tests. Original-file replacement/deletion cannot change review bytes.

Strict Go/JS lint, formatting and PostgreSQL race suites passed. The full local
quality gate passed before the receipt-delivery correction; after correction,
all PostgreSQL race tests, lint and the focused mouse/touch document browser
suite passed again. Fresh Code QA and independent Functional QA passed. The
functional report is `qa.local/functional-proofs-pass1/report.md`; it includes
two restarts, pending-selection persistence, mouse/touch file download, owner
isolation, stale attempts and paid locks. The shared fixture covers BE routing;
independent RU and closed-deadline cases need the next stand controls.
