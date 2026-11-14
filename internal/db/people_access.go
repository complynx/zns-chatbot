package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func (s *Store) peopleReadTx(ctx context.Context, scope PaymentScope, event uuid.UUID) (pgx.Tx, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	mode, err := s.peopleReadScope(ctx, tx, scope, event)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, "", err
	}
	return tx, mode, nil
}

func (s *Store) peopleReadScope(ctx context.Context, tx pgx.Tx, scope PaymentScope, event uuid.UUID) (string, error) {
	var err error
	q := s.q.WithTx(tx)
	if event != uuid.Nil {
		if _, err = q.LockContextEvent(
			ctx,
			sqlc.LockContextEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
		); err != nil {
			return "", err
		}
	}
	if err = lockProfileOwner(
		ctx,
		tx,
		ProfileScope{BotID: scope.BotID, Organization: scope.Organization, Subject: scope.Subject, UserID: scope.UserID},
		false,
	); err != nil {
		return "", err
	}
	mode := peopleScopePublic
	if scope.GlobalAdmin {
		mode = peopleScopeAll
		if event != uuid.Nil {
			mode = peopleScopeRegistrations
		}
		return mode, nil
	}
	if event == uuid.Nil {
		return mode, nil
	}
	var authority bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_event_authorities
 WHERE event_id=$1 AND user_id=$2 AND role IN ('ambassador','administrator'))`, event, scope.UserID).Scan(&authority)
	if err != nil {
		return "", err
	}
	if authority {
		return peopleScopeRegistrations, nil
	}
	ids, err := q.OwnActiveMassageSpecialistIDs(
		ctx,
		sqlc.OwnActiveMassageSpecialistIDsParams{EventID: event, UserID: scope.UserID},
	)
	if err != nil {
		return "", err
	}
	if len(ids) > 0 {
		mode = peopleScopeMassage
		return mode, nil
	}
	return "", ErrPeopleDenied
}

// The same row predicate gates search, listing and cards. Parameters are $1 bot,
// $2 organization, $3 scope mode, $4 event, $5 caller, and $6 optional target UUID.
const peopleAccessSQL = `u.bot_id=$1 AND u.zitadel_org=$2 AND ($3<>'public' OR NOT u.revoked)
 AND ($6::uuid IS NULL OR u.id=$6)
 AND ($3='all' OR ($3='public' AND (u.visible OR ($6::uuid=$5 AND u.id=$5)))
 OR ($3='event_registrations' AND EXISTS(SELECT 1 FROM shuttle_registrations r WHERE r.event_id=$4 AND r.user_id=u.id))
 OR ($3='massage_clients' AND EXISTS(SELECT 1 FROM shuttle_massage_bookings b
 JOIN shuttle_massage_specialist_bindings sb ON sb.event_id=b.event_id AND sb.specialist_id=b.specialist_id
 WHERE b.event_id=$4 AND b.user_id=u.id AND b.state='booked' AND sb.user_id=$5 AND sb.active)))`

const basicPersonSQL = `u.id,left(coalesce(nullif(p.full_name,''),
 btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name'))),256) AS full_name,
 left(coalesce(u.profile->>'username',''),64) AS username,u.tg_user_id,
 coalesce(p.gender,'') AS gender,coalesce(p.pronouns,'') AS pronouns,
 coalesce(p.gender_source,'') AS gender_source,coalesce(p.pronouns_source,'') AS pronouns_source`
