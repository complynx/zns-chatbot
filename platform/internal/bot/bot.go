package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type Bot struct {
	Onboarding          TelegramOnboarding
	BrowserAuth         *browserauth.Service
	OrderEventID        string
	Lineup              *agent.LineupSource
	WebAppURL           string
	Logger              *slog.Logger
	DB                  *pgxpool.Pool
	API                 APIClient
	TG                  telegram.Client
	Model               agent.Model
	AV                  AVProcessor
	Stickers            AssetDescriber
	Scripts             ScriptEvaluator
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
	raw, e := json.Marshal(content)
	if e != nil {
		return e
	}
	// Reject stale archives before a reply can be selected for rendering.
	if e = b.archiveReply(ctx, owner, id, kind, content); e != nil {
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
		return e
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
type cachedPlan struct {
	HistoryGeneration         int64                        `json:"history_generation"`
	HistoryRedacted           bool                         `json:"history_redacted,omitempty"`
	MediaResolvedFood         *agent.FoodReceiptTarget     `json:"media_resolved_food,omitempty"`
	SystemNotice              i18n.ID                      `json:"system_notice,omitempty"`
	RegistrationAssignment    *passbooking.AdminAssignment `json:"registration_assignment,omitempty"`
	RegistrationCommand       *passbooking.Command         `json:"registration_command,omitempty"`
	RegistrationMenu          *passMenuState               `json:"registration_menu,omitempty"`
	AVIDs                     []string                     `json:"av_ids,omitempty"`
	MediaID                   string                       `json:"media_id,omitempty"`
	MediaCandidates           []agent.MediaCandidate       `json:"media_candidates,omitempty"`
	MediaSelected             string                       `json:"media_selected,omitempty"`
	MediaResolvedOrder        string                       `json:"media_resolved_order,omitempty"`
	MediaResolvedRegistration string                       `json:"media_resolved_registration,omitempty"`
	MediaResolvedVersion      int64                        `json:"media_resolved_version,omitempty"`
	ProfileVersion            int64                        `json:"profile_version"`
	Plan                      agent.Plan                   `json:"plan"`
	Version                   int64                        `json:"version"`
	OrderCommand              *orders.Command              `json:"order_command,omitempty"`
	ProfileCommand            *passes.Command              `json:"profile_command,omitempty"`
	KnowledgeCommand          *knowledge.Command           `json:"knowledge_command,omitempty"`
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
	// A user can become inactive after admission. Never replay that user's
	// rejected action after reactivation; unrelated durable updates can proceed.
	defer func() {
		if errors.Is(resultErr, identity.ErrZitadelUserInactive) {
			b.denyOnboarding(ctx, in, u)
			resultErr = nil
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
	ctx, diagnostic := b.startAgentDiagnostics(ctx, in.owner, u.ID)
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
		b.acknowledge(ctx, u.Callback.ID)
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
		core.Action{
			Name:    parts[0],
			SlotID:  parts[1],
			Version: version,
			Key:     fmt.Sprintf("tg-%d", id),
			Origin:  originManual,
		},
	)
}

func (b *Bot) executePlan(ctx context.Context, in incoming, id int64, cached cachedPlan) (string, error) {
	if err := b.validateHistoryPlan(ctx, in.owner, id, cached); err != nil {
		return "", err
	}
	if cached.RegistrationCommand != nil {
		return b.executeRegistrationCommand(ctx, in, id, *cached.RegistrationCommand)
	}
	if cached.RegistrationAssignment != nil {
		return b.executeAdminAssignment(ctx, in, id, *cached.RegistrationAssignment)
	}
	if cached.KnowledgeCommand != nil {
		return b.executeKnowledgeCommand(ctx, in, id, *cached.KnowledgeCommand)
	}
	if cached.ProfileCommand != nil {
		return b.executeProfileCommand(ctx, in, id, *cached.ProfileCommand)
	}
	if cached.OrderCommand != nil {
		if cached.OrderCommand.Name == actionExport {
			scoped := *b
			scoped.OrderEventID = cached.OrderCommand.EventID
			return scoped.exportOrders(ctx, in, id)
		}
		if cached.OrderCommand.Name == actionInstructions {
			return b.showPaymentInstructions(ctx, in, cached.OrderCommand.OrderID)
		}
		command := *cached.OrderCommand
		command.Key = fmt.Sprintf("tg-order-%d", id)
		return b.executeOrder(ctx, in.owner, id, command)
	}
	if cached.Plan.Action == nil {
		return cached.Plan.Text, nil
	}
	// A proposal is not a completed action; the executor supplies the outcome.
	action := cached.Plan.Action
	return b.execute(
		ctx,
		in.owner,
		id,
		core.Action{
			Name:    action.Name,
			SlotID:  action.SlotID,
			Version: cached.Version,
			Key:     fmt.Sprintf("tg-%d", id),
			Origin:  originAgent,
		},
	)
}

func (b *Bot) planForUpdate(ctx context.Context, in incoming, id int64) (cachedPlan, error) {
	var cached cachedPlan
	err := b.DB.QueryRow(ctx, `SELECT plan FROM bot.replies WHERE update_id=$1`, id).Scan(&cached)
	if err == nil {
		if err = b.validateHistoryPlan(ctx, in.owner, id, cached); err != nil {
			return cachedPlan{}, err
		}
		diagnosticPlanReplay(ctx)
		return cached, b.clearAVResults(ctx, in.owner, cached.AVIDs)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return cached, err
	}
	generation, err := b.API.historyGeneration(ctx, in.owner)
	if err != nil {
		return cachedPlan{}, err
	}
	if err = b.validateHistoryInteractions(ctx, in.owner, id, generation); err != nil {
		return cachedPlan{}, err
	}
	cached, err = b.createPlan(ctx, in, id)
	if err != nil {
		return cached, err
	}
	cached.HistoryGeneration = generation
	if err = b.validateHistoryPlan(ctx, in.owner, id, cached); err != nil {
		return cachedPlan{}, err
	}
	// A conflicting writer must use the already saved proposal and its version.
	err = b.DB.QueryRow(ctx, `INSERT INTO bot.replies(update_id,plan) VALUES($1,$2) ON CONFLICT(update_id) DO UPDATE SET plan=bot.replies.plan RETURNING plan`, id, cached).
		Scan(&cached)
	if err == nil {
		err = b.validateHistoryPlan(ctx, in.owner, id, cached)
	}
	if err == nil {
		err = b.clearAVResults(ctx, in.owner, cached.AVIDs)
	}
	return cached, err
}

func (b *Bot) createAllowedPlan(ctx context.Context, in incoming, updateID int64, remaining int) (cachedPlan, error) {
	workflow, err := b.API.Current(ctx, in.owner)
	if err != nil {
		return cachedPlan{}, err
	}
	catalog, err := b.API.Catalog(ctx, in.owner)
	if err != nil {
		return cachedPlan{}, err
	}
	input := agent.Input{Text: in.text, Workflow: workflow, Catalog: catalog, AssistantQuestionsRemaining: &remaining}
	if remaining < 0 {
		input.AssistantQuestionsRemaining = nil
	}
	if err = b.addCurrentAV(ctx, in, &input); err != nil {
		return cachedPlan{}, err
	}
	if err = b.addOrderContext(ctx, in.owner, &input); err != nil {
		return cachedPlan{}, err
	}
	if err = b.addProfileContext(ctx, in.owner, &input); err != nil {
		return cachedPlan{}, err
	}
	if err = b.addMediaContext(ctx, in, &input); err != nil {
		return cachedPlan{}, err
	}
	if err = b.addSupportingContext(ctx, in, updateID, &input); err != nil {
		return cachedPlan{}, err
	}
	// Refinement adds visual evidence; current spoken selection stays tied to
	// the original user input, not to speech inside an inspected recording.
	requestInput := input
	plan, avIDs, err := b.planWithAV(ctx, in, &input, updateID)
	cached := cachedPlan{}
	if err == nil {
		err = b.bindPlanCommands(ctx, in.owner, plan, input, requestInput, &cached)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return cachedPlan{}, err
	}
	if err != nil {
		notice := paidFailureNotice(err)
		text, translateErr := i18n.Translate(input.Language, notice, nil)
		if translateErr != nil {
			return cachedPlan{}, translateErr
		}
		plan = agent.Plan{
			View: input.View,
			Text: text,
		}
		cached = cachedPlan{SystemNotice: notice}
	}
	if isNonAVMediaReply(in, input, plan) {
		plan.View = agent.MediaView
	}
	cached.Plan = plan
	cached.Version = input.Workflow.Version
	cached.ProfileVersion = requestInput.Profile.Version
	if input.Profile != nil {
		cached.ProfileVersion = input.Profile.Version
	}
	cached.AVIDs = avIDs
	cached.MediaID = in.mediaID
	cached.Plan.KnowledgeAction = nil
	cached.Plan.RegistrationAction = nil
	cacheMediaSelection(&cached, in, input, requestInput)
	cacheProfileCommand(&cached, plan, cached.ProfileVersion)
	return cached, nil
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

func cacheProfileCommand(cached *cachedPlan, plan agent.Plan, version int64) {
	if plan.ProfileAction != nil {
		proposal := plan.ProfileAction
		cached.ProfileCommand = &passes.Command{Name: proposal.Name, Field: proposal.Field, Value: proposal.Value,
			Version: version, Origin: originAgent}
		// The private execution cache owns this value; it is never interaction history.
		cached.Plan.ProfileAction = nil
	}
}

func (b *Bot) acknowledge(ctx context.Context, id string) {
	if err := b.TG.Call(ctx, "answerCallbackQuery", map[string]string{"callback_query_id": id}, nil); err != nil {
		b.logger().WarnContext(ctx, "callback acknowledgement failed")
	}
}
func (b *Bot) execute(ctx context.Context, owner string, id int64, a core.Action) (string, error) {
	w, e := b.API.Execute(ctx, owner, a)
	if e != nil {
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
		map[string]any{originField: a.Origin, actionField: a.Name, "workflow": w},
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
	notice, native, err := b.latestNotice(ctx, owner, preference.Language)
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
	return b.deliverCard(ctx, owner, payload)
}

func (b *Bot) latestNotice(ctx context.Context, owner, language string) (string, bool, error) {
	var raw []byte
	var native bool
	var update int64
	var systemNotice i18n.ID
	err := b.DB.QueryRow(ctx, `SELECT content,native_markdown,update_id,
 COALESCE((SELECT plan->>'system_notice' FROM bot.replies WHERE update_id=i.update_id),'')
 FROM bot.interactions i WHERE owner=$1 AND kind='reply' ORDER BY id DESC LIMIT 1`, owner).
		Scan(&raw, &native, &update, &systemNotice)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	visible, err := b.historyReplyVisible(ctx, owner, update)
	if err != nil || !visible {
		return "", false, err
	}
	if systemNotice != "" {
		text, translateErr := i18n.Translate(language, systemNotice, nil)
		return text, false, translateErr
	}
	var notice string
	err = json.Unmarshal(raw, &notice)
	if err == nil && !native {
		notice, err = b.localizeWorkflowNotice(ctx, owner, update, language, notice)
	}
	return notice, native, err
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
	wf core.Workflow,
	slots []core.Slot,
	notice string,
	native bool,
) (telegram.Send, error) {
	m := &orderMessages{language: language}
	text := notice + "\n\n" + m.text(i18n.WorkflowStatus, map[string]string{
		workflowStateParameter: workflowState(m, wf.State), "revision": strconv.FormatInt(wf.Version, 10),
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
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	var id int64
	var previous string
	err := b.DB.QueryRow(ctx, `SELECT message_id,view_hash FROM bot.messages WHERE owner=$1`, owner).
		Scan(&id, &previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id > 0 && previous == hash {
		return nil
	}
	payload.MessageID = id
	id, err = b.editOrSend(ctx, payload)
	if err != nil {
		return err
	}
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.messages(owner,chat_id,message_id,view_hash) VALUES($1,$2,$3,$4) ON CONFLICT(owner) DO UPDATE SET message_id=$3,chat_id=$2,view_hash=$4`,
		owner,
		payload.ChatID,
		id,
		hash,
	)
	return err
}

func (b *Bot) editOrSend(ctx context.Context, payload telegram.Send) (int64, error) {
	prepared, prepareErr := telegram.PrepareSend(payload)
	if prepareErr != nil {
		return 0, prepareErr
	}
	payload = prepared
	if payload.MessageID > 0 {
		err := b.TG.Edit(ctx, payload)
		if err == nil {
			return payload.MessageID, nil
		}
		var apiError *telegram.APIError
		if !errors.As(err, &apiError) || apiError.Code != http.StatusBadRequest {
			return 0, err
		}
		switch {
		case strings.Contains(apiError.Description, "message is not modified"):
			return payload.MessageID, nil
		case strings.Contains(apiError.Description, "message to edit not found"),
			strings.Contains(apiError.Description, "message can't be edited"):
			payload.MessageID = 0
		default:
			return 0, err
		}
	}
	message, err := b.TG.Send(ctx, payload)
	return message.ID, err
}

const (
	pollInterval  = 300 * time.Millisecond
	unlockTimeout = 5 * time.Second
)

// Run holds a PostgreSQL session lock: only one poller/renderer may own this bot.
func (b *Bot) Run(ctx context.Context) (runErr error) {
	defer func() {
		runErr = b.creditCutoverRunResult(ctx, runErr)
	}()
	if err := b.validateCreditCutover(ctx); err != nil {
		return err
	}
	conn, err := b.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(918273)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("bot already running")
	}
	defer b.unlock(ctx, conn)
	offset, err := b.startTelegramPolling(ctx)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		if err = conn.Ping(ctx); err != nil {
			return err
		}
		offset, err = b.poll(ctx, offset)
		if err != nil {
			if _, invalid := errors.AsType[creditCutoverError](err); invalid {
				return err
			}
			b.logger().WarnContext(ctx, "bot retry pending", "error", err)
		}
		if err = b.reconcileAllViews(ctx); err != nil {
			return err
		}
		b.deliverQueuedNotifications(ctx)
		if err = b.reconcileMassageViews(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
	return nil
}

func (b *Bot) unlock(ctx context.Context, conn *pgxpool.Conn) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
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
		return err
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
			return err
		}
		views = append(views, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range views {
		viewContext, authErr := b.API.notificationContext(ctx, v.owner, v.chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "view identity pending")
			continue
		}
		if err = b.Render(viewContext, v.owner, v.chat); err != nil {
			b.logger().WarnContext(ctx, "view reconciliation pending", "error", err)
		}
	}
	return nil
}

const originField = "origin"
const textField = "text"
