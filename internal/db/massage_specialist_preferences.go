package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// MassageSpecialistPreferences applies to this account across all its events.
// No stored row means enabled at version zero.
type MassageSpecialistPreferences struct {
	UserID         uuid.UUID `json:"user_id"`
	NotifyBookings bool      `json:"notify_bookings"`
	NotifyNext     bool      `json:"notify_next"`
	Version        int64     `json:"version"`
}

type MassageSpecialistPreferenceChange struct {
	EventID         uuid.UUID `json:"event_id"`
	ExpectedVersion int64     `json:"expected_version"          minimum:"0"`
	NotifyBookings  *bool     `json:"notify_bookings,omitempty"`
	NotifyNext      *bool     `json:"notify_next,omitempty"`
}

func (s *Store) MassageSpecialistPreferences(ctx context.Context, scope ProfileScope,
	eventID uuid.UUID,
) (MassageSpecialistPreferences, error) {
	return s.massageSpecialistPreferences(ctx, scope, nil, eventID)
}

func (s *Store) MiniAppMassageSpecialistPreferences(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID uuid.UUID,
) (MassageSpecialistPreferences, error) {
	return s.massageSpecialistPreferences(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		eventID,
	)
}

func (s *Store) massageSpecialistPreferences(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID uuid.UUID,
) (MassageSpecialistPreferences, error) {
	tx, q, actor, err := s.massagePreferenceTx(ctx, scope, session, eventID, uuid.Nil)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readMassageSpecialistPreferences(ctx, q, actor.UserID)
}

// Both interfaces use event -> owner UPDATE locks. No SHARE-to-UPDATE upgrade is
// allowed: one account can change preferences through different events at once.
func (s *Store) massagePreferenceTx(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID, operation uuid.UUID,
) (pgx.Tx, *sqlc.Queries, ProfileScope, error) {
	if eventID == uuid.Nil {
		return nil, nil, scope, massage.ErrInvalid
	}
	var tx pgx.Tx
	var q *sqlc.Queries
	var event sqlc.ShuttleEvent
	var err error
	if session != nil {
		tx, q, event, scope, err = s.miniAppMassageOperationTx(
			ctx,
			EventScope{BotID: scope.BotID, Organization: scope.Organization},
			*session,
			eventID,
			operation,
			"massage-preference",
		)
	} else {
		tx, err = s.pool.Begin(ctx)
		if err == nil {
			q = s.q.WithTx(tx)
			event, err = q.LockRegistrationEvent(
				ctx,
				sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
			)
		}
		if err == nil {
			err = lockProfileOwner(ctx, tx, scope, true)
		}
	}
	if err != nil {
		return nil, nil, scope, rollbackMassagePreference(ctx, tx, err)
	}
	if !slices.Contains(event.Modules, "massage") {
		err = pgx.ErrNoRows
	}
	if err == nil {
		var own []string
		own, err = q.OwnActiveMassageSpecialistIDs(
			ctx,
			sqlc.OwnActiveMassageSpecialistIDsParams{EventID: eventID, UserID: scope.UserID},
		)
		if err == nil && len(own) == 0 {
			err = pgx.ErrNoRows
		}
	}
	if err != nil {
		return nil, nil, scope, rollbackMassagePreference(ctx, tx, err)
	}
	return tx, q, scope, nil
}

func rollbackMassagePreference(ctx context.Context, tx pgx.Tx, err error) error {
	if tx != nil {
		rollbackLegacyArchive(ctx, tx)
	}
	return err
}

func readMassageSpecialistPreferences(ctx context.Context, q *sqlc.Queries,
	userID uuid.UUID,
) (MassageSpecialistPreferences, error) {
	row, err := q.ReadMassageSpecialistPreference(ctx, userID)
	return MassageSpecialistPreferences{
		UserID:         userID,
		NotifyBookings: row.NotifyBookings,
		NotifyNext:     row.NotifyNext,
		Version:        row.Version,
	}, err
}

// SetMassageSpecialistPreferences changes only the current actor's account.
// An explicit same-value set keeps the version; its immutable receipt is retained.
func (s *Store) SetMassageSpecialistPreferences(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, args MassageSpecialistPreferenceChange,
) (MassageSpecialistPreferences, error) {
	return s.setMassageSpecialistPreferences(ctx, scope, nil, operation, args)
}

func (s *Store) SetMiniAppMassageSpecialistPreferences(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation uuid.UUID, args MassageSpecialistPreferenceChange,
) (MassageSpecialistPreferences, error) {
	return s.setMassageSpecialistPreferences(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		operation,
		args,
	)
}

func (s *Store) setMassageSpecialistPreferences(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, operation uuid.UUID, args MassageSpecialistPreferenceChange,
) (MassageSpecialistPreferences, error) {
	if (args.NotifyBookings == nil && args.NotifyNext == nil) || args.EventID == uuid.Nil || args.ExpectedVersion < 0 ||
		operation == uuid.Nil {
		return MassageSpecialistPreferences{}, massage.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	hash := sha256.Sum256(raw)
	tx, q, actor, err := s.massagePreferenceTx(ctx, scope, session, args.EventID, operation)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	source := "agent"
	if session != nil {
		source = "miniapp"
	}
	result, err := mutateMassageSpecialistPreference(ctx, q, actor.UserID, operation, args, hash[:], source)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func mutateMassageSpecialistPreference(ctx context.Context, q *sqlc.Queries,
	userID, operation uuid.UUID, args MassageSpecialistPreferenceChange, hash []byte, source string,
) (MassageSpecialistPreferences, error) {
	prior, err := q.ReadMassageSpecialistPreferenceOperation(
		ctx,
		sqlc.ReadMassageSpecialistPreferenceOperationParams{UserID: userID, OperationID: operation},
	)
	if err == nil {
		if prior.EventID != args.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return MassageSpecialistPreferences{}, ErrConflict
		}
		return decodeMassageSpecialistPreference(prior.Result)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MassageSpecialistPreferences{}, err
	}
	current, err := readMassageSpecialistPreferences(ctx, q, userID)
	if err != nil {
		return current, err
	}
	if current.Version != args.ExpectedVersion {
		return current, ErrConflict
	}
	notifyBookings, notifyNext := current.NotifyBookings, current.NotifyNext
	if args.NotifyBookings != nil {
		notifyBookings = *args.NotifyBookings
	}
	if args.NotifyNext != nil {
		notifyNext = *args.NotifyNext
	}
	if current.NotifyBookings != notifyBookings || current.NotifyNext != notifyNext {
		if current.Version == math.MaxInt64 {
			return current, ErrConflict
		}
		row, e := q.SetMassageSpecialistPreference(ctx, sqlc.SetMassageSpecialistPreferenceParams{
			UserID:          userID,
			NotifyBookings:  notifyBookings,
			NotifyNext:      notifyNext,
			ExpectedVersion: current.Version,
			NextVersion:     current.Version + 1,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			return current, ErrConflict
		}
		if e != nil {
			return current, e
		}
		current.NotifyBookings, current.NotifyNext, current.Version = row.NotifyBookings, row.NotifyNext, row.Version
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return current, err
	}
	err = q.InsertMassageSpecialistPreferenceOperation(ctx, sqlc.InsertMassageSpecialistPreferenceOperationParams{
		UserID: userID, OperationID: operation, EventID: args.EventID, Source: source, RequestHash: hash, Result: raw,
	})
	return current, err
}

// MassageSpecialistPreferenceOperation recovers an immutable receipt without
// repeating the change. Current event access is required even for old receipts.
func (s *Store) MassageSpecialistPreferenceOperation(ctx context.Context, scope ProfileScope,
	eventID, operation uuid.UUID,
) (MassageSpecialistPreferences, error) {
	return s.massageSpecialistPreferenceOperation(ctx, scope, nil, eventID, operation)
}

func (s *Store) MiniAppMassageSpecialistPreferenceOperation(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, operation uuid.UUID,
) (MassageSpecialistPreferences, error) {
	return s.massageSpecialistPreferenceOperation(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		eventID,
		operation,
	)
}

func (s *Store) massageSpecialistPreferenceOperation(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID, operation uuid.UUID,
) (MassageSpecialistPreferences, error) {
	if operation == uuid.Nil {
		return MassageSpecialistPreferences{}, massage.ErrInvalid
	}
	tx, q, actor, err := s.massagePreferenceTx(ctx, scope, session, eventID, uuid.Nil)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadMassageSpecialistPreferenceOperation(
		ctx,
		sqlc.ReadMassageSpecialistPreferenceOperationParams{UserID: actor.UserID, OperationID: operation},
	)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	if prior.EventID != eventID {
		return MassageSpecialistPreferences{}, pgx.ErrNoRows
	}
	return decodeMassageSpecialistPreference(prior.Result)
}

// OwnMassageSpecialistPreferenceOperation supports Loom status lookups, which
// carry only the operation ID. Immutable metadata selects the original event;
// the existing receipt read then rechecks current event/owner/specialist access.
func (s *Store) OwnMassageSpecialistPreferenceOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageSpecialistPreferences, error) {
	if operation == uuid.Nil {
		return MassageSpecialistPreferences{}, massage.ErrInvalid
	}
	row, err := s.q.ReadMassageSpecialistPreferenceOperation(
		ctx,
		sqlc.ReadMassageSpecialistPreferenceOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return MassageSpecialistPreferences{}, err
	}
	return s.MassageSpecialistPreferenceOperation(ctx, scope, row.EventID, operation)
}

// Receipts recorded before notify_next existed retain its enabled default.
func decodeMassageSpecialistPreference(raw json.RawMessage) (MassageSpecialistPreferences, error) {
	result := MassageSpecialistPreferences{NotifyNext: true}
	err := json.Unmarshal(raw, &result)
	return result, err
}
