ALTER TABLE core.users ADD COLUMN language text NOT NULL DEFAULT ''
  CHECK (char_length(language) <= 64);
UPDATE core.users SET language='ru' WHERE id IN ('alice','bob','visitor');

ALTER TABLE core.order_events ADD COLUMN transfer_instructions_localized jsonb NOT NULL DEFAULT '{}'
  CHECK (jsonb_typeof(transfer_instructions_localized)='object');
UPDATE core.order_events SET transfer_instructions_localized = jsonb_build_object(
  'ru', transfer_instructions,
  'en', 'TEST PAYMENT DETAILS. Recipient: Sandbox. Bank: Fake Bank. Do not transfer real money.')
  WHERE id='sandbox-festival';
