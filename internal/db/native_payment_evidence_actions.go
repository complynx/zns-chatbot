package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	nativePaymentEvidenceHistoryLimit = 5
	nativePaymentEvidenceRecentLimit  = 20
)

// NativePaymentEvidenceAction contains locators only; its state is the original
// submission receipt, not the current human decision or consent to repeat it.
type NativePaymentEvidenceAction struct {
	OperationID string    `json:"operation_id"`
	EventID     uuid.UUID `json:"event_id"`
	PassID      uuid.UUID `json:"pass_id"`
	EvidenceID  uuid.UUID `json:"evidence_id"`
	FileID      uuid.UUID `json:"file_id"`
	EventName   string    `json:"event_name"`
	Timezone    string    `json:"timezone"`
	State       string    `json:"state"`
	CompletedAt time.Time `json:"completed_at"`
}

func nativePaymentEvidenceActions(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	task uuid.UUID,
	limit int32,
) ([]NativePaymentEvidenceAction, error) {
	rows, err := tx.Query(ctx, "SELECT a.operation_id,e.id,(op.result->>'pass_id')::uuid,\n (op.result->>" +
"'id')::uuid,(op.result->>'file_id')::uuid,e.name,e.timezone,op.result->>" +
"'state',a.completed_at\n FROM shuttle_native_payment_evidence_actions a\n " +
"JOIN shuttle_payment_operations op ON op.bot_id=a.bot_id AND op.organiza" +
"tion_id=a.organization_id\n AND op.actor=a.actor AND op.operation_id=a.op" +
"eration_id AND op.kind='submit'\n JOIN shuttle_events e ON e.id=(op.resul" +
"t->>'event_id')::uuid\n WHERE a.user_id=$1 AND a.bot_id=$2 AND a.organiza" +
"tion_id=$3 AND a.actor=$4\n AND (op.result->>'user_id')::uuid=$1 AND e.bo" +
"t_id=$2 AND e.organization_id=$3\n AND 'registrations'=ANY(e.modules)\n AN" +
"D ($5::uuid='00000000-0000-0000-0000-000000000000' OR EXISTS(\n SELECT 1 " +
"FROM shuttle_tasks t WHERE t.id=$5 AND t.user_id=$1 AND a.completed_at<t" +
".created_at))\n ORDER BY a.completed_at DESC,a.operation_id DESC LIMIT $6", scope.UserID, scope.BotID, scope.Organization, scope.Subject, task, limit)
	if err != nil {
		return nil, err
	}
	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (NativePaymentEvidenceAction, error) {
		var a NativePaymentEvidenceAction
		scanErr := row.Scan(
			&a.OperationID,
			&a.EventID,
			&a.PassID,
			&a.EvidenceID,
			&a.FileID,
			&a.EventName,
			&a.Timezone,
			&a.State,
			&a.CompletedAt,
		)
		a.CompletedAt = a.CompletedAt.UTC()
		return a, scanErr
	})
	if result == nil && err == nil {
		result = []NativePaymentEvidenceAction{}
	}
	return result, err
}

// RecentNativePaymentEvidenceActions is bounded by a later incoming task and
// current owner/module access. It creates no task and supplies no new consent.
func (s *Store) RecentNativePaymentEvidenceActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]NativePaymentEvidenceAction, error) {
	if taskID == uuid.Nil {
		return []NativePaymentEvidenceAction{}, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	return nativePaymentEvidenceActions(ctx, tx, scope, taskID, nativePaymentEvidenceHistoryLimit)
}

func (s *Store) MiniAppRecentPaymentEvidenceActions(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentEvidenceSession,
) ([]NativePaymentEvidenceAction, error) {
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, uuid.Nil, "")
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, err := nativePaymentEvidenceActions(
		ctx,
		tx,
		ProfileScope{
			BotID:        scope.BotID,
			Organization: scope.Organization,
			Subject:      scope.Subject,
			UserID:       scope.UserID,
		},
		uuid.Nil,
		nativePaymentEvidenceRecentLimit,
	)
	if err != nil {
		return nil, err
	}
	return result, paymentEvidenceFence(scope, session)
}
