package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type MiniAppPaymentTarget struct {
	EventID         uuid.UUID                  `json:"event_id"`
	EventName       string                     `json:"event_name"`
	Timezone        string                     `json:"timezone"`
	Registration    registrations.Registration `json:"registration"`
	Balance         payments.Balance           `json:"balance"`
	PendingEvidence int64                      `json:"pending_evidence"`
	ObservedAt      time.Time                  `json:"observed_at"`
}
type MiniAppPaymentTargetsPage struct {
	Targets []MiniAppPaymentTarget `json:"targets"`
	Next    *uuid.UUID             `json:"next,omitempty"`
}
type MiniAppPaymentEvidenceFile = MiniAppOrderEvidenceFile
type MiniAppPaymentEvidenceFilesPage struct {
	Files []MiniAppPaymentEvidenceFile `json:"files"`
	Next  *uuid.UUID                   `json:"next,omitempty"`
}
type MiniAppPaymentEvidencePage struct {
	Evidence []payments.Evidence `json:"evidence"`
	Next     *uuid.UUID          `json:"next,omitempty"`
}

func nativePaymentPageLimit(limit int32) bool { return limit >= 1 && limit <= 20 }

func miniAppPaymentTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	event, pass uuid.UUID,
) (MiniAppPaymentTarget, error) {
	var result MiniAppPaymentTarget
	e, err := q.GetEvent(ctx, sqlc.GetEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return result, err
	}
	entry, err := paymentPass(ctx, q, scope, event, pass)
	if err != nil {
		return result, err
	}
	if entry.UserID != scope.UserID || entry.Pass == nil {
		return result, pgx.ErrNoRows
	}
	result.EventID, result.EventName, result.Timezone = event, e.Name, e.Timezone
	result.Registration = entry
	result.Balance, err = paymentBalance(ctx, q, *entry.Pass)
	if err != nil {
		return result, err
	}
	result.PendingEvidence, err = q.PendingPassEvidence(ctx, pass)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	result.ObservedAt = now.Time.UTC()
	return result, err
}

// MiniAppPaymentTargets includes paid assigned passes so their proof history stays discoverable.
func (s *Store) MiniAppPaymentTargets(ctx context.Context, scope PaymentScope, session MiniAppPaymentEvidenceSession,
	after uuid.UUID, limit int32,
) (MiniAppPaymentTargetsPage, error) {
	result := MiniAppPaymentTargetsPage{Targets: []MiniAppPaymentTarget{}}
	if !nativePaymentPageLimit(limit) {
		return result, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, uuid.Nil, "")
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := tx.Query(ctx, `SELECT e.id,p.id FROM shuttle_registrations r
 JOIN shuttle_passes p ON p.registration_id=r.id JOIN shuttle_events e ON e.id=r.event_id
 WHERE r.user_id=$1 AND r.state='assigned' AND e.bot_id=$2 AND e.organization_id=$3
 AND 'registrations'=ANY(e.modules) AND p.id>$4 ORDER BY p.id LIMIT $5`, scope.UserID, scope.BotID, scope.Organization, after, limit+1)
	if err != nil {
		return result, err
	}
	type locator struct{ event, pass uuid.UUID }
	locators, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (locator, error) {
		var l locator
		scanErr := row.Scan(&l.event, &l.pass)
		return l, scanErr
	})
	if err != nil {
		return result, err
	}
	more := len(locators) > int(limit)
	if more {
		locators = locators[:limit]
	}
	q := s.q.WithTx(tx)
	for _, l := range locators {
		target, readErr := miniAppPaymentTarget(ctx, q, scope, l.event, l.pass)
		if readErr != nil {
			return result, readErr
		}
		result.Targets = append(result.Targets, target)
	}
	if more {
		last := result.Targets[len(result.Targets)-1].Balance.PassID
		result.Next = &last
	}
	return result, paymentEvidenceFence(scope, session)
}

func (s *Store) MiniAppOwnPaymentTarget(ctx context.Context, scope PaymentScope, session MiniAppPaymentEvidenceSession,
	event, pass uuid.UUID,
) (MiniAppPaymentTarget, error) {
	if event == uuid.Nil || pass == uuid.Nil {
		return MiniAppPaymentTarget{}, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, "")
	if err != nil {
		return MiniAppPaymentTarget{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	target, err := miniAppPaymentTarget(ctx, s.q.WithTx(tx), scope, event, pass)
	if err != nil {
		return target, err
	}
	return target, paymentEvidenceFence(scope, session)
}

func (s *Store) MiniAppPaymentEvidenceFiles(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentEvidenceSession,
	event, after uuid.UUID,
	limit int32,
) (MiniAppPaymentEvidenceFilesPage, error) {
	result := MiniAppPaymentEvidenceFilesPage{Files: []MiniAppPaymentEvidenceFile{}}
	if event == uuid.Nil || !nativePaymentPageLimit(limit) {
		return result, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, "")
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := s.q.WithTx(tx).
		ListMiniAppOrderEvidenceFiles(ctx, sqlc.ListMiniAppOrderEvidenceFilesParams{UserID: scope.UserID, AfterID: after, PageLimit: limit + 1})
	if err != nil {
		return result, err
	}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		result.Files = append(
			result.Files,
			MiniAppPaymentEvidenceFile{
				ID:         row.ID,
				Filename:   row.Filename,
				MIMEType:   row.MimeType,
				Kind:       row.Kind,
				SizeBytes:  row.SizeBytes,
				SentAt:     row.SentAt.Time.UTC(),
				AcceptedAt: row.AcceptedAt.Time.UTC(),
			},
		)
	}
	if more {
		last := result.Files[len(result.Files)-1].ID
		result.Next = &last
	}
	return result, paymentEvidenceFence(scope, session)
}

// MiniAppPaymentEvidenceSource must be checked again after external file IO.
func (s *Store) MiniAppPaymentEvidenceSource(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentEvidenceSession,
	event, file uuid.UUID,
) (File, error) {
	if event == uuid.Nil || file == uuid.Nil {
		return File{}, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, "")
	if err != nil {
		return File{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		GetOwnedFile(ctx, sqlc.GetOwnedFileParams{ID: file, ID_2: scope.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject})
	if err != nil {
		return File{}, err
	}
	return storedFile(row), paymentEvidenceFence(scope, session)
}

func (s *Store) MiniAppOwnPaymentEvidence(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentEvidenceSession,
	event, evidence uuid.UUID,
) (payments.Evidence, error) {
	if event == uuid.Nil || evidence == uuid.Nil {
		return payments.Evidence{}, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, "")
	if err != nil {
		return payments.Evidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).ReadPaymentEvidence(ctx, sqlc.ReadPaymentEvidenceParams{ID: evidence, EventID: event})
	if err != nil {
		return payments.Evidence{}, err
	}
	if row.ShuttlePaymentEvidence.UserID != scope.UserID {
		return payments.Evidence{}, pgx.ErrNoRows
	}
	result, err := reviewedEvidenceRecord(row.ShuttlePaymentEvidence, row.State, row.Review)
	if err != nil {
		return result, err
	}
	return result, paymentEvidenceFence(scope, session)
}

func (s *Store) MiniAppOwnPaymentEvidenceList(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentEvidenceSession,
	event, pass, after uuid.UUID,
	limit int32,
) (MiniAppPaymentEvidencePage, error) {
	result := MiniAppPaymentEvidencePage{Evidence: []payments.Evidence{}}
	if event == uuid.Nil || !nativePaymentPageLimit(limit) {
		return result, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, "")
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := tx.Query(ctx, `SELECT ev.id FROM shuttle_payment_evidence ev
 WHERE ev.event_id=$1 AND ev.user_id=$2 AND ($3::uuid='00000000-0000-0000-0000-000000000000' OR ev.pass_id=$3)
 AND ev.id>$4 ORDER BY ev.id LIMIT $5`, event, scope.UserID, pass, after, limit+1)
	if err != nil {
		return result, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return result, err
	}
	more := len(ids) > int(limit)
	if more {
		ids = ids[:limit]
	}
	q := s.q.WithTx(tx)
	for _, id := range ids {
		row, readErr := q.ReadPaymentEvidence(ctx, sqlc.ReadPaymentEvidenceParams{ID: id, EventID: event})
		if readErr != nil {
			return result, readErr
		}
		item, readErr := reviewedEvidenceRecord(row.ShuttlePaymentEvidence, row.State, row.Review)
		if readErr != nil {
			return result, readErr
		}
		result.Evidence = append(result.Evidence, item)
	}
	if more {
		last := result.Evidence[len(result.Evidence)-1].ID
		result.Next = &last
	}
	return result, paymentEvidenceFence(scope, session)
}
