ALTER TABLE core.order_events ADD COLUMN transfer_instructions text NOT NULL DEFAULT ''
  CHECK (char_length(transfer_instructions) <= 1000);
ALTER TABLE core.order_admins ADD COLUMN region text NOT NULL DEFAULT ''
  CHECK (char_length(region) <= 80);

UPDATE core.order_events SET transfer_instructions =
  'ТЕСТОВЫЕ РЕКВИЗИТЫ. Получатель: Sandbox. Банк: Fake Bank. Не переводите реальные деньги.'
  WHERE id = 'sandbox-festival';
UPDATE core.order_admins SET region = 'Тестовый Минск'
  WHERE event_id = 'sandbox-festival' AND owner = 'bob';
