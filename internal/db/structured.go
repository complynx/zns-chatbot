package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type StructuredRequest struct {
	SelectedEvent  *RegistrationEntry `json:"selected_event,omitempty"`
	Command        string             `json:"command"`
	ProfileVersion *int64             `json:"profile_version,omitempty"`
	RequestedAt    time.Time          `json:"requested_at"`
}

// RecentStructuredRequests returns bounded navigation/profile facts for the next user-triggered turn.
func (s *Store) RecentStructuredRequests(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]StructuredRequest, error) {
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
		"SELECT coalesce(prior.request->>'structured_command','/name'),prior.crea"+
			"ted_at,(op.result->>'version')::bigint,prior.snapshot->'registration_ent"+
			"ry'\nFROM shuttle_tasks current_task JOIN shuttle_tasks prior\n ON prior.c"+
			"onversation_id=current_task.conversation_id AND prior.user_id=current_ta"+
			"sk.user_id\n AND prior.seq<current_task.seq\nLEFT JOIN shuttle_profile_ope"+
			"rations op ON op.user_id=prior.user_id AND op.operation_id=prior.id\n AND"+
			" prior.snapshot#>>'{profile_change,kind}' IN ('set_name','complete_name'"+
			",'request_name')\nWHERE current_task.id=$1 AND current_task.user_id=$2\n A"+
			"ND prior.route='structured' AND prior.state IN ('completed','delivery_un"+
			"known','delivery_failed','closed')\n AND (prior.request->>'structured_com"+
			"mand' IN ('/start','/avatar','/pairs','/menu','/profile','/registrations"+
			"','/register','/orders','/massage','/knowledge','/payment_reviews','/pay"+
			"ments') OR op.user_id IS NOT NULL)\nORDER BY prior.seq DESC LIMIT 5",
		taskID,
		scope.UserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]StructuredRequest, 0)
	for rows.Next() {
		var request StructuredRequest
		var selection []byte
		if err = rows.Scan(&request.Command, &request.RequestedAt, &request.ProfileVersion, &selection); err != nil {
			return nil, err
		}
		if len(selection) > 0 {
			var entry RegistrationEntry
			if err = json.Unmarshal(selection, &entry); err != nil {
				return nil, err
			}
			request.SelectedEvent = &entry
		}
		request.RequestedAt = request.RequestedAt.UTC()
		result = append(result, request)
	}
	return result, rows.Err()
}
