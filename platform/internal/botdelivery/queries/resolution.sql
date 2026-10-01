-- name: InsertBotDeliveryAttempt :exec
INSERT INTO bot.delivery_attempts(bot_id,operation_key,effect_key,attempt,method,payload_sha256,continuation)
VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: GetBotDeliveryAttempt :one
SELECT method,payload_sha256,continuation FROM bot.delivery_attempts
WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND attempt=$4;

-- name: GetBotDeliveryResolution :one
SELECT request,result FROM bot.delivery_resolutions WHERE actor=$1 AND operation_key=$2;

-- name: InsertBotDeliveryResolution :exec
INSERT INTO bot.delivery_resolutions(actor,operation_key,request,result) VALUES($1,$2,$3,$4);
