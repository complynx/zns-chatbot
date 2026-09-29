-- name: GetBotDeliveryIntent :one
SELECT bot_id,operation_key,effect_key,owner,chat_id,reference,state,phase,
 target_message_id,attempt,not_before,message_id,continuation_done,receipt
FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3;

-- name: LockBotDeliveryIntent :one
SELECT bot_id,operation_key,effect_key,owner,chat_id,reference,state,phase,
 target_message_id,attempt,not_before,message_id,continuation_done,receipt
FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 FOR UPDATE;

-- name: CompleteBotDeliveryReceipt :exec
UPDATE bot.delivery_intents SET continuation_done=true
WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND state='sent' AND message_id=$4;
