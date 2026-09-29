package botdelivery

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type familyRead struct {
	menu                      PassMenu
	events                    []string
	massageEvent, massageView string
}

func (s Service) prepareFamily(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	exportEvents []string,
) (familyRead, []readsource.Authority, error) {
	var f familyRead
	refs := readsource.CloneAuthorities(i.Reference.Authorities)
	if i.Reference.Source != nil {
		var err error
		refs, err = readsource.Merge(refs, i.Reference.Source.Authorities)
		if err != nil {
			return f, nil, err
		}
	}
	extra, err := s.AdminMessages.DeliveryAuthorities(ctx, tx, i.Owner, i.Reference.Family, i.Reference.Version)
	if err != nil {
		return f, nil, err
	}
	refs, err = readsource.Merge(refs, extra)
	if err != nil {
		return f, nil, err
	}
	switch i.Reference.Family {
	case "passes", "pass_redaction":
		var revision int64
		if err = tx.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1`, i.Owner).
			Scan(&f.menu, &revision); err != nil {
			return f, nil, err
		}
		if revision != i.Reference.Revision || !reflect.DeepEqual(f.menu.Source, i.Reference.Source) {
			return f, nil, ErrStale
		}
		if f.menu.Event != "" {
			f.events = []string{f.menu.Event}
		}
	case "pass_export":
		f.events = exportEvents
		if len(f.events) == 0 {
			f.events, err = passbooking.DeliveryExportEvents(ctx, tx, i.Owner)
		}
	case "pass_proof", "pass_tier":
		f.events = []string{i.Reference.Event}
	case "massage":
		var revision int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(state->>'event',''),COALESCE(state->>'view',''),revision FROM bot.massage_views WHERE owner=$1`, i.Owner).
			Scan(&f.massageEvent, &f.massageView, &revision)
		if err == nil && revision != i.Reference.Revision {
			err = ErrStale
		}
	}
	return f, refs, err
}
func (s Service) lockFamily(ctx context.Context, tx pgx.Tx, i Intent, f familyRead) error {
	r := i.Reference
	switch r.Family {
	case "static", "legacy_order", "workflow", "orders", "profile", "language", "media":
		return nil
	case "knowledge":
		private := strings.HasPrefix(r.CardKey, "knowledge:memo:") ||
			strings.HasPrefix(r.CardKey, "knowledge:proposal:")
		if private && !r.Continuation.Retired && (r.Version <= 0 || len(r.Authorities) == 0) {
			return ErrBinding
		}
		return nil
	case "payment":
		return orders.LockDeliveryPaymentInTx(ctx, tx, i.Owner, r.Event, r.Object)
	case "credits":
		return credits.LockDeliveryReadInTx(ctx, tx, i.Owner, r.Object)
	case "model_settings":
		return modelsettings.LockDeliveryReadInTx(ctx, tx, i.Owner, r.Object)
	case "admin_view", "admin_page", "admin_prompt", "admin_expiry", "admin_utility", "admin_file":
		return s.AdminMessages.LockDeliveryInTx(ctx, tx, i.Owner, r.Family, r.Version, i.Chat)
	case "order_export", "modern_order_export":
		return orders.LockDeliveryExportInTx(ctx, tx, i.Owner, r.Event)
	case "order_proof", "modern_order_proof":
		return orders.LockDeliveryProofInTx(ctx, tx, i.Owner, r.Event, r.Object, r.Version, r.ProofAttempt)
	case "pass_export":
		return passbooking.LockDeliveryExportInTx(ctx, tx, i.Owner, f.events)
	case "pass_proof":
		return passbooking.LockDeliveryProofInTx(ctx, tx, i.Owner, r.Event, r.Object, r.Version, r.ProofAttempt)
	case "passes", "pass_redaction":
		return lockPassMenu(ctx, tx, i, f)
	case "pass_tier":
		return lockRegistrationCapability(ctx, tx, i.Owner, r.Event, "admin_assign")
	case "massage":
		return lockMassageMenu(ctx, tx, i, f)
	default:
		return s.lockFood(ctx, tx, i)
	}
}
func lockRegistrationCapability(ctx context.Context, tx pgx.Tx, owner, event, action string) error {
	valid, err := passbooking.LockReadAuthorities(
		ctx,
		tx,
		owner,
		[]passbooking.ReadAuthority{{Kind: passbooking.ReadCapability, Event: event, Action: action}},
	)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return ErrStale
	}
	return nil
}
func lockPassMenu(ctx context.Context, tx pgx.Tx, i Intent, f familyRead) error {
	current := f.menu
	if i.Reference.Family == "pass_redaction" {
		if !current.Redacted {
			return ErrStale
		}
		return nil
	}
	if current.Redacted {
		return ErrStale
	}
	action := ""
	switch current.View {
	case "queue", "admin_target":
		action = "admin_assign"
	case "payment_queue":
		action = "proof_accept"
	case "takeover_target":
		action = "takeover"
	}
	if action != "" {
		return lockRegistrationCapability(ctx, tx, i.Owner, current.Event, action)
	}
	return nil
}
func lockMassageMenu(ctx context.Context, tx pgx.Tx, i Intent, f familyRead) error {
	event, view := f.massageEvent, f.massageView
	if !slices.Contains([]string{"clients", "legacy_clients", "timetable", "preferences", "instant"}, view) {
		return nil
	}
	valid, err := massage.LockReadAuthority(ctx, tx, i.Owner, massage.ReadAuthority{Event: event, Owner: i.Owner})
	if err != nil {
		return err
	}
	if !valid {
		return ErrStale
	}
	return nil
}
func (s Service) lockFood(ctx context.Context, tx pgx.Tx, i Intent) error {
	r := i.Reference
	in := legacyfood.DeliveryRead{Event: r.Event, Order: r.Object, Generation: r.Attempt, Version: r.Version}
	switch r.Family {
	case "food", "food_closed":
	case "food_review":
		in.Scope = "review"
	case "food_orders_export", "food_summary_export":
		in.Scope = "export"
	case "food_proof_meals":
		in.Kind = legacyfood.Meals
	case "food_proof_activity":
		in.Kind = legacyfood.Activity
	case "food_review_meals":
		in.Kind = legacyfood.Meals
		in.Scope = "review"
	case "food_review_activity":
		in.Kind = legacyfood.Activity
		in.Scope = "review"
	default:
		return ErrBinding
	}
	return s.Food.LockDeliveryReadInTx(ctx, tx, i.Owner, in)
}
func familyMissing(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	return err
}

// View rows follow source and history locks, before the immutable intent lock.
func lockViewBinding(ctx context.Context, tx pgx.Tx, i Intent, f familyRead) error {
	switch i.Reference.Family {
	case "passes", "pass_redaction":
		var current PassMenu
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&current, &revision); err != nil {
			return err
		}
		if revision != i.Reference.Revision || !reflect.DeepEqual(current, f.menu) {
			return ErrStale
		}
	case "massage":
		var event, view string
		var revision int64
		err := tx.QueryRow(ctx, `SELECT COALESCE(state->>'event',''),COALESCE(state->>'view',''),revision FROM bot.massage_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&event, &view, &revision)
		if err != nil {
			return err
		}
		if revision != i.Reference.Revision || event != f.massageEvent || view != f.massageView {
			return ErrStale
		}
	}
	return nil
}

func (s Service) lockFamilyEvent(ctx context.Context, tx pgx.Tx, i Intent) error {
	switch i.Reference.Family {
	case "order_export", "modern_order_export", "order_proof", "modern_order_proof", "payment":
		return orders.LockEvent(ctx, tx, i.Reference.Event)
	case "food",
		"food_closed",
		"food_review",
		"food_orders_export",
		"food_summary_export",
		"food_proof_meals",
		"food_proof_activity",
		"food_review_meals",
		"food_review_activity":
		return s.Food.LockEvent(ctx, tx, i.Reference.Event)
	default:
		return nil
	}
}
func lockRenderedTarget(ctx context.Context, tx pgx.Tx, i Intent, target int64) error {
	var chat, message int64
	var err error
	switch i.Reference.Family {
	case "workflow":
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.messages WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&chat, &message)
	case "passes", "pass_redaction":
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.pass_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&chat, &message)
	case "massage":
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.massage_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&chat, &message)
	default:
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.order_cards WHERE owner=$1 AND card_key=$2 FOR SHARE`, i.Owner, i.Reference.CardKey).
			Scan(&chat, &message)
	}
	if errors.Is(err, pgx.ErrNoRows) && target == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if chat != i.Chat || message != target {
		return ErrStale
	}
	return nil
}
