CREATE INDEX order_notifications_recipient_pending ON core.order_notifications(recipient,id)
  WHERE delivered_at IS NULL AND failure='';
CREATE INDEX orders_due_reminder ON core.orders(created_at,id)
  WHERE reminder_claimed_at IS NULL AND state IN ('unpaid','cash');
