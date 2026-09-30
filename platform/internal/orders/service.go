package orders

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const actionDeleteOrder = "delete"

const actionCancelProof = "cancel_proof"

const originAgent = "agent"

const actionEdit = "edit"

type Service struct {
	Delivery    delivery.Settings
	DB          *pgxpool.Pool
	LegacyBotID int64
}
type Order struct {
	ID           string     `json:"id"`
	EventID      string     `json:"event_id"`
	Owner        string     `json:"owner"`
	Version      int64      `json:"version"`
	Choice       Choice     `json:"choice"`
	State        string     `json:"state"`
	Attempt      string     `json:"attempt"`
	AttemptAt    *time.Time `json:"attempt_at,omitempty"`
	ProofFile    string     `json:"proof_file,omitempty"`
	PaymentAdmin string     `json:"payment_admin,omitempty"`
	Country      string     `json:"country,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (o Order) reserves() bool { return o.State == "proof" || o.State == statePaid }

type Command struct {
	HistoryGeneration *int64       `json:"history_generation,omitempty"`
	CatalogSnapshot   string       `json:"catalog_snapshot,omitempty"`
	EventID           string       `json:"event_id"`
	OrderID           string       `json:"order_id,omitempty"`
	Name              string       `json:"name"`
	Version           int64        `json:"version"`
	Attempt           string       `json:"attempt,omitempty"`
	Key               string       `json:"key"`
	Origin            string       `json:"origin"`
	Choice            *ChoiceInput `json:"choice,omitempty"`
	ProofFile         string       `json:"proof_file,omitempty"`
	PaymentAdmin      string       `json:"payment_admin,omitempty"`
	Country           string       `json:"country,omitempty"`
}
type Event struct {
	ID       string           `json:"id"`
	Deadline time.Time        `json:"deadline"`
	Menu     json.RawMessage  `json:"menu"`
	Extras   map[string]Extra `json:"extras"`
}

func problem(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }
func token() string                         { return rand.Text() }

func Seed(ctx context.Context, db *pgxpool.Pool) error {
	extras, err := json.Marshal(Extras())
	if err != nil {
		return err
	}
	_, err = db.Exec(
		ctx,
		`INSERT INTO core.order_events(id,deadline,menu,extras,transfer_instructions,transfer_instructions_localized)
	VALUES('sandbox-festival','2030-09-25T00:00:00+03:00',$1,$2,
	'ТЕСТОВЫЕ РЕКВИЗИТЫ. Получатель: Sandbox. Банк: Fake Bank. Не переводите реальные деньги.',
	jsonb_build_object('en','TEST PAYMENT DETAILS. Recipient: Sandbox. Bank: Fake Bank. Do not transfer real money.'))
	ON CONFLICT DO NOTHING`,
		MenuJSON,
		extras,
	)
	return core.DatabaseOperationError(err)
}

func (s Service) Event(ctx context.Context, id string) (Event, error) {
	var e Event
	err := core.DatabaseOperationError(
		s.DB.QueryRow(ctx, `SELECT id,deadline,menu,extras FROM core.order_events WHERE id=$1`, id).
			Scan(&e.ID, &e.Deadline, &e.Menu, &e.Extras),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, problem(http.StatusNotFound, "event_not_found")
	}
	return e, err
}
func (s Service) Quote(ctx context.Context, actor, event string, in ChoiceInput) (Choice, error) {
	var allowed bool
	err := core.DatabaseOperationError(
		s.DB.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1`, actor).Scan(&allowed),
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return Choice{}, problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return Choice{}, err
	}
	e, err := s.Event(ctx, event)
	if err != nil {
		return Choice{}, err
	}
	var c Catalog
	if err = json.Unmarshal(e.Menu, &c); err != nil {
		return Choice{}, err
	}
	choice, err := Canonicalize(in, c, e.Extras)
	if err != nil {
		return Choice{}, problem(http.StatusBadRequest, "invalid_choice")
	}
	return choice, nil
}

const columns = `id,event_id,owner,version,choice,state,attempt,attempt_at,proof_file,payment_admin,country,created_at`

func scan(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(
		&o.ID,
		&o.EventID,
		&o.Owner,
		&o.Version,
		&o.Choice,
		&o.State,
		&o.Attempt,
		&o.AttemptAt,
		&o.ProofFile,
		&o.PaymentAdmin,
		&o.Country,
		&o.CreatedAt,
	)
	return o, core.DatabaseOperationError(err)
}
func (s Service) List(ctx context.Context, actor, event string) ([]Order, error) {
	rows, err := s.DB.Query(
		ctx,
		`SELECT `+columns+` FROM core.orders WHERE owner=$1 AND event_id=$2 AND state<>'deleted' ORDER BY created_at,id`,
		actor,
		event,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, scanError := scan(rows)
		if scanError != nil {
			return nil, scanError
		}
		out = append(out, o)
	}
	return out, core.DatabaseOperationError(rows.Err())
}
func save(ctx context.Context, tx pgx.Tx, o Order) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.orders(`+columns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	ON CONFLICT(id) DO UPDATE SET version=excluded.version,choice=excluded.choice,state=excluded.state,
	attempt=excluded.attempt,attempt_at=excluded.attempt_at,proof_file=excluded.proof_file,
	payment_admin=excluded.payment_admin,country=excluded.country,updated_at=now()`,
		o.ID,
		o.EventID,
		o.Owner,
		o.Version,
		o.Choice,
		o.State,
		o.Attempt,
		o.AttemptAt,
		o.ProofFile,
		o.PaymentAdmin,
		o.Country,
		o.CreatedAt,
	)
	return core.DatabaseOperationError(err)
}
func clearPayment(o *Order) {
	o.State = "unpaid"
	o.Attempt = ""
	o.AttemptAt = nil
	o.ProofFile = ""
	o.PaymentAdmin = ""
	o.Country = ""
}

const (
	stateUnpaid    = "unpaid"
	statePaid      = "paid"
	stateProof     = "proof"
	stateCash      = "cash"
	actionCreate   = "create"
	actionAccept   = "accept"
	maxKeyLength   = 200
	maxEventLength = 100
	maxProofLength = 512
)

const actionReject = "reject"
const actionCountry = "country"

type operation struct {
	notificationRegistrations []delivery.Registration
	deliveryBotID             int64
	tx                        pgx.Tx
	event                     Event
	actor                     string
	command                   Command
	now                       time.Time
	before                    Choice
	previous                  Order
}

// Execute serializes event writes. State, capacity notices and the retry receipt
// are one transaction; policy is checked again before returning a cached result.
func (s Service) Execute(ctx context.Context, actor string, c Command) (Order, error) {
	if err := c.validate(); err != nil {
		return Order{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Order{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported operation error.
	prepared, err := s.PrepareInTx(ctx, tx, actor, c)
	if err != nil {
		return Order{}, err
	}
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return Order{}, err
	}
	// A failed commit has an uncertain outcome; keep its SQL provenance.
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

func (c Command) isAdmin() bool { return c.Name == actionAccept || c.Name == actionReject }
func (c Command) validate() error {
	if c.HistoryGeneration != nil &&
		(*c.HistoryGeneration < 0 || c.Origin != originAgent || (c.Name != actionCreate && c.Name != actionEdit)) {
		return problem(http.StatusBadRequest, "invalid_action")
	}
	if c.Key == "" || len(c.Key) > maxKeyLength || len(c.EventID) > maxEventLength ||
		(c.Origin != "manual" && c.Origin != originAgent) {
		return problem(http.StatusBadRequest, "invalid_action")
	}
	// Agent commands use the same owner, payment-attempt and transaction checks.
	// Receipt upload and generation remain host-bound before command admission.
	if c.Origin == originAgent &&
		!slices.Contains([]string{actionCreate, actionEdit, "proof", actionDeleteOrder, stateCash,
			actionAccept, actionReject, actionCancelProof, actionCountry}, c.Name) {
		return problem(http.StatusForbidden, "human_action_required")
	}
	return nil
}

func (op *operation) authorize(ctx context.Context) error {
	// Event precedes actor, matching derived-source transaction preparation.
	if err := LockEvent(ctx, op.tx, op.command.EventID); err != nil {
		return err
	}
	// Foreign-key checks for another user's capacity notice must remain compatible.
	var allowed bool
	err := core.DatabaseOperationError(
		op.tx.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1 FOR NO KEY UPDATE`, op.actor).
			Scan(&allowed),
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return err
	}
	e := &op.event
	err = core.DatabaseOperationError(
		op.tx.QueryRow(ctx, `SELECT id,deadline,menu,extras FROM core.order_events WHERE id=$1`, op.command.EventID).
			Scan(&e.ID, &e.Deadline, &e.Menu, &e.Extras),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusNotFound, "event_not_found")
	}
	if err != nil {
		return err
	}
	if !op.command.isAdmin() {
		return nil
	}
	var admin bool
	err = core.DatabaseOperationError(
		op.tx.QueryRow(ctx, `SELECT true FROM core.order_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, e.ID, op.actor).
			Scan(&admin),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return err
	}
	if !admin {
		return problem(http.StatusForbidden, "forbidden")
	}
	return nil
}

func (op *operation) replay(ctx context.Context, hash string) (Order, bool, error) {
	var previous string
	var out Order
	err := core.DatabaseOperationError(
		op.tx.QueryRow(ctx, `SELECT request_hash,result FROM core.order_operations WHERE actor=$1 AND key=$2`, op.actor, op.command.Key).
			Scan(&previous, &out),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	if previous != hash {
		return Order{}, true, problem(http.StatusConflict, "key_conflict")
	}
	return out, true, nil
}

func (op *operation) loadOrder(ctx context.Context) (Order, error) {
	c := op.command
	if c.Name == actionCreate {
		if c.OrderID != "" || c.Version != 0 {
			return Order{}, problem(http.StatusBadRequest, "invalid_action")
		}
		return Order{
			ID:        token(),
			EventID:   op.event.ID,
			Owner:     op.actor,
			Version:   1,
			State:     stateUnpaid,
			CreatedAt: op.now,
		}, nil
	}
	o, err := scan(
		op.tx.QueryRow(
			ctx,
			`SELECT `+columns+` FROM core.orders WHERE id=$1 AND event_id=$2 AND state<>'deleted'`,
			c.OrderID,
			op.event.ID,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem(http.StatusNotFound, "order_not_found")
	}
	if err != nil {
		return Order{}, err
	}
	if !c.isAdmin() && o.Owner != op.actor {
		return Order{}, problem(http.StatusNotFound, "order_not_found")
	}
	if o.Version != c.Version {
		return Order{}, problem(http.StatusConflict, "stale_version")
	}
	o.Version++
	return o, nil
}

func (op *operation) apply(ctx context.Context, o *Order) error {
	switch op.command.Name {
	case actionCreate, actionEdit:
		return op.edit(ctx, o)
	case actionDeleteOrder:
		if o.reserves() {
			return problem(http.StatusConflict, "payment_locked")
		}
		clearPayment(o)
		o.State = "deleted"
		return nil
	case stateProof, stateCash:
		return op.startPayment(ctx, o)
	case actionAccept, actionReject, actionCancelProof, actionCountry:
		return op.finishPayment(ctx, o)
	default:
		return problem(http.StatusBadRequest, "invalid_action")
	}
}

func (op *operation) edit(ctx context.Context, o *Order) error {
	if o.State != stateUnpaid && o.State != stateCash {
		return problem(http.StatusConflict, "payment_locked")
	}
	if op.command.Choice == nil {
		return problem(http.StatusBadRequest, "invalid_choice")
	}
	var menu Catalog
	if err := json.Unmarshal(op.event.Menu, &menu); err != nil {
		return err
	}
	choice, err := Canonicalize(*op.command.Choice, menu, op.event.Extras)
	if err != nil {
		return problem(http.StatusBadRequest, "invalid_choice")
	}
	o.Choice = choice
	clearPayment(o)
	for key, extra := range op.event.Extras {
		if _, selected := choice.Extras[key]; !selected || extra.Capacity <= 0 {
			continue
		}
		available, checkErr := capacityAvailable(ctx, op.tx, op.event.ID, key, extra.Capacity)
		if checkErr != nil {
			return checkErr
		}
		if !available {
			return problem(http.StatusConflict, "sold_out")
		}
	}
	return nil
}

func (op *operation) paymentAdmin(ctx context.Context, admin, country string) error {
	var exists bool
	err := op.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_admins WHERE event_id=$1 AND owner=$2 AND country=$3)`, op.event.ID, admin, country).
		Scan(&exists)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !exists {
		return problem(http.StatusBadRequest, "invalid_payment_admin")
	}
	return nil
}

func (op *operation) startPayment(ctx context.Context, o *Order) error {
	if o.reserves() {
		return problem(http.StatusConflict, "payment_locked")
	}
	c := op.command
	clearPayment(o)
	o.Attempt = token()
	o.AttemptAt = &op.now
	o.State = c.Name
	if c.Name == stateProof {
		if c.ProofFile == "" || len(c.ProofFile) > maxProofLength || c.ProofFile == stateCash {
			return problem(http.StatusBadRequest, "invalid_proof")
		}
		var owned bool
		if err := op.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_proofs WHERE id=$1 AND owner=$2)`, c.ProofFile, o.Owner).
			Scan(&owned); err != nil {
			return core.DatabaseOperationError(err)
		}
		if !owned {
			return problem(http.StatusBadRequest, "invalid_proof")
		}
		o.ProofFile = c.ProofFile
		return nil
	}
	if err := op.paymentAdmin(ctx, c.PaymentAdmin, "be"); err != nil {
		return err
	}
	o.PaymentAdmin = c.PaymentAdmin
	o.Country = "be"
	return nil
}

func (op *operation) finishPayment(ctx context.Context, o *Order) error {
	c := op.command
	if err := op.checkPaymentAttempt(ctx, o); err != nil {
		return err
	}
	if o.State != stateProof && o.State != stateCash {
		return problem(http.StatusConflict, "payment_locked")
	}
	switch c.Name {
	case actionAccept:
		o.State = statePaid
	case actionReject:
		clearPayment(o)
	case actionCancelProof:
		if o.State != stateProof {
			return problem(http.StatusConflict, "payment_locked")
		}
		clearPayment(o)
	case actionCountry:
		if o.State != stateProof || (c.Country != "ru" && c.Country != "be") {
			return problem(http.StatusBadRequest, "invalid_country")
		}
		if err := op.paymentAdmin(ctx, c.PaymentAdmin, c.Country); err != nil {
			return err
		}
		o.Country = c.Country
		o.PaymentAdmin = c.PaymentAdmin
	}
	return nil
}

func (op *operation) persist(ctx context.Context, o Order, hash string) (Order, error) {
	if err := save(ctx, op.tx, o); err != nil {
		return Order{}, err
	}
	if err := releaseCapacity(ctx, op.tx, op.previous, o); err != nil {
		return Order{}, err
	}
	if err := reconcile(ctx, op.tx, op.event, op.deliveryBotID, &op.notificationRegistrations); err != nil {
		return Order{}, err
	}
	result, err := scan(op.tx.QueryRow(ctx, `SELECT `+columns+` FROM core.orders WHERE id=$1`, o.ID))
	if err != nil {
		return Order{}, err
	}
	c := op.command
	err = recordChange(ctx, op.tx, op.actor, c.Origin, c.Name, op.before, result)
	if err != nil {
		return Order{}, err
	}
	if err = op.notifyPayment(ctx, result); err != nil {
		return Order{}, err
	}
	_, err = op.tx.Exec(
		ctx,
		`INSERT INTO core.order_operations(actor,key,request_hash,result) VALUES($1,$2,$3,$4)`,
		op.actor,
		c.Key,
		hash,
		result,
	)
	if err = core.DatabaseOperationError(err); err == nil {
		err = delivery.RegisterBatch(ctx, op.tx, op.deliveryBotID, op.notificationRegistrations)
	}
	return result, err
}

// Historical dishes remain untouched during capacity reconciliation. The saved
// choice is authoritative; removing an extra must not reprice an old meal.
func reconcile(ctx context.Context, tx pgx.Tx, e Event, deliveryBotID int64, pending *[]delivery.Registration) error {
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM core.orders WHERE event_id=$1 AND state<>'deleted'
	ORDER BY CASE WHEN state IN ('proof','paid') THEN 0 ELSE 1 END,COALESCE(attempt_at,created_at),id`, e.ID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	all := []Order{}
	for rows.Next() {
		o, scanError := scan(rows)
		if scanError != nil {
			rows.Close()
			return scanError
		}
		all = append(all, o)
	}
	err = core.DatabaseOperationError(rows.Err())
	rows.Close()
	if err != nil {
		return err
	}
	for _, o := range all {
		oldTotal := o.Choice.Total
		before := o.Choice
		before.Extras = maps.Clone(o.Choice.Extras)
		removed, capacityErr := reconcileOrderCapacity(ctx, tx, &o, e)
		if capacityErr != nil {
			return capacityErr
		}
		if len(removed) == 0 {
			continue
		}
		slices.Sort(removed)
		o.Version++
		if err = save(ctx, tx, o); err != nil {
			return err
		}
		if err = createCapacityRefund(ctx, tx, o, before, removed); err != nil {
			return err
		}
		if err = recordChange(ctx, tx, o.Owner, "system", "capacity", before, o); err != nil {
			return err
		}
		if err = enqueueNotification(ctx, tx, deliveryBotID, pending, o.Owner, "capacity", o, removed); err != nil {
			return err
		}
		_, err = tx.Exec(
			ctx,
			`INSERT INTO core.order_notices(order_id,owner,removed,old_total,new_total) VALUES($1,$2,$3,$4,$5)`,
			o.ID,
			o.Owner,
			removed,
			int64(oldTotal),
			int64(o.Choice.Total),
		)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}
