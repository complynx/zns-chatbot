package db

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

// RegistrationEntry is an immutable event/version choice presented to one owner.
type RegistrationEntry struct {
	// DetailsOperation binds a native task to an atomic missing-details outcome.
	// It is a transport guard, not part of the canonical registration receipt.
	DetailsOperation uuid.UUID `json:"-"`
	EventID          uuid.UUID `json:"event_id"`
	EventVersion     int64     `json:"event_version"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// RegistrationEntryForCallback binds a role to our exact delivered message in the owner's chat.
// Expiry is checked in the mutation, after receipt replay and the event/user locks.
func (s *Store) RegistrationEntryForCallback(
	ctx context.Context,
	scope ProfileScope,
	taskID, promptID uuid.UUID,
	messageID int64,
	role string,
) (RegistrationEntry, error) {
	if !registrations.ValidRole(role) || messageID <= 0 || promptID == uuid.Nil {
		return RegistrationEntry{}, pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RegistrationEntry{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return RegistrationEntry{}, err
	}
	var raw []byte
	err = tx.QueryRow(
		ctx,
		"SELECT prior.snapshot->'registration_entry'\n"+
			"FROM shuttle_tasks current_task JOIN shuttle_tasks prior\n"+
			" ON prior.user_id=current_task.user_id AND "+
			"prior.conversation_id=current_task.conversation_id AND prior.seq<current_task.seq\n"+
			"JOIN shuttle_outbox o ON o.task_id=prior.id\n"+
			"WHERE current_task.id=$1 AND current_task.user_id=$2 AND prior.id=$3\n"+
			" AND prior.route='structured' AND prior.state='completed' AND "+
			"prior.request->>'structured_command'='/register'\n"+
			" AND prior.snapshot->'registration_entry' IS NOT NULL AND o.state='delivered'\n"+
			" AND o.receipt->>'message_id'=$4\n"+
			" AND o.payload#>'{reply_markup,inline_keyboard}' @> "+
			"jsonb_build_array(jsonb_build_array(jsonb_build_object('callback_data',$5::text)))\n"+
			"LIMIT 1",
		taskID,
		scope.UserID,
		promptID,
		strconv.FormatInt(messageID, 10),
		"g:"+promptID.String()+":"+role,
	).Scan(&raw)
	if err != nil {
		return RegistrationEntry{}, err
	}
	var entry RegistrationEntry
	err = json.Unmarshal(raw, &entry)
	return entry, err
}

// RegisterPresented reuses canonical join receipts; the transport guard does not alter their hash.
// Callers persist the verified presentation and exact change before invoking this method.
func (s *Store) RegisterPresented(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	change RegistrationChange,
	entry RegistrationEntry,
) (registrations.Registration, error) {
	if change.Kind != registrationActionRegister || change.EventID != entry.EventID || entry.EventVersion <= 0 ||
		entry.ExpiresAt.IsZero() {
		return registrations.Registration{}, registrations.ErrInvalid
	}
	return s.changeRegistration(ctx, scope, actor, operation, change, uuid.Nil, &entry, nil)
}
