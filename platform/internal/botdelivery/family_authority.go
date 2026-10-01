package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
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
	refundAmbassador          string
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
	case familyRefund:
		f.events = []string{i.Reference.Event}
	case familyPasses, familyPassRedaction:
		f.menu, err = readPassMenuFamily(ctx, tx, i)
		if f.menu.Event != "" {
			f.events = []string{f.menu.Event}
		}
	case familyPassExport:
		f.events = exportEvents
		if len(f.events) == 0 {
			f.events, err = passbooking.DeliveryExportEvents(ctx, tx, i.Owner)
		}
	case familyPassProof, "pass_tier":
		f.events = []string{i.Reference.Event}
	case familyMassage:
		var revision int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(state->>'event',''),COALESCE(state->>'view',''),revision FROM bot.massage_views WHERE owner=$1`, i.Owner).
			Scan(&f.massageEvent, &f.massageView, &revision)
		err = core.DatabaseOperationContextError(ctx, err)
		if err == nil && revision != i.Reference.Revision {
			err = ErrStale
		}
	}
	return f, refs, err
}

func readPassMenuFamily(ctx context.Context, tx pgx.Tx, i Intent) (PassMenu, error) {
	var menu PassMenu
	var state []byte
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1`, i.Owner).
		Scan(&state, &revision); err != nil {
		if i.Reference.Family == familyPasses && errors.Is(err, pgx.ErrNoRows) {
			// Missing projection cannot skip immutable receipt/source authority.
			return menu, ErrStale
		}
		return menu, core.DatabaseOperationContextError(ctx, err)
	}
	// Incompatible stored state keeps its decode provenance.
	if err := json.Unmarshal(state, &menu); err != nil {
		return PassMenu{}, err
	}
	if revision != i.Reference.Revision || !passMenuSourceMatches(menu, i.Reference) {
		return menu, ErrStale
	}
	return menu, nil
}

func passMenuSourceMatches(menu PassMenu, reference Reference) bool {
	if reference.Family == familyPassRedaction {
		// Only the fixed tombstone notice has source-free authority.
		expected := Reference{
			Kind: CardIntent, Family: familyPassRedaction, CardKey: familyPasses, Revision: reference.Revision,
			Continuation: Continuation{Kind: familyPassRedaction, Revision: reference.Revision},
		}
		return reflect.DeepEqual(reference, expected)
	}
	return reflect.DeepEqual(menu.Source, reference.Source)
}

func (s Service) lockFamily(ctx context.Context, tx pgx.Tx, i Intent, f familyRead) error {
	r := i.Reference
	switch r.Family {
	case familyStatic, "legacy_order", "workflow", "profile", "language", "media":
		return nil
	case "orders":
		if strings.HasPrefix(r.CardKey, "refund:") && r.CardKey != "refund:list" {
			return ErrBinding
		}
		return nil
	case familyRefund, familyRefundRedaction:
		return lockRefundCard(ctx, tx, i, f.refundAmbassador)
	case "knowledge":
		private := strings.HasPrefix(r.CardKey, "knowledge:memo:") ||
			strings.HasPrefix(r.CardKey, "knowledge:proposal:")
		if private && !r.Continuation.Retired && (r.Version <= 0 || len(r.Authorities) == 0) {
			return ErrBinding
		}
		return nil
	case familyPayment:
		return lockPaymentCard(ctx, tx, i)
	case "credits":
		return credits.LockDeliveryReadInTx(ctx, tx, i.Owner, r.Object)
	case "model_settings":
		return modelsettings.LockDeliveryReadInTx(ctx, tx, i.Owner, r.Object)
	case "admin_view", "admin_page", "admin_prompt", "admin_expiry", "admin_utility", "admin_file":
		return s.AdminMessages.LockDeliveryInTx(ctx, tx, i.Owner, r.Family, r.Version, i.Chat)
	case familyOrderExport, familyModernOrderExport:
		return orders.LockDeliveryExportInTx(ctx, tx, i.Owner, r.Event)
	case familyOrderProof, familyModernOrderProof:
		return orders.LockDeliveryProofInTx(ctx, tx, i.Owner, r.Event, r.Object, r.Version, r.ProofAttempt)
	case familyPassExport:
		return passbooking.LockDeliveryExportInTx(ctx, tx, i.Owner, f.events)
	case familyPassProof:
		return passbooking.LockDeliveryProofInTx(ctx, tx, i.Owner, r.Event, r.Object, r.Version, r.ProofAttempt)
	case familyPasses, familyPassRedaction:
		return lockPassMenu(ctx, tx, i, f)
	case "pass_tier":
		return lockRegistrationCapability(ctx, tx, i.Owner, r.Event, "admin_assign")
	case familyMassage:
		return lockMassageMenu(ctx, tx, i, f)
	default:
		return s.lockFood(ctx, tx, i)
	}
}

func lockPaymentCard(ctx context.Context, tx pgx.Tx, i Intent) error {
	r := i.Reference
	if r.Notice != i18n.PaymentUnavailable {
		if r.PaymentRetirement != nil {
			return ErrBinding
		}
		return orders.LockDeliveryPaymentInTx(ctx, tx, i.Owner, r.Event, r.Object)
	}
	if !r.Continuation.Retired || r.Kind != CardIntent || r.Object == "" || r.CardKey != "payment:"+r.Object ||
		r.Source != nil || len(r.Authorities) != 0 || r.Update != 0 ||
		r.Continuation.Kind != "order_card" || r.Continuation.Key != r.CardKey ||
		i.Phase != phaseEdit || i.Target <= 0 || r.PaymentRetirement == nil ||
		r.PaymentRetirement.Operation == "" || r.PaymentRetirement.Effect == "" || r.PaymentRetirement.ViewHash == "" {
		return ErrBinding
	}
	var event, state string
	var canBook bool
	err := tx.QueryRow(ctx, `SELECT o.event_id,o.state,u.can_book FROM core.orders o
 JOIN core.users u ON u.id=o.owner WHERE o.owner=$1 AND o.id=$2 FOR SHARE OF o,u`, i.Owner, r.Object).
		Scan(&event, &state, &canBook)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if event != r.Event || (state != "deleted" && canBook) {
		return ErrStale
	}
	return lockPaymentRetirementProjection(ctx, tx, i)
}

func lockPaymentRetirementProjection(ctx context.Context, tx pgx.Tx, i Intent) error {
	var chat, message int64
	var hash string
	var visible bool
	err := tx.QueryRow(ctx, `SELECT chat_id,message_id,view_hash,visible FROM bot.order_cards
 WHERE owner=$1 AND card_key=$2 FOR SHARE`, i.Owner, i.Reference.CardKey).Scan(&chat, &message, &hash, &visible)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	prior := i.Reference.PaymentRetirement
	if chat != i.Chat || message != i.Target || hash != prior.ViewHash || !visible {
		return ErrStale
	}
	var operation, effect string
	err = tx.QueryRow(ctx, `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE bot_id=$1 AND owner=$2 AND chat_id=$3 AND message_id=$4 AND state='sent'
 AND reference->>'family'='payment' AND reference->>'card_key'=$5
 AND NOT(operation_key=$6 AND effect_key=$7)
 ORDER BY attempted_at DESC NULLS LAST,created_at DESC,operation_key DESC,effect_key DESC LIMIT 1 FOR SHARE`,
		i.BotID, i.Owner, i.Chat, i.Target, i.Reference.CardKey, i.Operation, i.Effect).Scan(&operation, &effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if operation != prior.Operation || effect != prior.Effect {
		return ErrStale
	}
	return lockPaymentRetirementSource(ctx, tx, i)
}

func lockPaymentRetirementSource(ctx context.Context, tx pgx.Tx, i Intent) error {
	bound := i.Reference.PaymentRetirement
	prior, err := Read(ctx, tx, i.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: bound.Operation, Effect: bound.Effect}, false)
	if err != nil {
		return err
	}
	r := prior.Reference
	if prior.Owner != i.Owner || prior.Chat != i.Chat || prior.MessageID != i.Target ||
		prior.State != delivery.Succeeded || !prior.ContinuationDone || prior.Receipt.ViewHash != bound.ViewHash ||
		r.Kind != CardIntent || r.Family != familyPayment || r.CardKey != i.Reference.CardKey ||
		r.Object != i.Reference.Object || r.Event != i.Reference.Event || r.PaymentRetirement != nil {
		return ErrStale
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions
 WHERE owner=$1 AND update_id=0 AND kind=$2 FOR SHARE`, i.Owner, "payment_source:"+r.Object).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	var source struct {
		Original bool                   `json:"original"`
		Source   *readsource.Derivation `json:"source"`
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		return err
	}
	if source.Original != (source.Source == nil) || !reflect.DeepEqual(source.Source, r.Source) {
		return ErrStale
	}
	return nil
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
	if i.Reference.Family == familyPassRedaction {
		if !current.Redacted || i.Phase != phaseEdit || i.Target <= 0 {
			return ErrStale
		}
		return nil
	}
	if current.Redacted {
		return ErrStale
	}
	action := PassCardBinding(current.RegistrationMenu, 0).Capability
	if action != "" {
		err := lockRegistrationCapability(ctx, tx, i.Owner, current.Event, action)
		if errors.Is(err, ErrStale) {
			return passMenuDenied(i, err)
		}
		return err
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
		in.Scope = foodReviewScope
	case familyFoodOrdersExport, familyFoodSummaryExport:
		in.Scope = "export"
	case familyFoodProofMeals:
		in.Kind = legacyfood.Meals
	case familyFoodProofActivity:
		in.Kind = legacyfood.Activity
	case familyFoodReviewMeals:
		in.Kind = legacyfood.Meals
		in.Scope = foodReviewScope
	case familyFoodReviewActivity:
		in.Kind = legacyfood.Activity
		in.Scope = foodReviewScope
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
	case familyPasses, familyPassRedaction:
		var current PassMenu
		var state []byte
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&state, &revision); err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
		if err := json.Unmarshal(state, &current); err != nil {
			return err
		}
		if revision != i.Reference.Revision || !reflect.DeepEqual(current, f.menu) {
			return ErrStale
		}
		if i.Reference.Family == familyPassRedaction {
			return lockRenderedTarget(ctx, tx, i, i.Target)
		}
	case familyMassage:
		var event, view string
		var revision int64
		err := tx.QueryRow(ctx, `SELECT COALESCE(state->>'event',''),COALESCE(state->>'view',''),revision FROM bot.massage_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&event, &view, &revision)
		if err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
		if revision != i.Reference.Revision || event != f.massageEvent || view != f.massageView {
			return ErrStale
		}
	}
	return nil
}

func (s Service) lockFamilyEvent(ctx context.Context, tx pgx.Tx, i Intent) error {
	switch i.Reference.Family {
	case familyOrderExport,
		familyModernOrderExport,
		familyOrderProof,
		familyModernOrderProof,
		familyPayment,
		familyRefund:
		return orders.LockEvent(ctx, tx, i.Reference.Event)
	case "food",
		"food_closed",
		"food_review",
		familyFoodOrdersExport,
		familyFoodSummaryExport,
		familyFoodProofMeals,
		familyFoodProofActivity,
		familyFoodReviewMeals,
		familyFoodReviewActivity:
		return s.Food.LockEvent(ctx, tx, i.Reference.Event)
	default:
		return nil
	}
}

func lockRefundCard(ctx context.Context, tx pgx.Tx, i Intent, ambassador string) error {
	r := i.Reference
	id, err := strconv.ParseInt(r.Object, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.Object || r.CardKey != "refund:"+r.Object ||
		r.Kind != CardIntent || r.Source != nil || len(r.Authorities) != 0 {
		return ErrBinding
	}
	if r.Family == familyRefundRedaction {
		if r.Refund != nil || !r.Continuation.Retired || i.Phase != phaseEdit || i.Target <= 0 {
			return ErrBinding
		}
		return lockRenderedTarget(ctx, tx, i, i.Target)
	}
	if r.Refund == nil || r.Refund.ID != id || r.Continuation.Retired {
		return ErrBinding
	}
	return orders.LockDeliveryRefundInTx(ctx, tx, i.Owner, r.Event, ambassador, *r.Refund)
}
func lockRenderedTarget(ctx context.Context, tx pgx.Tx, i Intent, target int64) error {
	var chat, message int64
	var err error
	switch i.Reference.Family {
	case "workflow":
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.messages WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&chat, &message)
	case familyPasses, familyPassRedaction:
		err = tx.QueryRow(ctx, `SELECT chat_id,message_id FROM bot.pass_views WHERE owner=$1 FOR SHARE`, i.Owner).
			Scan(&chat, &message)
	case familyMassage:
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
		return core.DatabaseOperationContextError(ctx, err)
	}
	if chat != i.Chat || message != target {
		return ErrStale
	}
	return nil
}
