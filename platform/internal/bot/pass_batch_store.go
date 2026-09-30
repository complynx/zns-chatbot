package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// registrationBatchInputStore adapts the existing manual input record, not a
// second execution ledger. pass_batch_input has no conversation archive side
// effect; the INSERT preserves the same first-writer semantics as Bot.record.
type registrationBatchInputStore struct{ DB *pgxpool.Pool }

func (s registrationBatchInputStore) LoadBatchInput(ctx context.Context, owner string,
	updateID int64,
) (passbooking.RuntimeBatch, bool, error) {
	var raw []byte
	err := s.DB.QueryRow(ctx,
		`SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='pass_batch_input'`,
		owner, updateID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return passbooking.RuntimeBatch{}, false, nil
	}
	if err != nil {
		return passbooking.RuntimeBatch{}, false, core.DatabaseOperationContextError(ctx, err)
	}
	var command passbooking.RuntimeBatch
	if err = json.Unmarshal(raw, &command); err != nil {
		return passbooking.RuntimeBatch{}, false, err
	}
	return command, true, nil
}

func (s registrationBatchInputStore) SaveBatchInput(ctx context.Context, owner string,
	updateID int64, command passbooking.RuntimeBatch,
) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES($1,$2,'pass_batch_input',$3) ON CONFLICT DO NOTHING`, owner, updateID, raw)
	return core.DatabaseOperationContextError(ctx, err)
}
