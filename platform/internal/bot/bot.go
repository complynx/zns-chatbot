package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

type Bot struct {
	RegistrationClock   registrationingress.Clock
	Delivery            delivery.Settings
	Onboarding          TelegramOnboarding
	BrowserAuth         *browserauth.Service
	OrderEventID        string
	Lineup              *agent.LineupSource
	WebAppURL           string
	Logger              *slog.Logger
	DB                  *pgxpool.Pool
	API                 appclient.Client
	Host                appclient.Host
	TG                  telegram.Client
	Model               agent.Model
	AV                  AVProcessor
	Stickers            AssetDescriber
	Scripts             agenthost.ScriptEvaluator
	HistoryLimit        int
	AssistantDailyLimit int
	CreditsEnforce      bool
	Observer            Observer
}

func (b *Bot) logger() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}

func (b *Bot) record(ctx context.Context, owner string, id int64, kind string, content any) error {
	switch kind {
	case historyReply, historyOrdersReply, knowledgeReply, registrationReply, historyProfileReply:
		return b.recordReply(ctx, owner, id, kind, content, false, interaction.TrustedReply)
	}
	raw, e := json.Marshal(content)
	if e != nil {
		return e
	}
	// Reject stale archives before a reply can be selected for rendering.
	if e = b.archiveReply(ctx, owner, id, kind, content, interaction.TrustedReply); e != nil {
		return e
	}
	_, e = b.DB.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		owner,
		id,
		kind,
		raw,
	)
	if e != nil {
		return core.DatabaseOperationContextError(ctx, e)
	}
	return nil
}

const (
	maxMessageBytes = 5000
	callbackParts   = 3
	originManual    = "manual"
	originAgent     = "agent"
)

type incoming struct {
	assetMessage        *telegram.Message
	owner, text, origin string
	mediaID             string
	language            string
	chat                int64
}

func parseUpdate(u telegram.Update) (incoming, bool) {
	var from telegram.User
	var chat telegram.Chat
	var text string
	switch {
	case u.Callback != nil:
		from = u.Callback.From
		chat = u.Callback.Message.Chat
		text = u.Callback.Data
	case u.Message != nil:
		from = u.Message.From
		chat = u.Message.Chat
		text = u.Message.Text
	default:
		return incoming{}, false
	}
	if chat.Type != "private" || chat.ID != from.ID || from.IsBot || from.ID <= 0 || from.ID >= 1<<52 {
		return incoming{}, false
	}
	if len(text) > maxMessageBytes {
		text = text[:maxMessageBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	origin := originManual
	if u.Callback == nil && (u.Message == nil || u.Message.Document == nil) && !strings.HasPrefix(text, "/") {
		origin = originAgent
	}
	return incoming{text: text, origin: origin, chat: chat.ID, language: from.LanguageCode,
		assetMessage: u.Message}, true
}

func (b *Bot) handle(ctx context.Context, u telegram.Update) (resultErr error) {
	ctx, in, accepted, authErr := b.authenticatedUpdate(ctx, u)
	if !accepted {
		return authErr
	}
	ctx = withPaymentOpeningCapture(ctx, in.owner, in.chat)
	// A user can become inactive after admission. Never replay that user's
	// rejected action after reactivation; unrelated durable updates can proceed.
	defer func() {
		if errors.Is(resultErr, errPassPlanTerminal) {
			if deliveryErr := b.deliverPassTerminalNotice(ctx, in); deliveryErr != nil {
				resultErr = deliveryErr
			}
		}
		if errors.Is(resultErr, identity.ErrZitadelUserInactive) {
			resultErr = b.denyOnboarding(ctx, in, u)
		}
	}()
	ctx = credits.WithScope(
		ctx,
		credits.Scope{Actor: in.owner, Payer: in.owner, Key: "telegram:" + strconv.FormatInt(u.ID, 10)},
	)
	ctx, authErr = b.bindBudget(ctx, in.owner, u.ID)
	if authErr != nil {
		return authErr
	}
	ctx, diagnostic, diagnosticErr := b.startAgentDiagnostics(ctx, in.owner, u.ID)
	if diagnosticErr != nil {
		return diagnosticErr
	}
	defer func() { diagnostic.Finish(resultErr) }()
	if err := b.recordPrivateUpdate(ctx, in, u); err != nil {
		return err
	}
	if err := b.purgeExpiredAV(ctx); err != nil {
		return err
	}
	if in.language != "" {
		if _, err := b.API.SetLanguage(ctx, in.owner, in.language, true); err != nil {
			return err
		}
	}
	scoped, err := b.forOrderUpdate(ctx, in, u)
	if err != nil {
		return err
	}
	return scoped.dispatchUpdate(ctx, in, u)
}

func (b *Bot) dispatchAdminUpdates(ctx context.Context, in incoming, u telegram.Update) (bool, error) {
	if isCreditsUpdate(in.text) {
		return true, b.handleCredits(ctx, in, u)
	}
	if b.isBrowserAuth(u) {
		return true, b.handleBrowserAuth(ctx, in.owner, u)
	}
	if isModelSettingsUpdate(in, u) {
		return true, b.handleModelSettings(ctx, in, u)
	}
	if isAdminUtilityUpdate(in, u) {
		return true, b.handleAdminUtility(ctx, in, u)
	}
	if isAdminMessageUpdate(in, u) {
		return true, b.handleAdminMessage(ctx, in, u)
	}
	if handled, inputErr := b.handleAdminMessageInput(ctx, in, u); handled || inputErr != nil {
		return handled, inputErr
	}
	if isPassBatchUpdate(in, u) {
		return true, b.handlePassBatch(ctx, in, u)
	}
	return false, nil
}

func (b *Bot) dispatchUpdate(ctx context.Context, in incoming, u telegram.Update) error {
	ctx = withAdminMessageSource(ctx, in, u)
	if handled, err := b.dispatchAdminUpdates(ctx, in, u); handled || err != nil {
		return err
	}
	if in.text == "/passes_table" {
		return b.handlePassExport(ctx, in, u)
	}
	if isPassMenuUpdate(in, u) {
		return b.handlePassMenu(ctx, in, u)
	}
	if isMassageUpdate(in, u) {
		return b.handleMassage(ctx, in, u)
	}
	if u.Callback != nil && strings.HasPrefix(in.text, mediaPrefix) {
		return b.handleMediaCallback(ctx, in, u)
	}
	if u.Message != nil && (u.Message.Document != nil || len(u.Message.Photo) > 0 || hasAV(*u.Message)) {
		return b.handleMediaUpload(ctx, in, u)
	}
	if isLanguageAction(in.text) {
		return b.handleLanguage(ctx, in, u)
	}
	if in.text == "/knowledge" || in.text == "/memo" ||
		(u.Callback != nil && strings.HasPrefix(in.text, knowledgePrefix)) {
		return b.handleKnowledge(ctx, in, u)
	}
	if isProfileUpdate(in, u) {
		return b.handleProfile(ctx, in, u)
	}
	if in.origin == originAgent {
		return b.handleAgentUpdate(ctx, in, u.ID)
	}
	return b.handleManual(ctx, in, u)
}

func (b *Bot) handleManual(ctx context.Context, in incoming, u telegram.Update) error {
	if in.text == "/exportfoodorders" ||
		(u.Callback != nil && (strings.HasPrefix(in.text, legacyFoodPrefix) || strings.HasPrefix(in.text, foodPrefix))) {
		return b.handleFood(ctx, in, u)
	}
	if u.Callback != nil && strings.HasPrefix(in.text, legacyOrderPrefix) {
		return b.handleLegacyOrder(ctx, in, u)
	}
	if err := b.record(
		ctx,
		in.owner,
		u.ID,
		"input",
		map[string]string{originField: in.origin},
	); err != nil {
		return err
	}
	if in.text == "/orders" ||
		(u.Callback != nil && strings.HasPrefix(in.text, orderCallbackPrefix)) {
		return b.handleOrders(ctx, in, u)
	}
	var notice string
	var err error
	if u.Callback != nil {
		notice, err = b.handleCallback(ctx, in, u.ID)
	}
	if err != nil {
		return err
	}
	if err = b.record(ctx, in.owner, u.ID, "reply", notice); err != nil {
		return err
	}
	if err = b.Render(ctx, in.owner, in.chat); err != nil {
		return err
	}
	if u.Callback != nil {
		return b.acknowledge(ctx, u.Callback.ID)
	}
	return nil
}

func (b *Bot) handleCallback(ctx context.Context, in incoming, id int64) (string, error) {
	parts := strings.Split(in.text, ":")
	if len(parts) != callbackParts {
		return b.invalidWorkflowCallback(ctx, in.owner, id)
	}
	version, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return b.invalidWorkflowCallback(ctx, in.owner, id)
	}
	return b.execute(
		ctx,
		in.owner,
		id,
		workflow.Action{
			Name:    parts[0],
			SlotID:  parts[1],
			Version: version,
			Key:     fmt.Sprintf("tg-%d", id),
			Origin:  originManual,
		},
	)
}

func (b *Bot) executePlan(
	ctx context.Context,
	in incoming,
	id int64,
	cached interaction.SavedPlan,
) (interaction.Reply, error) {
	if err := b.planAuthorization().ValidateAgentPlan(ctx, in.owner, id, cached); err != nil {
		return interaction.Reply{}, err
	}
	if cached.RegistrationCommand != nil {
		return interaction.TrustedResult(b.executePlannedRegistration(ctx, in, id, cached))
	}
	if cached.RegistrationAssignment != nil {
		return interaction.TrustedResult(b.executePlannedAssignment(ctx, in, id, cached))
	}
	if cached.KnowledgeCommand != nil {
		return interaction.TrustedResult(b.executePlannedKnowledge(ctx, in, id, cached))
	}
	if cached.ProfileCommand != nil {
		return interaction.TrustedResult(b.executePlannedProfile(ctx, in, id, cached))
	}
	if cached.OrderCommand != nil {
		if cached.OrderCommand.Name == actionExport || cached.OrderCommand.Name == actionInstructions {
			return interaction.TrustedResult(b.executePlannedOrderRead(ctx, in, id, cached))
		}
		return interaction.TrustedResult(b.executePlannedOrder(ctx, in, id, cached))
	}
	if cached.Plan.Action == nil {
		if cached.Kind == interaction.NoticePlan {
			return interaction.TrustedResult(cached.Plan.Text, nil)
		}
		return interaction.Reply{Text: cached.Plan.Text, Origin: interaction.DerivedReply}, nil
	}
	return interaction.TrustedResult(b.executePlannedWorkflow(ctx, in, id, cached))
}

func (b *Bot) planForUpdate(ctx context.Context, in incoming, id int64) (interaction.SavedPlan, error) {
	plan, replay, err := (interaction.TurnCoordinator{Store: interaction.Store{DB: b.DB}}).ResumeOrPlan(
		ctx, in.owner, id, b.turnHost(in, id),
	)
	if replay {
		diagnosticPlanReplay(ctx)
	}
	return plan, err
}
func (b *Bot) addProfileContext(ctx context.Context, owner string, input *agent.Input) error {
	profile, err := b.API.PassProfile(ctx, owner)
	if err != nil {
		return err
	}
	input.Profile = &agent.ProfileContext{
		Version:      profile.Version,
		Pending:      profile.Pending,
		Frozen:       profile.Frozen,
		HasLegalName: profile.LegalName != "",
		HasPassport:  profile.Passport != "",
		Role:         profile.Role,
	}
	input.Profile.History, err = b.API.PassProfileHistory(ctx, owner)
	if err != nil {
		return err
	}
	return nil
}

const callbackAcknowledgementTimeout = 5 * time.Second

// Retry configured control deferrals within the callback budget. Uncontrolled
// calls retain caller deadlines; unknown outcomes remain best-effort.
func (b *Bot) acknowledge(ctx context.Context, id string) error {
	call := func(attempt context.Context) error {
		callErr := b.TG.Call(attempt, "answerCallbackQuery", map[string]string{"callback_query_id": id}, nil)
		if core.IsDatabaseFailure(callErr) {
			return core.ErrDatabase
		}
		return callErr
	}
	var err error
	if b.TG.Control == nil {
		err = call(ctx)
	} else {
		callCtx, cancel := context.WithTimeout(ctx, callbackAcknowledgementTimeout)
		defer cancel()
		err = telegram.RetryControl(callCtx, call)
	}
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if err != nil {
		b.logger().WarnContext(ctx, "callback acknowledgement failed")
	}
	return nil
}
func (b *Bot) execute(ctx context.Context, owner string, id int64, a workflow.Action) (string, error) {
	w, e := b.API.Execute(ctx, owner, a)
	return b.workflowOutcome(ctx, owner, id, a, w, e)
}

func (b *Bot) workflowOutcome(ctx context.Context, owner string, id int64,
	a workflow.Action, w workflow.Workflow, e error) (string, error) {
	if e != nil {
		// Positive SQL provenance must not become a recorded workflow refusal.
		if core.IsDatabaseFailure(e) {
			return "", core.ErrDatabase
		}
		var p *core.ProblemError
		if errors.As(e, &p) && p.Status < 500 {
			if e = b.record(
				ctx,
				owner,
				id,
				"result",
				map[string]any{originField: a.Origin, actionField: a.Name, "error": p.Code},
			); e != nil {
				return "", e
			}
			return b.orderMessage(ctx, owner, i18n.WorkflowFailed, map[string]string{"code": p.Code})
		}
		return "", e
	}
	if e = b.record(
		ctx,
		owner,
		id,
		"result",
		map[string]any{originField: a.Origin, actionField: a.Name, scriptWorkflowView: w},
	); e != nil {
		return "", e
	}
	return b.workflowUpdated(ctx, owner, w.State)
}

const (
	stateDraft  = "draft"
	stateBooked = "booked"
)

// Render always reads current API state. Both controllers share the same card.
func (b *Bot) Render(ctx context.Context, owner string, chat int64) error {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	workflow, err := b.API.Current(ctx, owner)
	if err != nil {
		return err
	}
	catalog, err := b.API.Catalog(ctx, owner)
	if err != nil {
		return err
	}
	notice, native, receipt, err := b.latestNotice(ctx, owner, preference.Language)
	if err != nil {
		return err
	}
	if notice == "" {
		notice, err = defaultNotice(preference.Language, workflow.State)
		if err != nil {
			return err
		}
	}
	payload, err := renderPayload(preference.Language, chat, workflow, catalog, notice, native)
	if err != nil {
		return err
	}
	if err = b.addModelSettingsMenu(ctx, owner, preference.Language, &payload); err != nil {
		return err
	}
	if receipt != nil {
		ctx = withBotCard(ctx, receipt.ref)
	}
	return b.deliverCard(ctx, owner, payload)
}

func (b *Bot) latestNotice(
	ctx context.Context, owner, language string,
) (string, bool, *registrationReceiptNotice, error) {
	notice, err := (interaction.Store{DB: b.DB}).LatestNotice(ctx, owner)
	raw, native, update := notice.Content, notice.Native, notice.UpdateID
	systemNotice, passRedacted := notice.System, notice.SourceRevoked
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil, nil
	}
	if err != nil {
		return "", false, nil, err
	}
	if passRedacted {
		text, translateErr := i18n.Translate(language, i18n.AgentSourceUnavailable, nil)
		return text, false, nil, translateErr
	}
	visible, err := b.derivedReplyVisible(ctx, owner, update)
	if err != nil || !visible {
		return "", false, nil, err
	}
	if systemNotice == i18n.AgentUnavailable {
		receipt, found, receiptErr := b.registrationReceiptNotice(ctx, owner, language, update)
		if receiptErr != nil {
			return "", false, nil, receiptErr
		}
		if found {
			return receipt.text, false, &receipt, nil
		}
	}
	if systemNotice != "" {
		text, translateErr := i18n.Translate(language, systemNotice, nil)
		return text, false, nil, translateErr
	}
	var text string
	err = json.Unmarshal(raw, &text)
	if err == nil && !native {
		text, err = b.localizeWorkflowNotice(ctx, owner, update, language, text)
	}
	return text, native, nil, err
}

func defaultNotice(language, state string) (string, error) {
	id := i18n.WorkflowChoose
	switch state {
	case stateBooked:
		id = i18n.WorkflowBooked
	case stateDraft:
		id = i18n.WorkflowDraft
	}
	return i18n.Translate(language, id, nil)
}

func renderPayload(
	language string,
	chat int64,
	wf workflow.Workflow,
	slots []workflow.Slot,
	notice string,
	native bool,
) (telegram.Send, error) {
	m := &orderMessages{language: language}
	text := notice + "\n\n" + m.text(i18n.WorkflowStatus, map[string]string{
		workflowStateParameter: workflowState(m, wf.State), revisionParameter: strconv.FormatInt(wf.Version, 10),
	}) + "\n"
	rows := [][]telegram.Button{}
	var selectedDescription strings.Builder
	for _, slot := range slots {
		if slot.ID == wf.SlotID {
			fmt.Fprintf(&selectedDescription, "%s — %d %s\n", slot.Title, slot.Price, slot.Currency)
		}
	}
	text += selectedDescription.String()
	if wf.State == stateDraft {
		text += m.text(i18n.WorkflowConfirmHint, nil) + "\n"
		rows = append(
			rows,
			[]telegram.Button{{Text: m.text(i18n.WorkflowConfirm, nil), Data: fmt.Sprintf("confirm::%d", wf.Version)}},
		)
	}
	if wf.State == stateDraft || wf.State == stateBooked {
		rows = append(
			rows,
			[]telegram.Button{{Text: m.text(i18n.WorkflowCancel, nil), Data: fmt.Sprintf("cancel::%d", wf.Version)}},
		)
	}
	if wf.State != stateBooked {
		for _, slot := range slots {
			rows = append(
				rows,
				[]telegram.Button{
					{
						Text: m.text(i18n.WorkflowSlot, map[string]string{
							"title": slot.Title, passPriceParameter: strconv.Itoa(slot.Price),
							"currency": slot.Currency, "remaining": strconv.Itoa(slot.Remaining),
						}),
						Data: fmt.Sprintf("select:%s:%d", slot.ID, wf.Version),
					},
				},
			)
		}
	}
	payload := telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: rows}}
	if native {
		payload.Text, payload.NativeMarkdown, payload.LiteralSuffix = notice, true, strings.TrimPrefix(text, notice)
	}
	return telegram.FormatSend(payload), m.err
}

func (b *Bot) deliverCard(ctx context.Context, owner string, payload telegram.Send) error {
	hash, err := botCardHash(payload)
	if err != nil {
		return err
	}
	ref := botdelivery.Reference{Family: scriptWorkflowView, CardKey: scriptWorkflowView}
	if receipt, ok := ctx.Value(botCardContextKey{}).(botdelivery.Reference); ok {
		ref = receipt
		if strings.HasPrefix(ref.Object, registrationReceiptObject) {
			hash, err = registrationReceiptViewHash(hash, ref)
			if err != nil {
				return err
			}
		}
	}
	var previous string
	err = b.DB.QueryRow(ctx, "SELECT message_id,view_hash FROM bot.messages WHERE owner=$1", owner).
		Scan(&payload.MessageID, &previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if payload.MessageID > 0 && previous == hash {
		return nil
	}
	return b.queueBotCard(
		ctx,
		owner,
		payload,
		ref,
		botdelivery.Continuation{Kind: "workflow_card", ViewHash: hash},
	)
}

const (
	pollInterval  = 300 * time.Millisecond
	unlockTimeout = 5 * time.Second
)

// Run holds a PostgreSQL session lock: only one poller/renderer may own this bot.
func (b *Bot) Run(ctx context.Context) (runErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		runErr = b.creditCutoverRunResult(ctx, runErr)
	}()
	if err := b.validateCreditCutover(ctx); err != nil {
		return err
	}
	conn, err := b.DB.Acquire(ctx)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(918273)`).Scan(&locked); err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if !locked {
		return errors.New("bot already running")
	}
	defer b.unlock(ctx, conn)
	offset, err := b.startTelegramPolling(ctx)
	if err != nil {
		return err
	}
	fatal := make(chan error, 1)
	stopDelivery := startBotDelivery(ctx, b.dispatchQueuedDeliveries, func(err error) {
		runtimeapp.BeginShutdown(ctx)
		select {
		case fatal <- err:
		default:
		}
		cancel()
	})
	defer func() { runErr = finishBotDelivery(stopDelivery, fatal, runErr) }()
	return b.runPolling(ctx, conn, offset)
}

func (b *Bot) runPolling(ctx context.Context, conn *pgxpool.Conn, offset int64) error {
	var err error
	for ctx.Err() == nil {
		if err = conn.Ping(ctx); err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
		offset, err = b.poll(ctx, offset)
		if core.IsDatabaseFailure(err) {
			return core.ErrDatabase
		}
		if cancellation := ctx.Err(); cancellation != nil {
			runtimeapp.BeginShutdown(ctx)
			return cancellation
		}
		if err = b.handlePollError(ctx, err); err != nil {
			return err
		}
		if err = b.reconcileAllViews(ctx); err != nil {
			return err
		}
		if err = b.reconcileMassageViews(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			runtimeapp.BeginShutdown(ctx)
			return nil
		case <-time.After(pollInterval):
		}
	}
	runtimeapp.BeginShutdown(ctx)
	return nil
}

// SQL and invalid cutover state require a new runtime, not another poll attempt.
func (b *Bot) handlePollError(ctx context.Context, err error) error {
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if _, invalid := errors.AsType[creditCutoverError](err); invalid {
		return err
	}
	if err != nil {
		b.logger().WarnContext(ctx, "bot retry pending", "error", err)
	}
	return nil
}

func (b *Bot) unlock(ctx context.Context, conn *pgxpool.Conn) {
	runtimeapp.BeginShutdown(ctx)
	cleanup, cancel := runtimeapp.CompletionContext(ctx, unlockTimeout)
	defer cancel()
	if _, err := conn.Exec(cleanup, `SELECT pg_advisory_unlock(918273)`); err != nil {
		b.logger().WarnContext(ctx, "bot lock cleanup failed", "error", err)
		// Never return a connection with an uncertain session lock to the pool.
		_ = conn.Conn().Close(cleanup)
	}
}

func (b *Bot) poll(ctx context.Context, offset int64) (int64, error) {
	if err := b.validateCreditCutover(ctx); err != nil {
		return offset, err
	}
	if err := b.drainInbox(ctx); err != nil {
		return offset, err
	}
	updates, err := b.TG.Updates(ctx, offset)
	if err != nil {
		return offset, err
	}
	next, err := b.saveBatch(ctx, offset, updates)
	if err != nil {
		return offset, err
	}
	return next, b.drainInbox(ctx)
}

func (b *Bot) reconcileViews(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.messages`)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	type view struct {
		owner string
		chat  int64
	}
	views := []view{}
	for rows.Next() {
		var v view
		if err = rows.Scan(&v.owner, &v.chat); err != nil {
			rows.Close()
			return core.DatabaseOperationContextError(ctx, err)
		}
		views = append(views, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	for _, v := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, v.owner, v.chat)
		if authErr != nil {
			if failure := reconcileDatabaseFailure(authErr); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "view identity pending")
			continue
		}
		if err = b.Render(viewContext, v.owner, v.chat); err != nil {
			if failure := reconcileDatabaseFailure(err); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "view reconciliation pending", "error", err)
		}
	}
	return nil
}

const originField = "origin"
const textField = "text"
const revisionParameter = "revision"
