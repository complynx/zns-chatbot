-- name: StoreOrderNotificationReceipt :exec
INSERT INTO bot.notification_deliveries(id,message_id) VALUES(sqlc.arg(id)::bigint,sqlc.arg(message_id)::bigint) ON CONFLICT DO NOTHING;

-- name: StorePassNotificationReceipt :exec
INSERT INTO bot.pass_notification_deliveries(notice_id,message_id) VALUES(sqlc.arg(id)::bigint,sqlc.arg(message_id)::bigint) ON CONFLICT DO NOTHING;

-- name: StoreMassageNotificationReceipt :exec
INSERT INTO bot.massage_deliveries(notice_id,message_id) VALUES(sqlc.arg(id)::bigint,sqlc.arg(message_id)::bigint) ON CONFLICT DO NOTHING;

-- name: MassageNotificationViewOpened :one
SELECT EXISTS(SELECT 1 FROM bot.massage_views WHERE owner=sqlc.arg(owner)::text)::boolean;

-- name: StoreFoodNotificationReceipt :exec
INSERT INTO bot.order_cards(owner,card_key,chat_id,message_id,view_hash)
VALUES(sqlc.arg(owner)::text,sqlc.arg(key)::text,sqlc.arg(chat)::bigint,sqlc.arg(message_id)::bigint,'')
ON CONFLICT(owner,card_key) DO NOTHING;
