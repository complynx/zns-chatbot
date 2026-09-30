package botdelivery

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func Read(
	ctx context.Context,
	db dbgen.DBTX,
	botID int64,
	ref delivery.Reference,
	lock bool,
) (Intent, error) {
	var row dbgen.GetBotDeliveryIntentRow
	var err error
	queries := dbgen.New(db)
	if lock {
		locked, readErr := queries.LockBotDeliveryIntent(
			ctx,
			dbgen.LockBotDeliveryIntentParams{BotID: botID, OperationKey: ref.Key, EffectKey: ref.Effect},
		)
		row, err = dbgen.GetBotDeliveryIntentRow(locked), readErr
	} else {
		row, err = queries.GetBotDeliveryIntent(
			ctx,
			dbgen.GetBotDeliveryIntentParams{BotID: botID, OperationKey: ref.Key, EffectKey: ref.Effect},
		)
	}
	if err != nil {
		return Intent{}, core.DatabaseOperationError(err)
	}
	i := Intent{
		BotID:            row.BotID,
		Operation:        row.OperationKey,
		Effect:           row.EffectKey,
		Owner:            row.Owner,
		Chat:             row.ChatID,
		State:            delivery.Kind(row.State),
		Phase:            row.Phase,
		Target:           row.TargetMessageID,
		Attempt:          row.Attempt,
		NotBefore:        row.NotBefore,
		MessageID:        row.MessageID,
		ContinuationDone: row.ContinuationDone,
	}
	if err = json.Unmarshal(row.Reference, &i.Reference); err != nil {
		return i, err
	}
	err = json.Unmarshal(row.Receipt, &i.Receipt)
	return i, err
}
