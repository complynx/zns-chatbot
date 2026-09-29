package migrate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func insertMassageStage(ctx context.Context, tx pgx.Tx, p preparedMassage, owners map[int64]string) error {
	for _, event := range p.eventNames() {
		if err := insertMassageConfiguration(ctx, tx, p, event, owners); err != nil {
			return err
		}
	}
	for _, row := range p.Plan.Records {
		if err := insertMassageReference(ctx, tx, p, row, owners); err != nil {
			return err
		}
	}
	return nil
}

func insertMassageBooking(
	ctx context.Context,
	tx pgx.Tx,
	p preparedMassage,
	b *MassageCandidate,
	owners map[int64]string,
) error {
	captured, err := time.Parse(time.RFC3339Nano, p.Plan.CapturedAt)
	if err != nil {
		return errors.New("massage_capture_invalid")
	}
	created := b.Created
	if created == nil {
		created = b.Finalized
	}
	var cancelled any
	if b.Deleted {
		cancelled = captured
	}
	prices := [6]int{43, 57, 90, 110, 125, 150}
	rub := [6]int{1200, 1600, 2500, 3000, 3500, 4000}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub,cancelled_at,instant,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		b.ID,
		b.Event,
		massagePartyID(b.Event, b.Day),
		owners[b.Owner],
		owners[b.Specialist],
		b.Slot,
		b.Length,
		b.Start,
		b.End,
		prices[b.Length-1],
		rub[b.Length-1],
		cancelled,
		b.Created == nil && b.Owner == b.Specialist,
		created,
	); err != nil {
		return errors.New("massage_booking_conflict")
	}
	for _, n := range []struct {
		kind  string
		owner string
		sent  bool
	}{{"prior_long", owners[b.Owner], b.LongSent}, {"prior_short", owners[b.Owner], b.ShortSent}, {"next", owners[b.Specialist], b.SpecialistSent}} {
		if n.sent {
			if _, err = tx.Exec(
				ctx,
				`INSERT INTO core.massage_notices(booking_id,owner,kind,created_at,sent_at) VALUES($1,$2,$3,$4,$4)`,
				b.ID,
				n.owner,
				n.kind,
				captured,
			); err != nil {
				return errors.New("massage_notice_conflict")
			}
		}
	}
	if b.Additional && !b.Deleted && !b.Start.Before(captured) {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.massage_notices(booking_id,owner,kind,created_at) VALUES($1,$2,'additional',$3)`,
			b.ID,
			owners[b.Owner],
			captured,
		); err != nil {
			return errors.New("massage_notice_conflict")
		}
	}
	return nil
}

func insertMassageConfiguration(
	ctx context.Context,
	tx pgx.Tx,
	p preparedMassage,
	event string,
	owners map[int64]string,
) error {
	c := p.Configurations[event]
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.massage_events(id,daily_limit,prior_long,prior_short) VALUES($1,$2,$3*interval '1 second',$4*interval '1 second')`,
		event,
		c.DailyLimit,
		c.PriorLong,
		c.PriorShort,
	); err != nil {
		return errors.New("massage_target_conflict")
	}
	for i, party := range c.Parties {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables,position,is_open) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			massagePartyID(event, party.day()),
			event,
			party.Start,
			party.End,
			party.Tables,
			i,
			party.Open,
		); err != nil {
			return errors.New("massage_party_conflict")
		}
	}
	for id, s := range p.Specialists {
		if err := insertMassageSpecialist(ctx, tx, event, owners[id], s); err != nil {
			return err
		}
	}

	return nil
}

func insertMassageSpecialist(ctx context.Context, tx pgx.Tx, event, owner string, s *MassageSpecialist) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.massage_specialists(event_id,owner,name,icon,about,min_length,max_length,legacy_table_flag,notify_bookings,notify_next) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event,
		owner,
		s.Name,
		s.Icon,
		s.About,
		s.Min,
		s.Max,
		s.Table,
		s.Bookings,
		s.Next,
	); err != nil {
		return errors.New("massage_specialist_conflict")
	}
	for _, span := range s.Work {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at) VALUES($1,$2,$3,$4)`,
			event,
			owner,
			span.Start,
			span.End,
		); err != nil {
			return errors.New("massage_work_conflict")
		}
	}
	return nil
}

func insertMassageReference(
	ctx context.Context,
	tx pgx.Tx,
	p preparedMassage,
	row MassagePlanRecord,
	owners map[int64]string,
) error {
	kind, target, event := "excluded", "", ""
	var owner any
	if !row.Excluded {
		switch {
		case row.Configuration != nil:
			kind, event = "configuration", row.Configuration.Event
			target = event
		case row.Specialist != nil:
			kind = "specialist"
			owner = owners[row.Specialist.Owner]
			target = owners[row.Specialist.Owner]
		case row.Booking != nil:
			kind, event, target = "booking", row.Booking.Event, row.Booking.ID
			owner = owners[row.Booking.Owner]
			if row.Booking.Draft {
				kind = "draft"
			} else if err := insertMassageBooking(ctx, tx, p, row.Booking, owners); err != nil {
				return err
			}
		}
	}
	var eventValue any
	if event != "" {
		eventValue = event
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		row.Legacy.Key,
		p.Plan.BotID,
		kind,
		row.Legacy.RecordSHA256,
		row.Record,
		target,
		owner,
		eventValue,
	); err != nil {
		return errors.New("massage_source_conflict")
	}
	if row.Booking != nil && row.Booking.Draft && !row.Excluded {
		state, err := normalizeMassageDraft(row.Record, row.Booking, p, owners)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.legacy_massage_drafts(id,source_key,owner,event_id,state,closed) VALUES($1,$2,$3,$4,$5,$6)`,
			target,
			row.Legacy.Key,
			owner,
			event,
			state,
			row.Booking.Deleted,
		); err != nil {
			return errors.New("massage_draft_conflict")
		}
	}
	if row.Specialist != nil && !row.Excluded {
		result, err := tx.Exec(
			ctx,
			`UPDATE core.legacy_user_deferred_domains SET completed=true WHERE source_key=$1 AND domain='massage' AND source_record=$2::jsonb->'massage_specialist' AND NOT completed`,
			row.Legacy.Key,
			row.Record,
		)
		if err != nil || result.RowsAffected() != 1 {
			return errors.New("massage_deferred_user_unresolved")
		}
	}
	return nil
}
