package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

func (s *Store) CommunicationInput(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (json.RawMessage, string, error) {
	var raw json.RawMessage
	var run string
	err := s.pool.QueryRow(ctx, `SELECT d.personalization_input,coalesce(d.personalization_run_id,'')
 FROM shuttle_communication_deliveries d JOIN shuttle_communications c ON c.id=d.communication_id
 WHERE d.id=$1 AND c.bot_id=$2 AND c.organization_id=$3`, id, scope.BotID, scope.Organization).Scan(&raw, &run)
	return raw, run, err
}

// SaveCommunicationInput freezes the request before inference. Retries reuse the
// same body and idempotency key even if recipient memory has since changed.
func (s *Store) SaveCommunicationInput(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
	input json.RawMessage,
) (json.RawMessage, string, error) {
	var saved json.RawMessage
	var run string
	err := s.pool.QueryRow(ctx, `
UPDATE shuttle_communication_deliveries d SET
personalization_input=coalesce(d.personalization_input,$4),
personalization_started_at=coalesce(d.personalization_started_at,clock_timestamp())
 FROM shuttle_communications c WHERE c.id=d.communication_id AND d.id=$1 AND
c.bot_id=$2 AND
c.organization_id=$3 AND d.state IN ('pending','retryable') AND d.personalization_run_id IS NULL
 RETURNING d.personalization_input,coalesce(d.personalization_run_id,'')
`, id, scope.BotID, scope.Organization, input).
		Scan(&saved, &run)
	return saved, run, err
}

func (s *Store) SaveCommunicationRun(ctx context.Context, id uuid.UUID, run string) error {
	_, err := s.pool.Exec(
		ctx,
		`
UPDATE shuttle_communication_deliveries SET personalization_run_id=$2,personalization_input=NULL WHERE id=$1 AND

(personalization_run_id IS NULL OR personalization_run_id=$2)
`,
		id,
		run,
	)
	return err
}

func (s *Store) SaveCommunicationText(ctx context.Context, id uuid.UUID, text string) error {
	_, err := s.pool.Exec(
		ctx,
		`
UPDATE shuttle_communication_deliveries SET rendered_text=$2 WHERE id=$1 AND
rendered_text IS NULL AND state IN ('pending','retryable')
`,
		id,
		text,
	)
	return err
}
