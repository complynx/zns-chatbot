package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PairOperationFact struct {
	OperationID  string     `json:"operation_id"`
	Kind         string     `json:"kind"`
	EventID      uuid.UUID  `json:"event_id"`
	InvitationID *uuid.UUID `json:"invitation_id,omitempty"`
	PairID       *uuid.UUID `json:"pair_id,omitempty"`
	State        string     `json:"state"`
	CompletedAt  time.Time  `json:"completed_at"`
}

// RecentPairOperations shares committed owner receipts across native and agent entry.
// Historical facts contain no partner names, credentials or financial details.
func (s *Store) RecentPairOperations(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]PairOperationFact, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		"SELECT op.operation_id,op.kind,CASE WHEN op.kind='dissolve' THEN op.resu" +
"lt ELSE op.result->'invitation' END,op.created_at\nFROM shuttle_tasks cur" +
"rent_task JOIN shuttle_pair_operations op\n ON op.bot_id=$3 AND op.organi" +
"zation_id=$4 AND op.actor=$5\nWHERE current_task.id=$1 AND current_task.u" +
"ser_id=$2 AND op.created_at<current_task.created_at\n AND op.kind IN ('in" +
"vite','accept','decline','revoke','dissolve')\nORDER BY op.created_at DES" +
"C,op.operation_id LIMIT 5",
		taskID,
		scope.UserID,
		scope.BotID,
		scope.Organization,
		scope.Subject,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PairOperationFact, 0)
	for rows.Next() {
		var fact PairOperationFact
		var raw []byte
		if err = rows.Scan(&fact.OperationID, &fact.Kind, &raw, &fact.CompletedAt); err != nil {
			return nil, err
		}
		if fact.Kind == pairDissolve {
			var pair registrations.Pair
			if err = json.Unmarshal(raw, &pair); err != nil {
				return nil, err
			}
			fact.EventID = pair.EventID
			fact.PairID = &pair.ID
			fact.State = pair.State
		} else {
			var invitation registrations.Invitation
			if err = json.Unmarshal(raw, &invitation); err != nil {
				return nil, err
			}
			fact.EventID = invitation.EventID
			fact.InvitationID = &invitation.ID
			fact.State = invitation.State
		}
		fact.CompletedAt = fact.CompletedAt.UTC()
		result = append(result, fact)
	}
	return result, rows.Err()
}
