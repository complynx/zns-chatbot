package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (b *Bot) updateScriptTools(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	change func(pgx.Tx, []scriptRecord) error,
) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = scriptLock(ctx, tx, owner, updateID); err != nil {
		return err
	}
	var records []scriptRecord
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, scriptRunsKind).
		Scan(&records); err != nil {
		return err
	}
	if index < 0 || index >= len(records) {
		return errors.New("script reservation missing")
	}
	generation, generationErr := b.API.historyGeneration(ctx, owner)
	if generationErr != nil {
		return generationErr
	}
	redactHistoryScript(&records[index], generation)
	if err = change(tx, records); err != nil {
		return err
	}
	redactHistoryScript(&records[index], generation)
	redactProfileScript(&records[index])
	if records[index].MemoryRedacted {
		redactDeletedScript(&records[index], records[index].MemoryState)
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		scriptRunsKind,
		records,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (b *Bot) reserveScriptTool(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	call *scriptToolRecord,
) (int, error) {
	sequence := 0
	err := b.updateScriptTools(ctx, owner, updateID, index, func(tx pgx.Tx, records []scriptRecord) error {
		record := &records[index]
		if len(record.Calls) >= maxScriptCalls || record.Run.Error != scriptInterrupted {
			return errors.New("script call budget exhausted")
		}
		sequence = len(record.Calls)
		key := scriptToolKey(updateID, index, sequence)
		if err := admitModernChoice(ctx, tx, owner, updateID, index, sequence, records, call); err != nil {
			return err
		}
		bindScriptToolKey(call, key, index*maxScriptCalls+sequence+1)
		record.Calls = append(record.Calls, *call)
		return nil
	})
	return sequence, err
}

func (b *Bot) finishScriptTool(
	ctx context.Context,
	owner string,
	updateID int64,
	index, sequence int,
	call scriptToolRecord,
) error {
	return b.updateScriptTools(ctx, owner, updateID, index, func(_ pgx.Tx, records []scriptRecord) error {
		record := &records[index]
		if sequence < 0 || sequence >= len(record.Calls) {
			return errors.New("script call reservation missing")
		}
		record.Calls[sequence] = call
		return nil
	})
}

func bindScriptToolKey(call *scriptToolRecord, key string, foodSequence int) {
	if call.Food != nil {
		call.Food.Key = key
		call.FoodSequence = foodSequence
	}
	if call.Profile != nil {
		call.Profile.Key = key
	}
	if call.Language != nil {
		call.Language.Key = core.LanguageOperationKey(key)
	}
	if call.Model != nil && call.Model.Change != nil {
		call.Model.Change.OperationKey = key
	}
	if call.Massage != nil && call.Massage.Command != nil {
		call.Massage.Command.Key = key
	}
	if call.Memory != nil {
		call.Memory.Key = key
	}
	if call.Order != nil {
		call.Order.Key = key
		call.Order.CatalogSnapshot = call.ChoiceCatalog
		call.Order.HistoryGeneration = call.ChoiceGeneration
	}
	if call.Action != nil {
		call.Action.Key = key
	}
	bindPassToolKey(call.Pass, key)
}
