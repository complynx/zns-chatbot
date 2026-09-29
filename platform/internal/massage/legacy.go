package massage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const legacySourceAction = "source"
const legacySourceBack = "source_back"
const legacySourceCancel = "source_cancel"
const legacyBack = "back"
const legacyMaxPage = 100000
const legacyBook = "book"
const legacyCancel = "cancel"

// LegacyChoice contains validated target identities, never source permissions.
type LegacyChoice struct {
	Party      string `json:"party,omitempty"`
	Page       int    `json:"page,omitempty"`
	Slot       *int   `json:"slot,omitempty"`
	Specialist string `json:"specialist,omitempty"`
}

type LegacyState struct {
	Party    string                  `json:"party"`
	Length   int                     `json:"length"`
	Page     int                     `json:"page"`
	Selected map[string]bool         `json:"selected"`
	Choices  map[string]LegacyChoice `json:"choices"`
}

type LegacyDraft struct {
	ID        string      `json:"id"`
	Event     string      `json:"event"`
	Version   int64       `json:"version"`
	State     LegacyState `json:"state"`
	Booking   string      `json:"booking,omitempty"`
	Closed    bool        `json:"closed"`
	Cancelled bool        `json:"cancelled"`
}

type LegacyCommand struct {
	ID        string       `json:"id"`
	Event     string       `json:"event"`
	Key       string       `json:"key"`
	Version   int64        `json:"version"`
	Action    string       `json:"action"`
	Choice    int          `json:"choice,omitempty"`
	Length    int          `json:"length,omitempty"`
	Selection LegacyChoice `json:"selection"`
}

func readLegacyDraft(ctx context.Context, q queryer, actor, event, id string, lock bool) (LegacyDraft, error) {
	query := `SELECT d.id,d.event_id,d.version,d.state,COALESCE(d.booking_id,''),d.closed,b.cancelled_at IS NOT NULL
 FROM core.legacy_massage_drafts d JOIN core.legacy_massage_import_references r ON r.source_key=d.source_key
 LEFT JOIN core.massage_bookings b ON b.id=d.booking_id
 WHERE d.owner=$1 AND d.event_id=$2 AND (d.id=$3 OR r.source_record->'_id'->>'$oid'=$3)`
	if lock {
		query += " FOR UPDATE OF d"
	}
	var result LegacyDraft
	err := q.QueryRow(ctx, query, actor, event, id).
		Scan(&result.ID, &result.Event, &result.Version, &result.State, &result.Booking, &result.Closed, &result.Cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "not_found")
	}
	return result, err
}

func (s Service) LegacyDraft(ctx context.Context, actor, event, id string) (LegacyDraft, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return LegacyDraft{}, err
	}
	return readLegacyDraft(ctx, s.DB, actor, event, id, false)
}

// ExecuteLegacy serializes the imported draft and invokes the existing booking
// executor inside the same transaction. Source callbacks are consumed once.
func (s Service) ExecuteLegacy(ctx context.Context, actor string, c LegacyCommand) (LegacyDraft, error) {
	if c.ID == "" || c.Event == "" || c.Key == "" || len(c.Key) > 100 || c.Version < 0 {
		return LegacyDraft{}, problem(http.StatusBadRequest, "invalid_command")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return LegacyDraft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authenticated(ctx, tx, actor); err != nil {
		return LegacyDraft{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('massage:'||$1,0))`, actor); err != nil {
		return LegacyDraft{}, err
	}
	draft, err := readLegacyDraft(ctx, tx, actor, c.Event, c.ID, true)
	if err != nil {
		if p, ok := errors.AsType[*core.ProblemError](err); ok && p.Status == http.StatusNotFound {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				return draft, rollbackErr
			}
			return s.executeLegacyBooking(ctx, actor, c)
		}
		return draft, err
	}
	return s.executeLockedLegacy(ctx, tx, actor, c, draft)
}

func (s Service) executeLockedLegacy(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c LegacyCommand,
	draft LegacyDraft,
) (LegacyDraft, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return draft, err
	}
	digest := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(digest[:])
	var prior string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM core.massage_operations WHERE actor=$1 AND key=$2`, actor, "legacy:"+c.Key).
		Scan(&prior)
	if err == nil {
		if prior != fingerprint {
			return draft, problem(http.StatusConflict, "idempotency_conflict")
		}
		return draft, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return draft, err
	}
	if draft.Closed {
		return draft, tx.Commit(ctx)
	}
	if c.Action == legacySourceAction || c.Action == legacySourceBack || c.Action == legacySourceCancel {
		if draft.Version != 0 {
			return draft, tx.Commit(ctx)
		}
		c, err = legacySourceCommand(c, draft.State)
		if err != nil {
			return draft, err
		}
	} else if c.Version != draft.Version {
		return draft, problem(http.StatusConflict, "stale_version")
	}
	if err = s.applyLegacy(ctx, tx, actor, &draft, c); err != nil {
		return draft, err
	}
	draft.Version++
	_, err = tx.Exec(
		ctx,
		`UPDATE core.legacy_massage_drafts SET version=$2,state=$3,booking_id=NULLIF($4,''),closed=$5 WHERE id=$1`,
		draft.ID,
		draft.Version,
		draft.State,
		draft.Booking,
		draft.Closed,
	)
	if err != nil {
		return draft, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.massage_operations(actor,key,request_hash,result) VALUES($1,$2,$3,$4)`,
		actor,
		"legacy:"+c.Key,
		fingerprint,
		draft,
	)
	if err != nil {
		return draft, err
	}
	return draft, tx.Commit(ctx)
}

func legacySourceCommand(c LegacyCommand, state LegacyState) (LegacyCommand, error) {
	switch c.Action {
	case legacySourceCancel:
		c.Action = legacyCancel
	case legacySourceBack:
		c.Action = legacyBack
	case legacySourceAction:
		switch {
		case c.Choice == 0:
			c.Action = "resume"
		case c.Choice < 0:
			c.Action = legacyBack
		case state.Length == 0:
			c.Action = "length"
			c.Length = c.Choice
		default:
			choice, ok := state.Choices[strconv.Itoa(c.Choice)]
			if !ok {
				return c, problem(http.StatusConflict, "stale_version")
			}
			c.Action = "select"
			c.Selection = choice
		}
	}
	return c, nil
}

func (s Service) applyLegacy(ctx context.Context, tx pgx.Tx, actor string, d *LegacyDraft, c LegacyCommand) error {
	switch c.Action {
	case "resume":
		return nil
	case legacyCancel:
		d.Closed = true
	case legacyBack:
		if d.State.Length == 0 {
			d.Closed = true
		} else {
			d.State.Length = 0
			d.State.Page = 0
			d.State.Selected = map[string]bool{}
		}
	case "length":
		if !regularLength(c.Length) {
			return problem(http.StatusBadRequest, "invalid_length")
		}
		d.State.Length = c.Length
		d.State.Page = 0
	case "page":
		if c.Choice < 0 || c.Choice > legacyMaxPage {
			return problem(http.StatusBadRequest, "invalid_page")
		}
		d.State.Page = c.Choice
	case "select":
		return s.selectLegacy(ctx, tx, actor, d, c.Selection)
	default:
		return problem(http.StatusBadRequest, "invalid_action")
	}
	return nil
}

func (s Service) selectLegacy(ctx context.Context, tx pgx.Tx, actor string, d *LegacyDraft, c LegacyChoice) error {
	switch {
	case c.Party != "":
		party, err := loadParty(ctx, tx, d.Event, c.Party, false)
		if err != nil {
			return err
		}
		if party.Open || !party.End.After(s.now()) {
			return problem(http.StatusConflict, "stale_version")
		}
		d.State.Party = c.Party
	case c.Page != 0:
		if c.Page != 1 && c.Page != -1 {
			return problem(http.StatusBadRequest, "invalid_page")
		}
		d.State.Page = max(0, min(legacyMaxPage, d.State.Page+c.Page))
	case c.Specialist != "":
		return s.selectLegacySpecialist(ctx, tx, actor, d, c)
	default:
		return problem(http.StatusBadRequest, "invalid_choice")
	}
	return nil
}

func (s Service) selectLegacySpecialist(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	d *LegacyDraft,
	c LegacyChoice,
) error {
	var eligible bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.massage_specialists WHERE event_id=$1 AND owner=$2 AND min_length<=$3 AND max_length>=$3)`, d.Event, c.Specialist, d.State.Length).
		Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return problem(http.StatusConflict, "specialist_unavailable")
	}
	if c.Slot == nil {
		if d.State.Selected == nil {
			d.State.Selected = map[string]bool{}
		}
		selected, present := d.State.Selected[c.Specialist]
		d.State.Selected[c.Specialist] = present && !selected
		return nil
	}
	if selected, present := d.State.Selected[c.Specialist]; present && !selected {
		return problem(http.StatusConflict, "specialist_unavailable")
	}
	result, err := s.book(
		ctx,
		tx,
		actor,
		Command{
			Action:     legacyBook,
			Event:      d.Event,
			Party:      d.State.Party,
			Specialist: c.Specialist,
			Length:     d.State.Length,
			Slot:       *c.Slot,
		},
	)
	if err != nil {
		return err
	}
	d.Booking = result.ID
	d.Closed = true
	return nil
}
