package orders

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// LegacyCallbackRequest carries callback data, never a trusted bot namespace.
type LegacyCallbackRequest struct {
	Data  string `json:"data"`
	Event string `json:"event"`
}

// LegacyCallbackBinding is pinned by the delivery host before any effects.
type LegacyCallbackBinding struct {
	Action  string  `json:"action"`
	EventID string  `json:"event_id"`
	OrderID string  `json:"order_id,omitempty"`
	Command Command `json:"command"`
}

const legacyAccept = "adm_acc"
const legacyReject = "adm_rej"
const legacyCancel = "pcancel"
const legacyCallbackWithArgument = 4
const legacyCallbackWithOrder = 3

type legacyCallback struct {
	action string
	id     string
	token  string
	admin  int64
}

func parseLegacyCallback(data string) (legacyCallback, error) {
	parts := strings.Split(data, "|")
	if len(data) > 64 || len(parts) < 2 || parts[0] != "orders" {
		return legacyCallback{}, problem(http.StatusBadRequest, "invalid_callback")
	}
	callback := legacyCallback{action: parts[1]}
	count := 3
	switch callback.action {
	case "start", "close", "xlsx":
		count = 2
	case "del", "pay", "paid":
	case stateCash:
		count = 4
	case legacyAccept, legacyReject, legacyCancel:
		if len(parts) == legacyCallbackWithArgument {
			count = 4
			callback.token = parts[3]
		}
	default:
		return legacyCallback{}, problem(http.StatusBadRequest, "invalid_callback")
	}
	if len(parts) != count {
		return legacyCallback{}, problem(http.StatusBadRequest, "invalid_callback")
	}
	if count >= legacyCallbackWithOrder {
		callback.id = strings.ToLower(parts[2])
		if decoded, err := hex.DecodeString(callback.id); err != nil || len(decoded) != 12 {
			return legacyCallback{}, problem(http.StatusBadRequest, "invalid_callback")
		}
	}
	if callback.action == "cash" {
		var err error
		callback.admin, err = strconv.ParseInt(parts[3], 10, 64)
		if err != nil || callback.admin <= 0 {
			return legacyCallback{}, problem(http.StatusBadRequest, "invalid_callback")
		}
	}
	return callback, nil
}

// ResolveLegacyCallback reads only Core data and checks current permissions.
// LegacyBotID is startup configuration, never accepted in the request body.
func (s Service) ResolveLegacyCallback(
	ctx context.Context,
	actor string,
	request LegacyCallbackRequest,
) (LegacyCallbackBinding, error) {
	if s.LegacyBotID <= 0 {
		return LegacyCallbackBinding{}, problem(http.StatusConflict, "legacy_callbacks_disabled")
	}
	callback, err := parseLegacyCallback(request.Data)
	if err != nil {
		return LegacyCallbackBinding{}, err
	}
	var allowed bool
	if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1 AND can_book)`, actor).
		Scan(&allowed); err != nil {
		return LegacyCallbackBinding{}, err
	}
	if !allowed {
		return LegacyCallbackBinding{}, problem(http.StatusForbidden, "forbidden")
	}
	binding := LegacyCallbackBinding{Action: callback.action, EventID: request.Event}
	if callback.id == "" {
		return s.legacyNavigation(ctx, actor, binding)
	}
	order, source, err := s.legacyOrder(ctx, actor, callback)
	if err != nil {
		return LegacyCallbackBinding{}, err
	}
	binding.EventID, binding.OrderID = order.EventID, order.ID
	binding.Command = Command{EventID: order.EventID, OrderID: order.ID,
		Version: order.Version, Attempt: order.Attempt, Origin: "manual"}
	if err = s.legacyCommand(ctx, callback, order, source, &binding); err != nil {
		return LegacyCallbackBinding{}, err
	}
	return binding, nil
}

func (s Service) legacyNavigation(
	ctx context.Context,
	actor string,
	binding LegacyCallbackBinding,
) (LegacyCallbackBinding, error) {
	if _, err := s.Event(ctx, binding.EventID); err != nil {
		return LegacyCallbackBinding{}, problem(http.StatusNotFound, "event_not_found")
	}
	if binding.Action == "xlsx" {
		if err := s.authorizeInbox(ctx, actor, binding.EventID); err != nil {
			return LegacyCallbackBinding{}, err
		}
	}
	return binding, nil
}

func (s Service) legacyOrder(
	ctx context.Context,
	actor string,
	callback legacyCallback,
) (Order, map[string]json.RawMessage, error) {
	rows, err := s.DB.Query(ctx, `SELECT target_id,event_id,source_record FROM core.legacy_order_import_references
	WHERE bot_id=$1 AND source_domain='orders' AND lower(source_record->'_id'->>'$oid')=$2 LIMIT 2`, s.LegacyBotID, callback.id)
	if err != nil {
		return Order{}, nil, err
	}
	defer rows.Close()
	type reference struct {
		ID, Event string
		Source    map[string]json.RawMessage
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (reference, error) {
		var ref reference
		scanErr := row.Scan(&ref.ID, &ref.Event, &ref.Source)
		return ref, scanErr
	})
	if err != nil {
		return Order{}, nil, err
	}
	if len(refs) != 1 {
		return Order{}, nil, problem(http.StatusNotFound, "order_not_found")
	}
	ref := refs[0]
	admin := callback.action == legacyAccept || callback.action == legacyReject
	if admin {
		if err = s.authorizeInbox(ctx, actor, ref.Event); err != nil {
			return Order{}, nil, err
		}
	}
	order, err := scan(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM core.orders
	WHERE id=$1 AND event_id=$2 AND state<>'deleted' AND (owner=$3 OR $4)`, ref.ID, ref.Event, actor, admin))
	if errors.Is(err, pgx.ErrNoRows) {
		err = problem(http.StatusNotFound, "order_not_found")
	}
	return order, ref.Source, err
}

func (s Service) legacyCommand(ctx context.Context, callback legacyCallback, order Order,
	source map[string]json.RawMessage, binding *LegacyCallbackBinding) error {
	switch callback.action {
	case "pay":
		return nil
	case "del":
		binding.Command.Name = "delete"
	case stateCash:
		binding.Command.Name = stateCash
		admin, err := s.legacyAdmin(ctx, order.EventID, callback.admin, "be")
		if err != nil {
			return err
		}
		binding.Command.PaymentAdmin = admin
	case "paid":
		if order.State != stateProof {
			return problem(http.StatusConflict, "payment_locked")
		}
		binding.Command.Name, binding.Command.Country = actionCountry, "ru"
		adminID, err := s.legacyRUAdmin(ctx, order.EventID)
		if err != nil {
			return err
		}
		admin, err := s.legacyAdmin(ctx, order.EventID, adminID, "ru")
		if err != nil {
			return err
		}
		binding.Command.PaymentAdmin = admin
	case legacyAccept, legacyReject, legacyCancel:
		if callback.action == legacyCancel && order.State != stateProof {
			return problem(http.StatusConflict, "payment_locked")
		}
		if err := legacyPaymentGuard(callback.token, order, source); err != nil {
			return err
		}
		binding.Command.Name = map[string]string{legacyAccept: actionAccept, legacyReject: actionReject, legacyCancel: actionCancelProof}[callback.action]
	}
	return nil
}

func (s Service) legacyRUAdmin(ctx context.Context, event string) (int64, error) {
	var count int
	var raw *string
	err := s.DB.QueryRow(ctx, `SELECT count(*),min(source_record->>'payment_admin_ru')
	FROM core.legacy_order_import_references WHERE bot_id=$1 AND event_id=$2 AND source_domain='configuration'`,
		s.LegacyBotID, event).Scan(&count, &raw)
	if err != nil {
		return 0, err
	}
	if count != 1 || raw == nil {
		return 0, problem(http.StatusConflict, "payment_context_unavailable")
	}
	id, err := strconv.ParseInt(*raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, problem(http.StatusConflict, "payment_context_unavailable")
	}
	return id, nil
}

func legacyPaymentGuard(token string, order Order, source map[string]json.RawMessage) error {
	if order.State != stateCash && order.State != stateProof {
		return problem(http.StatusConflict, "payment_locked")
	}
	raw, present := source["payment_attempt_token"]
	var original string
	if present && (json.Unmarshal(raw, &original) != nil || original == "" || token != original) {
		return problem(http.StatusConflict, "stale_attempt")
	}
	if !present {
		if token != "" {
			return problem(http.StatusConflict, "stale_attempt")
		}
		var proof string
		_ = json.Unmarshal(source["proof_file"], &proof)
		if proof != "" && proof != stateCash {
			original = "legacy-proof:" + proof
		}
	}
	if order.Attempt != original {
		return problem(http.StatusConflict, "stale_attempt")
	}
	return nil
}

func (s Service) legacyAdmin(ctx context.Context, event string, telegramID int64, country string) (string, error) {
	var owner string
	err := s.DB.QueryRow(ctx, `SELECT a.owner FROM core.order_admins a
	JOIN core.telegram_identities t ON t.owner=a.owner WHERE a.event_id=$1 AND a.country=$2
	AND t.bot_id=$3 AND t.telegram_id=$4`, event, country, s.LegacyBotID, telegramID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		err = problem(http.StatusBadRequest, "invalid_payment_admin")
	}
	return owner, err
}
