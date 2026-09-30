package massage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const reservationColumns = `id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub,version,cancelled_at,instant`

func (s Service) Parties(ctx context.Context, actor, event string) ([]EventParty, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT p.id,p.event_id,p.starts_at,p.ends_at,p.tables,p.is_open,e.daily_limit
	FROM core.massage_parties p JOIN core.massage_events e ON e.id=p.event_id WHERE p.event_id=$1 ORDER BY p.position,p.starts_at,p.id`, event)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	parties, err := pgx.CollectRows(rows, pgx.RowToStructByPos[EventParty])
	return parties, core.DatabaseOperationError(err)
}

func loadParty(ctx context.Context, q queryer, event, id string, lock bool) (EventParty, error) {
	query := `SELECT p.id,p.event_id,p.starts_at,p.ends_at,p.tables,p.is_open,e.daily_limit
	FROM core.massage_parties p JOIN core.massage_events e ON e.id=p.event_id WHERE p.event_id=$1 AND p.id=$2`
	if lock {
		query += " FOR UPDATE OF p"
	}
	var party EventParty
	err := q.QueryRow(ctx, query, event, id).
		Scan(&party.ID, &party.Event, &party.Start, &party.End, &party.Tables, &party.Open, &party.DailyLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return party, problem(http.StatusNotFound, "not_found")
	}
	return party, core.DatabaseOperationError(err)
}

func providers(ctx context.Context, q queryer, event string) ([]Provider, error) {
	return readProviders(ctx, q, event, false)
}

func readProviders(ctx context.Context, q queryer, event string, navigation bool) ([]Provider, error) {
	rows, err := q.Query(
		ctx,
		`SELECT s.owner,u.telegram_id,CASE WHEN $2 THEN left(s.name,$3) ELSE s.name END,CASE WHEN $2 THEN left(s.icon,$3) ELSE s.icon END,CASE WHEN $2 THEN '{}'::jsonb ELSE s.about END,s.min_length,s.max_length,
	s.legacy_table_flag,s.notify_bookings,s.notify_next FROM core.massage_specialists s
	JOIN core.users u ON u.id=s.owner WHERE s.event_id=$1 ORDER BY u.telegram_id`,
		event,
		navigation,
		core.ReadExcerptRunes,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	result := []Provider{}
	for rows.Next() {
		var p Provider
		var about []byte
		err = rows.Scan(
			&p.Owner,
			&p.ID,
			&p.Name,
			&p.Icon,
			&about,
			&p.MinLength,
			&p.MaxLength,
			&p.LegacyTableFlag,
			&p.NotifyBookings,
			&p.NotifyNext,
		)
		if err != nil {
			return nil, core.DatabaseOperationError(err)
		}
		if about != nil {
			if err = json.Unmarshal(about, &p.About); err != nil {
				return nil, err
			}
		}
		result = append(result, p)
	}
	if err = rows.Err(); err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	rows.Close()
	for index := range result {
		work, workErr := q.Query(
			ctx,
			`SELECT starts_at,ends_at FROM core.massage_work WHERE event_id=$1 AND specialist=$2 ORDER BY starts_at`,
			event,
			result[index].Owner,
		)
		if workErr != nil {
			return nil, core.DatabaseOperationError(workErr)
		}
		result[index].Work, err = pgx.CollectRows(work, pgx.RowToStructByPos[Span])
		if err != nil {
			return nil, core.DatabaseOperationError(err)
		}
	}
	return result, nil
}

func reservations(ctx context.Context, q queryer, event, party string) ([]Reservation, error) {
	rows, err := q.Query(
		ctx,
		`SELECT `+reservationColumns+` FROM core.massage_bookings WHERE event_id=$1 AND party_id=$2 AND cancelled_at IS NULL ORDER BY starts_at,id`,
		event,
		party,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	bookings, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Reservation])
	return bookings, core.DatabaseOperationError(err)
}

func (s Service) Slots(ctx context.Context, actor, event, partyID string, length int) (Availability, error) {
	return s.slots(ctx, actor, event, partyID, length, false)
}

func (s Service) slots(
	ctx context.Context,
	actor, event, partyID string,
	length int,
	navigation bool,
) (Availability, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return Availability{}, err
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Availability{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	party, err := loadParty(ctx, tx, event, partyID, false)
	if err != nil {
		return Availability{}, err
	}
	staff, bookings, err := readSlotSnapshot(ctx, tx, event, partyID, navigation)
	if err != nil {
		return Availability{}, err
	}
	if !regularLength(length) || party.Open {
		return Availability{}, problem(http.StatusBadRequest, "invalid_length_or_party")
	}
	available, own, isStaff, err := availability(party, staff, bookings, actor, length, s.now())
	if err != nil {
		return Availability{}, err
	}
	result := Availability{Party: party, Providers: publicProviders(staff), Slots: []SlotChoice{}}
	for slot, ids := range available {
		for _, provider := range staff {
			if !ids[provider.ID] || length < provider.MinLength || length > provider.MaxLength {
				continue
			}
			request := Request{
				Slot:              slot,
				Length:            length,
				SpecialistID:      provider.ID,
				ActorIsSpecialist: isStaff,
				DailyLimit:        party.DailyLimit,
			}
			if Eligibility(party.rules(), s.now(), request, available, own) == Allowed {
				result.Slots = append(
					result.Slots,
					SlotChoice{Slot: slot, Specialist: provider.Owner, Start: SlotTime(party.rules(), slot)},
				)
			}
		}
	}
	sort.Slice(result.Slots, func(i, j int) bool {
		if result.Slots[i].Slot != result.Slots[j].Slot {
			return result.Slots[i].Slot < result.Slots[j].Slot
		}
		return result.Slots[i].Specialist < result.Slots[j].Specialist
	})
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

func snapshot(ctx context.Context, q queryer, event, party string) ([]Provider, []Reservation, error) {
	staff, err := providers(ctx, q, event)
	if err != nil {
		return nil, nil, err
	}
	bookings, err := reservations(ctx, q, event, party)
	return staff, bookings, err
}

// Bookings returns the actor's own records, a specialist's client list, or the
// full timetable for a specialist or an existing event food administrator.
func (s Service) Bookings(ctx context.Context, actor, event, party, view string) ([]Reservation, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return nil, err
	}
	condition, err := s.viewCondition(ctx, actor, event, view)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT `+reservationColumns+` FROM core.massage_bookings
	WHERE event_id=$1 AND ($2='' OR party_id=$2) AND `+condition+` ORDER BY starts_at,id`, event, party, actor)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	bookings, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Reservation])
	return bookings, core.DatabaseOperationError(err)
}

func (s Service) viewCondition(ctx context.Context, actor, event, view string) (string, error) {
	if view == "" || view == "mine" {
		return "owner=$3", nil
	}
	var specialist, admin bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.massage_specialists WHERE event_id=$1 AND owner=$2),
	EXISTS(SELECT 1 FROM core.order_admins WHERE event_id=$1 AND owner=$2)`, event, actor).Scan(&specialist, &admin)
	if err != nil {
		return "", core.DatabaseOperationError(err)
	}
	switch {
	case view == "clients" && specialist:
		return "specialist=$3 AND cancelled_at IS NULL", nil
	case view == "timetable" && (specialist || admin):
		return "$3<>'' AND cancelled_at IS NULL", nil
	default:
		return "", problem(http.StatusForbidden, "forbidden")
	}
}

func regularLength(length int) bool { return length == 1 || length == 2 || length == 3 || length == 5 }
