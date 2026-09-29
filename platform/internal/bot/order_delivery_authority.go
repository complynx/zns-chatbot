package bot

import (
	"context"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (b *Bot) executePlannedOrderRead(
	ctx context.Context,
	in incoming,
	update int64,
	plan interaction.SavedPlan,
) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	if plan.OrderCommand.Name == actionExport {
		scoped := *b
		scoped.OrderEventID = plan.OrderCommand.EventID
		return scoped.exportOrdersWithSource(ctx, in, update, &source)
	}
	return b.showPaymentInstructionsWithSource(ctx, in, plan.OrderCommand.OrderID, &source)
}

func (b *Bot) checkOrderDeliverySource(ctx context.Context, owner string, source *readsource.Derivation) error {
	if source == nil {
		return nil
	}
	if !source.Valid() {
		return appclient.ErrReadStale
	}
	if err := b.Host.CheckReadAuthorities(ctx, owner, source.Authorities); err != nil {
		return err
	}
	return b.API.CheckHistoryGeneration(ctx, owner, *source.Generation)
}

func (b *Bot) checkOrderExportDelivery(ctx context.Context, owner, event string, source *readsource.Derivation) error {
	capability, err := b.API.BusinessCapabilities(ctx, owner, event)
	if err != nil {
		return err
	}
	if !capability.CanBook || !capability.CanExportOrders {
		return appclient.ErrReadStale
	}
	return b.checkOrderDeliverySource(ctx, owner, source)
}

func (b *Bot) checkOrderProofDelivery(
	ctx context.Context,
	owner, event, id string,
	proof orders.Proof,
	source *readsource.Derivation,
) error {
	current, err := b.API.OrderProof(ctx, owner, event, id)
	if err != nil {
		return err
	}
	if current.ID != proof.ID || current.Version != proof.Version || current.Attempt != proof.Attempt ||
		current.Filename != proof.Filename {
		return appclient.ErrReadStale
	}
	return b.checkOrderDeliverySource(ctx, owner, source)
}

type paymentSourceBinding struct {
	Original bool                   `json:"original"`
	Source   *readsource.Derivation `json:"source"`
}

// A refresh retains the source of the opened card. Only an explicit new open can
// establish a new source, after reading a fresh payment payload.
func (b *Bot) bindPaymentSource(ctx context.Context, owner, id string, source *readsource.Derivation) error {
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,0,$2,$3)
	ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=$3`, owner, "payment_source:"+id, paymentSourceBinding{Original: source == nil, Source: source})
	return err
}

func (b *Bot) paymentSource(ctx context.Context, owner, id string) (*readsource.Derivation, error) {
	var binding paymentSourceBinding
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=0 AND kind=$2`, owner, "payment_source:"+id).
		Scan(&binding)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, appclient.ErrReadStale
	}
	if err == nil &&
		(binding.Original != (binding.Source == nil) || (binding.Source != nil && !binding.Source.Valid())) {
		return nil, appclient.ErrReadStale
	}
	return binding.Source, err
}

func (b *Bot) checkPaymentDelivery(
	ctx context.Context,
	owner, event string,
	info orders.PaymentInstructions,
	source *readsource.Derivation,
) error {
	bound, err := b.paymentSource(ctx, owner, info.OrderID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(bound, source) {
		return appclient.ErrReadStale
	}
	current, err := b.API.PaymentInstructions(ctx, owner, event, info.OrderID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, info) {
		return appclient.ErrReadStale
	}
	return b.checkOrderDeliverySource(ctx, owner, bound)
}
