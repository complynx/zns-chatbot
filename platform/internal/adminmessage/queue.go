package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
)

const queueCandidateLimit = 100

func adminReference(id int64) delivery.Reference {
	return delivery.Reference{Owner: delivery.Admin, Key: strconv.FormatInt(id, 10), Effect: "send"}
}

// Resolve immutable preview destinations after releasing all database locks.
func (s Service) publicationBindings(ctx context.Context, actor string, id int64) (map[string]string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := owned(ctx, tx, actor, id)
	if err != nil {
		return nil, preserveSourceRefusal(ctx, tx, err)
	}
	if message.State == statePreparing {
		return nil, problem(http.StatusConflict, "admin_message_preparing")
	}
	rows, err := tx.Query(
		ctx,
		`SELECT DISTINCT destination->>'chat' FROM core.admin_message_recipients WHERE message_id=$1 ORDER BY 1`,
		id,
	)
	if err != nil {
		return nil, err
	}
	chats, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if s.Delivery.BotID <= 0 || message.State == "queued" {
		return map[string]string{}, nil
	}
	bindings, err := destination.Resolve(ctx, s.DestinationResolver, chats)
	if err != nil {
		return nil, problem(http.StatusServiceUnavailable, "destination_unavailable")
	}
	return bindings, nil
}

func (s Service) enqueueRegistered(ctx context.Context, tx pgx.Tx, id int64, bindings map[string]string) error {
	rows, err := tx.Query(ctx, `INSERT INTO core.admin_message_deliveries(message_id,destination,content,bot_id)
 SELECT message_id,destination,content,CASE WHEN $2::bigint>0 THEN $2::bigint END
 FROM core.admin_message_recipients WHERE message_id=$1 ORDER BY position ON CONFLICT DO NOTHING
 RETURNING id,destination`, id, s.Delivery.BotID)
	if err != nil {
		return err
	}
	type inserted struct {
		id          int64
		destination Destination
		chat        string
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (inserted, error) {
		var item inserted
		var raw []byte
		if scanErr := row.Scan(&item.id, &raw); scanErr != nil {
			return item, scanErr
		}
		scanErr := json.Unmarshal(raw, &item.destination)
		item.chat = bindings[item.destination.Chat]
		return item, scanErr
	})
	if err != nil {
		return err
	}
	if s.Delivery.BotID <= 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	registrations := make([]delivery.Registration, 0, len(items))
	for _, item := range items {
		if item.chat == "" {
			return problem(http.StatusServiceUnavailable, "destination_unavailable")
		}
		registrations = append(
			registrations,
			delivery.Registration{
				Reference:   adminReference(item.id),
				Destination: delivery.Destination{Chat: item.chat, Thread: item.destination.Thread},
				Class:       delivery.Background,
			},
		)
	}
	return delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, registrations)
}

// PrepareDelivery claims only the requested owner reference; Begin remains the
// authoritative shared-head and current source check before a wire call.
func (s Service) PrepareDelivery(ctx context.Context, id int64) (Delivery, bool, error) {
	if err := s.Delivery.Validate(); err != nil {
		return Delivery{}, false, err
	}
	var candidate dbgen.NextAdminDeliveriesRow
	err := s.DB.QueryRow(ctx, `SELECT d.id,d.message_id,m.actor FROM core.admin_message_deliveries d
 JOIN core.admin_messages m ON m.id=d.message_id WHERE d.id=$1 AND d.bot_id=$2`, id, s.Delivery.BotID).
		Scan(&candidate.ID, &candidate.MessageID, &candidate.Actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, err
	}
	return s.prepareDelivery(ctx, candidate)
}

type adminQueueRow struct {
	id, bot int64
	state   string
}

// Lock every owner row before any shared lane, including in-flight attempts.
func lockAdminPublication(ctx context.Context, tx pgx.Tx, id int64) ([]adminQueueRow, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id,COALESCE(bot_id,0),state FROM core.admin_message_deliveries WHERE message_id=$1 ORDER BY id FOR UPDATE`,
		id,
	)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminQueueRow, error) {
		var item adminQueueRow
		scanErr := row.Scan(&item.id, &item.bot, &item.state)
		return item, scanErr
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].bot < items[j].bot })
	for begin := 0; begin < len(items); {
		end := begin + 1
		for end < len(items) && items[end].bot == items[begin].bot {
			end++
		}
		if items[begin].bot > 0 {
			refs := make([]delivery.Reference, 0, end-begin)
			for _, item := range items[begin:end] {
				refs = append(refs, adminReference(item.id))
			}
			if err = delivery.LockReferences(ctx, tx, items[begin].bot, refs); err != nil {
				return nil, err
			}
		}
		begin = end
	}
	return items, nil
}
func projectAdminCancellation(ctx context.Context, tx pgx.Tx, items []adminQueueRow) error {
	for _, item := range items {
		if item.bot > 0 && item.state == statePending {
			if err := delivery.Project(
				ctx,
				tx,
				item.bot,
				adminReference(item.id),
				delivery.Cancelled,
				time.Time{},
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecoverDeliveries records expired or historical uncertainty before bounded
// resends. Source authority is locked before each owner and its shared lane.
func (s Service) RecoverDeliveries(ctx context.Context) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT d.id FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id
 WHERE d.bot_id=$1 AND (d.state='unknown' OR (d.state='sending' AND d.lease_until<=clock_timestamp()) OR
 (d.state='pending' AND (m.state='cancelled' OR NOT EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=m.actor))))
 ORDER BY d.id LIMIT 100`,
		s.Delivery.BotID,
	)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.recoverDelivery(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

type adminRecovery struct {
	state, failure   string
	attempt, resends int64
	leaseLive        bool
	available        time.Time
}

func (s Service) recoverDelivery(ctx context.Context, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	eligible, err := s.lockRecoveryPublication(ctx, tx, id)
	if err != nil {
		return err
	}
	var row adminRecovery
	err = tx.QueryRow(ctx, `SELECT state,failure,attempt,uncertain_resends,COALESCE(lease_until>clock_timestamp(),false),available_at
 FROM core.admin_message_deliveries WHERE id=$1 AND bot_id=$2 FOR UPDATE SKIP LOCKED`, id, s.Delivery.BotID).
		Scan(&row.state, &row.failure, &row.attempt, &row.resends, &row.leaseLive, &row.available)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	uncertain := row.state == string(delivery.Uncertain) || (row.state == string(delivery.Sending) && !row.leaseLive)
	if !uncertain && (row.state != statePending || eligible) {
		return tx.Commit(ctx)
	}
	q := dbgen.New(tx)
	outcome, deadline, err := s.adminRecoveryOutcome(ctx, q, id, row, eligible)
	if err != nil {
		return err
	}
	if err = delivery.Project(ctx, tx, s.Delivery.BotID, adminReference(id), outcome.Kind, deadline); err != nil {
		return err
	}
	if err = s.finishDelivery(ctx, q, id, row.attempt, outcome, deadline); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) lockRecoveryPublication(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	var actor string
	var messageID int64
	if err := tx.QueryRow(ctx, `SELECT m.actor,m.id FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id WHERE d.id=$1 AND d.bot_id=$2`, id, s.Delivery.BotID).
		Scan(&actor, &messageID); err != nil {
		return false, err
	}
	source, err := messageSource(ctx, tx, actor, messageID)
	if err != nil {
		return false, err
	}
	if err = source.prelock(ctx, tx, actor); err != nil {
		return false, err
	}
	valid, err := source.validity(ctx, tx, actor)
	if err != nil {
		return false, err
	}
	current, err := adminPublicationCurrent(ctx, tx, messageID)
	if err != nil {
		return false, err
	}
	if !valid {
		if err = retireMessage(ctx, tx, messageID); err != nil {
			return false, err
		}
	}
	return valid && current, nil
}

func (s Service) adminRecoveryOutcome(
	ctx context.Context,
	q *dbgen.Queries,
	id int64,
	row adminRecovery,
	eligible bool,
) (delivery.Outcome, time.Time, error) {
	deadline := time.Now()
	cancelled := delivery.Outcome{Kind: delivery.Cancelled, Reason: "publication_cancelled"}
	if row.state == statePending {
		return cancelled, deadline, nil
	}
	reason := "telegram_outcome_unknown"
	if row.state == string(delivery.Uncertain) && row.failure != "" {
		reason = row.failure
	}
	if err := q.RecordAdminUncertainty(
		ctx,
		dbgen.RecordAdminUncertaintyParams{ID: id, BotID: s.Delivery.BotID, Attempt: row.attempt, Reason: reason},
	); err != nil {
		return delivery.Outcome{}, time.Time{}, err
	}
	if !eligible {
		return cancelled, deadline, nil
	}
	outcome := adminRetryOutcome(
		delivery.Outcome{Kind: delivery.Uncertain, Reason: reason},
		row.resends,
		true,
		s.Delivery,
	)
	if outcome.Kind != delivery.Deferred {
		return outcome, deadline, nil
	}
	deadline, representable := delivery.Deadline(deadline, outcome.RetryAfter)
	if !representable {
		return delivery.Outcome{Kind: delivery.Parked, Reason: "telegram_invalid_cooldown"}, time.Now(), nil
	}
	if row.available.After(deadline) {
		deadline = row.available
	}
	return outcome, deadline, nil
}
