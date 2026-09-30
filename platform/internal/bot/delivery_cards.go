package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type botDeliveryOriginKey struct{}

// withBotDeliveryOrigin is a host-only parent receipt identity. Attempts are
// deliberately excluded: a followup retry must find the same child effects.
func withBotDeliveryOrigin(ctx context.Context, parent delivery.Reference) context.Context {
	return context.WithValue(ctx, botDeliveryOriginKey{}, parent)
}

func botDeliveryChild(ctx context.Context, ref botdelivery.Reference) (string, string, bool, error) {
	parent, ok := ctx.Value(botDeliveryOriginKey{}).(delivery.Reference)
	if !ok {
		return "", "", false, nil
	}
	switch parent.Owner {
	case delivery.Orders, delivery.Passes, delivery.Food, delivery.Massage, delivery.Admin, delivery.Announcement:
	case delivery.Bot:
		return "", "", true, botdelivery.ErrBinding
	default:
		return "", "", true, botdelivery.ErrBinding
	}
	if parent.Key == "" || parent.Effect == "" || len(parent.Key) > 200 || len(parent.Effect) > 100 {
		return "", "", true, botdelivery.ErrBinding
	}
	// These field names and their order preserve the persisted parent hash.
	raw, err := json.Marshal(struct {
		Owner  delivery.Owner `json:"Owner"`
		Key    string         `json:"Key"`
		Effect string         `json:"Effect"`
	}{parent.Owner, parent.Key, parent.Effect})
	if err != nil {
		return "", "", true, err
	}
	parentHash := sha256.Sum256(raw)
	childHash := sha256.Sum256([]byte(ref.Family + ":" + ref.CardKey))
	return "followup:" + hex.EncodeToString(parentHash[:]), "card:" + hex.EncodeToString(childHash[:]), true, nil
}

type botRetiredCardKey struct{}
type botCardContextKey struct{}
type botCardCaptureKey struct{}
type botCardCapture struct {
	owner     string
	reference botdelivery.Reference
	rendered  botRenderedDelivery
	found     bool
}

func withBotCard(ctx context.Context, ref botdelivery.Reference) context.Context {
	return context.WithValue(ctx, botCardContextKey{}, ref)
}

// queueBotCard is also the render-only boundary during dispatch. A reconstruction
// may visit several cards, but only the selected stable effect is captured.
func (b *Bot) queueBotCard(
	ctx context.Context,
	owner string,
	payload telegram.Send,
	ref botdelivery.Reference,
	receipt botdelivery.Continuation,
) error {
	ref.Kind = botdelivery.CardIntent
	operation, effect, child, childErr := botDeliveryChild(ctx, ref)
	if childErr != nil {
		return childErr
	}
	if child {
		found, err := b.existingBotChildCard(ctx, owner, payload.ChatID, operation, effect, ref)
		if err != nil || found {
			return err
		}
	}
	if err := b.Delivery.Validate(); err != nil {
		return err
	}
	if err := b.bindBotCardSource(ctx, owner, &ref); err != nil {
		return err
	}
	if capture, ok := ctx.Value(botCardCaptureKey{}).(*botCardCapture); ok {
		// A matching pass retirement is a durable cleanup effect, not the old card.
		retirement := capture.owner == owner && capture.reference.Family == botFamilyPasses &&
			capture.reference.Source != nil &&
			ref.Family == botFamilyPassRedaction && ref.CardKey == capture.reference.CardKey &&
			ref.Revision == capture.reference.Revision && receipt.Kind == botFamilyPassRedaction &&
			payload.MessageID > 0
		if !retirement {
			return capture.capture(owner, ref, payload, receipt)
		}
		saved, revision, err := b.passMenuRecord(ctx, owner)
		if err != nil {
			return err
		}
		if !saved.Redacted || revision != ref.Revision ||
			!reflect.DeepEqual(saved.Source, capture.reference.Source) {
			return botdelivery.ErrStale
		}
	}
	ref.Continuation = receipt
	return b.storeBotCard(ctx, owner, payload, ref, operation, effect, child)
}

// storeBotCard locks its source before the card identity and shared delivery lane.
func (b *Bot) storeBotCard(
	ctx context.Context,
	owner string,
	payload telegram.Send,
	ref botdelivery.Reference,
	operation, effect string,
	child bool,
) error {
	return b.Host.EnqueueBotCard(
		ctx,
		botdelivery.CardRequest{
			Owner:     owner,
			Chat:      payload.ChatID,
			Target:    payload.MessageID,
			Reference: ref,
			Operation: operation,
			Effect:    effect,
			Child:     child,
		},
	)
}

func botCardHash(payload telegram.Send) (string, error) {
	payload.MessageID = 0
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (b *Bot) orderCardReference(ctx context.Context, owner, key string) (botdelivery.Reference, error) {
	if ref, ok := ctx.Value(botCardContextKey{}).(botdelivery.Reference); ok {
		ref.CardKey = key
		return ref, nil
	}
	ref := botdelivery.Reference{
		Kind:    botdelivery.CardIntent,
		Family:  ownOrdersScope,
		CardKey: key,
		Event:   b.currentOrderEvent(),
	}
	switch {
	case strings.HasPrefix(key, refundCardPrefix) && key != refundCardPrefix+"list":
		retired, _ := ctx.Value(botRetiredCardKey{}).(bool)
		if !retired {
			return ref, botdelivery.ErrBinding
		}
		ref.Family, ref.Object = botFamilyRefundRedaction, strings.TrimPrefix(key, refundCardPrefix)
	case key == profileCardKey:
		ref.Family = profileCardKey
	case key == languageKey:
		ref.Family = languageKey
	case strings.HasPrefix(key, knowledgePrefix):
		ref.Family = botFamilyKnowledge
	case strings.HasPrefix(key, mediaPrefix):
		ref.Family, ref.Object = "media", strings.TrimPrefix(key, mediaPrefix)
	case strings.HasPrefix(key, paymentCardPrefix):
		ref.Family, ref.Object = registrationPayment, strings.TrimPrefix(key, paymentCardPrefix)
		source, err := b.paymentSource(ctx, owner, ref.Object)
		if err != nil {
			return ref, err
		}
		ref.Source = source
	case strings.HasPrefix(key, foodPrefix):
		return ref, botdelivery.ErrBinding
	}
	return ref, nil
}

func botCardReplyKinds(ref botdelivery.Reference) []string {
	switch ref.Family {
	case scriptWorkflowView:
		return []string{historyReply}
	case ownOrdersScope:
		if ref.CardKey == "menu" {
			return []string{historyOrdersReply}
		}
	case profileCardKey:
		return []string{historyProfileReply, profileAnswer}
	case botFamilyKnowledge:
		return []string{knowledgeReply}
	}
	return nil
}

func (b *Bot) bindBotCardSource(ctx context.Context, owner string, ref *botdelivery.Reference) error {
	if ref.Family == botFamilyPassRedaction {
		return nil
	}
	generation, err := b.API.HistoryGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if strings.HasPrefix(ref.Object, registrationReceiptObject) {
		return b.bindRegistrationReceiptCard(ctx, owner, ref, generation)
	}
	ref.Generation = &generation
	kinds := botCardReplyKinds(*ref)
	if len(kinds) == 0 {
		return nil
	}
	var update int64
	err = b.DB.QueryRow(ctx, "SELECT update_id FROM bot.interactions WHERE owner=$1 AND kind=ANY($2) ORDER BY id DESC LIMIT 1", owner, kinds).
		Scan(&update)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ref.Update = update
	visible, err := b.derivedReplyVisible(ctx, owner, update)
	if err != nil || !visible {
		return err
	}
	origin, err := (interaction.Store{DB: b.DB}).ReplyOrigin(ctx, owner, update)
	if err == nil && origin == interaction.TrustedReply {
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, owner, update)
	if err != nil {
		return err
	}
	source, err := savedPlanSource(plan)
	if err != nil {
		return err
	}
	ref.Source = &source
	return nil
}

func (b *Bot) renderBotCard(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	if i.Reference.Family == botdelivery.PassReceiptRedactionFamily {
		return b.renderBotPassReceiptRedaction(ctx, i)
	}
	// The causal reply/source must still match; never attach the old effect to a
	// newly valid model result after revocation or history deletion.
	observed := i.Reference
	observed.Source = i.Reference.Source
	if err := b.bindBotCardSource(ctx, i.Owner, &observed); err != nil {
		return botRenderedDelivery{}, err
	}
	if observed.Update != i.Reference.Update || !reflect.DeepEqual(observed.Generation, i.Reference.Generation) ||
		!reflect.DeepEqual(observed.Source, i.Reference.Source) {
		return botRenderedDelivery{}, botdelivery.ErrStale
	}
	capture := &botCardCapture{owner: i.Owner, reference: i.Reference}
	ctx = context.WithValue(ctx, botCardCaptureKey{}, capture)
	scoped := *b
	scoped.OrderEventID = i.Reference.Event
	var err error
	switch i.Reference.Family {
	case scriptWorkflowView:
		err = scoped.Render(ctx, i.Owner, i.Chat)
	case ownOrdersScope, botFamilyRefund:
		err = scoped.RenderOrders(ctx, i.Owner, i.Chat)
	case botFamilyRefundRedaction:
		return scoped.renderRefundRedaction(ctx, i)
	case profileCardKey:
		err = scoped.RenderProfile(ctx, i.Owner, i.Chat)
	case botFamilyKnowledge:
		err = scoped.RenderKnowledge(ctx, i.Owner, i.Chat)
	case "media":
		err = scoped.RenderMedia(ctx, i.Owner, i.Chat, i.Reference.Object)
		if errors.Is(err, pgx.ErrNoRows) {
			err = scoped.retireMediaView(ctx, mediaView{Owner: i.Owner, Chat: i.Chat, ID: i.Reference.Object})
		}
	case botFamilyMassage:
		err = scoped.RenderMassage(ctx, i.Owner, i.Chat, i.Reference.Notice)
	case botFamilyPasses:
		err = scoped.RenderPassMenu(ctx, i.Owner, i.Chat, i.Reference.Notice)
	case botFamilyPassRedaction:
		return scoped.renderBotPassRedaction(ctx, i)
	case languageKey:
		err = scoped.renderBotLanguage(ctx, i)
	case "food", botFamilyFoodReview:
		err = scoped.renderFood(
			ctx,
			incoming{owner: i.Owner, chat: i.Chat},
			i.Reference.Event,
			i.Reference.Object,
			i.Reference.Family == botFamilyFoodReview,
		)
	case registrationPayment:
		_, err = scoped.paymentInstructionsWithSource(
			ctx,
			incoming{owner: i.Owner, chat: i.Chat},
			i.Reference.Object,
			i.Reference.Source,
			false,
		)
	default:
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
	if err != nil {
		return botRenderedDelivery{}, err
	}
	if !capture.found {
		return botRenderedDelivery{}, botdelivery.ErrStale
	}
	return capture.rendered, nil
}

func (b *Bot) renderBotLanguage(ctx context.Context, i botdelivery.Intent) error {
	pref, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return err
	}
	text, err := i18n.Translate(pref.Language, i18n.LanguageCurrent, map[string]string{languageKey: pref.Language})
	if err != nil {
		return err
	}
	if i.Reference.Notice != "" {
		notice, noticeErr := i18n.Translate(
			pref.Language,
			i.Reference.Notice,
			map[string]string{languageKey: pref.Language},
		)
		if noticeErr != nil {
			return noticeErr
		}
		text = notice + "\n" + text
	}
	ctx = withBotCard(ctx, i.Reference)
	payload := telegram.Send{ChatID: i.Chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	for _, locale := range i18n.SupportedLocales() {
		payload.Markup.Rows = append(
			payload.Markup.Rows,
			[]telegram.Button{{Text: string(locale), Data: languageCallbackPrefix + string(locale)}},
		)
	}
	return b.deliverOrderCard(ctx, i.Owner, languageKey, payload)
}

// existingBotChildCard accepts only the immutable binding established by its parent.
func (b *Bot) existingBotChildCard(
	ctx context.Context,
	owner string,
	chat int64,
	operation, effect string,
	ref botdelivery.Reference,
) (bool, error) {
	existing, err := botdelivery.Read(
		ctx,
		b.DB,
		b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect},
		false,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing.Owner != owner || existing.Chat != chat || existing.Reference.Kind != botdelivery.CardIntent ||
		existing.Reference.Family != ref.Family ||
		existing.Reference.CardKey != ref.CardKey {
		return false, botdelivery.ErrBinding
	}
	return true, nil
}

func (c *botCardCapture) capture(
	owner string,
	ref botdelivery.Reference,
	payload telegram.Send,
	receipt botdelivery.Continuation,
) error {
	if c.owner != owner || c.reference.CardKey != ref.CardKey || c.reference.Family != ref.Family {
		return nil
	}
	if strings.HasPrefix(c.reference.Object, registrationReceiptObject) ||
		strings.HasPrefix(ref.Object, registrationReceiptObject) {
		if c.reference.Update != ref.Update || !reflect.DeepEqual(c.reference.Generation, ref.Generation) {
			return botdelivery.ErrStale
		}
	}
	if c.reference.Revision != ref.Revision || c.reference.Version != ref.Version || c.reference.Object != ref.Object ||
		!reflect.DeepEqual(c.reference.Refund, ref.Refund) ||
		!reflect.DeepEqual(c.reference.Authorities, ref.Authorities) ||
		!reflect.DeepEqual(c.reference.Source, ref.Source) {
		return botdelivery.ErrStale
	}
	c.rendered = botRenderedDelivery{Payload: payload, Receipt: receipt}
	c.found = true
	return nil
}
